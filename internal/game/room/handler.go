package room

import (
	"context"
	"encoding/json"
	"time"

	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type WSHandler struct {
	hub *Hub
}

func NewWSHandler(hub *Hub) *WSHandler {
	return &WSHandler{hub: hub}
}

func (h *WSHandler) HandleMessage(ctx context.Context, client *ws.Client, msg ws.Message) {
	log := observability.FromContext(ctx)

	switch msg.Event {
	case "CreateRoom":
		var payload CreateRoomPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			h.sendError(client, model.ErrBadRequest)
			return
		}

		// Query player display name
		var displayName string
		query := "SELECT display_name FROM player_profiles WHERE player_id = $1"
		err := h.hub.db.Pool.QueryRow(ctx, query, client.PlayerID).Scan(&displayName)
		if err != nil {
			displayName = "Player_" + client.PlayerID[:6]
		}

		room, err := h.hub.CreateRoom(ctx, client.PlayerID, displayName, payload)
		if err != nil {
			errResp := model.AppError{Code: "ROOM_CREATE_FAILED", Message: err.Error(), Status: 400}
			h.sendError(client, errResp)
			return
		}

		// Join host player immediately
		err = room.Join(client, nil)
		if err != nil {
			errResp := model.AppError{Code: "ROOM_JOIN_FAILED", Message: err.Error(), Status: 400}
			h.sendError(client, errResp)
		}

	case "JoinRoom":
		var payload JoinRoomPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			h.sendError(client, model.ErrBadRequest)
			return
		}

		room, err := h.hub.FindRoom(payload.RoomID)
		if err != nil {
			errResp := model.AppError{Code: "ROOM_NOT_FOUND", Message: err.Error(), Status: 404}
			h.sendError(client, errResp)
			return
		}

		err = room.Join(client, payload.Password)
		if err != nil {
			errResp := model.AppError{Code: "ROOM_JOIN_FAILED", Message: err.Error(), Status: 400}
			h.sendError(client, errResp)
		}

	default:
		// Route room/match specific messages
		if client.RoomID == "" {
			errResp := model.AppError{Code: "ROOM_REQUIRED", Message: "You must join a room first", Status: 400}
			h.sendError(client, errResp)
			return
		}

		room, err := h.hub.FindRoom(client.RoomID)
		if err != nil {
			log.Warn().Str("roomId", client.RoomID).Msg("client belongs to non-existent room")
			client.RoomID = ""
			return
		}

		// Send message down to Room actor event loop
		room.ProcessEvent(ctx, client, msg)
	}
}

func (h *WSHandler) Unregister(client *ws.Client) {
	if client.RoomID != "" {
		room, err := h.hub.FindRoom(client.RoomID)
		if err == nil {
			room.Leave(client)
		}
	}
}

func (h *WSHandler) sendError(client *ws.Client, appErr model.AppError) {
	errResp := model.ErrorResponse{}
	errResp.Error.Code = appErr.Code
	errResp.Error.Message = appErr.Message
	errResp.Error.CorrelationID = "ws-error"

	payload, err := json.Marshal(errResp)
	if err != nil {
		return
	}

	client.Send <- ws.Message{
		Event:     "RoomError",
		Data:      payload,
		Timestamp: time.Now().Unix(),
	}
}
