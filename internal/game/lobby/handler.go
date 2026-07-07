package lobby

import (
	"context"
	"encoding/json"

	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type WSHandler struct {
	hub *LobbyHub
}

func NewWSHandler(hub *LobbyHub) *WSHandler {
	return &WSHandler{hub: hub}
}

// HandleLobbyMessage returns true if the event was handled, false if not a lobby event.
func (h *WSHandler) HandleLobbyMessage(ctx context.Context, client *ws.Client, msg ws.Message) bool {
	switch msg.Event {
	case "CreateLobby":
		h.handleCreateLobby(ctx, client)
		return true
	case "JoinLobby":
		var payload JoinLobbyPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			h.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
			return true
		}
		h.handleJoinLobby(ctx, client, payload)
		return true
	case "LeaveLobby", "UpdateLoadout":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err != nil {
			client.LobbyID = ""
			return true
		}
		lobby.ProcessEvent(ctx, client, msg)
		return true
	}
	return false
}

func (h *WSHandler) handleCreateLobby(ctx context.Context, client *ws.Client) {
	if client.LobbyID != "" {
		h.sendError(client, "ALREADY_IN_LOBBY", "You are already in a lobby")
		return
	}
	if client.RoomID != "" {
		h.sendError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	var displayName string
	err := h.hub.db.Pool.QueryRow(ctx,
		`SELECT display_name FROM player_profiles WHERE player_id = $1`, client.PlayerID).
		Scan(&displayName)
	if err != nil {
		displayName = "Player_" + client.PlayerID[:6]
	}

	rating, tier := loadPlayerRating(ctx, h.hub.db, client.PlayerID)

	lobby, err := h.hub.CreateLobby(ctx, client.PlayerID, displayName, rating, tier)
	if err != nil {
		h.sendError(client, "LOBBY_CREATE_FAILED", err.Error())
		return
	}

	lobby.Join(client)
}

func (h *WSHandler) handleJoinLobby(ctx context.Context, client *ws.Client, payload JoinLobbyPayload) {
	if client.LobbyID != "" {
		h.sendError(client, "ALREADY_IN_LOBBY", "Leave your current lobby first")
		return
	}
	if client.RoomID != "" {
		h.sendError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	lobby, err := h.hub.FindLobby(payload.LobbyID)
	if err != nil {
		h.sendError(client, "LOBBY_NOT_FOUND", err.Error())
		return
	}

	lobby.Join(client)
}

func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
	if client.LobbyID != "" {
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err == nil {
			lobby.Leave(client)
		}
	}
}

func (h *WSHandler) sendError(client *ws.Client, code, message string) {
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
