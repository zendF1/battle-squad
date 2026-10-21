package maps

import (
	"net/http"
	"strings"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// List: GET /maps → MapList. ETag is the list's version; If-None-Match with it → 304.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.List(r.Context())
	if err != nil {
		log := observability.FromContext(r.Context())
		log.Error().Err(err).Msg("failed to list maps")
		model.WriteServiceError(w, r, err)
		return
	}
	etag := `"` + out.Version + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	model.WriteOK(w, out)
}

// Get: GET /maps/{mapId} → MapDetail (tiles and spawn points), MAP_NOT_FOUND.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.service.Get(r.Context(), chi.URLParam(r, "mapId"))
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, out)
}
