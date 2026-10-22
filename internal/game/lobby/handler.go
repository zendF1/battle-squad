package lobby

import (
	"context"
	"encoding/json"

	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type WSHandler struct {
	hub        *LobbyHub
	matchmaker *matchmaker.Matchmaker
}

func NewWSHandler(hub *LobbyHub, mm *matchmaker.Matchmaker) *WSHandler {
	hub.matchmaker = mm
	return &WSHandler{hub: hub, matchmaker: mm}
}

// HandleLobbyMessage returns true if the event was handled, false if not a lobby event.
// Everything but CreateLobby / JoinLobby / ListOnlinePlayers / RespondInvite runs
// on the lobby's goroutine, in the order the client sent it.
func (h *WSHandler) HandleLobbyMessage(ctx context.Context, client *ws.Client, msg ws.Message) bool {
	switch msg.Event {
	case "CreateLobby":
		h.handleCreateLobby(ctx, client)
		return true
	case "JoinLobby":
		var payload JoinLobbyPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			sendLobbyError(client, "INVALID_PAYLOAD", "Invalid payload")
			return true
		}
		h.handleJoinLobby(ctx, client, payload)
		return true
	case "ListOnlinePlayers":
		var payload ListOnlinePlayersPayload
		if len(msg.Data) > 0 && json.Unmarshal(msg.Data, &payload) != nil {
			sendLobbyError(client, "INVALID_PAYLOAD", "Invalid payload")
			return true
		}
		h.handleListOnlinePlayers(ctx, client, payload)
		return true
	case "RespondInvite":
		var payload RespondInvitePayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil || payload.InviteID == "" {
			sendLobbyError(client, "INVALID_PAYLOAD", "Invalid payload")
			return true
		}
		h.handleRespondInvite(ctx, client, payload)
		return true
	case "LeaveLobby", "UpdateLoadout", "StartQueue", "CancelQueue", "InviteToLobby", "KickMember", "SetReady":
		if client.LobbyID == "" {
			sendLobbyError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err != nil {
			client.LobbyID = ""
			if msg.Event == "InviteToLobby" {
				// the lobby is gone: the client creates one before inviting
				sendLobbyError(client, "LOBBY_REQUIRED", "You must be in a lobby")
				return true
			}
			sendLobbyError(client, "LOBBY_NOT_FOUND", "Lobby not found")
			return true
		}
		lobby.ProcessEvent(ctx, client, msg)
		return true
	}
	return false
}

// handleCreateLobby creates a lobby hosted by the client. A host already in its
// lobby gets that lobby back (LobbyUpdated) while it is open, or after it left
// its match alone; a lobby that no longer exists is forgotten.
func (h *WSHandler) handleCreateLobby(ctx context.Context, client *ws.Client) {
	if client.LobbyID != "" {
		existing, err := h.hub.FindLobby(client.LobbyID)
		if err != nil {
			client.LobbyID = ""
		} else if existing.reuseForHost(client) {
			existing.Join(client)
			return
		} else {
			sendLobbyError(client, "ALREADY_IN_LOBBY", "You are already in a lobby")
			return
		}
	}
	if client.RoomID != "" {
		sendLobbyError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	lobby, err := h.hub.CreateLobby(h.hub.store.Member(ctx, client.PlayerID))
	if err != nil {
		sendLobbyError(client, "LOBBY_CREATE_FAILED", err.Error())
		return
	}

	// Set now so the client's next messages (UpdateLoadout, StartQueue) reach the
	// lobby; they queue behind the join
	client.LobbyID = lobby.ID
	lobby.Join(client)
}

func (h *WSHandler) handleJoinLobby(ctx context.Context, client *ws.Client, payload JoinLobbyPayload) {
	if client.LobbyID != "" {
		sendLobbyError(client, "ALREADY_IN_LOBBY", "Leave your current lobby first")
		return
	}
	if client.RoomID != "" {
		sendLobbyError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	lobby, err := h.hub.FindLobby(payload.LobbyID)
	if err != nil {
		sendLobbyError(client, "LOBBY_NOT_FOUND", err.Error())
		return
	}

	lobby.Join(client)
}

// handleListOnlinePlayers answers ListOnlinePlayers {query?} with OnlinePlayers
// {players} (LobbyHub.ListOnline).
func (h *WSHandler) handleListOnlinePlayers(ctx context.Context, client *ws.Client, payload ListOnlinePlayersPayload) {
	players, err := h.hub.ListOnline(ctx, client, payload.Query)
	if err != nil {
		log := observability.FromContext(ctx)
		log.Error().Err(err).Msg("failed to list online players")
		sendLobbyError(client, model.ErrInternalServer.Code, "Failed to list online players")
		return
	}
	send(client, "OnlinePlayers", OnlinePlayersPayload{Players: players})
}

// handleRespondInvite is RespondInvite {inviteId, accept} from the invitee.
// Decline: InviteDeclined {playerId, displayName} to the inviter. Accept: the
// invite pending (INVITE_EXPIRED), its lobby there (LOBBY_NOT_FOUND), preparing
// (INVALID_STATUS) with room (LOBBY_FULL), the invitee in no room (IN_ROOM); an
// invitee hosting a lobby alone leaves it first (in another one:
// ALREADY_IN_LOBBY), then joins on the lobby's goroutine (processAccept) like
// JoinLobby.
func (h *WSHandler) handleRespondInvite(ctx context.Context, client *ws.Client, payload RespondInvitePayload) {
	inv := h.hub.peekInvite(payload.InviteID)
	if inv == nil || inv.InviteeID != client.PlayerID {
		sendLobbyError(client, "INVITE_EXPIRED", "The invite has expired")
		return
	}
	if !payload.Accept {
		if h.hub.takeInvite(inv.ID) == nil {
			sendLobbyError(client, "INVITE_EXPIRED", "The invite has expired")
			return
		}
		if inviter := h.hub.OnlineClient(inv.InviterID); inviter != nil {
			send(inviter, "InviteDeclined", InviteDeclinedPayload{PlayerID: client.PlayerID,
				DisplayName: h.hub.store.DisplayName(ctx, client.PlayerID)})
		}
		return
	}

	lobby, err := h.hub.FindLobby(inv.LobbyID)
	if err != nil {
		sendLobbyError(client, "LOBBY_NOT_FOUND", "Lobby not found")
		return
	}
	status, members := lobby.statusAndSize()
	if status != "preparing" {
		sendLobbyError(client, "INVALID_STATUS", "The lobby is not accepting members")
		return
	}
	if members >= 2 {
		sendLobbyError(client, "LOBBY_FULL", "The lobby is full")
		return
	}
	if client.RoomID != "" {
		sendLobbyError(client, "IN_ROOM", "Leave your current room first")
		return
	}
	if client.LobbyID != "" && client.LobbyID != lobby.ID {
		own, err := h.hub.FindLobby(client.LobbyID)
		switch {
		case err != nil:
			client.LobbyID = ""
		case own.soloHost(client):
			own.LeaveAndWait(client)
		default:
			sendLobbyError(client, "ALREADY_IN_LOBBY", "Leave your current lobby first")
			return
		}
	}

	// Set now so the client's next messages (UpdateLoadout, SetReady) reach the
	// lobby; processAccept clears it when the join fails
	client.LobbyID = lobby.ID
	data, _ := json.Marshal(inv.ID)
	lobby.ProcessEvent(ctx, client, ws.Message{Event: "__lobby_accept", Data: data})
}

// RegisterOnline marks the client's player online (presence.go).
func (h *WSHandler) RegisterOnline(client *ws.Client) {
	h.hub.SetOnline(client)
}

// UnregisterFromLobby leaves the client's lobby when its connection closes
// (a queued lobby leaves the queue, see processLeave) and marks it offline.
func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
	h.hub.SetOffline(client)
	if client.LobbyID != "" {
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err == nil {
			lobby.Leave(client)
		}
	}
}

// sendLobbyError sends LobbyError {error: {code, message, correlationId}}.
func sendLobbyError(client *ws.Client, code, message string) {
	errResp := model.ErrorResponse{}
	errResp.Error.Code = code
	errResp.Error.Message = message
	errResp.Error.CorrelationID = "ws-error"

	payload, err := json.Marshal(errResp)
	if err != nil {
		return
	}
	select {
	case client.Send <- ws.Message{Event: "LobbyError", Data: payload}:
	default:
		observability.Log.Warn().
			Str("player_id", client.PlayerID).
			Str("code", code).
			Msg("lobby error send buffer full — dropping error message")
	}
}
