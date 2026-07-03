package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"runtime/debug"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"math"
	"battle-squad/internal/shared/observability"
)

type matchEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
}

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
		mPlayers[p.PlayerID] = p
		turnOrder = append(turnOrder, p.PlayerID)
	}

	terrain := NewTerrain(1600, 900, mapID)

	// Land players safely on terrain initially
	for _, p := range mPlayers {
		p.Position.Y = terrain.GetLandingY(p.Position.X, 0)
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
	}
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
			if m.State.Status != "in_progress" {
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
	log := observability.FromContext(ev.ctx)
	client := ev.client
	msg := ev.msg

	log.Debug().Str("event", msg.Event).Str("matchId", m.State.MatchID).Msg("processing match event")

	// Resolve the acting player ID — bots send events with a nil client.
	actorID := m.State.CurrentPlayerID
	if client != nil {
		actorID = client.PlayerID
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

	player := m.State.Players[m.State.CurrentPlayerID]
	player.MoveEnergy = 100 // Reset move energy to full

	// Decrement skill cooldown at the start of each turn
	if player.SkillCooldown > 0 {
		player.SkillCooldown--
	}

	// Reset shot modifiers
	UpdatePlayerStatusEffects(player)
	TickPlayerStatusEffects(player)

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
	payload, _ := json.Marshal(map[string]interface{}{
		"turnIndex":       m.State.TurnIndex,
		"currentPlayerId": m.State.CurrentPlayerID,
		"wind":            m.State.Wind,
		"moveEnergy":      player.MoveEnergy,
	})
	m.broadcast(ws.Message{
		Event: "TurnStarted",
		Data:  payload,
	})

	// If it is a bot's turn, trigger bot decision block after a short delay
	if player.IsBot {
		go func() {
			time.Sleep(1500 * time.Millisecond) // wait for player to realize it's bot turn
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

func (m *Match) processMove(ctx context.Context, client *ws.Client, action MoveAction) {
	// Validate current player
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}

	player := m.State.Players[m.State.CurrentPlayerID]
	if !player.IsAlive {
		return
	}

	// Calculate move cost
	distance := math.Abs(action.TargetX - player.Position.X)
	energyCost := int(math.Round(distance * 0.5)) // 1 energy per 2 pixels

	if energyCost > player.MoveEnergy {
		// Limit movement distance to maximum possible by energy
		maxDist := float64(player.MoveEnergy * 2)
		if action.TargetX > player.Position.X {
			action.TargetX = player.Position.X + maxDist
		} else {
			action.TargetX = player.Position.X - maxDist
		}
		energyCost = player.MoveEnergy
	}

	player.MoveEnergy -= energyCost
	player.Position.X = action.TargetX

	// Landing check Y position on terrain
	player.Position.Y = m.Terrain.GetLandingY(player.Position.X, player.Position.Y)

	// Ice terrain: slide 50px in the movement direction
	iceTerrainType := m.Terrain.GetTerrainTypeAt(player.Position.X, player.Position.Y)
	if iceTerrainType == "ice" {
		slideDir := 50.0
		if action.TargetX < player.Position.X {
			slideDir = -50.0
		}
		player.Position.X += slideDir
		player.Position.Y = m.Terrain.GetLandingY(player.Position.X, player.Position.Y)
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

		// Reject if on cooldown
		if player.SkillCooldown > 0 {
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
			player.SkillCooldown = skillConfig.CooldownTurn

			payload, _ := json.Marshal(map[string]interface{}{
				"playerId": player.PlayerID,
				"skillId":  skillID,
				"hp":       player.HP,
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

		drillMode := HasEffect(player, "drill_bomb")

		var results []*ProjectileResult
		var damagedPlayers []map[string]interface{}

		for _, shot := range shots {
			r := SimulateProjectile(
				player.PlayerID,
				player.Position,
				shot.angle,
				action.Power,
				weaponConfig,
				m.State.Wind,
				m.Terrain,
				m.State.Players,
				drillMode,
			)
			r.SkillID = skillID
			r.ExplosionRadius = shot.radius

			// Track shot fired for each projectile in the skill
			player.ShotsFired++

			if r.ExplosionPoint != nil {
				m.Terrain.DestroyCircle(r.ExplosionPoint.X, r.ExplosionPoint.Y, r.ExplosionRadius)
				r.TerrainDestroyed = true

				for _, p := range m.State.Players {
					if !p.IsAlive {
						continue
					}
					damage := CalculateExplosionDamage(
						p.Position,
						*r.ExplosionPoint,
						float64(weaponConfig.Damage)*damageFactor,
						r.ExplosionRadius,
						p.Defense,
					)
					if damage > 0 {
						p.HP -= damage
						isKilled := false
						if p.HP <= 0 {
							p.HP = 0
							p.IsAlive = false
							isKilled = true
						}

						// Track stats for shooter (only damage to other players)
						if p.PlayerID != player.PlayerID {
							player.DamageDealt += damage
							player.ShotsHit++
							if isKilled {
								player.KillCount++
							}
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

		// Set cooldown after use
		player.SkillCooldown = skillConfig.CooldownTurn

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
			m.endTurn(ctx)
		}
		return
	}
	// ── End skill mode ───────────────────────────────────────────────────────

	// Determine projectile origin, angle, and power — may be overridden by air_strike
	shootOrigin := player.Position
	shootAngle := action.Angle
	shootPower := action.Power

	// air_strike: fire from top of map straight down at TargetX
	if HasEffect(player, "air_strike") {
		shootOrigin = Vector2{X: action.TargetX, Y: 0}
		shootAngle = 270.0  // straight down in Y-down coords (270° = negative Y direction conventionally, handled by sin)
		shootPower = 80.0   // fixed high power for airstrike
	}

	// drill_bomb: projectile passes through first terrain hit
	drillMode := HasEffect(player, "drill_bomb")

	// 1. Simulate Projectile Trajectory
	result := SimulateProjectile(
		player.PlayerID,
		shootOrigin,
		shootAngle,
		shootPower,
		weaponConfig,
		m.State.Wind,
		m.Terrain,
		m.State.Players,
		drillMode,
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
	if result.ExplosionPoint != nil {
		// Fragile terrain: double the explosion radius
		if m.Terrain.GetTerrainTypeAt(result.ExplosionPoint.X, result.ExplosionPoint.Y) == "fragile" {
			result.ExplosionRadius *= 2
		}

		// Destroy terrain
		m.Terrain.DestroyCircle(result.ExplosionPoint.X, result.ExplosionPoint.Y, result.ExplosionRadius)
		result.TerrainDestroyed = true

		// Check player splash damage
		for _, p := range m.State.Players {
			if !p.IsAlive {
				continue
			}

			damage := CalculateExplosionDamage(
				p.Position,
				*result.ExplosionPoint,
				float64(weaponConfig.Damage)*damageFactor,
				result.ExplosionRadius,
				p.Defense,
			)

			if damage > 0 {
				p.HP -= damage
				isKilled := false
				if p.HP <= 0 {
					p.HP = 0
					p.IsAlive = false
					isKilled = true
				}

				// Track stats for shooter (only damage to other players)
				if p.PlayerID != player.PlayerID {
					player.DamageDealt += damage
					player.ShotsHit++
					if isKilled {
						player.KillCount++
					}
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
			if landY > p.Position.Y {
				// Player fell down
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

	// spider_net: apply "net" effect (reduces MoveEnergy to 50) to all players hit by the explosion
	if HasEffect(player, "spider_net") && result.HitPlayerID != "" {
		if target, ok := m.State.Players[result.HitPlayerID]; ok && target.IsAlive {
			ApplyStatusEffect(target, StatusEffect{
				EffectID:       "net",
				TargetPlayerID: target.PlayerID,
				DurationTurn:   1,
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

	// 3. Complete shooting event, check win conditions, end turn
	m.checkWinCondition(ctx)
	if m.State.Status == "in_progress" {
		m.endTurn(ctx)
	}
}

func (m *Match) processUseItem(ctx context.Context, client *ws.Client, action UseItemAction) {
	if client != nil && client.PlayerID != m.State.CurrentPlayerID {
		return
	}

	log := observability.FromContext(ctx)
	err := ApplyImmediateItem(ctx, &m.State, m.State.CurrentPlayerID, action.ItemID, action.TargetPosition, m.Terrain)
	if err != nil {
		log.Warn().Err(err).Msg("failed to apply item")
		return
	}

	// Broadcast ItemUsed
	payload, _ := json.Marshal(map[string]interface{}{
		"playerId": m.State.CurrentPlayerID,
		"itemId":   action.ItemID,
		"players":  m.State.Players, // sync updated HP/Position values
		"wind":     m.State.Wind,
	})
	m.broadcast(ws.Message{
		Event: "ItemUsed",
		Data:  payload,
	})
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

	// Send full match state sync
	payload, _ := json.Marshal(m.State)
	client.Send <- ws.Message{
		Event: "MatchStateSync",
		Data:  payload,
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
				PlayerID: p.PlayerID,
				TeamID:   p.TeamID,
				Damage:   p.DamageDealt,
				Kills:    p.KillCount,
				Accuracy: calculateAccuracy(p.ShotsFired, p.ShotsHit),
				IsWinner: isWinner,
				IsDraw:   winningTeam == 0,
			}
		}

		// Collect per-player items for reservation consumption
		playerItems := make(map[string][]string)
		for _, p := range m.State.Players {
			if len(p.Items) > 0 {
				playerItems[p.PlayerID] = p.Items
			}
		}

		rewards, err := ProcessMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats, playerItems)
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
	windMin := 0
	windMax := 4
	if mapCfg, ok := gamedata.Data.Maps[m.State.MapID]; ok && len(mapCfg.DefaultWindPowerRange) == 2 {
		windMin = mapCfg.DefaultWindPowerRange[0]
		windMax = mapCfg.DefaultWindPowerRange[1]
	}
	windRange := windMax - windMin + 1
	if windRange < 1 {
		windRange = 1
	}

	// Random wind power and direction
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m.State.Wind.Power = windMin + r.Intn(windRange)
	if m.State.Wind.Power == 0 {
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
	ctx := context.Background()
	tx, err := m.db.Pool.Begin(ctx)
	if err != nil {
		observability.Log.Error().Err(err).Str("matchId", m.State.MatchID).Msg("failed to begin tx for reservation release")
	} else {
		releaseOk := true
		for _, p := range m.State.Players {
			for _, itemID := range p.Items {
				_, execErr := tx.Exec(ctx,
					`UPDATE inventory_reservations SET status = 'released', updated_at = CURRENT_TIMESTAMP
					 WHERE player_id = $1 AND match_id = $2 AND item_id = $3 AND status = 'reserved'`,
					p.PlayerID, m.State.MatchID, itemID,
				)
				if execErr != nil {
					observability.Log.Error().Err(execErr).
						Str("matchId", m.State.MatchID).
						Str("playerId", p.PlayerID).
						Str("itemId", itemID).
						Msg("failed to release item reservation")
					releaseOk = false
				}
			}
		}
		if releaseOk {
			if commitErr := tx.Commit(ctx); commitErr != nil {
				observability.Log.Error().Err(commitErr).Str("matchId", m.State.MatchID).Msg("failed to commit reservation release")
				tx.Rollback(ctx)
			}
		} else {
			tx.Rollback(ctx)
		}
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
	m.cancel()
}

func calculateAccuracy(fired, hit int) float64 {
	if fired == 0 {
		return 0
	}
	return float64(hit) / float64(fired)
}

func (m *Match) broadcast(msg ws.Message) {
	for _, client := range m.Clients {
		select {
		case client.Send <- msg:
		default:
		}
	}
}

func (m *Match) broadcastMatchStarted() {
	payload, _ := json.Marshal(m.State)
	m.broadcast(ws.Message{
		Event: "MatchStarted",
		Data:  payload,
	})
	m.el.Log("MatchStarted", "", m.State)
}
