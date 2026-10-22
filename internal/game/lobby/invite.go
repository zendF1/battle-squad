package lobby

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/observability"
)

// invite is a pending InviteToLobby: from a member of LobbyID to InviteeID, until
// the invitee answers (RespondInvite), it expires (inviteTimeout: InviteExpired to
// both) or the lobby fills up or goes away (InviteExpired to the invitee).
type invite struct {
	ID, LobbyID, InviterID, InviteeID string
	timer                             *time.Timer
}

// inviteTimeout is the matchmaking config's inviteTimeoutSec (30 s by default).
func (h *LobbyHub) inviteTimeout() time.Duration {
	if h.inviteTTL > 0 {
		return h.inviteTTL
	}
	sec := 0
	if h.matchmaker != nil {
		sec = h.matchmaker.GetConfig().InviteTimeoutSec
	}
	if sec <= 0 {
		sec = matchmaker.DefaultMatchmakingConfig().InviteTimeoutSec
	}
	return time.Duration(sec) * time.Second
}

// hasPendingInvite reports whether the lobby already invited the player.
func (h *LobbyHub) hasPendingInvite(lobbyID, inviteeID string) bool {
	h.invMu.Lock()
	defer h.invMu.Unlock()
	for _, inv := range h.invites {
		if inv.LobbyID == lobbyID && inv.InviteeID == inviteeID {
			return true
		}
	}
	return false
}

// addInvite records an invite expiring after ttl, nil when the lobby already
// invited the player.
func (h *LobbyHub) addInvite(lobbyID, inviterID, inviteeID string, ttl time.Duration) *invite {
	h.invMu.Lock()
	defer h.invMu.Unlock()
	for _, inv := range h.invites {
		if inv.LobbyID == lobbyID && inv.InviteeID == inviteeID {
			return nil
		}
	}
	inv := &invite{ID: generateID(), LobbyID: lobbyID, InviterID: inviterID, InviteeID: inviteeID}
	inv.timer = time.AfterFunc(ttl, func() { h.expireInvite(inv.ID) })
	h.invites[inv.ID] = inv
	return inv
}

// peekInvite is a copy of the pending invite, nil when there is none.
func (h *LobbyHub) peekInvite(id string) *invite {
	h.invMu.Lock()
	defer h.invMu.Unlock()
	inv, ok := h.invites[id]
	if !ok {
		return nil
	}
	cp := *inv
	return &cp
}

// takeInvite removes the pending invite and returns it, nil when it is gone
// (answered, expired or cancelled): exactly one of accept, decline and expiry wins.
func (h *LobbyHub) takeInvite(id string) *invite {
	h.invMu.Lock()
	defer h.invMu.Unlock()
	inv, ok := h.invites[id]
	if !ok {
		return nil
	}
	inv.timer.Stop()
	delete(h.invites, id)
	return inv
}

// expireInvite (the invite's timer) sends InviteExpired {playerId} to the inviter
// and {inviteId} to the invitee.
func (h *LobbyHub) expireInvite(id string) {
	inv := h.takeInvite(id)
	if inv == nil {
		return
	}
	if c := h.OnlineClient(inv.InviterID); c != nil {
		send(c, "InviteExpired", InviteExpiredPayload{PlayerID: inv.InviteeID})
	}
	if c := h.OnlineClient(inv.InviteeID); c != nil {
		send(c, "InviteExpired", InviteExpiredPayload{InviteID: inv.ID})
	}
}

// cancelLobbyInvites drops the lobby's pending invites (it is full or gone):
// InviteExpired {inviteId} to each invitee.
func (h *LobbyHub) cancelLobbyInvites(lobbyID string) {
	h.invMu.Lock()
	var dropped []*invite
	for id, inv := range h.invites {
		if inv.LobbyID == lobbyID {
			inv.timer.Stop()
			delete(h.invites, id)
			dropped = append(dropped, inv)
		}
	}
	h.invMu.Unlock()
	for _, inv := range dropped {
		if c := h.OnlineClient(inv.InviteeID); c != nil {
			send(c, "InviteExpired", InviteExpiredPayload{InviteID: inv.ID})
		}
	}
}

// processInvite is InviteToLobby {playerId}, from a member of a preparing lobby
// with room: InviteSent {playerId, expiresInSec} to the inviter and LobbyInvite to
// the invitee. Checks, in order: a member (LOBBY_REQUIRED), preparing
// (INVALID_STATUS), room (LOBBY_FULL), invitee online (PLAYER_OFFLINE) and idle
// (PLAYER_BUSY), not invited yet (INVITE_PENDING), not oneself (INVALID_PAYLOAD).
// The lock is released before the invitee's status is read (it locks the
// invitee's lobby).
func (l *LobbyRoom) processInvite(ctx context.Context, client *ws.Client, msg ws.Message) {
	var payload InvitePayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.PlayerID == "" {
		l.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
		return
	}

	l.mu.RLock()
	status, members, maxMembers := l.State.Status, len(l.State.Members), l.maxMembersLocked()
	var from *LobbyMember
	for i := range l.State.Members {
		if l.State.Members[i].PlayerID == client.PlayerID {
			m := l.State.Members[i]
			from = &m
		}
	}
	l.mu.RUnlock()

	switch {
	case from == nil:
		l.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
		return
	case status != "preparing":
		l.sendError(client, "INVALID_STATUS", "Invites can only be sent while the lobby is preparing")
		return
	case members >= maxMembers:
		l.sendError(client, "LOBBY_FULL", "The lobby is full")
		return
	}
	invitee := l.hub.OnlineClient(payload.PlayerID)
	if invitee == nil {
		l.sendError(client, "PLAYER_OFFLINE", "The player is not online")
		return
	}
	if l.hub.Status(invitee) != StatusIdle {
		l.sendError(client, "PLAYER_BUSY", "The player is busy")
		return
	}
	if l.hub.hasPendingInvite(l.ID, payload.PlayerID) {
		l.sendError(client, "INVITE_PENDING", "The player is already invited")
		return
	}
	if payload.PlayerID == client.PlayerID {
		l.sendError(client, "INVALID_PAYLOAD", "You cannot invite yourself")
		return
	}

	ttl := l.hub.inviteTimeout()
	inv := l.hub.addInvite(l.ID, client.PlayerID, payload.PlayerID, ttl)
	if inv == nil {
		l.sendError(client, "INVITE_PENDING", "The player is already invited")
		return
	}
	expiresIn := int(math.Ceil(ttl.Seconds()))
	l.sendEvent(client, "InviteSent", InviteSentPayload{PlayerID: payload.PlayerID, ExpiresInSec: expiresIn})
	l.sendEvent(invitee, "LobbyInvite", LobbyInvitePayload{
		InviteID: inv.ID, LobbyID: l.ID, Mode: l.State.Queue, ExpiresInSec: expiresIn,
		From: OnlinePlayer{PlayerID: from.PlayerID, DisplayName: from.DisplayName, Level: from.Level, Tier: from.Tier,
			Division: from.Division, CharacterID: from.CharacterID, Status: StatusIdle},
	})

	observability.Log.Info().
		Str("lobby_id", l.ID).
		Str("player_id", client.PlayerID).
		Str("invitee_id", payload.PlayerID).
		Msg("lobby invite sent")
}

// processAccept (internal __lobby_accept {inviteId}, see WSHandler.handleRespondInvite)
// joins the invitee: the lobby still preparing with room (INVALID_STATUS /
// LOBBY_FULL), the invite still pending (INVITE_EXPIRED). The lobby's other
// invites are cancelled.
func (l *LobbyRoom) processAccept(ctx context.Context, client *ws.Client, msg ws.Message) {
	var inviteID string
	_ = json.Unmarshal(msg.Data, &inviteID)
	member := l.hub.store.Member(ctx, client.PlayerID)

	l.mu.Lock()
	defer l.mu.Unlock()
	fail := func(code, message string) {
		if client.LobbyID == l.ID {
			client.LobbyID = ""
		}
		l.sendError(client, code, message)
	}
	if l.State.Status != "preparing" {
		fail("INVALID_STATUS", "The lobby is not accepting members")
		return
	}
	if len(l.State.Members) >= l.maxMembersLocked() {
		fail("LOBBY_FULL", "The lobby is full")
		return
	}
	inv := l.hub.takeInvite(inviteID)
	if inv == nil || inv.LobbyID != l.ID || inv.InviteeID != client.PlayerID {
		fail("INVITE_EXPIRED", "The invite has expired")
		return
	}
	l.addMemberLocked(client, member)
}

// send sends an event to the client without blocking (dropped when its buffer is full).
func send(client *ws.Client, event string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		observability.Log.Error().Err(err).Str("event", event).Msg("failed to marshal lobby event payload")
		return
	}
	select {
	case client.Send <- ws.Message{Event: event, Data: data}:
	default:
		observability.Log.Warn().
			Str("player_id", client.PlayerID).
			Str("event", event).
			Msg("client send buffer full — dropping message")
	}
}
