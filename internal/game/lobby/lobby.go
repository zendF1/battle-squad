package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/room"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

const (
	lobbyEventBufSize = 64
	lobbyIdleTimeout  = 30 * time.Minute
	// leaveWait bounds LeaveAndWait
	leaveWait = 2 * time.Second
)

// lobbyEvent bundles a client message with the originating client and context.
type lobbyEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
	done   chan struct{} // closed once handled (LeaveAndWait), nil otherwise
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
		l.hub.cancelLobbyInvites(l.ID)
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
			if ev.done != nil {
				close(ev.done)
			}

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

// LeaveAndWait leaves the lobby like Leave and waits (at most leaveWait) until
// the lobby handled it; the client's LobbyID is cleared either way.
func (l *LobbyRoom) LeaveAndWait(client *ws.Client) {
	done := make(chan struct{})
	select {
	case l.Events <- lobbyEvent{client: client, msg: ws.Message{Event: "__lobby_leave"}, ctx: context.Background(), done: done}:
		select {
		case <-done:
		case <-time.After(leaveWait):
			observability.Log.Warn().Str("lobby_id", l.ID).Str("player_id", client.PlayerID).Msg("lobby leave not handled in time")
		}
	default:
		observability.Log.Warn().Str("lobby_id", l.ID).Msg("lobby event buffer full — dropping leave")
	}
	if client.LobbyID == l.ID {
		client.LobbyID = ""
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
	case "StartQueue":
		l.processStartQueue(ctx, client)
	case "CancelQueue":
		l.processCancelQueue(ctx, client)
	case "InviteToLobby":
		l.processInvite(ctx, client, msg)
	case "__lobby_accept":
		l.processAccept(ctx, client, msg)
	case "KickMember":
		l.processKick(ctx, client, msg)
	case "SetReady":
		l.processSetReady(ctx, client, msg)
	case "SetLobbyQueue":
		l.processSetLobbyQueue(ctx, client, msg)
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

	l.addMemberLocked(client, l.hub.store.Member(ctx, client.PlayerID))
}

// addMemberLocked adds the client's player as a member, not ready, and broadcasts
// LobbyUpdated; a full lobby's pending invites are cancelled. Caller holds l.mu.
func (l *LobbyRoom) addMemberLocked(client *ws.Client, member LobbyMember) {
	member.Ready = false
	l.State.Members = append(l.State.Members, member)
	l.Clients[client.PlayerID] = client
	client.LobbyID = l.ID

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", client.PlayerID).
		Msg("player joined lobby")

	l.broadcastLobbyUpdatedLocked()
	if len(l.State.Members) >= 2 {
		l.hub.cancelLobbyInvites(l.ID)
	}
}

// ---------------------------------------------------------------------------
// processLeave
// ---------------------------------------------------------------------------

func (l *LobbyRoom) processLeave(ctx context.Context, client *ws.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.Clients, client.PlayerID)
	client.LobbyID = ""

	// If lobby was in queue, take it out of the queue and notify remaining clients.
	if l.State.Status == "in_queue" {
		if l.hub.matchmaker != nil {
			if _, err := l.hub.matchmaker.CancelQueue(ctx, client.PlayerID); err != nil {
				observability.Log.Warn().Err(err).Str("lobby_id", l.ID).Msg("failed to leave the queue on lobby leave")
			}
		}
		l.State.Status = "preparing"
		l.State.QueueEntryID = ""
		payload, _ := json.Marshal(map[string]string{"reason": "teammate disconnected"})
		for _, c := range l.Clients {
			select {
			case c.Send <- ws.Message{Event: "QueueCancelled", Data: payload}:
			default:
			}
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

	if l.State.Status != "preparing" {
		l.sendError(client, "INVALID_STATUS", "Loadout can only change while the lobby is preparing")
		return
	}

	member := l.State.Members[memberIdx]
	if payload.CharacterID != nil {
		member.CharacterID = *payload.CharacterID
	}
	if payload.Items != nil {
		member.Items = payload.Items
	}
	if err := room.ValidateLoadout(ctx, l.hub.db.Pool, client.PlayerID, member.CharacterID, member.Items); err != nil {
		var loadoutErr *room.LoadoutError
		if errors.As(err, &loadoutErr) {
			l.sendError(client, model.ErrLoadoutInvalid.Code, loadoutErr.Reason)
			return
		}
		observability.Log.Error().Err(err).Str("player_id", client.PlayerID).Msg("failed to validate lobby loadout")
		l.sendError(client, model.ErrInternalServer.Code, "Failed to validate loadout")
		return
	}

	member.Level, member.Gun = l.hub.store.Look(ctx, client.PlayerID, member.CharacterID)
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
// Queue
// ---------------------------------------------------------------------------

// processStartQueue (host only, lobby preparing, the member ready: else
// MEMBER_NOT_READY) refreshes the members' ratings
// and enqueues the lobby in its queue: LobbyUpdated (in_queue), then QueueStarted
// {estimatedWait: botFillAfter seconds}. The lock is held until the lobby is
// queued, so a MatchFound (Notify) always follows QueueStarted.
func (l *LobbyRoom) processStartQueue(ctx context.Context, client *ws.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.State.HostPlayerID != client.PlayerID {
		l.sendError(client, "NOT_HOST", "Only the host can start queue")
		return
	}
	if l.State.Status != "preparing" {
		l.sendError(client, "INVALID_STATUS", "Lobby is not in preparing state")
		return
	}
	for _, m := range l.State.Members {
		if m.PlayerID != l.State.HostPlayerID && !m.Ready {
			l.sendError(client, "MEMBER_NOT_READY", "Your teammate is not ready")
			return
		}
	}
	if l.hub.matchmaker == nil {
		l.sendError(client, "QUEUE_FAILED", "Matchmaking is not available")
		return
	}

	playerIDs := make([]string, 0, len(l.State.Members))
	playerRatings := make(map[string]int)
	playerChars := make(map[string]string)
	playerItems := make(map[string][]string)
	playerNames := make(map[string]string)
	for i, m := range l.State.Members {
		// The rating may have changed since the member joined (an earlier match)
		m.Rating, m.Tier, m.Division = l.hub.store.Rating(ctx, m.PlayerID)
		l.State.Members[i] = m

		playerIDs = append(playerIDs, m.PlayerID)
		playerRatings[m.PlayerID] = m.Rating
		playerChars[m.PlayerID] = m.CharacterID
		playerItems[m.PlayerID] = m.Items
		playerNames[m.PlayerID] = m.DisplayName
	}

	entry, err := l.hub.matchmaker.EnqueueLobby(ctx, l.State.Queue, l.ID, playerIDs, playerRatings, playerChars, playerItems, playerNames)
	if err != nil {
		l.sendError(client, "QUEUE_FAILED", err.Error())
		return
	}
	l.State.Status = "in_queue"
	l.State.QueueEntryID = entry.EntryID
	l.broadcastLobbyUpdatedLocked()
	for _, c := range l.Clients {
		l.sendEvent(c, "QueueStarted", map[string]int{"estimatedWait": l.hub.matchmaker.GetConfig().BotFillAfter})
	}
}

// processCancelQueue takes a queued lobby out of the queue: LobbyUpdated
// (preparing), then QueueCancelled {reason: "cancelled"}. When the matchmaker
// already took the entry, the cancel is too late: nothing is sent and the
// MatchFound follows.
func (l *LobbyRoom) processCancelQueue(ctx context.Context, client *ws.Client) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.State.Status != "in_queue" {
		l.sendError(client, "NOT_IN_QUEUE", "Lobby is not in queue")
		return
	}
	if l.hub.matchmaker != nil {
		if _, err := l.hub.matchmaker.CancelQueue(ctx, client.PlayerID); err != nil {
			if errors.Is(err, matchmaker.ErrAlreadyMatched) {
				return
			}
			observability.Log.Warn().Err(err).Str("lobby_id", l.ID).Msg("failed to cancel queue")
		}
	}
	l.State.Status = "preparing"
	l.State.QueueEntryID = ""
	l.broadcastLobbyUpdatedLocked()
	for _, c := range l.Clients {
		l.sendEvent(c, "QueueCancelled", map[string]string{"reason": "cancelled"})
	}
}

// processKick is KickMember {playerId} (host only, lobby preparing): the member
// gets LobbyKicked {lobbyId} and leaves (its LobbyID cleared), the rest
// LobbyUpdated.
func (l *LobbyRoom) processKick(ctx context.Context, client *ws.Client, msg ws.Message) {
	var payload KickMemberPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		l.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.State.HostPlayerID != client.PlayerID {
		l.sendError(client, "NOT_HOST", "Only the host can kick a member")
		return
	}
	if l.State.Status != "preparing" {
		l.sendError(client, "INVALID_STATUS", "Members can only be kicked while the lobby is preparing")
		return
	}
	idx := -1
	for i, m := range l.State.Members {
		if m.PlayerID == payload.PlayerID && m.PlayerID != l.State.HostPlayerID {
			idx = i
		}
	}
	if idx < 0 {
		l.sendError(client, "INVALID_PAYLOAD", "The player is not a member of this lobby")
		return
	}
	l.State.Members = append(l.State.Members[:idx], l.State.Members[idx+1:]...)
	if kicked, ok := l.Clients[payload.PlayerID]; ok {
		delete(l.Clients, payload.PlayerID)
		if kicked.LobbyID == l.ID {
			kicked.LobbyID = ""
		}
		l.sendEvent(kicked, "LobbyKicked", LobbyKickedPayload{LobbyID: l.ID})
	}

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", payload.PlayerID).
		Msg("member kicked from lobby")

	l.broadcastLobbyUpdatedLocked()
}

// processSetReady is SetReady {ready} from a non-host member while the lobby is
// preparing: LobbyUpdated.
func (l *LobbyRoom) processSetReady(ctx context.Context, client *ws.Client, msg ws.Message) {
	var payload SetReadyPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		l.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	idx := -1
	for i, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			idx = i
		}
	}
	switch {
	case idx < 0:
		l.sendError(client, "LOBBY_REQUIRED", "You are not a member of this lobby")
		return
	case client.PlayerID == l.State.HostPlayerID:
		l.sendError(client, "INVALID_PAYLOAD", "The host is always ready")
		return
	case l.State.Status != "preparing":
		l.sendError(client, "INVALID_STATUS", "Ready can only change while the lobby is preparing")
		return
	}
	l.State.Members[idx].Ready = payload.Ready
	l.broadcastLobbyUpdatedLocked()
}

// processSetLobbyQueue is SetLobbyQueue {queue} from the host (NOT_HOST) while the
// lobby is preparing (INVALID_STATUS): a queue the matchmaker runs (INVALID_QUEUE)
// becomes the lobby's, and everyone gets LobbyUpdated.
func (l *LobbyRoom) processSetLobbyQueue(ctx context.Context, client *ws.Client, msg ws.Message) {
	var payload SetLobbyQueuePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.Queue == "" {
		l.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	switch {
	case l.State.HostPlayerID != client.PlayerID:
		l.sendError(client, "NOT_HOST", "Only the host can change the mode")
		return
	case l.State.Status != "preparing":
		l.sendError(client, "INVALID_STATUS", "The mode can only change while the lobby is preparing")
		return
	case !matchmaker.KnownQueue(payload.Queue):
		sendInvalidQueue(client, payload.Queue)
		return
	}
	l.State.Queue = payload.Queue
	l.broadcastLobbyUpdatedLocked()
}

// statusAndSize is the lobby's status and member count.
func (l *LobbyRoom) statusAndSize() (string, int) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.State.Status, len(l.State.Members)
}

// soloHost reports whether the client hosts this lobby alone and it is not in the
// queue (accepting an invite then leaves it first).
func (l *LobbyRoom) soloHost(client *ws.Client) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.State.HostPlayerID == client.PlayerID && len(l.State.Members) == 1 && l.State.Status != "in_queue"
}

// reuseForHost reports whether the client's CreateLobby {queue} can return this
// lobby: it hosts it and the lobby is open (preparing), or it is alone in it and
// has left its match (the lobby then opens again). A returned lobby switches to
// queue.
func (l *LobbyRoom) reuseForHost(client *ws.Client, queue string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.State.HostPlayerID != client.PlayerID {
		return false
	}
	switch {
	case l.State.Status == "preparing":
	case l.State.Status == "in_match" && client.RoomID == "" && len(l.State.Members) == 1:
		l.State.Status = "preparing"
	default:
		return false
	}
	l.State.Queue = queue
	return true
}

// ---------------------------------------------------------------------------
// DB helpers (called with mu held or before acquiring it)
// ---------------------------------------------------------------------------

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
	sendLobbyError(client, code, message)
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
