package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
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
	lastAction   map[string]time.Time // playerID → last action timestamp (rate limit)
	turnSeq      int                  // incremented every startTurn; stale callbacks compare against it
	resolving    bool                 // the current turn's action is animating; no further actions or timer ticks
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

	return &Match{
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
		lastAction:   make(map[string]time.Time),
	}
}

func (m *Match) signalDone() {
	m.doneOnce.Do(func() {
		close(m.MatchDone)
	})
}

func (m *Match) Run() {
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

	// If it is a bot's turn, trigger bot decision block after a short delay
	if player.IsBot {
		observability.Log.Info().
			Str("matchId", m.State.MatchID).
			Str("botId", player.PlayerID).
			Msg("[RANKED-DEBUG] startTurn: scheduling bot action")
		frozen := HasEffect(player, "freeze")
		go func() {
			// Random delay 2-6s so bots feel natural, not instant
			botDelay := 2000 + rand.Intn(4000)
			time.Sleep(time.Duration(botDelay) * time.Millisecond)

			// Idle bots (tutorial) and frozen bots just end their turn
			if frozen || (strings.HasPrefix(player.PlayerID, "bot_") && player.DisplayName == "Target Bot") {
				m.Events <- matchEvent{
					client: nil,
					msg:    ws.Message{Event: "EndTurn"},
					ctx:    context.Background(),
				}
				return
			}

			brain := NewBotBrain("normal")
			action := brain.DecideAction(player, &m.State)
			if act, ok := action.(ShootAction); ok {
				payload, _ := json.Marshal(act)
				m.Events <- matchEvent{
					client: nil,
					msg:    ws.Message{Event: "Shoot", Data: payload},
					ctx:    context.Background(),
				}
			} else if act, ok := action.(UseItemAction); ok {
				payload, _ := json.Marshal(act)
				m.Events <- matchEvent{
					client: nil,
					msg:    ws.Message{Event: "UseItem", Data: payload},
					ctx:    context.Background(),
				}
			}
		}()
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
func (m *Match) scheduleEndTurn(ctx context.Context, flightTime float64) {
	delay := flightTime + 1.2 // explosion + damage popup + buffer
	if delay < 1.5 {
		delay = 1.5
	}
	m.resolveTurnAfter(ctx, delay, m.endTurn)
}

// resolveTurnAfter locks the current turn and runs fn on the match loop after
// delaySec seconds, unless the match ended or the turn changed in the meantime.
func (m *Match) resolveTurnAfter(ctx context.Context, delaySec float64, fn func(ctx context.Context)) {
	m.resolving = true
	turn := m.turnSeq
	go func() {
		timer := time.NewTimer(time.Duration(delaySec * float64(time.Second)))
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

	// Broadcast PlayerMoved
	payload, _ := json.Marshal(map[string]interface{}{
		"playerId":   player.PlayerID,
		"position":   player.Position,
		"moveEnergy": player.MoveEnergy,
	})
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

	// ── Skill mode ──────────────────────────────────────────────────────────
	if action.ActionMode == "skill" {
		skillID := charConfig.SkillID
		skillConfig, skillExists := gamedata.Data.Skills[skillID]
		if !skillExists {
			return
		}

		// Reject if not enough skill energy
		if player.SkillEnergy < 100 {
			return
		}

		// healing_bloom: no projectile — just heal self and end turn
		if skillConfig.EffectType == "heal" {
			healAmount := 30
			player.HP += healAmount
			if player.HP > player.MaxHP {
				player.HP = player.MaxHP
			}
			ApplyStatusEffect(player, StatusEffect{
				EffectID:       skillConfig.StatusEffectID,
				TargetPlayerID: player.PlayerID,
				DurationTurn:   1,
				Value:          0,
				SourcePlayerID: player.PlayerID,
			})
			player.SkillEnergy = 0

			payload, _ := json.Marshal(map[string]interface{}{
				"playerId":    player.PlayerID,
				"skillId":     skillID,
				"hp":          player.HP,
				"skillEnergy": player.SkillEnergy,
			})
			m.broadcast(ws.Message{Event: "SkillUsed", Data: payload})

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
		damageFactor *= skillConfig.DamageMultiplier

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

		var results []*ProjectileResult
		var damagedPlayers []map[string]interface{}

		for _, shot := range shots {
			r := SimulateProjectile(
				player.PlayerID,
				player.TeamID,
				player.Position,
				shot.angle,
				action.Power,
				weaponConfig,
				m.State.Wind,
				m.Terrain,
				m.State.Players,
				pierce,
			)
			r.SkillID = skillID
			r.ExplosionRadius = shot.radius

			// Track shot fired for each projectile in the skill
			player.ShotsFired++

			if r.ExplosionPoint != nil {
				m.Terrain.DestroyCircle(r.ExplosionPoint.X, r.ExplosionPoint.Y, r.ExplosionRadius)
				r.TerrainDestroyed = true
				r.TerrainVersion = m.Terrain.Version

				for _, p := range m.State.Players {
					if !p.IsAlive {
						continue
					}
					// No friendly fire — skip teammates
					if p.TeamID == player.TeamID {
						continue
					}

					// Direct hit: use player's own position as explosion center (distance=0, full damage)
					explosionCenter := *r.ExplosionPoint
					if p.PlayerID == r.HitPlayerID {
						explosionCenter = p.Position
					}

					damage := CalculateExplosionDamage(
						p.Position,
						explosionCenter,
						float64(weaponConfig.Damage+player.DamageBonus)*damageFactor,
						r.ExplosionRadius,
						p.Defense,
					)
					damage, _ = ApplyCritical(damage, player.CritChance)
					if damage > 0 {
						p.HP -= damage
						isKilled := false
						if p.HP <= 0 {
							p.HP = 0
							p.IsAlive = false
							isKilled = true
						}

						player.DamageDealt += damage
						player.ShotsHit++
						if isKilled {
							player.KillCount++
						}

						damagedPlayers = append(damagedPlayers, map[string]interface{}{
							"playerId": p.PlayerID,
							"damage":   damage,
							"hp":       p.HP,
							"isAlive":  p.IsAlive,
							"isKilled": isKilled,
						})
					}
				}

				// shock_field: apply "net" status to hit player
				if skillConfig.EffectType == "debuff" && skillConfig.StatusEffectID != "" && r.HitPlayerID != "" {
					if target, ok := m.State.Players[r.HitPlayerID]; ok && target.IsAlive {
						ApplyStatusEffect(target, StatusEffect{
							EffectID:       skillConfig.StatusEffectID,
							TargetPlayerID: target.PlayerID,
							DurationTurn:   1,
							SourcePlayerID: player.PlayerID,
						})
					}
				}

				// Handle terrain collapse / fall damage
				for _, p := range m.State.Players {
					if !p.IsAlive {
						continue
					}
					landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
					if landY >= float64(m.Terrain.Height) {
						// Fell off the map — instant death
						remainingHP := p.HP
						p.HP = 0
						p.IsAlive = false
						p.Position.Y = landY

						player.KillCount++

						damagedPlayers = append(damagedPlayers, map[string]interface{}{
							"playerId": p.PlayerID,
							"damage":   remainingHP,
							"hp":       0,
							"isAlive":  false,
							"isKilled": true,
							"type":     "fall",
						})
						continue
					}
					if landY > p.Position.Y {
						fallDistance := landY - p.Position.Y
						p.Position.Y = landY
						fallDamage := CalculateFallDamage(fallDistance)
						if fallDamage > 0 {
							p.HP -= fallDamage
							isKilled := false
							if p.HP <= 0 {
								p.HP = 0
								p.IsAlive = false
								isKilled = true
							}
							damagedPlayers = append(damagedPlayers, map[string]interface{}{
								"playerId": p.PlayerID,
								"damage":   fallDamage,
								"hp":       p.HP,
								"isAlive":  p.IsAlive,
								"isKilled": isKilled,
								"type":     "fall",
							})
						}
					}
				}
			}

			results = append(results, r)
		}

		// Reset skill energy after use
		player.SkillEnergy = 0
		skillPayload, _ := json.Marshal(map[string]interface{}{
			"playerId":    player.PlayerID,
			"skillId":     skillID,
			"skillEnergy": player.SkillEnergy,
		})
		m.broadcast(ws.Message{Event: "SkillUsed", Data: skillPayload})

		// Broadcast each projectile result
		for _, r := range results {
			payloadResult, _ := json.Marshal(r)
			m.broadcast(ws.Message{Event: "ProjectileResult", Data: payloadResult})
		}

		if len(damagedPlayers) > 0 {
			payloadDamaged, _ := json.Marshal(damagedPlayers)
			m.broadcast(ws.Message{Event: "PlayerDamaged", Data: payloadDamaged})
			m.el.Log("PlayerDamaged", "", damagedPlayers)
		}

		m.checkWinCondition(ctx)
		if m.State.Status == "in_progress" {
			flightTime := 0.0
			for _, r := range results {
				if len(r.Path) > 0 {
					t := r.Path[len(r.Path)-1].Time
					if t > flightTime {
						flightTime = t
					}
				}
			}
			m.scheduleEndTurn(ctx, flightTime)
		}
		return
	}
	// ── End skill mode ───────────────────────────────────────────────────────

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
	}
	// ── End item mode ───────────────────────────────────────────────────────

	// Determine projectile origin, angle, and power — may be overridden by air_strike
	shootOrigin := player.Position
	shootAngle := action.Angle
	shootPower := action.Power

	// air_strike: fire from top of map straight down at TargetX
	if HasEffect(player, "air_strike") {
		shootOrigin = Vector2{X: m.airStrikeX(player, action, weaponConfig), Y: 0}
		shootAngle = 270.0  // straight down in Y-down coords (270° = negative Y direction conventionally, handled by sin)
		shootPower = 80.0   // fixed high power for airstrike
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

	// Track shot fired
	player.ShotsFired++

	// Calculate base damage multiplication factor (e.g. from Power Shot item)
	damageFactor := 1.0
	if HasEffect(player, "power_shot") {
		damageFactor = 1.5
	}

	// 2. Resolve hit and damage
	var damagedPlayers []map[string]interface{}
	var hitEnemies []*BattlePlayerState // enemies damaged by the explosion itself
	if result.ExplosionPoint != nil {
		// Fragile terrain: double the explosion radius
		if m.Terrain.GetTerrainTypeAt(result.ExplosionPoint.X, result.ExplosionPoint.Y) == "fragile" {
			result.ExplosionRadius *= 2
		}

		// Destroy terrain
		m.Terrain.DestroyCircle(result.ExplosionPoint.X, result.ExplosionPoint.Y, result.ExplosionRadius)
		result.TerrainDestroyed = true
		result.TerrainVersion = m.Terrain.Version

		// Check player splash damage (no friendly fire — skip teammates)
		for _, p := range m.State.Players {
			if !p.IsAlive {
				continue
			}
			if p.TeamID == player.TeamID {
				continue
			}

			// Direct hit: use player's own position as explosion center (distance=0, full damage)
			explosionCenter := *result.ExplosionPoint
			if p.PlayerID == result.HitPlayerID {
				explosionCenter = p.Position
			}

			damage := CalculateExplosionDamage(
				p.Position,
				explosionCenter,
				float64(weaponConfig.Damage+player.DamageBonus)*damageFactor,
				result.ExplosionRadius,
				p.Defense,
			)
			damage, _ = ApplyCritical(damage, player.CritChance)

			if damage > 0 {
				p.HP -= damage
				isKilled := false
				if p.HP <= 0 {
					p.HP = 0
					p.IsAlive = false
					isKilled = true
				}
				hitEnemies = append(hitEnemies, p)

				player.DamageDealt += damage
				player.ShotsHit++
				if isKilled {
					player.KillCount++
				}

				damagedPlayers = append(damagedPlayers, map[string]interface{}{
					"playerId": p.PlayerID,
					"damage":   damage,
					"hp":       p.HP,
					"isAlive":  p.IsAlive,
					"isKilled": isKilled,
				})
			}
		}

		// Handle terrain collapse check (falling players)
		for _, p := range m.State.Players {
			if !p.IsAlive {
				continue
			}

			landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
			if landY >= float64(m.Terrain.Height) {
				// Fell off the map — instant death
				remainingHP := p.HP
				p.HP = 0
				p.IsAlive = false
				p.Position.Y = landY

				player.KillCount++

				damagedPlayers = append(damagedPlayers, map[string]interface{}{
					"playerId": p.PlayerID,
					"damage":   remainingHP,
					"hp":       0,
					"isAlive":  false,
					"isKilled": true,
					"type":     "fall",
				})
				continue
			}
			if landY > p.Position.Y {
				fallDistance := landY - p.Position.Y
				p.Position.Y = landY

				fallDamage := CalculateFallDamage(fallDistance)
				if fallDamage > 0 {
					p.HP -= fallDamage
					isKilled := false
					if p.HP <= 0 {
						p.HP = 0
						p.IsAlive = false
						isKilled = true
					}

					damagedPlayers = append(damagedPlayers, map[string]interface{}{
						"playerId": p.PlayerID,
						"damage":   fallDamage,
						"hp":       p.HP,
						"isAlive":  p.IsAlive,
						"isKilled": isKilled,
						"type":     "fall",
					})
				}
			}
		}
	}

	// spider_net / freeze_bomb: control every enemy hit by the explosion for their next turn
	for _, target := range hitEnemies {
		if !target.IsAlive {
			continue
		}
		if HasEffect(player, "spider_net") {
			ApplyStatusEffect(target, StatusEffect{
				EffectID:       "net",
				TargetPlayerID: target.PlayerID,
				DurationTurn:   1,
				SourcePlayerID: player.PlayerID,
			})
		}
		if HasEffect(player, "freeze_bomb") {
			ApplyStatusEffect(target, StatusEffect{
				EffectID:       "freeze",
				TargetPlayerID: target.PlayerID,
				DurationTurn:   int(itemValue("freeze_bomb", 1)),
				SourcePlayerID: player.PlayerID,
			})
		}
	}

	// Broadcast ProjectileResult and PlayerDamaged
	payloadResult, _ := json.Marshal(result)
	m.broadcast(ws.Message{
		Event: "ProjectileResult",
		Data:  payloadResult,
	})

	if len(damagedPlayers) > 0 {
		payloadDamaged, _ := json.Marshal(damagedPlayers)
		m.broadcast(ws.Message{
			Event: "PlayerDamaged",
			Data:  payloadDamaged,
		})
		m.el.Log("PlayerDamaged", "", damagedPlayers)
	}

	// Broadcast updated positions for all players (handles terrain fall)
	if result.TerrainDestroyed {
		for _, p := range m.State.Players {
			if !p.IsAlive {
				continue
			}
			posPayload, _ := json.Marshal(map[string]interface{}{
				"playerId":   p.PlayerID,
				"position":   p.Position,
				"moveEnergy": p.MoveEnergy,
			})
			m.broadcast(ws.Message{Event: "PlayerMoved", Data: posPayload})
		}
	}

	// 3. Complete shooting event, check win conditions, schedule delayed end turn
	m.checkWinCondition(ctx)
	if m.State.Status == "in_progress" {
		flightTime := 0.0
		if len(result.Path) > 0 {
			flightTime = result.Path[len(result.Path)-1].Time
		}
		m.scheduleEndTurn(ctx, flightTime)
	}
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
	result.TerrainDestroyed = false
	result.ExplosionRadius = 0

	player.ShotsFired++
	consumeItem(player, itemIdx)

	// Broadcast projectile path (so client can animate the flight)
	payloadResult, _ := json.Marshal(result)
	m.broadcast(ws.Message{Event: "ProjectileResult", Data: payloadResult})

	var target *Vector2
	if result.ExplosionPoint != nil && result.ExplosionPoint.Y < float64(m.Terrain.Height) {
		landY := m.Terrain.GetLandingY(result.ExplosionPoint.X, result.ExplosionPoint.Y)
		if landY < float64(m.Terrain.Height) {
			target = &Vector2{X: result.ExplosionPoint.X, Y: landY}
		}
	}

	flightTime := 0.0
	if len(result.Path) > 0 {
		flightTime = result.Path[len(result.Path)-1].Time
	}
	delay := flightTime + 0.3 // small buffer after animation
	if delay < 0.5 {
		delay = 0.5
	}

	m.resolveTurnAfter(ctx, delay, func(ctx context.Context) {
		if target != nil && player.IsAlive {
			player.Position = *target
			posPayload, _ := json.Marshal(map[string]interface{}{
				"playerId":   player.PlayerID,
				"position":   player.Position,
				"moveEnergy": player.MoveEnergy,
			})
			m.broadcast(ws.Message{Event: "PlayerMoved", Data: posPayload})
		}
		m.broadcastItemUsed(player.PlayerID, "teleport")

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
	case client.Send <- ws.Message{Event: "MatchStateSync", Data: m.snapshotPayload()}:
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

		rewards, err := ProcessMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats, usedItems, m.TeamRatings, m.EloParams)
		if err != nil {
			observability.Log.Error().Err(err).Msg("failed to process match rewards")
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
	if _, err := m.db.Pool.Exec(context.Background(), releaseReservationsSQL, m.State.MatchID); err != nil {
		observability.Log.Error().Err(err).Str("matchId", m.State.MatchID).Msg("failed to release item reservations")
	}

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
