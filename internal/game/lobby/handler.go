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
// Everything but CreateLobby / JoinLobby runs on the lobby's goroutine, in the
// order the client sent it.
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
	case "LeaveLobby", "UpdateLoadout", "StartQueue", "CancelQueue":
		if client.LobbyID == "" {
			sendLobbyError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err != nil {
			client.LobbyID = ""
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

	var displayName string
	err := h.hub.db.Pool.QueryRow(ctx,
		`SELECT display_name FROM player_profiles WHERE player_id = $1`, client.PlayerID).
		Scan(&displayName)
	if err != nil {
		displayName = "Player_" + client.PlayerID[:min(6, len(client.PlayerID))]
	}

	rating, tier, division := loadPlayerRating(ctx, h.hub.db, client.PlayerID)

	lobby, err := h.hub.CreateLobby(ctx, client.PlayerID, displayName, rating, tier, division)
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

// UnregisterFromLobby leaves the client's lobby when its connection closes
// (a queued lobby leaves the queue, see processLeave).
func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
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
