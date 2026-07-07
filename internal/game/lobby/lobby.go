package lobby

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/observability"
)

const (
	lobbyEventBufSize = 64
	lobbyIdleTimeout  = 30 * time.Minute
)

// lobbyEvent bundles a client message with the originating client and context.
type lobbyEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
}

// LobbyRoom is a goroutine-based room for pre-match coordination.
type LobbyRoom struct {
	ID      string
	State   LobbyState
	Clients map[string]*ws.Client
	Events  chan lobbyEvent
	hub     *LobbyHub

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewLobbyRoom constructs a LobbyRoom without starting its goroutine.
func NewLobbyRoom(lobbyID string, state LobbyState, hub *LobbyHub) *LobbyRoom {
	ctx, cancel := context.WithCancel(context.Background())
	return &LobbyRoom{
		ID:      lobbyID,
		State:   state,
		Clients: make(map[string]*ws.Client),
		Events:  make(chan lobbyEvent, lobbyEventBufSize),
		hub:     hub,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Run is the lobby's event loop. Must be called as a goroutine.
func (l *LobbyRoom) Run() {
	defer func() {
		if r := recover(); r != nil {
			observability.Log.Error().
				Str("lobby_id", l.ID).
				Interface("panic", r).
				Msg("lobby goroutine panicked")
		}
		l.hub.UnregisterLobby(l.ID)
	}()

	idleTicker := time.NewTicker(lobbyIdleTimeout)
	defer idleTicker.Stop()

	observability.Log.Info().Str("lobby_id", l.ID).Msg("lobby event loop started")

	for {
		select {
		case ev := <-l.Events:
			idleTicker.Reset(lobbyIdleTimeout)
			l.handleEvent(ev.ctx, ev.client, ev.msg)

		case <-idleTicker.C:
			observability.Log.Info().Str("lobby_id", l.ID).Msg("lobby idle timeout — disbanding")
			l.mu.RLock()
			for _, c := range l.Clients {
				l.sendEvent(c, "LobbyDisbanded", map[string]string{"reason": "idle_timeout"})
			}
			l.mu.RUnlock()
			l.cancel()
			return

		case <-l.ctx.Done():
			observability.Log.Info().Str("lobby_id", l.ID).Msg("lobby context cancelled — shutting down")
			return
		}
	}
}

// Join enqueues a join event for the given client.
func (l *LobbyRoom) Join(client *ws.Client) {
	msg := ws.Message{Event: "__lobby_join"}
	select {
	case l.Events <- lobbyEvent{client: client, msg: msg, ctx: context.Background()}:
	default:
		observability.Log.Warn().Str("lobby_id", l.ID).Msg("lobby event buffer full — dropping join")
	}
}

// Leave enqueues a leave event for the given client.
func (l *LobbyRoom) Leave(client *ws.Client) {
	msg := ws.Message{Event: "__lobby_leave"}
	select {
	case l.Events <- lobbyEvent{client: client, msg: msg, ctx: context.Background()}:
	default:
		observability.Log.Warn().Str("lobby_id", l.ID).Msg("lobby event buffer full — dropping leave")
	}
}

// ProcessEvent enqueues an arbitrary client message.
func (l *LobbyRoom) ProcessEvent(ctx context.Context, client *ws.Client, msg ws.Message) {
	select {
	case l.Events <- lobbyEvent{client: client, msg: msg, ctx: ctx}:
	default:
		observability.Log.Warn().Str("lobby_id", l.ID).Msg("lobby event buffer full — dropping event")
	}
}

// SetStatus updates the lobby status under the lock and broadcasts.
func (l *LobbyRoom) SetStatus(status string) {
	l.mu.Lock()
	l.State.Status = status
	l.mu.Unlock()
	l.broadcastLobbyUpdated()
}

// ---------------------------------------------------------------------------
// Internal event routing
// ---------------------------------------------------------------------------

func (l *LobbyRoom) handleEvent(ctx context.Context, client *ws.Client, msg ws.Message) {
	switch msg.Event {
	case "__lobby_join":
		l.processJoin(ctx, client)
	case "__lobby_leave", "LeaveLobby":
		l.processLeave(ctx, client)
	case "UpdateLoadout":
		l.processUpdateLoadout(ctx, client, msg)
	default:
		observability.Log.Debug().
			Str("lobby_id", l.ID).
			Str("event", msg.Event).
			Msg("unhandled lobby event")
	}
}

// ---------------------------------------------------------------------------
// processJoin
// ---------------------------------------------------------------------------

func (l *LobbyRoom) processJoin(ctx context.Context, client *ws.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()

	status := l.State.Status

	// Reconnect path: player is already a member.
	for i, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			l.Clients[client.PlayerID] = client
			client.LobbyID = l.ID
			observability.Log.Info().
				Str("lobby_id", l.ID).
				Str("player_id", client.PlayerID).
				Int("member_index", i).
				Msg("player reconnected to lobby")
			l.broadcastLobbyUpdatedLocked()
			return
		}
	}

	// New join validations.
	if len(l.State.Members) >= 2 {
		l.sendError(client, "lobby_full", "lobby is full")
		return
	}
	if status != "preparing" {
		l.sendError(client, "lobby_not_open", "lobby is not accepting members")
		return
	}

	// Look up display name.
	displayName := l.queryDisplayName(ctx, client.PlayerID)

	// Load loadout and rating.
	characterID, items := loadPlayerLoadout(ctx, l.hub.db, client.PlayerID)
	rating, tier := loadPlayerRating(ctx, l.hub.db, client.PlayerID)

	member := LobbyMember{
		PlayerID:    client.PlayerID,
		DisplayName: displayName,
		CharacterID: characterID,
		Items:       items,
		Rating:      rating,
		Tier:        tier,
	}

	l.State.Members = append(l.State.Members, member)
	l.Clients[client.PlayerID] = client
	client.LobbyID = l.ID

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", client.PlayerID).
		Msg("player joined lobby")

	l.broadcastLobbyUpdatedLocked()
}

// ---------------------------------------------------------------------------
// processLeave
// ---------------------------------------------------------------------------

func (l *LobbyRoom) processLeave(ctx context.Context, client *ws.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.Clients, client.PlayerID)
	client.LobbyID = ""

	// If lobby was in queue, reset it and notify remaining clients.
	if l.State.Status == "in_queue" {
		l.State.Status = "preparing"
		for _, c := range l.Clients {
			l.sendEvent(c, "QueueCancelled", map[string]string{"reason": "member_left"})
		}
	}

	// Host left — disband the lobby.
	if client.PlayerID == l.State.HostPlayerID {
		for _, c := range l.Clients {
			l.sendEvent(c, "LobbyDisbanded", map[string]string{"reason": "host_left"})
		}
		observability.Log.Info().Str("lobby_id", l.ID).Msg("host left — disbanding lobby")
		l.cancel()
		return
	}

	// Non-host member left — remove from Members list.
	filtered := l.State.Members[:0]
	for _, m := range l.State.Members {
		if m.PlayerID != client.PlayerID {
			filtered = append(filtered, m)
		}
	}
	l.State.Members = filtered

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", client.PlayerID).
		Msg("player left lobby")

	l.broadcastLobbyUpdatedLocked()
}

// ---------------------------------------------------------------------------
// processUpdateLoadout
// ---------------------------------------------------------------------------

func (l *LobbyRoom) processUpdateLoadout(ctx context.Context, client *ws.Client, msg ws.Message) {
	var payload UpdateLoadoutPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		l.sendError(client, "invalid_payload", "cannot parse UpdateLoadout payload")
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// Find the member.
	memberIdx := -1
	for i, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			memberIdx = i
			break
		}
	}
	if memberIdx == -1 {
		l.sendError(client, "not_in_lobby", "you are not a member of this lobby")
		return
	}

	member := l.State.Members[memberIdx]

	// Validate and apply character change.
	if payload.CharacterID != nil {
		newChar := *payload.CharacterID
		if newChar != "rookie" {
			if !l.playerOwnsCharacter(ctx, client.PlayerID, newChar) {
				l.sendError(client, "character_not_owned", fmt.Sprintf("character %q not owned", newChar))
				return
			}
		}
		member.CharacterID = newChar
	}

	// Validate and apply items change.
	if payload.Items != nil {
		if len(payload.Items) > 3 {
			l.sendError(client, "too_many_items", "maximum 3 items allowed")
			return
		}
		if len(payload.Items) > 0 {
			ownedItems := l.queryOwnedItems(ctx, client.PlayerID)
			for _, itemID := range payload.Items {
				if !ownedItems[itemID] {
					l.sendError(client, "item_not_owned", fmt.Sprintf("item %q not owned or out of stock", itemID))
					return
				}
			}
		}
		member.Items = payload.Items
	}

	l.State.Members[memberIdx] = member

	// Persist loadout.
	l.persistLoadout(ctx, client.PlayerID, member.CharacterID, member.Items)

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", client.PlayerID).
		Str("character_id", member.CharacterID).
		Msg("loadout updated")

	l.broadcastLobbyUpdatedLocked()
}

// ---------------------------------------------------------------------------
// DB helpers (called with mu held or before acquiring it)
// ---------------------------------------------------------------------------

func (l *LobbyRoom) queryDisplayName(ctx context.Context, playerID string) string {
	var name string
	row := l.hub.db.Pool.QueryRow(ctx,
		`SELECT display_name FROM player_profiles WHERE player_id = $1`, playerID)
	if err := row.Scan(&name); err != nil {
		return playerID
	}
	return name
}

func (l *LobbyRoom) playerOwnsCharacter(ctx context.Context, playerID, characterID string) bool {
	var exists bool
	row := l.hub.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM player_characters WHERE player_id = $1 AND character_id = $2)`,
		playerID, characterID)
	_ = row.Scan(&exists)
	return exists
}

// queryOwnedItems returns a set of item IDs the player owns (quantity > 0, not expired).
func (l *LobbyRoom) queryOwnedItems(ctx context.Context, playerID string) map[string]bool {
	rows, err := l.hub.db.Pool.Query(ctx,
		`SELECT item_id FROM inventory_items
		 WHERE player_id = $1
		   AND quantity > 0
		   AND (expires_at IS NULL OR expires_at > now())`,
		playerID)
	if err != nil {
		return map[string]bool{}
	}
	defer rows.Close()

	owned := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			owned[id] = true
		}
	}
	return owned
}

func (l *LobbyRoom) persistLoadout(ctx context.Context, playerID, characterID string, items []string) {
	itemsJSON, err := json.Marshal(items)
	if err != nil {
		observability.Log.Error().Err(err).Str("player_id", playerID).Msg("failed to marshal items for loadout persist")
		return
	}

	_, err = l.hub.db.Pool.Exec(ctx,
		`INSERT INTO player_loadouts (player_id, character_id, items)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id) DO UPDATE
		   SET character_id = EXCLUDED.character_id,
		       items        = EXCLUDED.items`,
		playerID, characterID, itemsJSON)
	if err != nil {
		observability.Log.Error().Err(err).Str("player_id", playerID).Msg("failed to persist loadout")
	}
}

// ---------------------------------------------------------------------------
// Broadcast / send helpers
// ---------------------------------------------------------------------------

// broadcastLobbyUpdated acquires the read lock before broadcasting.
func (l *LobbyRoom) broadcastLobbyUpdated() {
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.broadcastLobbyUpdatedLocked()
}

// broadcastLobbyUpdatedLocked broadcasts the current LobbyState to all connected clients.
// Caller must hold at least a read lock on l.mu.
func (l *LobbyRoom) broadcastLobbyUpdatedLocked() {
	for _, c := range l.Clients {
		l.sendEvent(c, "LobbyUpdated", l.State)
	}
}

func (l *LobbyRoom) sendError(client *ws.Client, code, message string) {
	l.sendEvent(client, "Error", map[string]string{
		"code":    code,
		"message": message,
	})
}

func (l *LobbyRoom) sendEvent(client *ws.Client, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		observability.Log.Error().Err(err).Str("event", event).Msg("failed to marshal lobby event payload")
		return
	}
	msg := ws.Message{Event: event, Data: data}
	select {
	case client.Send <- msg:
	default:
		observability.Log.Warn().
			Str("lobby_id", l.ID).
			Str("player_id", client.PlayerID).
			Str("event", event).
			Msg("client send buffer full — dropping message")
	}
}
