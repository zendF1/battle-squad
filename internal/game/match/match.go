package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"runtime/debug"
	"sync"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type matchEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
	// internal runs on the match loop instead of msg. Only server code sets it,
	// so clients cannot trigger internal transitions.
	internal func(ctx context.Context)
}

const (
	maxDroppedMsgs = 10 // kick client after N consecutive dropped broadcasts

	// reservationReleaseTimeout bounds releasing a match's reservations; it runs on a fresh
	// context because the match context is usually already cancelled by then.
	reservationReleaseTimeout = 5 * time.Second
)

// Every rule and number the match plays with comes from its game data snapshot (gd), captured
// when the match is created and never swapped: a reload of the game data reaches new matches
// only, so the server and the clients of a running match agree on it for the whole match.
// MatchStarted and MatchStateSync carry its version (gamedataVersion), the GET /gamedata
// version of the same snapshot.

type Match struct {
	State        MatchState
	Terrain      *Terrain
	Clients      map[string]*ws.Client
	Events       chan matchEvent
	db           *database.PostgresDB
	redis        *database.RedisClient
	economyRepo  *economy.Repository
	hub          interface{} // backref to room hub to unregister
	lastActivity time.Time
	ctx          context.Context
	cancel       context.CancelFunc
	el           *EventLogger
	// gd is the game data the match plays with, from start to end.
	gd          *gamedata.Snapshot
	MatchDone   chan struct{}
	doneOnce    sync.Once
	TeamRatings map[int]int // teamID → avg rating for Elo
	EloParams   EloParams   // Elo configuration
	// StartDelay: Run waits this long before MatchStarted (matchmade games: the
	// clients' MatchFound reveal)
	StartDelay  time.Duration
	BotTier     matchmaker.BotTierConfig
	lastAction  map[string]time.Time // playerID → last action timestamp (rate limit)
	turnSeq     int                  // incremented every startTurn; stale callbacks compare against it
	resolving   bool                 // the current turn's action is animating; no further actions or timer ticks
	lastMoveSeq map[string]int       // playerID → seq of the last Move the loop took from them this turn (see noteMoveSeq)

	// selection is what the current player armed this turn (SelectAction, or a bot's skill /
	// shot item); selectionSent the last ActionSelected broadcast this turn; selectSentAt when
	// each player's last ActionSelected went out; selectFlushPending a coalesced broadcast is
	// scheduled (see queueSelectionBroadcast). Reset by startTurn and every resolved shot.
	selection          ActionSelected
	selectionSent      ActionSelected
	selectSentAt       map[string]time.Time
	selectFlushPending bool

	// processRewards settles a normally ended match: rewards, consuming spent items and
	// releasing the rest. releaseReservations returns every still-reserved item of the
	// match; nil without a database. Fields so tests can fake them.
	processRewards      func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error)
	releaseReservations func(ctx context.Context) error
	// reservationsSettled: rewards or a no-contest release already settled the
	// reservations, so Run's exit has nothing to release.
	reservationsSettled bool

	// CountsForMilestones: the room set this match up as one that counts for milestones
	// (matchmaking, or quick play against the AI bot); custom rooms and the idle practice
	// target do not.
	CountsForMilestones bool
}

// milestoneMinShotTurns is how many actions (shots, skills, teleports) a player must have
// resolved for a counted match to count for them.
const milestoneMinShotTurns = 3

// milestoneCounts reports whether the ended match counts for p's milestone counters: a
// counting match, a human who did not leave and resolved at least milestoneMinShotTurns actions.
func (m *Match) milestoneCounts(p *BattlePlayerState) bool {
	return m.CountsForMilestones && !p.IsBot && !p.Left && p.ShotTurns >= milestoneMinShotTurns
}

// Stop cancels the match context, causing the Run loop to exit.
func (m *Match) Stop() {
	m.cancel()
}

type RoomHubInterface interface {
	UnregisterRoom(ctx context.Context, roomID string)
}

// NewMatch is NewMatchWithData with the current game data.
func NewMatch(
	matchID string,
	roomID string,
	mode string,
	mapID string,
	players []*BattlePlayerState,
	clients map[string]*ws.Client,
	db *database.PostgresDB,
	redis *database.RedisClient,
	economyRepo *economy.Repository,
	hub interface{},
) *Match {
	return NewMatchWithData(gamedata.Current(), matchID, roomID, mode, mapID, players, clients, db, redis, economyRepo, hub)
}

// NewMatchWithData creates a match that plays with the game data gd for its whole life.
func NewMatchWithData(
	gd *gamedata.Snapshot,
	matchID string,
	roomID string,
	mode string,
	mapID string,
	players []*BattlePlayerState,
	clients map[string]*ws.Client,
	db *database.PostgresDB,
	redis *database.RedisClient,
	economyRepo *economy.Repository,
	hub interface{},
) *Match {
	ctx, cancel := context.WithCancel(context.Background())

	mPlayers := make(map[string]*BattlePlayerState)
	var turnOrder []string

	// Set turn order (1v1 alternating, or alternating teams for 2v2)
	// For simplicity, we randomize order
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	indices := r.Perm(len(players))
	for _, idx := range indices {
		p := players[idx]
		p.Items = append([]string(nil), p.Items...) // don't share the room's backing array
		p.SkillRank = weaponSkillRank(gd, p.WeaponItemID)
		mPlayers[p.PlayerID] = p
		turnOrder = append(turnOrder, p.PlayerID)
	}

	// Build terrain from map config, with legacy fallback
	mapCfgForTerrain := gamedata.MapConfig{
		MapID:  mapID,
		Width:  gamedata.DefaultMapWidth,
		Height: gamedata.DefaultMapHeight,
	}
	if mc, ok := gd.Data.Maps[mapID]; ok {
		mapCfgForTerrain = mc
	}
	terrain := NewTerrainFor(gd, mapCfgForTerrain)

	// Land players on the surface nearest their configured spawn height
	for _, p := range mPlayers {
		p.Position.Y = terrain.SpawnY(p.Position.X, p.Position.Y)
	}

	mapName, mapTheme := gd.MapDisplay(mapID)
	mState := MatchState{
		MatchID:         matchID,
		RoomID:          roomID,
		Mode:            mode,
		MapID:           mapID,
		MapName:         mapName,
		MapTheme:        mapTheme,
		TurnIndex:       0,
		CurrentPlayerID: turnOrder[0],
		Wind: WindState{
			Direction: 0,
			Power:     0,
		},
		Players:       mPlayers,
		Status:        "in_progress",
		TurnOrder:     turnOrder,
		TurnTimeLeft:  gd.Physics.TurnTimeSeconds,
		ActiveEffects: []StatusEffect{},
	}

	m := &Match{
		State:        mState,
		Terrain:      terrain,
		Clients:      clients,
		Events:       make(chan matchEvent, 256),
		db:           db,
		redis:        redis,
		economyRepo:  economyRepo,
		hub:          hub,
		lastActivity: time.Now(),
		ctx:          ctx,
		cancel:       cancel,
		el:           NewEventLogger(matchID, db),
		gd:           gd,
		MatchDone:    make(chan struct{}),
		TeamRatings:  map[int]int{},
		EloParams:    EloParams{},
		BotTier:      DefaultBotTier,
		lastAction:   make(map[string]time.Time),
		lastMoveSeq:  make(map[string]int),
		selectSentAt: make(map[string]time.Time),
	}
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		return processMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats, usedItems, m.TeamRatings, m.EloParams,
			m.gd.Rules.Player.ExpPerLevel)
	}
	if db != nil {
		m.releaseReservations = func(ctx context.Context) error {
			return ReleaseMatchReservations(ctx, db, matchID)
		}
	}
	return m
}

func (m *Match) signalDone() {
	m.doneOnce.Do(func() {
		close(m.MatchDone)
	})
}

func (m *Match) Run() {
	// Runs last, after any panic recovery: a match that stops without ending
	// (Stop, ctx cancel) must not keep its players' items reserved forever.
	defer m.releaseUnsettledReservations()

	// Pattern 1: Panic Recovery per Match
	defer func() {
		if r := recover(); r != nil {
			observability.MatchPanicTotal.Inc()
			observability.Log.Error().
				Str("matchId", m.State.MatchID).
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("match loop panic - ending as no-contest")
			m.endAsNoContest()
		}
	}()

	observability.ActiveMatches.Inc()
	observability.MatchStartedTotal.Inc()
	defer observability.ActiveMatches.Dec()

	// A matchmade game waits StartDelay after MatchFound (the clients' reveal);
	// events arriving meanwhile (a Leave) stay queued until the match starts
	if m.StartDelay > 0 {
		select {
		case <-time.After(m.StartDelay):
		case <-m.ctx.Done():
			return
		}
	}

	m.el.Start(m.ctx)
	m.lastActivity = time.Now()
	m.broadcastMatchStarted()

	// Initialize the first turn
	m.startTurn(context.Background())

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	watchdog := time.NewTicker(30 * time.Second)
	defer watchdog.Stop()

	for {
		select {
		case ev := <-m.Events:
			m.lastActivity = time.Now()
			m.handleEvent(ev)

		case <-ticker.C:
			if m.State.Status != "in_progress" || m.resolving {
				continue
			}

			m.State.TurnTimeLeft--
			m.broadcast(ws.Message{
				Event: "TurnTimerTick",
				Data:  json.RawMessage(fmt.Sprintf(`{"timeLeft":%d}`, m.State.TurnTimeLeft)),
			})

			// Handle turn timeout
			if m.State.TurnTimeLeft <= 0 {
				observability.Log.Info().Str("matchId", m.State.MatchID).Msg("turn timed out")
				m.endTurn(context.Background())
			}

		case <-watchdog.C:
			// Check if all human players disconnected (only bots remain)
			allDisconnected := true
			for _, p := range m.State.Players {
				if p.IsAlive && !p.IsBot {
					if _, connected := m.Clients[p.PlayerID]; connected {
						allDisconnected = false
						break
					}
				}
			}
			if allDisconnected {
				observability.Log.Info().Str("matchId", m.State.MatchID).Msg("all human players disconnected, ending match")
				m.endAsNoContest()
				return
			}

			// Pattern 10: Watchdog Timer to prevent stuck zombie matches (match.idle_timeout_minutes)
			if time.Since(m.lastActivity) > time.Duration(m.gd.Physics.IdleTimeoutMinutes)*time.Minute {
				observability.Log.Warn().Str("matchId", m.State.MatchID).Int("minutes", m.gd.Physics.IdleTimeoutMinutes).Msg("match stuck with no activity, terminating")
				m.endAsNoContest()
				return
			}

		case <-m.ctx.Done():
			return
		}
	}
}

// releaseUnsettledReservations releases the match's remaining reservations unless the
// match end already settled them. Only status 'reserved' rows of this match change, so
// it is safe to repeat.
func (m *Match) releaseUnsettledReservations() {
	if m.reservationsSettled || m.releaseReservations == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), reservationReleaseTimeout)
	defer cancel()
	if err := m.releaseReservations(ctx); err != nil {
		observability.Log.Error().Err(err).Str("matchId", m.State.MatchID).Msg("failed to release item reservations")
		return
	}
	m.reservationsSettled = true
	observability.Log.Info().Str("matchId", m.State.MatchID).Str("status", m.State.Status).Msg("released item reservations")
}

// LeaveAndStop processes the player's Leave on the match loop, then stops the match.
// Stopping only after the Leave was handled keeps the match end it causes (rewards,
// consuming spent items) from losing a race against the stop.
func (m *Match) LeaveAndStop(ctx context.Context, client *ws.Client) {
	m.Events <- matchEvent{ctx: ctx, internal: func(ctx context.Context) {
		m.processLeave(ctx, client)
		m.Stop()
	}}
}

func (m *Match) ProcessEvent(ctx context.Context, client *ws.Client, msg ws.Message) {
	m.Events <- matchEvent{
		client: client,
		msg:    msg,
		ctx:    ctx,
	}
}

func (m *Match) handleEvent(ev matchEvent) {
	if ev.internal != nil {
		ev.internal(ev.ctx)
		return
	}

	log := observability.FromContext(ev.ctx)
	client := ev.client
	msg := ev.msg

	log.Debug().Str("event", msg.Event).Str("matchId", m.State.MatchID).Msg("processing match event")

	// Resolve the acting player ID — bots send events with a nil client.
	actorID := m.State.CurrentPlayerID
	if client != nil {
		actorID = client.PlayerID
	}

	// Before anything below can drop the Move
	if client != nil && msg.Event == "Move" {
		m.noteMoveSeq(client.PlayerID, msg.Data)
	}

	// Rate limit player actions (bots bypass)
	if client != nil {
		switch msg.Event {
		case "Move", "Shoot", "UseItem", "EndTurn":
			now := time.Now()
			if last, ok := m.lastAction[actorID]; ok && now.Sub(last) < m.actionInterval() {
				log.Debug().Str("playerId", actorID).Str("event", msg.Event).Msg("action rate limited")
				return
			}
			m.lastAction[actorID] = now
		}
	}

	// The turn's action already resolved; wait for the scheduled turn end
	if m.resolving {
		switch msg.Event {
		case "Move", "Shoot", "UseItem", "EndTurn":
			return
		}
	}

	switch msg.Event {
	case "Move":
		var action MoveAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.el.Log("Move", actorID, action)
			m.processMove(ev.ctx, client, action)
		}
	case "Shoot":
		var action ShootAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.el.Log("Shoot", actorID, action)
			m.processShoot(ev.ctx, client, action)
		}
	case "UseItem":
		var action UseItemAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.el.Log("UseItem", actorID, action)
			m.processUseItem(ev.ctx, client, action)
		}
	case "EndTurn":
		m.el.Log("EndTurn", actorID, nil)
		m.processEndTurn(ev.ctx, client)
	case "SelectAction":
		// Not rate limited like the actions above (it would delay the Shoot that follows);
		// its broadcasts are coalesced instead
		var sel SelectAction
		if err := json.Unmarshal(msg.Data, &sel); err == nil {
			m.processSelectAction(ev.ctx, client, sel)
		}
	case "Reconnect":
		m.processReconnect(ev.ctx, client)
	case "Leave":
		m.processLeave(ev.ctx, client)
	}
}

// GameData is the game data the match plays with.
func (m *Match) GameData() *gamedata.Snapshot { return m.gd }

// actionInterval is the least time between two actions of a player (rules.timings).
func (m *Match) actionInterval() time.Duration {
	return time.Duration(m.gd.Rules.Timings.ActionIntervalSec * float64(time.Second))
}

// turnMoveEnergy is the move energy p gets at the start of each turn: MaxMoveEnergy, or, for
// a state built without it (tests, a snapshot from before it existed), the character's base
// move energy + MoveEnergyBonus, which is then stored in MaxMoveEnergy.
func (m *Match) turnMoveEnergy(p *BattlePlayerState) int {
	if p.MaxMoveEnergy <= 0 {
		p.MaxMoveEnergy = m.gd.BaseMoveEnergy(p.CharacterID) + p.MoveEnergyBonus
	}
	return p.MaxMoveEnergy
}

func (m *Match) startTurn(ctx context.Context) {
	m.State.TurnTimeLeft = m.gd.Physics.TurnTimeSeconds
	m.turnSeq++
	m.resolving = false
	clear(m.lastMoveSeq) // a new turn starts a new replay on the mover's client
	m.resetSelection()   // clients hide the badge at TurnStarted

	player := m.State.Players[m.State.CurrentPlayerID]
	player.MoveEnergy = m.turnMoveEnergy(player) // Reset move energy to full
	ApplyTurnStartEffects(m.gd, player)          // net / root / stun / freeze limit this turn's movement

	// Skill energy is incremented at end of turn, not start

	// Damage at the start of the turn: lava under the feet, then burn. Shield absorbs first;
	// a player it kills loses the turn.
	combat := m.gd.Rules.Combat
	if player.Position.Y >= float64(m.Terrain.Height) { // below the map: dead
		m.sendPlayerDamaged(player, killPlayer(player), "out_of_map")
	}
	if player.IsAlive && m.Terrain.GetTerrainTypeAt(player.Position.X, player.Position.Y) == "lava" {
		m.turnStartDamage(player, combat.LavaDamage, "lava")
	}
	if player.IsAlive && HasEffect(player, "burn") {
		m.turnStartDamage(player, int(math.Round(float64(player.MaxHP)*float64(combat.BurnMaxHPPercent)/100)), "burn")
	}
	if !player.IsAlive {
		m.checkWinCondition(ctx)
		if m.State.Status == "in_progress" {
			m.endTurn(ctx)
		}
		return
	}

	// Update wind: direction (-1 to 1), power (0 to 4)
	m.updateWind()

	// Broadcast TurnStarted (turnTimeLeft: the turn's seconds, match.turn_time_seconds)
	skillEnergy := make(map[string]int, len(m.State.Players))
	for id, p := range m.State.Players {
		skillEnergy[id] = p.SkillEnergy
	}
	started := map[string]interface{}{
		"turnIndex":       m.State.TurnIndex,
		"currentPlayerId": m.State.CurrentPlayerID,
		"wind":            m.State.Wind,
		"moveEnergy":      player.MoveEnergy,
		"maxMoveEnergy":   player.MaxMoveEnergy,
		"skillEnergy":     skillEnergy,
		"turnTimeLeft":    m.State.TurnTimeLeft,
	}
	skip := skipsTurn(m.gd, player)
	if skip != "" {
		started["skipped"] = skip
	}
	payload, _ := json.Marshal(started)
	m.broadcast(ws.Message{
		Event: "TurnStarted",
		Data:  payload,
	})

	// Stunned or frozen: the whole turn is lost; the server ends it after a short pause
	if skip != "" {
		m.resolveTurnAfter(ctx, m.gd.Rules.Timings.SkipTurnDelaySec, m.endTurn)
		return
	}

	if player.IsBot {
		// Short idle before acting; the bot then spends its "thinking" time visibly aiming
		botDelay := randomDelay(botTurnStartMinDelay, botTurnStartMaxDelay)
		m.runOnTurnAfter(ctx, botDelay, func(ctx context.Context) {
			m.botStep(ctx, botTurnState{}, 0)
		})
	}
}

func (m *Match) endTurn(ctx context.Context) {
	// Add skill energy to current player (rules.skill_energy: +perTurn per turn, at most max)
	currentPlayer := m.State.Players[m.State.CurrentPlayerID]
	if currentPlayer != nil && currentPlayer.IsAlive {
		addSkillEnergy(currentPlayer, m.gd.Rules.SkillEnergy.PerTurn, m.gd.Rules.SkillEnergy.Max)
	}

	// Effects count down at the end of their target's turn; global ones every turn
	if currentPlayer != nil {
		TickPlayerStatusEffects(currentPlayer)
	}
	m.State.ActiveEffects = tickEffects(m.State.ActiveEffects)

	// Find next player in order
	currentIdx := -1
	for idx, id := range m.State.TurnOrder {
		if id == m.State.CurrentPlayerID {
			currentIdx = idx
			break
		}
	}

	m.State.TurnIndex++

	// Alternate to next alive player
	nextIdx := currentIdx
	for {
		nextIdx = (nextIdx + 1) % len(m.State.TurnOrder)
		nextPlayer := m.State.Players[m.State.TurnOrder[nextIdx]]
		if nextPlayer.IsAlive {
			m.State.CurrentPlayerID = nextPlayer.PlayerID
			break
		}
		// If we looped back to same player and they're the only one alive, match is over
		if nextIdx == currentIdx {
			m.checkWinCondition(ctx)
			return
		}
	}

	m.startTurn(ctx)
}

// scheduleEndTurn delays the turn transition to let clients animate projectile
// flight, explosion, and damage display before the next turn starts.
// delay = flight time + 0.5s explosion + 0.2s damage popup + 0.5s buffer
func (m *Match) scheduleEndTurn(ctx context.Context, shotDuration float64) {
	delay := shotDuration + 1.2 // damage popup + buffer after the replay ends
	if delay < 1.5 {
		delay = 1.5
	}
	m.resolveTurnAfter(ctx, delay, m.endTurn)
}

// resolveTurnAfter locks the current turn and runs fn after delaySec seconds
// (see runOnTurnAfter).
func (m *Match) resolveTurnAfter(ctx context.Context, delaySec float64, fn func(ctx context.Context)) {
	m.resolving = true
	m.runOnTurnAfter(ctx, time.Duration(delaySec*float64(time.Second)), fn)
}

// runOnTurnAfter runs fn on the match loop after delay, unless the match ended
// or the turn changed in the meantime.
func (m *Match) runOnTurnAfter(ctx context.Context, delay time.Duration, fn func(ctx context.Context)) {
	turn := m.turnSeq
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-m.ctx.Done():
			return
		}
		ev := matchEvent{ctx: ctx, internal: func(ctx context.Context) {
			if m.State.Status == "in_progress" && m.turnSeq == turn {
				fn(ctx)
			}
		}}
		select {
		case m.Events <- ev:
		case <-m.ctx.Done():
		}
	}()
}

// noteMoveSeq records the seq of a Move from the current player as soon as the match loop takes
// it, before the rate limit, the resolving check or processMove can drop it. MatchStateSync
// reports it as lastMoveSeq, and the mover's client drops every command up to it from its
// replay: the snapshot already shows what each of them did, applied or not. A dropped Move must
// count too, or it would stay in that replay forever. Moves from another player are not recorded:
// they are dropped, and that player's client starts a new replay at its own TurnStarted, when
// startTurn clears the record. A Move without seq leaves the record as it is.
func (m *Match) noteMoveSeq(playerID string, data json.RawMessage) {
	if playerID != m.State.CurrentPlayerID {
		return
	}
	var action MoveAction
	if err := json.Unmarshal(data, &action); err != nil || action.Seq <= 0 {
		return
	}
	m.lastMoveSeq[playerID] = action.Seq
}

func (m *Match) processMove(ctx context.Context, client *ws.Client, action MoveAction) {
	// Validate current player
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive || !isFinite(action.TargetX) || !canMove(m.gd, player) {
		return
	}

	// Clamp target by max energy range (move.step_pixels px per move.energy_cost_per_2px energy)
	maxDist := float64(player.MoveEnergy) * m.gd.Physics.PxPerEnergy()
	if math.Abs(action.TargetX-player.Position.X) > maxDist {
		if action.TargetX > player.Position.X {
			action.TargetX = player.Position.X + maxDist
		} else {
			action.TargetX = player.Position.X - maxDist
		}
	}

	// Walk with terrain physics — player follows surface, blocked by steep walls
	finalX, finalY := m.Terrain.WalkTo(player.Position.X, player.Position.Y, action.TargetX)

	// Calculate energy cost based on actual horizontal distance moved
	actualDist := math.Abs(finalX - player.Position.X)
	energyCost := int(math.Round(actualDist * m.gd.Physics.EnergyPerPx()))
	if energyCost > player.MoveEnergy {
		energyCost = player.MoveEnergy
	}

	startY := player.Position.Y
	player.MoveEnergy -= energyCost
	player.Position.X = finalX
	player.Position.Y = finalY
	outOfMap := finalY >= float64(m.Terrain.Height)

	// Broadcast PlayerMoved; seq tells the mover which of its commands this answers;
	// outOfMap: the walk ended in a hole to the bottom of the map (PlayerDamaged follows)
	moved := map[string]interface{}{
		"playerId":   player.PlayerID,
		"position":   player.Position,
		"moveEnergy": player.MoveEnergy,
	}
	if action.Seq > 0 {
		moved["seq"] = action.Seq
	}
	if outOfMap {
		moved["outOfMap"] = true
	}
	payload, _ := json.Marshal(moved)
	m.broadcast(ws.Message{
		Event: "PlayerMoved",
		Data:  payload,
	})

	if outOfMap {
		m.fallOutOfMap(ctx, player, finalY-startY)
	}
}

// fallOutOfMap kills the current player, who walked into a hole to the bottom of the map
// (PlayerDamaged type out_of_map), and ends the turn once clients have shown the fall.
func (m *Match) fallOutOfMap(ctx context.Context, p *BattlePlayerState, fallDistance float64) {
	lost := killPlayer(p)
	m.sendPlayerDamaged(p, lost, "out_of_map")
	delay := fallDuration(max(fallDistance, 0), m.gd.Physics.Gravity) + 1.5 // the fall, then the death
	m.resolveTurnAfter(ctx, delay, func(ctx context.Context) {
		m.checkWinCondition(ctx)
		if m.State.Status == "in_progress" {
			m.endTurn(ctx)
		}
	})
}

func (m *Match) processShoot(ctx context.Context, client *ws.Client, action ShootAction) {
	// Validate turn
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}
	if !validShotInput(action.Angle, action.Power, m.gd.Rules.Aim.PowerMax) {
		return
	}

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive {
		return
	}

	// Stunned / frozen players lose the turn (the server ends it)
	if skipsTurn(m.gd, player) != "" {
		return
	}

	charConfig, exists := m.gd.Data.Characters[player.CharacterID]
	if !exists {
		return
	}

	weaponConfig, exists := m.gd.Data.Weapons[charConfig.WeaponID]
	if !exists {
		return
	}

	if action.ActionMode == "skill" {
		m.shootSkill(ctx, player, action, charConfig.SkillID, weaponConfig)
		return
	}

	shot := &ShotResolved{ShooterID: player.PlayerID, ActionMode: "weapon"}

	// ── Item mode ───────────────────────────────────────────────────────────
	if action.ActionMode == "item" {
		if action.ItemID == nil {
			return
		}
		itemID := *action.ItemID
		if itemID == "teleport" {
			m.shootTeleport(ctx, player, action, weaponConfig)
			return
		}
		// Immediate items (medkit, wind_stopper) are used via UseItem, not by shooting
		if !IsShotModifierItem(itemID) {
			return
		}
		if err := ApplyImmediateItem(ctx, m.gd, &m.State, player.PlayerID, itemID, nil, m.Terrain); err != nil {
			log := observability.FromContext(ctx)
			log.Warn().Err(err).Str("itemId", itemID).Msg("failed to apply shot item")
			return
		}
		m.broadcastItemUsed(player.PlayerID, itemID)
		shot.ActionMode = "item"
		shot.ItemID = itemID
	}
	// ── End item mode ───────────────────────────────────────────────────────

	// Determine projectile origin, angle, and power — may be overridden by air_strike
	shootOrigin := shotOrigin(m.Terrain, player)
	shootAngle := action.Angle
	shootPower := action.Power

	// air_strike: fire from top of map at TargetX, at the fixed rules.items angle (270: straight
	// down in Y-down coords, handled by sin) and power
	if HasEffect(player, "air_strike") {
		shootOrigin = Vector2{X: m.airStrikeX(player, action, weaponConfig), Y: 0}
		shootAngle = m.gd.Rules.Items.AirStrikeAngle
		shootPower = m.gd.Rules.Items.AirStrikePower
	}

	// drill_bomb: projectile drills through the first terrain block it meets
	pierce := m.drillPierce(player)

	// 1. Simulate Projectile Trajectory
	result := SimulateProjectile(
		player.PlayerID,
		player.TeamID,
		shootOrigin,
		shootAngle,
		shootPower,
		weaponConfig,
		m.State.Wind,
		m.Terrain,
		m.State.Players,
		pierce,
	)
	shot.Projectiles = []*ProjectileResult{result}

	// Track shot fired
	player.ShotsFired++

	// Calculate base damage multiplication factor (e.g. from Power Shot item)
	damageFactor := 1.0
	if HasEffect(player, "power_shot") {
		damageFactor = m.gd.PowerShotFactor()
	}

	// 2. Resolve explosion, damage and falls
	tl := m.newTimeline()
	if result.ExplosionPoint != nil {
		// Fragile terrain: double the explosion radius and the crater
		if m.Terrain.GetTerrainTypeAt(result.ExplosionPoint.X, result.ExplosionPoint.Y) == "fragile" {
			result.ExplosionRadius *= 2
			result.CraterRadius *= 2
		}

		baseDamage := float64(weaponConfig.Damage+player.DamageBonus) * damageFactor
		hitEnemies := m.explodeProjectile(player, result, baseDamage, tl)
		m.settleFalls(player, flightEnd(result), tl)

		// spider_net / freeze_bomb: control every enemy hit by the explosion for their next turn
		for _, target := range hitEnemies {
			if !target.IsAlive {
				continue
			}
			if HasEffect(player, "spider_net") {
				m.applyShotStatus(tl, flightEnd(result), player, target, "net", 1)
			}
			if HasEffect(player, "freeze_bomb") {
				m.applyShotStatus(tl, flightEnd(result), player, target, "freeze", int(m.gd.ItemValue("freeze_bomb", gamedata.DefaultFreezeTurns)))
			}
		}
	}

	// 3. Broadcast, check win conditions, schedule delayed end turn
	m.finishShot(ctx, shot, tl)
}

func (m *Match) processUseItem(ctx context.Context, client *ws.Client, action UseItemAction) {
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}

	log := observability.FromContext(ctx)

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive || skipsTurn(m.gd, player) != "" {
		return
	}
	// Teleport is fired as a projectile (Shoot with actionMode "item") so it follows
	// trajectory rules and ends the turn; it cannot be used directly
	if action.ItemID == "teleport" {
		log.Warn().Str("playerId", player.PlayerID).Msg("rejected direct teleport via UseItem")
		return
	}

	err := ApplyImmediateItem(ctx, m.gd, &m.State, m.State.CurrentPlayerID, action.ItemID, action.TargetPosition, m.Terrain)
	if err != nil {
		log.Warn().Err(err).Msg("failed to apply item")
		return
	}

	m.broadcastItemUsed(m.State.CurrentPlayerID, action.ItemID)
}

// broadcastItemUsed syncs player state (HP, position, items, effects) and wind after an item is used.
func (m *Match) broadcastItemUsed(playerID, itemID string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"playerId": playerID,
		"itemId":   itemID,
		"players":  m.State.Players,
		"wind":     m.State.Wind,
	})
	m.broadcast(ws.Message{
		Event: "ItemUsed",
		Data:  payload,
	})
}

// shootTeleport fires the teleport projectile and moves the player to its landing
// point once the flight animation has played, then ends the turn.
func (m *Match) shootTeleport(ctx context.Context, player *BattlePlayerState, action ShootAction, weaponConfig gamedata.WeaponConfig) {
	if !canMove(m.gd, player) {
		return // rooted: no teleport
	}
	itemIdx := -1
	for i, it := range player.Items {
		if it == "teleport" {
			itemIdx = i
			break
		}
	}
	if itemIdx == -1 {
		return
	}

	// Simulate projectile to find landing point
	result := SimulateProjectile(
		player.PlayerID,
		player.TeamID,
		shotOrigin(m.Terrain, player),
		action.Angle,
		action.Power,
		weaponConfig,
		m.State.Wind,
		m.Terrain,
		m.State.Players,
		0, // no drilling
	)
	result.ExplosionRadius = 0
	result.CraterRadius = 0

	player.ShotsFired++
	consumeItem(player, itemIdx)
	m.broadcastItemUsed(player.PlayerID, "teleport")

	tl := m.newTimeline()
	if result.ExplosionPoint != nil && result.ExplosionPoint.Y < float64(m.Terrain.Height) {
		landY := m.Terrain.GetLandingY(result.ExplosionPoint.X, result.ExplosionPoint.Y)
		if landY < float64(m.Terrain.Height) {
			from := player.Position
			player.Position = Vector2{X: result.ExplosionPoint.X, Y: landY}
			tl.add(ShotEvent{T: flightEnd(result), Type: "teleport", PlayerID: player.PlayerID, From: vecPtr(from), To: vecPtr(player.Position)})
		}
	}

	shot := &ShotResolved{
		ShooterID:   player.PlayerID,
		ActionMode:  "item",
		ItemID:      "teleport",
		Projectiles: []*ProjectileResult{result},
	}
	m.broadcastShot(shot, tl)

	delay := shot.Duration + 0.3 // small buffer after animation
	if delay < 0.5 {
		delay = 0.5
	}
	m.resolveTurnAfter(ctx, delay, func(ctx context.Context) {
		m.checkWinCondition(ctx)
		if m.State.Status == "in_progress" {
			m.endTurn(ctx)
		}
	})
}

// airStrikeX picks the air strike column: the client's TargetX when given,
// otherwise where the aimed shot would land.
func (m *Match) airStrikeX(player *BattlePlayerState, action ShootAction, weaponConfig gamedata.WeaponConfig) float64 {
	x := player.Position.X
	if action.TargetX != nil && isFinite(*action.TargetX) {
		x = *action.TargetX
	} else {
		aimed := SimulateProjectile(player.PlayerID, player.TeamID, shotOrigin(m.Terrain, player), action.Angle, action.Power,
			weaponConfig, m.State.Wind, m.Terrain, m.State.Players, 0)
		if aimed.ExplosionPoint != nil {
			x = aimed.ExplosionPoint.X
		} else if len(aimed.Path) > 0 {
			x = aimed.Path[len(aimed.Path)-1].Position.X
		}
	}
	return math.Max(0, math.Min(x, float64(m.Terrain.Width)))
}

// drillPierce returns how much terrain the player's shot may drill through this turn: the
// drill_bomb item value (50 px) when armed with it.
func (m *Match) drillPierce(player *BattlePlayerState) float64 {
	if HasEffect(player, "drill_bomb") {
		return m.gd.ItemValue("drill_bomb", gamedata.DefaultDrillPierce)
	}
	return 0
}

func (m *Match) processEndTurn(ctx context.Context, client *ws.Client) {
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}
	m.endTurn(ctx)
}

func (m *Match) processReconnect(ctx context.Context, client *ws.Client) {
	// Re-assign websocket connection
	m.Clients[client.PlayerID] = client
	client.RoomID = m.State.RoomID

	// Send full match state sync, including current terrain so missed explosions are recovered.
	// Non-blocking like broadcast: a stalled client must not stall the match loop.
	select {
	case client.Send <- ws.Message{Event: "MatchStateSync", Data: m.syncPayload(client.PlayerID)}:
	default:
		observability.Log.Warn().
			Str("matchId", m.State.MatchID).
			Str("playerId", client.PlayerID).
			Msg("reconnect: Send channel FULL, MatchStateSync dropped")
	}
}

func (m *Match) processLeave(ctx context.Context, client *ws.Client) {
	delete(m.Clients, client.PlayerID)
	// Kill player's character in match since they abandoned
	if player, ok := m.State.Players[client.PlayerID]; ok {
		if m.State.Status != "ended" {
			player.Left = true
		}
		player.IsAlive = false
		player.HP = 0
	}
	m.checkWinCondition(ctx)
}

func (m *Match) checkWinCondition(ctx context.Context) {
	// Events queued before the end (another Leave) must not pay out a second time
	if m.State.Status == "ended" {
		return
	}

	team1Alive := false
	team2Alive := false

	for _, p := range m.State.Players {
		if p.IsAlive {
			if p.TeamID == 1 {
				team1Alive = true
			} else if p.TeamID == 2 {
				team2Alive = true
			}
		}
	}

	if !team1Alive || !team2Alive {
		// Match is over!
		m.State.Status = "ended"
		observability.MatchEndedTotal.WithLabelValues("normal").Inc()

		winningTeam := 0
		if team1Alive {
			winningTeam = 1
		} else if team2Alive {
			winningTeam = 2
		}

		// Calculate rewards
		stats := make(map[string]*PlayerStats)
		for _, p := range m.State.Players {
			// A player who left is never a winner (nor in a draw)
			isWinner := p.TeamID == winningTeam && !p.Left
			stats[p.PlayerID] = &PlayerStats{
				PlayerID:    p.PlayerID,
				CharacterID: p.CharacterID,
				TeamID:      p.TeamID,
				Damage:      p.DamageDealt,
				Kills:       p.KillCount,
				Accuracy:    calculateAccuracy(p.ShotsFired, p.ShotsHit),
				ShotsHit:    p.ShotsHit,
				SkillUses:   p.SkillUses,
				ItemsUsed:   len(p.UsedItems),
				IsWinner:    isWinner,
				IsDraw:      winningTeam == 0 && !p.Left,
				Left:        p.Left,
				Counts:      m.milestoneCounts(p),
			}
		}

		// Items spent during the match; unused reservations are released
		usedItems := make(map[string][]string)
		for _, p := range m.State.Players {
			if len(p.UsedItems) > 0 {
				usedItems[p.PlayerID] = p.UsedItems
			}
		}

		rewards, err := m.processRewards(ctx, stats, usedItems)
		if err != nil {
			// Rolled back: Run's exit releases the reservations instead
			observability.Log.Error().Err(err).Msg("failed to process match rewards")
		} else {
			m.reservationsSettled = true
		}

		// Broadcast MatchEnded
		ended := map[string]interface{}{
			"winningTeam": winningTeam,
			"rewards":     rewards,
			"stats":       m.endStats(),
			"mvpPlayerId": matchMVP(m.State.Players, winningTeam),
		}
		payload, _ := json.Marshal(ended)
		m.broadcast(ws.Message{
			Event: "MatchEnded",
			Data:  payload,
		})
		m.el.Log("MatchEnded", "", ended)

		// Cleanup connections
		for _, client := range m.Clients {
			client.RoomID = ""
		}

		m.signalDone()
		m.cancel()
	}
}

// turnStartDamage takes damage from the current player at the start of its turn (lava, burn;
// no defense, shield first) and sends PlayerDamaged [{playerId, damage, hp, shield, isAlive,
// type}].
func (m *Match) turnStartDamage(p *BattlePlayerState, damage int, kind string) {
	hurtPlayer(p, damage)
	m.sendPlayerDamaged(p, damage, kind)
}

// sendPlayerDamaged sends PlayerDamaged [{playerId, damage, hp, shield, isAlive, type}] for damage p
// took outside a shot (lava, burn, out_of_map).
func (m *Match) sendPlayerDamaged(p *BattlePlayerState, damage int, kind string) {
	payload, _ := json.Marshal([]map[string]interface{}{{
		"playerId": p.PlayerID,
		"damage":   damage,
		"hp":       p.HP,
		"shield":   p.Shield,
		"isAlive":  p.IsAlive,
		"type":     kind,
	}})
	m.broadcast(ws.Message{Event: "PlayerDamaged", Data: payload})
	m.el.Log("PlayerDamaged", p.PlayerID, json.RawMessage(payload))
}

func (m *Match) updateWind() {
	// If WindStopper is active, wind = 0
	hasWindStop := false
	for _, e := range m.State.ActiveEffects {
		if e.EffectID == "wind_stop" && e.DurationTurn > 0 {
			hasWindStop = true
			break
		}
	}

	if hasWindStop {
		m.State.Wind.Power = 0
		m.State.Wind.Direction = 0
		return
	}

	// Determine wind power range from map config
	var windMin, windMax float64
	windMin = 0
	windMax = 4
	if mapCfg, ok := m.gd.Data.Maps[m.State.MapID]; ok && len(mapCfg.DefaultWindPowerRange) == 2 {
		windMin = mapCfg.DefaultWindPowerRange[0]
		windMax = mapCfg.DefaultWindPowerRange[1]
	}

	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m.State.Wind.Power = windMin + r.Float64()*(windMax-windMin)
	if m.State.Wind.Power < 0.01 {
		m.State.Wind.Direction = 0
	} else {
		if r.Float64() < 0.5 {
			m.State.Wind.Direction = -1
		} else {
			m.State.Wind.Direction = 1
		}
	}
}

func (m *Match) endAsNoContest() {
	m.State.Status = "ended"
	observability.MatchEndedTotal.WithLabelValues("no_contest").Inc()

	// Release all item reservations so players get their items back
	m.releaseUnsettledReservations()

	payload, _ := json.Marshal(map[string]interface{}{
		"winningTeam": 0,
		"result":      "no_contest",
		"message":     "Server terminated game due to error or shutdown",
		"stats":       m.endStats(),
		"mvpPlayerId": "",
	})
	m.broadcast(ws.Message{
		Event: "MatchEnded",
		Data:  payload,
	})
	m.signalDone()
	m.cancel()
}

// EndStats is one player's line of MatchEnded.stats.
type EndStats struct {
	DamageDealt int `json:"damageDealt"`
	Kills       int `json:"kills"`
	ShotsHit    int `json:"shotsHit"`
	ShotsFired  int `json:"shotsFired"`
}

// endStats is MatchEnded.stats: every player of the match, bots included.
func (m *Match) endStats() map[string]EndStats {
	out := make(map[string]EndStats, len(m.State.Players))
	for id, p := range m.State.Players {
		out[id] = EndStats{DamageDealt: p.DamageDealt, Kills: p.KillCount, ShotsHit: p.ShotsHit, ShotsFired: p.ShotsFired}
	}
	return out
}

// matchMVP is the player (bots included) with the most damage dealt; ties go to
// more kills, then the winning team, then the smaller player id. "" without players.
func matchMVP(players map[string]*BattlePlayerState, winningTeam int) string {
	var best *BattlePlayerState
	for _, p := range players {
		if best == nil {
			best = p
			continue
		}
		pw, bw := winningTeam != 0 && p.TeamID == winningTeam, winningTeam != 0 && best.TeamID == winningTeam
		switch {
		case p.DamageDealt != best.DamageDealt:
			if p.DamageDealt > best.DamageDealt {
				best = p
			}
		case p.KillCount != best.KillCount:
			if p.KillCount > best.KillCount {
				best = p
			}
		case pw != bw:
			if pw {
				best = p
			}
		case p.PlayerID < best.PlayerID:
			best = p
		}
	}
	if best == nil {
		return ""
	}
	return best.PlayerID
}

func calculateAccuracy(fired, hit int) float64 {
	if fired == 0 {
		return 0
	}
	return float64(hit) / float64(fired)
}

func (m *Match) broadcast(msg ws.Message) {
	for pid, client := range m.Clients {
		select {
		case client.Send <- msg:
			client.DroppedMsgs = 0
		default:
			client.DroppedMsgs++
			observability.Log.Warn().
				Str("matchId", m.State.MatchID).
				Str("event", msg.Event).
				Str("playerId", pid).
				Int("droppedCount", client.DroppedMsgs).
				Msg("broadcast: Send channel FULL, DROPPED")
			if client.DroppedMsgs >= maxDroppedMsgs {
				observability.Log.Warn().
					Str("matchId", m.State.MatchID).
					Str("playerId", pid).
					Msg("kicking slow client - too many consecutive dropped messages")
				close(client.Send)
				delete(m.Clients, pid)
				if player, ok := m.State.Players[pid]; ok {
					player.IsAlive = false
					player.HP = 0
				}
			}
		}
	}
}

func (m *Match) broadcastMatchStarted() {
	m.broadcast(ws.Message{
		Event: "MatchStarted",
		Data:  m.snapshotPayload(),
	})
	m.el.Log("MatchStarted", "", m.State)
}
