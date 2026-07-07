package lobby

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

// LobbyHub manages the in-memory registry of active lobby rooms.
type LobbyHub struct {
	sync.RWMutex
	lobbies map[string]*LobbyRoom
	db      *database.PostgresDB
	redis   *database.RedisClient
	nodeID  string
}

// NewLobbyHub constructs a LobbyHub.
func NewLobbyHub(db *database.PostgresDB, redis *database.RedisClient, nodeID string) *LobbyHub {
	return &LobbyHub{
		lobbies: make(map[string]*LobbyRoom),
		db:      db,
		redis:   redis,
		nodeID:  nodeID,
	}
}

// CreateLobby creates a new lobby owned by hostPlayerID and starts its goroutine.
func (h *LobbyHub) CreateLobby(ctx context.Context, hostPlayerID, hostDisplayName string, rating int, tier string) (*LobbyRoom, error) {
	h.Lock()
	defer h.Unlock()

	// Ensure the host does not already own a lobby.
	for _, l := range h.lobbies {
		l.mu.RLock()
		ownerID := l.State.HostPlayerID
		l.mu.RUnlock()
		if ownerID == hostPlayerID {
			return nil, errors.New("player already has an active lobby")
		}
	}

	lobbyID := generateID()

	characterID, items := loadPlayerLoadout(ctx, h.db, hostPlayerID)

	state := LobbyState{
		LobbyID:      lobbyID,
		HostPlayerID: hostPlayerID,
		Status:       "preparing",
		Members: []LobbyMember{
			{
				PlayerID:    hostPlayerID,
				DisplayName: hostDisplayName,
				CharacterID: characterID,
				Items:       items,
				Rating:      rating,
				Tier:        tier,
			},
		},
	}

	room := NewLobbyRoom(lobbyID, state, h)
	h.lobbies[lobbyID] = room
	go room.Run()

	observability.Log.Info().
		Str("lobby_id", lobbyID).
		Str("host_player_id", hostPlayerID).
		Msg("lobby created")

	return room, nil
}

// FindLobby returns the LobbyRoom for lobbyID or an error if not found.
func (h *LobbyHub) FindLobby(lobbyID string) (*LobbyRoom, error) {
	h.RLock()
	defer h.RUnlock()
	l, ok := h.lobbies[lobbyID]
	if !ok {
		return nil, errors.New("lobby not found")
	}
	return l, nil
}

// FindLobbyByPlayer returns the LobbyRoom that playerID currently belongs to, or nil.
func (h *LobbyHub) FindLobbyByPlayer(playerID string) *LobbyRoom {
	h.RLock()
	defer h.RUnlock()
	for _, l := range h.lobbies {
		l.mu.RLock()
		for _, m := range l.State.Members {
			if m.PlayerID == playerID {
				l.mu.RUnlock()
				return l
			}
		}
		l.mu.RUnlock()
	}
	return nil
}

// UnregisterLobby removes the lobby from the registry.
func (h *LobbyHub) UnregisterLobby(lobbyID string) {
	h.Lock()
	defer h.Unlock()
	delete(h.lobbies, lobbyID)
	observability.Log.Info().Str("lobby_id", lobbyID).Msg("lobby unregistered")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// loadPlayerLoadout reads characterID and items from player_loadouts.
// Defaults to "rookie" / empty slice when no row is found.
func loadPlayerLoadout(ctx context.Context, db *database.PostgresDB, playerID string) (string, []string) {
	const q = `SELECT character_id, items FROM player_loadouts WHERE player_id = $1`

	var characterID string
	var itemsRaw []byte

	row := db.Pool.QueryRow(ctx, q, playerID)
	if err := row.Scan(&characterID, &itemsRaw); err != nil {
		return "rookie", []string{}
	}

	var items []string
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		items = []string{}
	}
	return characterID, items
}

// loadPlayerRating reads the player's rating and tier from the active season.
// Defaults to 1000 / "bronze" when no row is found.
func loadPlayerRating(ctx context.Context, db *database.PostgresDB, playerID string) (int, string) {
	const q = `
		SELECT pr.rating, pr.tier
		FROM player_ranks pr
		JOIN rank_seasons rs ON rs.season_id = pr.season_id
		WHERE pr.player_id = $1 AND rs.status = 'active'
		LIMIT 1`

	var rating int
	var tier string

	row := db.Pool.QueryRow(ctx, q, playerID)
	if err := row.Scan(&rating, &tier); err != nil {
		return 1000, "bronze"
	}
	return rating, tier
}

// generateID produces a 32-character lowercase hex string (16 random bytes).
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("lobby: failed to generate random ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
