package gamedata

import (
	"net/http"
	"strings"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Get: GET /gamedata → GameData (public). ETag is "<version>"; If-None-Match with it → 304
// and no body.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Get(r.Context())
	if err != nil {
		log := observability.FromContext(r.Context())
		log.Error().Err(err).Msg("failed to build game data")
		model.WriteServiceError(w, r, err)
		return
	}
	etag := `"` + out.Version + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && (strings.Contains(match, etag) || strings.TrimSpace(match) == "*") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	model.WriteOK(w, out)
}
