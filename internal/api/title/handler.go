package title

import (
	"encoding/json"
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

func playerID(r *http.Request) string {
	id, _ := r.Context().Value(observability.PlayerIDKey).(string)
	return id
}

// List: GET /player/titles.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	pid := playerID(r)
	if pid == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	out, err := h.service.List(r.Context(), pid)
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, out)
}

// Equip: POST /player/titles/equip {titleId} (null, missing or "" unequips).
func (h *Handler) Equip(w http.ResponseWriter, r *http.Request) {
	pid := playerID(r)
	if pid == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req EquipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	titleID := ""
	if req.TitleID != nil {
		titleID = *req.TitleID
	}
	log := observability.FromContext(r.Context())
	ref, err := h.service.Equip(r.Context(), pid, titleID)
	if err != nil {
		log.Warn().Err(err).Str("playerId", pid).Str("title", titleID).Msg("title equip refused")
		model.WriteServiceError(w, r, err)
		return
	}
	log.Info().Str("playerId", pid).Str("title", titleID).Msg("title equipped")
	model.WriteOK(w, EquipResponse{Equipped: ref})
}
