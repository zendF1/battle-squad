package rooms

import (
	"encoding/json"
	"net/http"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

type Handler struct {
	redis *database.RedisClient
}

func NewHandler(redis *database.RedisClient) *Handler {
	return &Handler{redis: redis}
}

func (h *Handler) GetRooms(w http.ResponseWriter, r *http.Request) {
	modeFilter := r.URL.Query().Get("mode")

	result, err := h.redis.Client.HGetAll(r.Context(), "rooms:active").Result()
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	rooms := make([]RoomSummary, 0, len(result))
	for _, val := range result {
		var room RoomSummary
		if err := json.Unmarshal([]byte(val), &room); err != nil {
			continue
		}
		if modeFilter != "" && room.Mode != modeFilter {
			continue
		}
		rooms = append(rooms, room)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rooms)
}
