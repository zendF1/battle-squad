package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Hub struct {
	sync.RWMutex
	rooms         map[string]*Room
	redis         *database.RedisClient
	db            *database.PostgresDB
	nodeID        string
	lobbyNotifier func(lobbyID string, event string, data interface{}, battleRoom *Room)
	// devUnlockAll lets QuickPlay use any character and item without owning or
	// consuming them (config DEV_UNLOCK_ALL, never in production).
	devUnlockAll bool
}

// SetDevUnlockAll toggles the QuickPlay dev unlock; call before serving clients.
func (h *Hub) SetDevUnlockAll(enabled bool) {
	h.devUnlockAll = enabled
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

// CreateBattleFromMatch creates the battle room of a matchmade ranked 2v2 game:
// bots fill the free slots, every human's items are reserved, every human's
// lobby gets MatchFound (and its clients join the room), and the match starts
// mmConfig.MatchStartDelay seconds later with MatchStarted.
func (h *Hub) CreateBattleFromMatch(ctx context.Context, result matchmaker.MatchResult, botDiffConfig matchmaker.BotDifficultyConfig, eloConfig matchmaker.EloConfig, mmConfig matchmaker.MatchmakingConfig) error {
	roomID := generateID()
	matchID := generateID()

	humanIDs := make([]string, 0, 4)
	for pid := range result.HumanRatings() {
		humanIDs = append(humanIDs, pid)
	}
	if len(humanIDs) == 0 {
		return errors.New("ranked match without players")
	}
	var pool *pgxpool.Pool
	if h.db != nil {
		pool = h.db.Pool
	}
	var bots *botOutfitter
	if result.Teams[0].Bots+result.Teams[1].Bots > 0 {
		bots = newBotOutfitter(gamedata.Current(), loadBotWeaponsOrNone(ctx, querier(pool)), nil)
	}
	roster := buildRankedRoster(result, loadRankedProfiles(ctx, pool, humanIDs), func() string {
		return "bot_" + generateID()[:8]
	}, bots)
	roster.Found.RoomID = roomID
	roster.Found.MatchID = matchID

	roomState := RoomState{
		RoomID:       roomID,
		HostPlayerID: roster.Players[0].PlayerID,
		Mode:         RankedMode,
		MapID:        result.MapID,
		MaxPlayers:   4,
		Players:      roster.Players,
		Status:       "in_match",
		LobbyMapping: roster.LobbyMapping,
		HasBot:       result.HasBot,
	}

	room := NewRoom(roomID, roomState, h, h.db)
	room.bots = roster.Bots
	room.reserveRankedItems(matchID)

	h.Lock()
	h.rooms[roomID] = room
	h.Unlock()
	observability.ActiveRooms.Inc()

	// MatchFound to every human's lobby; the notifier registers the lobby
	// clients in the room before the match goroutine starts
	if h.lobbyNotifier != nil {
		notified := make(map[string]bool)
		for _, lid := range roster.LobbyMapping {
			if !notified[lid] {
				h.lobbyNotifier(lid, "MatchFound", roster.Found, room)
				notified[lid] = true
			}
		}
	}

	botModifier := 1.0
	if result.HasBot {
		botModifier = mmConfig.BotRatingModifier
	}
	eloParams := match.EloParams{
		KFactor:       eloConfig.KFactor,
		RatingFloor:   eloConfig.RatingFloor,
		DefaultRating: eloConfig.DefaultRating,
		BotModifier:   botModifier,
		HasBot:        result.HasBot,
	}

	// Bot difficulty from the humans' average rating
	tierName := ranked.Tier(roster.BotRating)
	room.startRankedMatch(matchID, botDiffConfig.Tiers[tierName], eloParams, roster.TeamRatings,
		time.Duration(mmConfig.MatchStartDelay)*time.Second)

	// Room goroutine after the match setup, so it forwards events to a running match
	go room.Run()
	h.saveToRedis(ctx, &roomState)

	observability.Log.Info().
		Str("roomId", roomID).
		Str("matchId", matchID).
		Str("botTier", tierName).
		Bool("hasBot", result.HasBot).
		Msg("ranked battle created")
	return nil
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}
