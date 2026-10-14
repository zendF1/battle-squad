package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"golang.org/x/crypto/bcrypt"
)

type roomEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
}

type Room struct {
	ID        string
	State     RoomState
	Clients   map[string]*ws.Client
	Events    chan roomEvent
	hub       *Hub
	db        *database.PostgresDB
	ctx       context.Context
	cancel    context.CancelFunc
	match     *match.Match
	matchDone chan struct{}
}

type milestoneConfig struct {
	Bonus6  float64
	Bonus10 float64
	Bonus14 float64
	Bonus16 float64
}

func (r *Room) getMilestoneConfig() milestoneConfig {
	cfg := milestoneConfig{Bonus6: 0.10, Bonus10: 0.20, Bonus14: 0.40, Bonus16: 1.00}
	keys := map[string]*float64{
		"equipment.milestone_6_bonus":  &cfg.Bonus6,
		"equipment.milestone_10_bonus": &cfg.Bonus10,
		"equipment.milestone_14_bonus": &cfg.Bonus14,
		"equipment.milestone_16_bonus": &cfg.Bonus16,
	}
	for key, target := range keys {
		var val string
		err := r.db.Pool.QueryRow(context.Background(),
			`SELECT value FROM game_settings WHERE key = $1`, key).Scan(&val)
		if err == nil {
			if parsed, err := strconv.ParseFloat(val, 64); err == nil {
				*target = parsed
			}
		}
	}
	return cfg
}

func (r *Room) getActualStats(playerID, characterID string, baseHP, baseDefense int) (hp, defense, damageBonus, moveEnergyBonus int, critChance float64) {
	var bonusHP, bonusDefense int
	err := r.db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(bonus_hp, 0), COALESCE(bonus_defense, 0) FROM player_characters WHERE player_id = $1 AND character_id = $2`,
		playerID, characterID).Scan(&bonusHP, &bonusDefense)
	if err != nil {
		return baseHP, baseDefense, 0, 0, 0
	}

	var cfgVal string
	err = r.db.Pool.QueryRow(context.Background(),
		`SELECT value FROM game_settings WHERE key = 'character_progression'`).Scan(&cfgVal)
	if err != nil {
		return baseHP, baseDefense, 0, 0, 0
	}

	type progConfig struct {
		StatMultipliers map[string]int `json:"statMultipliers"`
	}
	var cfg progConfig
	if json.Unmarshal([]byte(cfgVal), &cfg) != nil || cfg.StatMultipliers == nil {
		return baseHP, baseDefense, 0, 0, 0
	}

	hpMul := cfg.StatMultipliers["hp"]
	defMul := cfg.StatMultipliers["defense"]
	if hpMul == 0 {
		hpMul = 50
	}
	if defMul == 0 {
		defMul = 5
	}

	var equipHP, equipDMG, equipDEF, equipMoveEnergy int
	var equipCrit float64

	ms := r.getMilestoneConfig()

	eqRows, eqErr := r.db.Pool.Query(context.Background(),
		`SELECT cei.stat_hp, cei.stat_damage, cei.stat_defense, cei.stat_crit, cei.stat_move_energy,
		        pe.upgrade_level, pe.category, pe.tier, pe.gem_slot_1::text, pe.gem_slot_2::text
		 FROM player_equipment pe
		 JOIN config_equipment_items cei ON pe.item_id = cei.item_id
		 WHERE pe.player_id = $1 AND pe.equipped_on = $2 AND pe.is_equipped = TRUE`,
		playerID, characterID)
	if eqErr == nil {
		defer eqRows.Close()
		craftedTierCount := make(map[string]int)
		for eqRows.Next() {
			var statHP, statDMG, statDEF, statMoveEnergy, upgradeLevel int
			var statCrit float64
			var category string
			var tier, gem1, gem2 *string
			if err := eqRows.Scan(&statHP, &statDMG, &statDEF, &statCrit, &statMoveEnergy,
				&upgradeLevel, &category, &tier, &gem1, &gem2); err != nil {
				continue
			}
			upgradeBonus := float64(upgradeLevel) * 0.02
			milestoneBonus := 0.0
			if upgradeLevel >= 6 {
				milestoneBonus += ms.Bonus6
			}
			if upgradeLevel >= 10 {
				milestoneBonus += ms.Bonus10
			}
			if upgradeLevel >= 14 {
				milestoneBonus += ms.Bonus14
			}
			if upgradeLevel >= 16 {
				milestoneBonus += ms.Bonus16
			}
			mul := 1.0 + upgradeBonus + milestoneBonus

			equipHP += int(math.Round(float64(statHP) * mul))
			equipDMG += int(math.Round(float64(statDMG) * mul))
			equipDEF += int(math.Round(float64(statDEF) * mul))
			equipCrit += statCrit * mul
			equipMoveEnergy += int(math.Round(float64(statMoveEnergy) * mul))

			if category == "crafted" && tier != nil {
				craftedTierCount[*tier]++
			}

			// BUG 2 fix: handle all 4 gem types (hp, damage, defense, critical)
			for _, gemID := range []*string{gem1, gem2} {
				if gemID == nil {
					continue
				}
				var gemType string
				var statVal float64
				gerr := r.db.Pool.QueryRow(context.Background(),
					`SELECT g.gem_type, cg.stat_value
					 FROM player_gems g JOIN config_gems cg ON g.gem_type = cg.gem_type AND g.gem_level = cg.gem_level
					 WHERE g.gem_id = $1`, *gemID).Scan(&gemType, &statVal)
				if gerr != nil {
					continue
				}
				switch gemType {
				case "hp":
					equipHP += int(statVal)
				case "damage":
					equipDMG += int(statVal)
				case "defense":
					equipDEF += int(statVal)
				case "critical":
					equipCrit += statVal
				}
			}
		}

		// BUG 4+5 fix: additive set bonus (sum all % first, apply once) + include bonus_dmg_pct
		for t, count := range craftedTierCount {
			var totalHPPct, totalDMGPct, totalDEFPct, totalCritPct float64
			bonusRows, berr := r.db.Pool.Query(context.Background(),
				`SELECT pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
				 FROM config_set_bonuses WHERE tier = $1 ORDER BY pieces_required`, t)
			if berr != nil {
				continue
			}
			for bonusRows.Next() {
				var pieces int
				var hpPct, dmgPct, defPct, critPct float64
				if err := bonusRows.Scan(&pieces, &hpPct, &dmgPct, &defPct, &critPct); err != nil {
					continue
				}
				if count >= pieces {
					totalHPPct += hpPct
					totalDMGPct += dmgPct
					totalDEFPct += defPct
					totalCritPct += critPct
				}
			}
			bonusRows.Close()
			// Apply additive bonus once
			if totalHPPct > 0 {
				equipHP = int(math.Round(float64(equipHP) * (1 + totalHPPct/100)))
			}
			if totalDMGPct > 0 {
				equipDMG = int(math.Round(float64(equipDMG) * (1 + totalDMGPct/100)))
			}
			if totalDEFPct > 0 {
				equipDEF = int(math.Round(float64(equipDEF) * (1 + totalDEFPct/100)))
			}
			equipCrit += totalCritPct
		}
	}

	return baseHP + bonusHP*hpMul + equipHP,
		baseDefense + bonusDefense*defMul + equipDEF,
		equipDMG,
		equipMoveEnergy,
		equipCrit
}

func NewRoom(roomID string, initialState RoomState, hub *Hub, db *database.PostgresDB) *Room {
	ctx, cancel := context.WithCancel(context.Background())
	return &Room{
		ID:      roomID,
		State:   initialState,
		Clients: make(map[string]*ws.Client),
		Events:  make(chan roomEvent, 128),
		hub:     hub,
		db:      db,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (r *Room) Run() {
	defer func() {
		if err := recover(); err != nil {
			observability.Log.Error().Interface("panic", err).Str("roomId", r.ID).Msg("room goroutine panic recovery")
		}
		r.hub.UnregisterRoom(context.Background(), r.ID)
	}()

	observability.Log.Info().
		Str("roomId", r.ID).
		Str("status", r.State.Status).
		Int("clientCount", len(r.Clients)).
		Bool("hasMatch", r.match != nil).
		Msg("[RANKED-DEBUG] Room.Run() goroutine started")

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		// matchDone is nil when no match is active, which makes the select case a no-op
		var matchDoneCh <-chan struct{}
		if r.matchDone != nil {
			matchDoneCh = r.matchDone
		}

		select {
		case ev := <-r.Events:
			r.handleEvent(ev)
		case <-matchDoneCh:
			// Match ended — notify players to return to their lobbies
			if r.State.LobbyMapping != nil {
				for playerID, lobbyID := range r.State.LobbyMapping {
					if client, ok := r.Clients[playerID]; ok {
						payload, _ := json.Marshal(map[string]string{"lobbyId": lobbyID})
						select {
						case client.Send <- ws.Message{Event: "ReturnToLobby", Data: payload}:
						default:
						}
					}
				}
			}
			observability.Log.Info().Str("roomId", r.ID).Msg("match ended, destroying room")
			return
		case <-ticker.C:
			// Destroy idle rooms if empty after 30 minutes
			if len(r.Clients) == 0 {
				observability.Log.Info().Str("roomId", r.ID).Msg("destroying empty idle room")
				return
			}
		case <-r.ctx.Done():
			return
		}
	}
}

func (r *Room) Join(client *ws.Client, password *string) error {
	// Simple validation
	if r.State.Status != "waiting" {
		return errors.New("match already in progress")
	}

	if len(r.State.Players) >= r.State.MaxPlayers {
		return errors.New("room is full")
	}

	if r.State.IsLocked {
		if password == nil {
			return errors.New("incorrect password")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(r.State.PasswordHash), []byte(*password)); err != nil {
			return errors.New("incorrect password")
		}
	}

	// Queue internal join event (uses internal name to prevent bypass via WebSocket)
	r.Events <- roomEvent{
		client: client,
		msg:    ws.Message{Event: "__internal_join"},
		ctx:    context.Background(),
	}
	return nil
}

func (r *Room) Leave(client *ws.Client) {
	r.Events <- roomEvent{
		client: client,
		msg:    ws.Message{Event: "__internal_leave"},
		ctx:    context.Background(),
	}
}

func (r *Room) ProcessEvent(ctx context.Context, client *ws.Client, msg ws.Message) {
	r.Events <- roomEvent{
		client: client,
		msg:    msg,
		ctx:    ctx,
	}
}

func (r *Room) handleEvent(ev roomEvent) {
	log := observability.FromContext(ev.ctx)
	client := ev.client
	msg := ev.msg

	log.Debug().Str("event", msg.Event).Str("roomId", r.ID).Msg("processing room event")

	if msg.Event == "__register_client" {
		r.processRegisterClient(client)
		return
	}

	if r.State.Status == "in_match" {
		if msg.Event == "Leave" || msg.Event == "__internal_leave" {
			r.processLeave(client)
			return
		}
		if r.match != nil {
			r.match.ProcessEvent(ev.ctx, client, msg)
		}
		return
	}

	switch msg.Event {
	case "__internal_join":
		r.processJoin(client)
	case "__internal_leave", "Leave":
		r.processLeave(client)
	case "ChangeTeam":
		var payload ChangeTeamPayload
		if err := json.Unmarshal(msg.Data, &payload); err == nil {
			r.processChangeTeam(client, payload.TeamID)
		}
	case "SelectCharacter":
		var payload SelectCharacterPayload
		if err := json.Unmarshal(msg.Data, &payload); err == nil {
			r.processSelectCharacter(client, payload.CharacterID)
		}
	case "SelectItems":
		var payload SelectItemsPayload
		if err := json.Unmarshal(msg.Data, &payload); err == nil {
			r.processSelectItems(client, payload.Items)
		}
	case "Ready":
		r.processReady(client)
	case "StartMatch":
		r.processStartMatch(client)
	case "__start_tutorial":
		r.processStartTutorial(client)
	}
}

// RegisterClient adds a WS client to the room without going through the normal
// join flow. Used by ranked matchmaking to transfer lobby clients into the
// battle room before the match goroutine starts.
func (r *Room) RegisterClient(client *ws.Client) {
	r.Clients[client.PlayerID] = client
	client.RoomID = r.ID
	observability.Log.Info().
		Str("roomId", r.ID).
		Str("playerId", client.PlayerID).
		Msg("registered ranked client in battle room")
}

func (r *Room) processRegisterClient(client *ws.Client) {
	r.Clients[client.PlayerID] = client
	client.RoomID = r.ID
	observability.Log.Info().
		Str("roomId", r.ID).
		Str("playerId", client.PlayerID).
		Msg("registered ranked client in battle room")
}

func (r *Room) processJoin(client *ws.Client) {
	// Add client
	r.Clients[client.PlayerID] = client
	client.RoomID = r.ID

	// Check if player already exists in state (e.g., host added at room creation)
	for _, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			r.broadcastRoomUpdated()
			return
		}
	}

	// Query player display name
	var displayName string
	query := "SELECT display_name FROM player_profiles WHERE player_id = $1"
	err := r.db.Pool.QueryRow(context.Background(), query, client.PlayerID).Scan(&displayName)
	if err != nil {
		displayName = "Player_" + client.PlayerID[:6]
	}

	// Default assignment: Team A (1) if it has space, otherwise Team B (2)
	teamID := 1
	teamACount := 0
	for _, p := range r.State.Players {
		if p.TeamID == 1 {
			teamACount++
		}
	}
	if teamACount >= r.State.MaxPlayers/2 {
		teamID = 2
	}

	player := RoomPlayer{
		PlayerID:    client.PlayerID,
		DisplayName: displayName,
		TeamID:      teamID,
		CharacterID: "rookie",
		Items:       []string{},
		IsReady:     false,
		IsHost:      false,
	}

	r.State.Players = append(r.State.Players, player)
	r.broadcastRoomUpdated()
}

func (r *Room) processLeave(client *ws.Client) {
	if r.match != nil {
		// Match goroutine owns the Clients map during in_match — let it
		// handle the delete via its own event loop to avoid a concurrent
		// map write between the room and match goroutines.
		r.match.ProcessEvent(context.Background(), client, ws.Message{Event: "Leave"})
		client.RoomID = ""

		// Tutorial room: destroy as soon as the user leaves (only bots remain)
		if r.State.IsTutorial {
			observability.Log.Info().Str("roomId", r.ID).Msg("tutorial room user left, destroying room")
			r.match.Stop()
			r.cancel()
		}
		return
	}

	delete(r.Clients, client.PlayerID)
	client.RoomID = ""

	// Tutorial room: destroy as soon as the user leaves (only bots remain)
	if r.State.IsTutorial {
		observability.Log.Info().Str("roomId", r.ID).Msg("tutorial room user left, destroying room")
		r.cancel()
		return
	}

	// Remove player from state
	foundIdx := -1
	for idx, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			foundIdx = idx
			break
		}
	}

	if foundIdx != -1 {
		wasHost := r.State.Players[foundIdx].IsHost
		r.State.Players = append(r.State.Players[:foundIdx], r.State.Players[foundIdx+1:]...)

		// If room is empty, destroy it
		if len(r.State.Players) == 0 {
			r.cancel()
			return
		}

		// Reassign host if host left
		if wasHost {
			r.State.Players[0].IsHost = true
			r.State.HostPlayerID = r.State.Players[0].PlayerID
		}
	}

	r.broadcastRoomUpdated()
}

func (r *Room) processChangeTeam(client *ws.Client, teamID int) {
	if teamID != 1 && teamID != 2 {
		return
	}

	// Check if target team has space
	teamCount := 0
	for _, p := range r.State.Players {
		if p.TeamID == teamID {
			teamCount++
		}
	}

	if teamCount >= r.State.MaxPlayers/2 {
		// Team full, reject change
		return
	}

	for idx, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			r.State.Players[idx].TeamID = teamID
			break
		}
	}

	r.broadcastRoomUpdated()
}

func (r *Room) processSelectCharacter(client *ws.Client, characterID string) {
	// Verify character is rookie, tanko, spark, flora
	if characterID != "rookie" && characterID != "tanko" && characterID != "spark" && characterID != "flora" {
		return
	}

	// Rookie is the default character — always available
	if characterID != "rookie" {
		// Verify player has unlocked character
		var exists bool
		query := "SELECT EXISTS(SELECT 1 FROM player_characters WHERE player_id = $1 AND character_id = $2)"
		err := r.db.Pool.QueryRow(context.Background(), query, client.PlayerID, characterID).Scan(&exists)
		if err != nil || !exists {
			return // Not unlocked
		}
	}

	for idx, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			r.State.Players[idx].CharacterID = characterID
			break
		}
	}

	r.broadcastRoomUpdated()
}

func (r *Room) processSelectItems(client *ws.Client, items []string) {
	if len(items) > 3 {
		return // max 3 items
	}

	// If items are selected, validate ownership against inventory_items
	if len(items) > 0 {
		query := `
			SELECT item_id FROM inventory_items
			WHERE player_id = $1 AND item_id = ANY($2) AND quantity > 0 AND (expires_at IS NULL OR expires_at > NOW())
		`
		rows, err := r.db.Pool.Query(context.Background(), query, client.PlayerID, items)
		if err != nil {
			observability.Log.Error().Err(err).Str("playerId", client.PlayerID).Msg("failed to query inventory for item validation")
			r.sendError(client, "Failed to validate items")
			return
		}
		defer rows.Close()

		ownedSet := make(map[string]bool)
		for rows.Next() {
			var itemID string
			if err := rows.Scan(&itemID); err != nil {
				continue
			}
			ownedSet[itemID] = true
		}
		rows.Close()

		for _, itemID := range items {
			if !ownedSet[itemID] {
				r.sendError(client, fmt.Sprintf("You do not own item: %s", itemID))
				return
			}
		}
	}

	for idx, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			r.State.Players[idx].Items = items
			break
		}
	}

	r.broadcastRoomUpdated()}

func (r *Room) processReady(client *ws.Client) {
	for idx, p := range r.State.Players {
		if p.PlayerID == client.PlayerID {
			// Host cannot toggle ready directly, host is implicitly ready or verified separately
			if p.IsHost {
				return
			}
			r.State.Players[idx].IsReady = !r.State.Players[idx].IsReady
			break
		}
	}

	r.broadcastRoomUpdated()
}

func (r *Room) processStartMatch(client *ws.Client) {
	// Verify host is requesting start
	if client.PlayerID != r.State.HostPlayerID {
		return
	}

	// Verify room is full
	if len(r.State.Players) < r.State.MaxPlayers {
		return
	}

	// Verify all guests are ready
	for _, p := range r.State.Players {
		if !p.IsHost && !p.IsReady {
			return
		}
	}

	// Re-verify item ownership and reserve items for all players in a transaction
	matchID := generateID()
	if err := r.reservePlayerItems(matchID); err != nil {
		observability.Log.Error().Err(err).Str("roomId", r.ID).Msg("failed to reserve player items before match start")
		r.sendError(client, "Failed to start match: "+err.Error())
		return
	}

	// Transition status to in_match
	r.State.Status = "in_match"
	r.broadcastRoomUpdated()

	// Launch Match engine
	observability.Log.Info().Str("roomId", r.ID).Msg("trigger start match - launch match engine")

	// Build team spawn point queues from map config
	team1Spawns := []gamedata.SpawnPoint{}
	team2Spawns := []gamedata.SpawnPoint{}
	if mapCfg, ok := gamedata.Data.Maps[r.State.MapID]; ok {
		mid := len(mapCfg.SpawnPoints) / 2
		for i, sp := range mapCfg.SpawnPoints {
			if i < mid {
				team1Spawns = append(team1Spawns, sp)
			} else {
				team2Spawns = append(team2Spawns, sp)
			}
		}
	}
	// Fallback defaults if map not found or no spawn points
	if len(team1Spawns) == 0 {
		team1Spawns = append(team1Spawns, gamedata.SpawnPoint{X: 200, Y: 0})
	}
	if len(team2Spawns) == 0 {
		team2Spawns = append(team2Spawns, gamedata.SpawnPoint{X: 1400, Y: 0})
	}
	team1Idx := 0
	team2Idx := 0

	matchPlayers := []*match.BattlePlayerState{}
	for _, p := range r.State.Players {
		// Assign spawn position from map config spawn points per team
		var spawnPos match.Vector2
		if p.TeamID == 2 {
			sp := team2Spawns[team2Idx%len(team2Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team2Idx++
		} else {
			sp := team1Spawns[team1Idx%len(team1Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team1Idx++
		}

		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp := 100
		defense := 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}
		hp, defense, dmgBonus, moveEnergyBonus, critChance := r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)

		matchPlayers = append(matchPlayers, &match.BattlePlayerState{
			PlayerID:        p.PlayerID,
			DisplayName:     p.DisplayName,
			TeamID:          p.TeamID,
			CharacterID:     p.CharacterID,
			HP:              hp,
			MaxHP:           hp,
			Defense:         defense,
			DamageBonus:     dmgBonus,
			Position:        spawnPos,
			MoveEnergy:      100,
			MoveEnergyBonus: moveEnergyBonus,
			CritChance:      critChance,
			Items:           p.Items,
			StatusEffects:   []match.StatusEffect{},
			IsAlive:         true,
			IsBot:           false,
		})
	}

	// Instantiate match engine
	r.match = match.NewMatch(
		matchID,
		r.ID,
		r.State.Mode,
		r.State.MapID,
		matchPlayers,
		r.Clients,
		r.db,
		r.hub.redis,
		economy.NewRepository(),
		r.hub,
	)

	// Track match completion
	r.matchDone = r.match.MatchDone

	// Run match loop
	go r.match.Run()

	// Notify room hub of status change
	r.hub.SyncRoomState(context.Background(), &r.State)
}

func (r *Room) processStartTutorial(client *ws.Client) {
	r.State.IsTutorial = true
	matchID := generateID()

	// Add idle bot to room state
	r.State.Players = append(r.State.Players, RoomPlayer{
		PlayerID:    "bot_" + matchID[:8],
		DisplayName: "Target Bot",
		TeamID:      2,
		CharacterID: "rookie",
		Items:       []string{},
		IsReady:     true,
		IsHost:      false,
	})

	r.State.Status = "in_match"
	r.broadcastRoomUpdated()

	observability.Log.Info().Str("roomId", r.ID).Msg("starting tutorial match with idle bot")

	// Build players for match
	matchPlayers := []*match.BattlePlayerState{}
	for _, p := range r.State.Players {
		var spawnPos match.Vector2
		if p.TeamID == 2 {
			spawnPos = match.Vector2{X: 1200, Y: 0}
		} else {
			spawnPos = match.Vector2{X: 400, Y: 0}
		}

		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp := 100
		defense := 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}

		isBot := p.PlayerID[:4] == "bot_"

		matchPlayers = append(matchPlayers, &match.BattlePlayerState{
			PlayerID:      p.PlayerID,
			DisplayName:   p.DisplayName,
			TeamID:        p.TeamID,
			CharacterID:   p.CharacterID,
			HP:            hp,
			MaxHP:         hp,
			Defense:       defense,
			Position:      spawnPos,
			MoveEnergy:    100,
			Items:         p.Items,
			StatusEffects: []match.StatusEffect{},
			IsAlive:       true,
			IsBot:         isBot,
		})
	}

	r.match = match.NewMatch(
		matchID,
		r.ID,
		r.State.Mode,
		r.State.MapID,
		matchPlayers,
		r.Clients,
		r.db,
		r.hub.redis,
		economy.NewRepository(),
		r.hub,
	)

	// Track match completion
	r.matchDone = r.match.MatchDone

	go r.match.Run()
	r.hub.SyncRoomState(context.Background(), &r.State)
}

func (r *Room) startRankedMatch(matchID string, botTierConfig matchmaker.BotTierConfig, eloConfig matchmaker.EloConfig, matchResult matchmaker.MatchResult) {
	observability.Log.Info().
		Str("roomId", r.ID).
		Str("matchId", matchID).
		Int("clientCount", len(r.Clients)).
		Int("playerCount", len(r.State.Players)).
		Msg("[RANKED-DEBUG] startRankedMatch called")
	// Build team spawn point queues from map config
	team1Spawns := []gamedata.SpawnPoint{}
	team2Spawns := []gamedata.SpawnPoint{}
	if mapCfg, ok := gamedata.Data.Maps[r.State.MapID]; ok {
		mid := len(mapCfg.SpawnPoints) / 2
		for i, sp := range mapCfg.SpawnPoints {
			if i < mid {
				team1Spawns = append(team1Spawns, sp)
			} else {
				team2Spawns = append(team2Spawns, sp)
			}
		}
	}
	if len(team1Spawns) == 0 {
		team1Spawns = append(team1Spawns, gamedata.SpawnPoint{X: 200, Y: 0})
	}
	if len(team2Spawns) == 0 {
		team2Spawns = append(team2Spawns, gamedata.SpawnPoint{X: 1400, Y: 0})
	}
	team1Idx := 0
	team2Idx := 0

	// Build match players
	matchPlayers := []*match.BattlePlayerState{}
	for _, p := range r.State.Players {
		var spawnPos match.Vector2
		if p.TeamID == 2 {
			sp := team2Spawns[team2Idx%len(team2Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team2Idx++
		} else {
			sp := team1Spawns[team1Idx%len(team1Spawns)]
			spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
			team1Idx++
		}

		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp := 100
		defense := 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}

		isBot := len(p.PlayerID) >= 4 && p.PlayerID[:4] == "bot_"
		var dmgBonus, moveEnergyBonus int
		var critChance float64
		if !isBot {
			hp, defense, dmgBonus, moveEnergyBonus, critChance = r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
		}

		matchPlayers = append(matchPlayers, &match.BattlePlayerState{
			PlayerID:        p.PlayerID,
			DisplayName:     p.DisplayName,
			TeamID:          p.TeamID,
			CharacterID:     p.CharacterID,
			HP:              hp,
			MaxHP:           hp,
			Defense:         defense,
			DamageBonus:     dmgBonus,
			Position:        spawnPos,
			MoveEnergy:      100,
			MoveEnergyBonus: moveEnergyBonus,
			CritChance:      critChance,
			Items:           p.Items,
			StatusEffects:   []match.StatusEffect{},
			IsAlive:         true,
			IsBot:           isBot,
		})
	}

	// Collect per-team ratings for Elo calculation
	team1Ratings := []int{}
	team2Ratings := []int{}
	for _, pid := range matchResult.Entry1.PlayerIDs {
		if r, ok := matchResult.Entry1.PlayerRatings[pid]; ok {
			team1Ratings = append(team1Ratings, r)
		}
	}
	for _, pid := range matchResult.Entry2.PlayerIDs {
		if r, ok := matchResult.Entry2.PlayerRatings[pid]; ok {
			team2Ratings = append(team2Ratings, r)
		}
	}

	avgTeam1 := match.TeamAvgRating(team1Ratings)
	avgTeam2 := match.TeamAvgRating(team2Ratings)

	// Build Elo params
	botModifier := 1.0
	if matchResult.HasBot {
		// Load bot rating modifier from matchmaking config
		mmCfg := matchmaker.LoadMatchmakingConfig(context.Background(), r.db)
		botModifier = mmCfg.BotRatingModifier
	}

	eloParams := match.EloParams{
		KFactor:     eloConfig.KFactor,
		RatingFloor: eloConfig.RatingFloor,
		BotModifier: botModifier,
		HasBot:      matchResult.HasBot,
	}

	// [RANKED-DEBUG] Log match players
	for _, mp := range matchPlayers {
		observability.Log.Info().
			Str("roomId", r.ID).
			Str("matchId", matchID).
			Str("playerId", mp.PlayerID).
			Str("displayName", mp.DisplayName).
			Int("teamId", mp.TeamID).
			Bool("isBot", mp.IsBot).
			Int("hp", mp.HP).
			Msg("[RANKED-DEBUG] match player")
	}

	observability.Log.Info().
		Str("roomId", r.ID).
		Int("clientCount", len(r.Clients)).
		Msg("[RANKED-DEBUG] r.Clients before NewMatch")

	for pid := range r.Clients {
		observability.Log.Info().
			Str("roomId", r.ID).
			Str("clientPlayerId", pid).
			Msg("[RANKED-DEBUG] client in r.Clients")
	}

	// Create match
	r.match = match.NewMatch(
		matchID,
		r.ID,
		r.State.Mode,
		r.State.MapID,
		matchPlayers,
		r.Clients,
		r.db,
		r.hub.redis,
		economy.NewRepository(),
		r.hub,
	)

	// Set Elo data on match
	r.match.TeamRatings = map[int]int{
		1: avgTeam1,
		2: avgTeam2,
	}
	r.match.EloParams = eloParams

	// Track match completion
	r.matchDone = r.match.MatchDone

	// Run match loop
	go r.match.Run()

	// Sync room state
	r.hub.SyncRoomState(context.Background(), &r.State)

	observability.Log.Info().
		Str("roomId", r.ID).
		Str("matchId", matchID).
		Int("team1Rating", avgTeam1).
		Int("team2Rating", avgTeam2).
		Msg("ranked match started")
}

func (r *Room) sendError(client *ws.Client, message string) {
	payload, _ := json.Marshal(map[string]string{"error": message})
	select {
	case client.Send <- ws.Message{Event: "Error", Data: payload}:
	default:
	}
}

// reservePlayerItems verifies all players own their selected items and creates reservations
// atomically. If any item is not owned or reservation fails, the transaction is rolled back.
func (r *Room) reservePlayerItems(matchID string) error {
	ctx := context.Background()
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, p := range r.State.Players {
		if len(p.Items) == 0 {
			continue
		}

		// Re-verify ownership
		ownershipQuery := `
			SELECT item_id FROM inventory_items
			WHERE player_id = $1 AND item_id = ANY($2) AND quantity > 0 AND (expires_at IS NULL OR expires_at > NOW())
		`
		rows, err := tx.Query(ctx, ownershipQuery, p.PlayerID, p.Items)
		if err != nil {
			return fmt.Errorf("failed to verify items for player %s: %w", p.PlayerID, err)
		}
		ownedSet := make(map[string]bool)
		for rows.Next() {
			var itemID string
			if scanErr := rows.Scan(&itemID); scanErr == nil {
				ownedSet[itemID] = true
			}
		}
		rows.Close()

		for _, itemID := range p.Items {
			if !ownedSet[itemID] {
				return fmt.Errorf("player %s does not own item %s", p.PlayerID, itemID)
			}
		}

		// Reserve each item (quantity 1 per selection)
		for _, itemID := range p.Items {
			reservationID := generateID()
			reserveQuery := `
				INSERT INTO inventory_reservations (reservation_id, player_id, match_id, item_id, quantity, status)
				VALUES ($1, $2, $3, $4, 1, 'reserved')
			`
			_, err := tx.Exec(ctx, reserveQuery, reservationID, p.PlayerID, matchID, itemID)
			if err != nil {
				return fmt.Errorf("failed to reserve item %s for player %s: %w", itemID, p.PlayerID, err)
			}
		}
	}

	return tx.Commit(ctx)
}

func (r *Room) broadcastRoomUpdated() {
	payload, err := json.Marshal(r.State)
	if err != nil {
		return
	}

	msg := ws.Message{
		Event: "RoomUpdated",
		Data:  payload,
	}

	for _, client := range r.Clients {
		select {
		case client.Send <- msg:
		default:
		}
	}

	// Sync to Redis registry
	r.hub.SyncRoomState(context.Background(), &r.State)
}
