package inventory

import (
	"net/http"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetInventory(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	items, err := h.service.GetAvailableInventory(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	// Make sure we return empty array instead of null if inventory is empty
	if items == nil {
		items = []InventoryItem{}
	}

	model.WriteOK(w, items)
}
