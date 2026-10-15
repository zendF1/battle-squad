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
	actionRateLimit = 150 * time.Millisecond // min interval between player actions
	maxDroppedMsgs  = 10                     // kick client after N consecutive dropped broadcasts

	// reservationReleaseTimeout bounds releasing a match's reservations; it runs on a fresh
	// context because the match context is usually already cancelled by then.
	reservationReleaseTimeout = 5 * time.Second

	skillHealAmount = 30.0 // healing_bloom heal before the skill power bonus
)

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
	MatchDone    chan struct{}
	doneOnce     sync.Once
	TeamRatings  map[int]int // teamID → avg rating for Elo
	EloParams    EloParams   // Elo configuration
	BotTier      matchmaker.BotTierConfig
	lastAction   map[string]time.Time // playerID → last action timestamp (rate limit)
	turnSeq      int                  // incremented every startTurn; stale callbacks compare against it
	resolving    bool                 // the current turn's action is animating; no further actions or timer ticks
	lastMoveSeq  map[string]int       // playerID → seq of the last Move the loop took from them this turn (see noteMoveSeq)

	// processRewards settles a normally ended match: rewards, consuming spent items and
	// releasing the rest. releaseReservations returns every still-reserved item of the
	// match; nil without a database. Fields so tests can fake them.
	processRewards      func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error)
	releaseReservations func(ctx context.Context) error
	// reservationsSettled: rewards or a no-contest release already settled the
	// reservations, so Run's exit has nothing to release.
	reservationsSettled bool
}

// Stop cancels the match context, causing the Run loop to exit.
func (m *Match) Stop() {
	m.cancel()
}

type RoomHubInterface interface {
	UnregisterRoom(ctx context.Context, roomID string)
}

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
		mPlayers[p.PlayerID] = p
		turnOrder = append(turnOrder, p.PlayerID)
	}

	// Build terrain from map config, with legacy fallback
	mapCfgForTerrain := gamedata.MapConfig{
		MapID:  mapID,
		Width:  gamedata.DefaultMapWidth,
		Height: gamedata.DefaultMapHeight,
	}
	if gamedata.Data != nil {
		if mc, ok := gamedata.Data.Maps[mapID]; ok {
			mapCfgForTerrain = mc
		}
	}
	terrain := NewTerrain(mapCfgForTerrain)

	// Land players on the surface nearest their configured spawn height
	for _, p := range mPlayers {
		p.Position.Y = terrain.SpawnY(p.Position.X, p.Position.Y)
	}

	mState := MatchState{
		MatchID:         matchID,
		RoomID:          roomID,
		Mode:            mode,
		MapID:           mapID,
		TurnIndex:       0,
		CurrentPlayerID: turnOrder[0],
		Wind: WindState{
			Direction: 0,
			Power:     0,
		},
		Players:       mPlayers,
		Status:        "in_progress",
		TurnOrder:     turnOrder,
		TurnTimeLeft:  20,
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
		MatchDone:    make(chan struct{}),
		TeamRatings:  map[int]int{},
		EloParams:    EloParams{},
		BotTier:      DefaultBotTier,
		lastAction:   make(map[string]time.Time),
		lastMoveSeq:  make(map[string]int),
	}
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		return ProcessMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats, usedItems, m.TeamRatings, m.EloParams)
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

	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Str("roomId", m.State.RoomID).
		Str("mode", m.State.Mode).
		Int("clientCount", len(m.Clients)).
		Int("playerCount", len(m.State.Players)).
		Str("status", m.State.Status).
		Msg("[RANKED-DEBUG] Match.Run() started")

	for pid := range m.Clients {
		observability.Log.Info().
			Str("matchId", m.State.MatchID).
			Str("clientPlayerId", pid).
			Msg("[RANKED-DEBUG] Match.Run() client")
	}

	for pid, p := range m.State.Players {
		observability.Log.Info().
			Str("matchId", m.State.MatchID).
			Str("playerId", pid).
			Bool("isBot", p.IsBot).
			Bool("isAlive", p.IsAlive).
			Int("teamId", p.TeamID).
			Msg("[RANKED-DEBUG] Match.Run() player state")
	}

	m.el.Start(m.ctx)
	m.lastActivity = time.Now()

	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Int("clientCount", len(m.Clients)).
		Msg("[RANKED-DEBUG] about to broadcastMatchStarted")
	m.broadcastMatchStarted()

	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Str("currentPlayerId", m.State.CurrentPlayerID).
		Msg("[RANKED-DEBUG] about to startTurn (first turn)")
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

			// Pattern 10: Watchdog Timer to prevent stuck zombie matches
			if time.Since(m.lastActivity) > 2*time.Minute {
				observability.Log.Warn().Str("matchId", m.State.MatchID).Msg("match stuck with no activity for 2 mins, terminating")
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
			if last, ok := m.lastAction[actorID]; ok && now.Sub(last) < actionRateLimit {
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
	case "Reconnect":
		m.processReconnect(ev.ctx, client)
	case "Leave":
		m.processLeave(ev.ctx, client)
	}
}

func (m *Match) startTurn(ctx context.Context) {
	m.State.TurnTimeLeft = 20
	m.turnSeq++
	m.resolving = false
	clear(m.lastMoveSeq) // a new turn starts a new replay on the mover's client

	player := m.State.Players[m.State.CurrentPlayerID]
	player.MoveEnergy = 100 + player.MoveEnergyBonus // Reset move energy to full
	ApplyTurnStartEffects(player)                    // net / freeze limit this turn's movement

	// Skill energy is incremented at end of turn, not start

	// Lava terrain: deal 5 damage to the current player at the start of their turn
	terrainType := m.Terrain.GetTerrainTypeAt(player.Position.X, player.Position.Y)
	if terrainType == "lava" {
		lavaDamage := 5
		player.HP -= lavaDamage
		if player.HP <= 0 {
			player.HP = 0
			player.IsAlive = false
		}
		lavaPayload, _ := json.Marshal([]map[string]interface{}{
			{
				"playerId": player.PlayerID,
				"damage":   lavaDamage,
				"hp":       player.HP,
				"isAlive":  player.IsAlive,
				"type":     "lava",
			},
		})
		m.broadcast(ws.Message{
			Event: "PlayerDamaged",
			Data:  lavaPayload,
		})
	}

	// Update wind: direction (-1 to 1), power (0 to 4)
	m.updateWind()

	// Broadcast TurnStarted
	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Int("turnIndex", m.State.TurnIndex).
		Str("currentPlayerId", m.State.CurrentPlayerID).
		Bool("isBot", player.IsBot).
		Int("clientCount", len(m.Clients)).
		Msg("[RANKED-DEBUG] startTurn: broadcasting TurnStarted")

	skillEnergy := make(map[string]int, len(m.State.Players))
	for id, p := range m.State.Players {
		skillEnergy[id] = p.SkillEnergy
	}
	payload, _ := json.Marshal(map[string]interface{}{
		"turnIndex":       m.State.TurnIndex,
		"currentPlayerId": m.State.CurrentPlayerID,
		"wind":            m.State.Wind,
		"moveEnergy":      player.MoveEnergy,
		"skillEnergy":     skillEnergy,
	})
	m.broadcast(ws.Message{
		Event: "TurnStarted",
		Data:  payload,
	})

	if player.IsBot {
		observability.Log.Info().
			Str("matchId", m.State.MatchID).
			Str("botId", player.PlayerID).
			Msg("[RANKED-DEBUG] startTurn: scheduling bot action")
		// Random delay 2-6s so bots feel natural, not instant
		botDelay := time.Duration(2000+rand.Intn(4000)) * time.Millisecond
		m.runOnTurnAfter(ctx, botDelay, func(ctx context.Context) {
			m.botStep(ctx, botTurnState{}, 0)
		})
	}
}

func (m *Match) endTurn(ctx context.Context) {
	// Add skill energy to current player (+20 per turn, max 100)
	currentPlayer := m.State.Players[m.State.CurrentPlayerID]
	if currentPlayer != nil && currentPlayer.IsAlive {
		currentPlayer.SkillEnergy += 20
		if currentPlayer.SkillEnergy > 100 {
			currentPlayer.SkillEnergy = 100
		}
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
	if !player.IsAlive || !isFinite(action.TargetX) || HasEffect(player, "freeze") {
		return
	}

	// Clamp target by max energy range
	maxDist := float64(player.MoveEnergy * 2) // 1 energy per 2 pixels
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
	energyCost := int(math.Round(actualDist * 0.5))
	if energyCost > player.MoveEnergy {
		energyCost = player.MoveEnergy
	}

	startX := player.Position.X
	player.MoveEnergy -= energyCost
	player.Position.X = finalX
	player.Position.Y = finalY

	// Ice terrain: slide 50px in the movement direction, only after an actual step
	iceTerrainType := m.Terrain.GetTerrainTypeAt(player.Position.X, player.Position.Y)
	if actualDist > 0 && iceTerrainType == "ice" {
		slideDir := 50.0
		if finalX < startX {
			slideDir = -50.0
		}
		slideX, slideY := m.Terrain.WalkTo(player.Position.X, player.Position.Y, player.Position.X+slideDir)
		player.Position.X = slideX
		player.Position.Y = slideY
	}

	// Broadcast PlayerMoved; seq tells the mover which of its commands this answers
	moved := map[string]interface{}{
		"playerId":   player.PlayerID,
		"position":   player.Position,
		"moveEnergy": player.MoveEnergy,
	}
	if action.Seq > 0 {
		moved["seq"] = action.Seq
	}
	payload, _ := json.Marshal(moved)
	m.broadcast(ws.Message{
		Event: "PlayerMoved",
		Data:  payload,
	})
}

func (m *Match) processShoot(ctx context.Context, client *ws.Client, action ShootAction) {
	// Validate turn
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}
	if !validShotInput(action.Angle, action.Power) {
		return
	}

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive {
		return
	}

	// freeze_bomb: frozen players cannot shoot, they can only EndTurn
	if HasEffect(player, "freeze") {
		return
	}

	charConfig, exists := gamedata.Data.Characters[player.CharacterID]
	if !exists {
		return
	}

	weaponConfig, exists := gamedata.Data.Weapons[charConfig.WeaponID]
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
		if err := ApplyImmediateItem(ctx, &m.State, player.PlayerID, itemID, nil, m.Terrain); err != nil {
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
	shootOrigin := player.Position
	shootAngle := action.Angle
	shootPower := action.Power

	// air_strike: fire from top of map straight down at TargetX
	if HasEffect(player, "air_strike") {
		shootOrigin = Vector2{X: m.airStrikeX(player, action, weaponConfig), Y: 0}
		shootAngle = 270.0 // straight down in Y-down coords (270° = negative Y direction conventionally, handled by sin)
		shootPower = 80.0  // fixed high power for airstrike
	}

	// drill_bomb: projectile drills through the first terrain block it meets
	pierce := drillPierce(player)

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
		damageFactor = 1.5
	}

	// 2. Resolve explosion, damage and falls
	tl := &shotTimeline{}
	if result.ExplosionPoint != nil {
		// Fragile terrain: double the explosion radius
		if m.Terrain.GetTerrainTypeAt(result.ExplosionPoint.X, result.ExplosionPoint.Y) == "fragile" {
			result.ExplosionRadius *= 2
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
				applyShotStatus(tl, flightEnd(result), player, target, "net", 1)
			}
			if HasEffect(player, "freeze_bomb") {
				applyShotStatus(tl, flightEnd(result), player, target, "freeze", int(itemValue("freeze_bomb", 1)))
			}
		}
	}

	// 3. Broadcast, check win conditions, schedule delayed end turn
	m.finishShot(ctx, shot, tl)
}

// shootSkill fires the character's skill. Most skills launch projectiles; healing_bloom heals
// the shooter instead.
func (m *Match) shootSkill(ctx context.Context, player *BattlePlayerState, action ShootAction, skillID string, weaponConfig gamedata.WeaponConfig) {
	skillConfig, skillExists := gamedata.Data.Skills[skillID]
	if !skillExists {
		return
	}

	// Reject if not enough skill energy
	if player.SkillEnergy < 100 {
		return
	}

	shot := &ShotResolved{ShooterID: player.PlayerID, ActionMode: "skill", SkillID: skillID}
	tl := &shotTimeline{}

	// healing_bloom: no projectile — just heal self and end turn
	if skillConfig.EffectType == "heal" {
		before := player.HP
		player.HP += int(math.Round(skillHealAmount * skillPowerFactor(player)))
		if player.HP > player.MaxHP {
			player.HP = player.MaxHP
		}
		tl.add(ShotEvent{Type: "heal", PlayerID: player.PlayerID, Amount: player.HP - before, HP: intPtr(player.HP)})
		if skillConfig.StatusEffectID != "" {
			applyShotStatus(tl, 0, player, player, skillConfig.StatusEffectID, 1)
		}
		player.SkillEnergy = 0

		m.broadcastShot(shot, tl)
		m.checkWinCondition(ctx)
		if m.State.Status == "in_progress" {
			m.endTurn(ctx)
		}
		return
	}

	// Calculate base damage factor (power_shot item still applies)
	damageFactor := 1.0
	if HasEffect(player, "power_shot") {
		damageFactor = 1.5
	}
	damageFactor *= skillConfig.DamageMultiplier * skillPowerFactor(player)
	baseDamage := float64(weaponConfig.Damage+player.DamageBonus) * damageFactor

	// Build list of (angle, explosionRadius) pairs per projectile
	type shotParams struct {
		angle  float64
		radius float64
	}
	var shots []shotParams

	switch skillConfig.EffectType {
	case "multi_projectile":
		// triple_shot: fire 3 projectiles at angle-10, angle, angle+10
		for _, offset := range []float64{-10, 0, 10} {
			shots = append(shots, shotParams{angle: action.Angle + offset, radius: float64(weaponConfig.ExplosionRadius)})
		}
	case "single_large_bomb":
		// heavy_bomb: single projectile, explosion radius * 1.5
		shots = append(shots, shotParams{angle: action.Angle, radius: float64(weaponConfig.ExplosionRadius) * 1.5})
	default:
		// shock_field and any other types: single normal projectile
		shots = append(shots, shotParams{angle: action.Angle, radius: float64(weaponConfig.ExplosionRadius)})
	}

	pierce := drillPierce(player)

	// Projectiles are resolved in firing order; each sees the craters of the ones before it.
	for _, s := range shots {
		r := SimulateProjectile(
			player.PlayerID,
			player.TeamID,
			player.Position,
			s.angle,
			action.Power,
			weaponConfig,
			m.State.Wind,
			m.Terrain,
			m.State.Players,
			pierce,
		)
		r.SkillID = skillID
		r.ExplosionRadius = s.radius
		shot.Projectiles = append(shot.Projectiles, r)

		// Track shot fired for each projectile in the skill
		player.ShotsFired++

		if r.ExplosionPoint == nil {
			continue
		}
		m.explodeProjectile(player, r, baseDamage, tl)

		// shock_field: apply "net" status to hit player
		if skillConfig.EffectType == "debuff" && skillConfig.StatusEffectID != "" && r.HitPlayerID != "" {
			if target, ok := m.State.Players[r.HitPlayerID]; ok && target.IsAlive {
				applyShotStatus(tl, flightEnd(r), player, target, skillConfig.StatusEffectID, 1)
			}
		}

		m.settleFalls(player, flightEnd(r), tl)
	}

	// Reset skill energy after use
	player.SkillEnergy = 0
	m.finishShot(ctx, shot, tl)
}

func (m *Match) processUseItem(ctx context.Context, client *ws.Client, action UseItemAction) {
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}

	log := observability.FromContext(ctx)

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive {
		return
	}
	// Teleport is fired as a projectile (Shoot with actionMode "item") so it follows
	// trajectory rules and ends the turn; it cannot be used directly
	if action.ItemID == "teleport" {
		log.Warn().Str("playerId", player.PlayerID).Msg("rejected direct teleport via UseItem")
		return
	}

	err := ApplyImmediateItem(ctx, &m.State, m.State.CurrentPlayerID, action.ItemID, action.TargetPosition, m.Terrain)
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
		player.Position,
		action.Angle,
		action.Power,
		weaponConfig,
		m.State.Wind,
		m.Terrain,
		m.State.Players,
		0, // no drilling
	)
	result.ExplosionRadius = 0

	player.ShotsFired++
	consumeItem(player, itemIdx)
	m.broadcastItemUsed(player.PlayerID, "teleport")

	tl := &shotTimeline{}
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
		aimed := SimulateProjectile(player.PlayerID, player.TeamID, player.Position, action.Angle, action.Power,
			weaponConfig, m.State.Wind, m.Terrain, m.State.Players, 0)
		if aimed.ExplosionPoint != nil {
			x = aimed.ExplosionPoint.X
		} else if len(aimed.Path) > 0 {
			x = aimed.Path[len(aimed.Path)-1].Position.X
		}
	}
	return math.Max(0, math.Min(x, float64(m.Terrain.Width)))
}

// drillPierce returns how much terrain the player's shot may drill through this turn.
func drillPierce(player *BattlePlayerState) float64 {
	if HasEffect(player, "drill_bomb") {
		return itemValue("drill_bomb", drillPierceDist)
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
			isWinner := p.TeamID == winningTeam
			stats[p.PlayerID] = &PlayerStats{
				PlayerID:    p.PlayerID,
				CharacterID: p.CharacterID,
				TeamID:      p.TeamID,
				Damage:      p.DamageDealt,
				Kills:       p.KillCount,
				Accuracy:    calculateAccuracy(p.ShotsFired, p.ShotsHit),
				IsWinner:    isWinner,
				IsDraw:      winningTeam == 0,
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
		payload, _ := json.Marshal(map[string]interface{}{
			"winningTeam": winningTeam,
			"rewards":     rewards,
		})
		m.broadcast(ws.Message{
			Event: "MatchEnded",
			Data:  payload,
		})
		m.el.Log("MatchEnded", "", map[string]interface{}{
			"winningTeam": winningTeam,
			"rewards":     rewards,
		})

		// Cleanup connections
		for _, client := range m.Clients {
			client.RoomID = ""
		}

		m.signalDone()
		m.cancel()
	}
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
	if mapCfg, ok := gamedata.Data.Maps[m.State.MapID]; ok && len(mapCfg.DefaultWindPowerRange) == 2 {
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
	})
	m.broadcast(ws.Message{
		Event: "MatchEnded",
		Data:  payload,
	})
	m.signalDone()
	m.cancel()
}

func calculateAccuracy(fired, hit int) float64 {
	if fired == 0 {
		return 0
	}
	return float64(hit) / float64(fired)
}

func (m *Match) broadcast(msg ws.Message) {
	observability.Log.Debug().
		Str("matchId", m.State.MatchID).
		Str("event", msg.Event).
		Int("clientCount", len(m.Clients)).
		Msg("[RANKED-DEBUG] broadcast")
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
	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Int("clientCount", len(m.Clients)).
		Msg("[RANKED-DEBUG] broadcastMatchStarted: broadcasting to clients")

	for pid := range m.Clients {
		observability.Log.Info().
			Str("matchId", m.State.MatchID).
			Str("targetPlayerId", pid).
			Msg("[RANKED-DEBUG] broadcastMatchStarted: will send to")
	}

	m.broadcast(ws.Message{
		Event: "MatchStarted",
		Data:  m.snapshotPayload(),
	})
	m.el.Log("MatchStarted", "", m.State)

	observability.Log.Info().
		Str("matchId", m.State.MatchID).
		Msg("[RANKED-DEBUG] broadcastMatchStarted: done")
}
