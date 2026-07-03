package matchhistory

import (
	"encoding/json"
	"net/http"
	"strconv"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetHistory(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}

	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 {
			if v > 50 {
				v = 50
			}
			limit = v
		}
	}

	offset := (page - 1) * limit

	entries, err := h.service.GetPlayerHistory(r.Context(), playerID, limit, offset)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(entries)
}
