package rooms

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

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

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 10 {
		limit = 10
	}

	result, err := h.redis.Client.HGetAll(r.Context(), "rooms:active").Result()
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	// Filter rooms that are in "waiting" status
	rooms := make([]RoomSummary, 0, len(result))
	for _, val := range result {
		var room RoomSummary
		if err := json.Unmarshal([]byte(val), &room); err != nil {
			continue
		}
		if modeFilter != "" && room.Mode != modeFilter {
			continue
		}
		if room.Status != "waiting" {
			continue
		}
		rooms = append(rooms, room)
	}

	// Sort by newest first (stable ordering by roomId as fallback)
	sort.Slice(rooms, func(i, j int) bool {
		return rooms[i].RoomID > rooms[j].RoomID
	})

	total := len(rooms)
	totalPages := (total + limit - 1) / limit
	if totalPages < 1 {
		totalPages = 1
	}

	// Paginate
	start := (page - 1) * limit
	if start >= total {
		rooms = []RoomSummary{}
	} else {
		end := start + limit
		if end > total {
			end = total
		}
		rooms = rooms[start:end]
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"rooms":      rooms,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": totalPages,
	})
}
