package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"golang.org/x/crypto/bcrypt"
)

type Hub struct {
	sync.RWMutex
	rooms        map[string]*Room
	redis        *database.RedisClient
	db           *database.PostgresDB
	nodeID       string
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

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}
