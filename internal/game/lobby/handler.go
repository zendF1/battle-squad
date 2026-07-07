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
	return &WSHandler{hub: hub, matchmaker: mm}
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

	case "StartQueue":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		h.handleStartQueue(ctx, client)
		return true

	case "CancelQueue":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		h.handleCancelQueue(ctx, client)
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

func (h *WSHandler) handleStartQueue(ctx context.Context, client *ws.Client) {
	lobby, err := h.hub.FindLobby(client.LobbyID)
	if err != nil {
		h.sendError(client, "LOBBY_NOT_FOUND", "Lobby not found")
		return
	}

	if lobby.State.HostPlayerID != client.PlayerID {
		h.sendError(client, "NOT_HOST", "Only the host can start queue")
		return
	}

	if lobby.State.Status != "preparing" {
		h.sendError(client, "INVALID_STATUS", "Lobby is not in preparing state")
		return
	}

	// Build queue entry data from lobby members.
	playerIDs := make([]string, 0, len(lobby.State.Members))
	playerRatings := make(map[string]int)
	playerChars := make(map[string]string)
	playerItems := make(map[string][]string)
	playerNames := make(map[string]string)

	for _, m := range lobby.State.Members {
		playerIDs = append(playerIDs, m.PlayerID)
		playerRatings[m.PlayerID] = m.Rating
		playerChars[m.PlayerID] = m.CharacterID
		playerItems[m.PlayerID] = m.Items
		playerNames[m.PlayerID] = m.DisplayName
	}

	entry, err := h.matchmaker.EnqueueLobby(ctx, lobby.ID, playerIDs, playerRatings, playerChars, playerItems, playerNames)
	if err != nil {
		h.sendError(client, "QUEUE_FAILED", err.Error())
		return
	}

	lobby.State.Status = "in_queue"
	lobby.State.QueueEntryID = entry.EntryID

	// Broadcast updated state and QueueStarted.
	lobby.broadcastLobbyUpdated()

	payload, _ := json.Marshal(map[string]interface{}{
		"estimatedWait": h.matchmaker.GetConfig().MaxWaitTime,
	})
	for _, c := range lobby.Clients {
		select {
		case c.Send <- ws.Message{Event: "QueueStarted", Data: payload}:
		default:
		}
	}
}

func (h *WSHandler) handleCancelQueue(ctx context.Context, client *ws.Client) {
	lobby, err := h.hub.FindLobby(client.LobbyID)
	if err != nil {
		return
	}

	if lobby.State.Status != "in_queue" {
		h.sendError(client, "NOT_IN_QUEUE", "Lobby is not in queue")
		return
	}

	_, cancelErr := h.matchmaker.CancelQueue(ctx, client.PlayerID)
	if cancelErr != nil {
		observability.Log.Warn().Err(cancelErr).Msg("failed to cancel queue")
	}

	lobby.State.Status = "preparing"
	lobby.State.QueueEntryID = ""
	lobby.broadcastLobbyUpdated()

	payload, _ := json.Marshal(map[string]string{"reason": "cancelled"})
	for _, c := range lobby.Clients {
		select {
		case c.Send <- ws.Message{Event: "QueueCancelled", Data: payload}:
		default:
		}
	}
}

func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
	if client.LobbyID != "" {
		if h.matchmaker.IsPlayerInQueue(context.Background(), client.PlayerID) {
			h.matchmaker.CancelQueue(context.Background(), client.PlayerID)
		}

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
