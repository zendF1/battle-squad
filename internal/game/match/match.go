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
	}
}

func (m *Match) Run() {
	// Pattern 1: Panic Recovery per Match
	defer func() {
		if r := recover(); r != nil {
			observability.Log.Error().
				Str("matchId", m.State.MatchID).
				Interface("panic", r).
				Str("stack", string(debug.Stack())).
				Msg("match loop panic - ending as no-contest")
			m.endAsNoContest()
		}
	}()

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

	switch msg.Event {
	case "Move":
		var action MoveAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.processMove(ev.ctx, client, action)
		}
	case "Shoot":
		var action ShootAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.processShoot(ev.ctx, client, action)
		}
	case "UseItem":
		var action UseItemAction
		if err := json.Unmarshal(msg.Data, &action); err == nil {
			m.processUseItem(ev.ctx, client, action)
		}
	case "EndTurn":
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

	// Reset shot modifiers
	UpdatePlayerStatusEffects(player)
	TickPlayerStatusEffects(player)

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

	charConfig, exists := gamedata.Data.Characters[player.CharacterID]
	if !exists {
		return
	}

	weaponConfig, exists := gamedata.Data.Weapons[charConfig.WeaponID]
	if !exists {
		return
	}

	// 1. Simulate Projectile Trajectory
	result := SimulateProjectile(
		player.PlayerID,
		player.Position,
		action.Angle,
		action.Power,
		weaponConfig,
		m.State.Wind,
		m.Terrain,
		m.State.Players,
	)

	// Calculate base damage multiplication factor (e.g. from Power Shot item)
	damageFactor := 1.0
	if HasEffect(player, "power_shot") {
		damageFactor = 1.5
	}

	// 2. Resolve hit and damage
	var damagedPlayers []map[string]interface{}
	if result.ExplosionPoint != nil {
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
				Damage:   100, // mock match stats for exp calculation
				Kills:    0,
				Accuracy: 1.0,
				IsWinner: isWinner,
				IsDraw:   winningTeam == 0,
			}
		}

		rewards, err := ProcessMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats)
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

	// Random wind power and direction
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m.State.Wind.Power = r.Intn(5) // 0 to 4
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
}
