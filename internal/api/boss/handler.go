package boss

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

// GetRuns answers GET /boss/runs with the RunsState.
func (h *Handler) GetRuns(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	st, err := h.service.Runs(r.Context(), playerID)
	if err != nil {
		log := observability.FromContext(r.Context())
		log.Error().Err(err).Str("playerId", playerID).Msg("failed to read boss runs")
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	model.WriteOK(w, st)
}
