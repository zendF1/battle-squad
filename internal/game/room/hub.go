package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"golang.org/x/crypto/bcrypt"
)

type Hub struct {
	sync.RWMutex
	rooms         map[string]*Room
	redis         *database.RedisClient
	db            *database.PostgresDB
	nodeID        string
	lobbyNotifier func(lobbyID string, event string, data interface{}, battleRoom *Room)
}

func (h *Hub) SetLobbyNotifier(fn func(lobbyID string, event string, data interface{}, battleRoom *Room)) {
	h.lobbyNotifier = fn
}

func NewHub(redis *database.RedisClient, db *database.PostgresDB, nodeID string) *Hub {
	return &Hub{
		rooms:  make(map[string]*Room),
		redis:  redis,
		db:     db,
		nodeID: nodeID,
	}
}

func (h *Hub) CreateRoom(ctx context.Context, hostPlayerID, hostDisplayName string, payload CreateRoomPayload) (*Room, error) {
	h.Lock()
	defer h.Unlock()

	// Validate mode
	if payload.Mode != "pvp_1v1" && payload.Mode != "pvp_2v2" {
		return nil, errors.New("unsupported room mode")
	}

	maxPlayers := 2
	if payload.Mode == "pvp_2v2" {
		maxPlayers = 4
	}

	roomID := generateID()
	roomState := RoomState{
		RoomID:       roomID,
		HostPlayerID: hostPlayerID,
		Mode:         payload.Mode,
		MapID:        payload.MapID,
		MaxPlayers:   maxPlayers,
		Players: []RoomPlayer{
			{
				PlayerID:    hostPlayerID,
				DisplayName: hostDisplayName,
				TeamID:      1,
				CharacterID: "rookie", // rookie default
				Items:       []string{},
				IsReady:     false,
				IsHost:      true,
			},
		},
		Status: "waiting",
	}

	if payload.Password != nil && *payload.Password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*payload.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, errors.New("failed to hash room password")
		}
		roomState.IsLocked = true
		roomState.PasswordHash = string(hash)
	}

	room := NewRoom(roomID, roomState, h, h.db)
	h.rooms[roomID] = room

	observability.ActiveRooms.Inc()

	// Spawn room goroutine
	go room.Run()

	// Sync to Redis
	h.saveToRedis(ctx, &roomState)

	return room, nil
}

func (h *Hub) FindRoom(roomID string) (*Room, error) {
	h.RLock()
	defer h.RUnlock()

	room, exists := h.rooms[roomID]
	if !exists {
		return nil, errors.New("room not found on this node")
	}
	return room, nil
}

func (h *Hub) UnregisterRoom(ctx context.Context, roomID string) {
	h.Lock()
	delete(h.rooms, roomID)
	h.Unlock()

	observability.ActiveRooms.Dec()

	// Remove from Redis
	if h.redis != nil {
		h.redis.Client.HDel(ctx, "rooms:active", roomID)
	}
}

func (h *Hub) SyncRoomState(ctx context.Context, state *RoomState) {
	h.saveToRedis(ctx, state)
}

func (h *Hub) saveToRedis(ctx context.Context, state *RoomState) {
	if h.redis == nil {
		return
	}

	data, err := json.Marshal(map[string]interface{}{
		"roomId":       state.RoomID,
		"hostPlayerId": state.HostPlayerID,
		"mode":         state.Mode,
		"mapId":        state.MapID,
		"maxPlayers":   state.MaxPlayers,
		"playerCount":  len(state.Players),
		"isLocked":     state.IsLocked,
		"status":       state.Status,
		"nodeId":       h.nodeID,
		"updatedAt":    time.Now().Unix(),
	})
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to marshal room state for redis")
		return
	}

	h.redis.Client.HSet(ctx, "rooms:active", state.RoomID, data)
}

func (h *Hub) CreateBattleFromMatch(ctx context.Context, result matchmaker.MatchResult, botDiffConfig matchmaker.BotDifficultyConfig, eloConfig matchmaker.EloConfig) error {
	h.Lock()
	defer h.Unlock()

	roomID := generateID()
	matchID := generateID()

	// Build players from both entries
	var players []RoomPlayer
	lobbyMapping := make(map[string]string)

	// Helper to add players from a queue entry
	addEntry := func(entry matchmaker.QueueEntry, teamID int) {
		for _, pid := range entry.PlayerIDs {
			charID := "rookie"
			if c, ok := entry.PlayerChars[pid]; ok {
				charID = c
			}
			var items []string
			if itm, ok := entry.PlayerItems[pid]; ok {
				items = itm
			} else {
				items = []string{}
			}
			name := "Player_" + pid[:6]
			if n, ok := entry.PlayerNames[pid]; ok {
				name = n
			}
			players = append(players, RoomPlayer{
				PlayerID:    pid,
				DisplayName: name,
				TeamID:      teamID,
				CharacterID: charID,
				Items:       items,
				IsReady:     true,
				IsHost:      false,
			})
			lobbyMapping[pid] = entry.LobbyID
		}
	}

	addEntry(result.Entry1, 1)
	addEntry(result.Entry2, 2)

	// Fill bots for teams that need 2 players each
	teamCount := map[int]int{}
	for _, p := range players {
		teamCount[p.TeamID]++
	}
	for teamID := 1; teamID <= 2; teamID++ {
		for teamCount[teamID] < 2 {
			botID := "bot_" + generateID()[:8]
			botName := "Bot " + botID[len(botID)-4:]
			players = append(players, RoomPlayer{
				PlayerID:    botID,
				DisplayName: botName,
				TeamID:      teamID,
				CharacterID: "rookie",
				Items:       []string{},
				IsReady:     true,
				IsHost:      false,
			})
			teamCount[teamID]++
		}
	}

	// Set first player as host
	if len(players) > 0 {
		players[0].IsHost = true
	}

	roomState := RoomState{
		RoomID:       roomID,
		HostPlayerID: players[0].PlayerID,
		Mode:         "ranked_2v2",
		MapID:        result.MapID,
		MaxPlayers:   4,
		Players:      players,
		Status:       "in_match",
		LobbyMapping: lobbyMapping,
		HasBot:       result.HasBot,
	}

	room := NewRoom(roomID, roomState, h, h.db)
	h.rooms[roomID] = room

	observability.ActiveRooms.Inc()

	// [RANKED-DEBUG] Log all players
	for i, p := range players {
		observability.Log.Info().
			Str("roomId", roomID).
			Int("idx", i).
			Str("playerId", p.PlayerID).
			Str("displayName", p.DisplayName).
			Int("teamId", p.TeamID).
			Str("characterId", p.CharacterID).
			Bool("isReady", p.IsReady).
			Msg("[RANKED-DEBUG] player in room")
	}

	// [RANKED-DEBUG] Log lobby mapping
	for pid, lid := range lobbyMapping {
		observability.Log.Info().
			Str("roomId", roomID).
			Str("playerId", pid).
			Str("lobbyId", lid).
			Msg("[RANKED-DEBUG] lobby mapping")
	}

	observability.Log.Info().
		Str("roomId", roomID).
		Int("clientCount", len(room.Clients)).
		Msg("[RANKED-DEBUG] room.Clients BEFORE lobbyNotifier")

	// Register lobby clients in battle room BEFORE starting the match,
	// so they receive the MatchStarted broadcast.
	// NOTE: We pass the room directly to avoid deadlock — lobbyNotifier
	// used to call FindRoom() which needs RLock, but we already hold Lock.
	if h.lobbyNotifier != nil {
		observability.Log.Info().Str("roomId", roomID).Msg("[RANKED-DEBUG] calling lobbyNotifier")
		matchFoundData := map[string]string{"roomId": roomID, "matchId": matchID}
		notifiedLobbies := make(map[string]bool)
		for _, lid := range lobbyMapping {
			if !notifiedLobbies[lid] {
				observability.Log.Info().
					Str("roomId", roomID).
					Str("lobbyId", lid).
					Msg("[RANKED-DEBUG] notifying lobby")
				h.lobbyNotifier(lid, "MatchFound", matchFoundData, room)
				notifiedLobbies[lid] = true
			}
		}
	} else {
		observability.Log.Warn().Str("roomId", roomID).Msg("[RANKED-DEBUG] lobbyNotifier is NIL!")
	}

	observability.Log.Info().
		Str("roomId", roomID).
		Int("clientCount", len(room.Clients)).
		Msg("[RANKED-DEBUG] room.Clients AFTER lobbyNotifier")

	// [RANKED-DEBUG] Log registered client IDs
	for pid := range room.Clients {
		observability.Log.Info().
			Str("roomId", roomID).
			Str("clientPlayerId", pid).
			Msg("[RANKED-DEBUG] registered client")
	}

	// Determine bot tier from average rating
	avgRating := (result.Entry1.Rating + result.Entry2.Rating) / 2
	tierName := ratingToTier(avgRating)
	botTierCfg := botDiffConfig.Tiers[tierName]

	observability.Log.Info().
		Str("roomId", roomID).
		Str("matchId", matchID).
		Str("botTier", tierName).
		Int("avgRating", avgRating).
		Msg("[RANKED-DEBUG] about to call startRankedMatch")

	// Start ranked match (spawns match goroutine which broadcasts MatchStarted)
	room.startRankedMatch(matchID, botTierCfg, eloConfig, result)

	observability.Log.Info().
		Str("roomId", roomID).
		Msg("[RANKED-DEBUG] startRankedMatch returned, spawning room.Run()")

	// Spawn room goroutine AFTER match setup so it processes events
	// from an already-running match.
	go room.Run()

	// Sync to Redis
	h.saveToRedis(ctx, &roomState)

	observability.Log.Info().
		Str("roomId", roomID).
		Str("matchId", matchID).
		Str("botTier", tierName).
		Bool("hasBot", result.HasBot).
		Msg("[RANKED-DEBUG] CreateBattleFromMatch completed")

	return nil
}

func ratingToTier(rating int) string {
	switch {
	case rating < 1000:
		return "bronze"
	case rating < 1200:
		return "silver"
	case rating < 1500:
		return "gold"
	case rating < 1800:
		return "platinum"
	case rating < 2200:
		return "diamond"
	default:
		return "master"
	}
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}

