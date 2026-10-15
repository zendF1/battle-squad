package room

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

	case "QuickPlay":
		// Create a tutorial room with a bot (idle or AI) and start match immediately
		payload := parseQuickPlayPayload(msg.Data)

		// Validate the loadout before any room exists
		ownership := poolOwnership{pool: h.hub.db.Pool}
		if err := validateQuickPlayLoadout(ctx, ownership, client.PlayerID, payload, h.hub.devUnlockAll); err != nil {
			var loadoutErr *LoadoutError
			if errors.As(err, &loadoutErr) {
				appErr := model.ErrLoadoutInvalid
				appErr.Message = loadoutErr.Reason
				h.sendError(client, appErr)
				return
			}
			log.Error().Err(err).Str("playerId", client.PlayerID).Msg("failed to validate quick play loadout")
			h.sendError(client, model.ErrInternalServer)
			return
		}

		var displayName string
		query := "SELECT display_name FROM player_profiles WHERE player_id = $1"
		err := h.hub.db.Pool.QueryRow(ctx, query, client.PlayerID).Scan(&displayName)
		if err != nil {
			displayName = "Player_" + client.PlayerID[:6]
		}

		room, err := h.hub.CreateRoom(ctx, client.PlayerID, displayName, CreateRoomPayload{
			Mode:  "pvp_1v1",
			MapID: "grassland_valley",
		})
		if err != nil {
			errResp := model.AppError{Code: "ROOM_CREATE_FAILED", Message: err.Error(), Status: 400}
			h.sendError(client, errResp)
			return
		}

		// Join host
		if err = room.Join(client, nil); err != nil {
			errResp := model.AppError{Code: "ROOM_JOIN_FAILED", Message: err.Error(), Status: 400}
			h.sendError(client, errResp)
			return
		}

		// Add the bot and start match via room event
		data, _ := json.Marshal(payload)
		room.Events <- roomEvent{
			client: client,
			msg:    ws.Message{Event: "__start_tutorial", Data: data},
			ctx:    ctx,
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
		// Internal room events (__start_tutorial, __register_client, ...) are only sent by the server
		if isInternalEvent(msg.Event) {
			h.sendError(client, model.ErrBadRequest)
			return
		}

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
	if msg, ok := roomErrorMessage(appErr); ok {
		client.Send <- msg
	}
}

// roomErrorMessage builds the RoomError event for appErr.
func roomErrorMessage(appErr model.AppError) (ws.Message, bool) {
	errResp := model.ErrorResponse{}
	errResp.Error.Code = appErr.Code
	errResp.Error.Message = appErr.Message
	errResp.Error.CorrelationID = "ws-error"

	payload, err := json.Marshal(errResp)
	if err != nil {
		return ws.Message{}, false
	}
	return ws.Message{
		Event:     "RoomError",
		Data:      payload,
		Timestamp: time.Now().Unix(),
	}, true
}

// isInternalEvent reports whether an event name is reserved for server-side room events.
func isInternalEvent(event string) bool {
	return strings.HasPrefix(event, "__")
}
