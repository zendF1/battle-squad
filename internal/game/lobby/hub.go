package lobby

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

// LobbyHub manages the in-memory registry of active lobby rooms, the players
// online on this node (presence.go) and the pending invites (invite.go).
type LobbyHub struct {
	sync.RWMutex
	lobbies map[string]*LobbyRoom
	db      *database.PostgresDB
	redis   *database.RedisClient
	nodeID  string
	// matchmaker queues the lobbies (set by NewWSHandler)
	matchmaker queuer
	// store reads the players' names, loadouts, ratings and guns (the DB)
	store memberStore

	onlineMu sync.RWMutex
	online   map[string]*ws.Client // playerId → newest connection

	// invMu guards invites; it is a leaf lock (nothing else is locked while it is held)
	invMu   sync.Mutex
	invites map[string]*invite
	// inviteTTL overrides the matchmaking config's inviteTimeoutSec (tests)
	inviteTTL time.Duration
}

// queuer is what the lobbies need of the matchmaker (*matchmaker.Matchmaker).
type queuer interface {
	EnqueueLobby(ctx context.Context, queue, lobbyID string, playerIDs []string, playerRatings map[string]int,
		playerChars map[string]string, playerItems map[string][]string, playerNames map[string]string) (*matchmaker.QueueEntry, error)
	CancelQueue(ctx context.Context, playerID string) (*matchmaker.QueueEntry, error)
	GetConfig() matchmaker.MatchmakingConfig
}

// NewLobbyHub constructs a LobbyHub.
func NewLobbyHub(db *database.PostgresDB, redis *database.RedisClient, nodeID string) *LobbyHub {
	return &LobbyHub{
		lobbies: make(map[string]*LobbyRoom),
		db:      db,
		redis:   redis,
		nodeID:  nodeID,
		store:   dbStore{db: db},
		online:  make(map[string]*ws.Client),
		invites: make(map[string]*invite),
	}
}

// CreateLobby creates a new lobby of queue hosted by host (ready) and starts its goroutine.
func (h *LobbyHub) CreateLobby(host LobbyMember, queue string) (*LobbyRoom, error) {
	h.Lock()
	defer h.Unlock()

	// Ensure the host does not already own a lobby.
	for _, l := range h.lobbies {
		l.mu.RLock()
		ownerID := l.State.HostPlayerID
		l.mu.RUnlock()
		if ownerID == host.PlayerID {
			return nil, errors.New("player already has an active lobby")
		}
	}

	lobbyID := generateID()
	host.Ready = true

	state := LobbyState{
		LobbyID:      lobbyID,
		HostPlayerID: host.PlayerID,
		Queue:        queue,
		Status:       "preparing",
		Members:      []LobbyMember{host},
	}

	room := NewLobbyRoom(lobbyID, state, h)
	h.lobbies[lobbyID] = room
	go room.Run()

	observability.Log.Info().
		Str("lobby_id", lobbyID).
		Str("host_player_id", host.PlayerID).
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

// Notify sends a matchmaking event to every client of the lobby. MatchFound
// moves the lobby to in_match and hands each client to join (the battle room);
// ReturnToLobby (match over) opens it again (preparing). Both broadcast
// LobbyUpdated.
func (h *LobbyHub) Notify(lobbyID, event string, data interface{}, join func(c *ws.Client)) error {
	l, err := h.FindLobby(lobbyID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// A lobby reused since its match (see reuseForHost) no longer waits for it
	if event == "ReturnToLobby" && l.State.Status != "in_match" {
		return nil
	}
	for _, c := range l.Clients {
		select {
		case c.Send <- ws.Message{Event: event, Data: payload}:
		default:
			observability.Log.Warn().Str("lobby_id", lobbyID).Str("player_id", c.PlayerID).Str("event", event).
				Msg("client send buffer full — dropping lobby notification")
		}
		if join != nil && c.RoomID == "" {
			join(c)
		}
	}
	switch event {
	case "MatchFound":
		l.State.Status = "in_match"
		l.State.QueueEntryID = ""
	case "ReturnToLobby":
		l.State.Status = "preparing"
	}
	l.broadcastLobbyUpdatedLocked()
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

// generateID produces a 32-character lowercase hex string (16 random bytes).
func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("lobby: failed to generate random ID: " + err.Error())
	}
	return hex.EncodeToString(b)
}
