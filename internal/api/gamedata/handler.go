package gamedata

import (
	"encoding/json"
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
// and no body. GET /gamedata?version=<v> → the game data of version v exactly (the one a match
// reports in MatchStarted.gamedataVersion), 404 GAMEDATA_VERSION_NOT_FOUND when no server
// published it.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	log := observability.FromContext(r.Context())
	if v := strings.TrimSpace(r.URL.Query().Get("version")); v != "" {
		raw, found, err := h.service.Version(r.Context(), v)
		if err != nil {
			log.Error().Err(err).Str("version", v).Msg("failed to read archived game data")
			model.WriteServiceError(w, r, err)
			return
		}
		if !found {
			model.WriteError(w, r, model.ErrGameDataVersionNotFound)
			return
		}
		writeVersion(w, r, v, raw)
		return
	}
	b, err := h.service.currentPayload(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("failed to build game data")
		model.WriteServiceError(w, r, err)
		return
	}
	writeVersion(w, r, b.payload.Version, b.raw)
}

// writeVersion answers with the payload of version: 304 when If-None-Match has it.
func writeVersion(w http.ResponseWriter, r *http.Request, version string, raw json.RawMessage) {
	etag := `"` + version + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if match := r.Header.Get("If-None-Match"); match != "" && (strings.Contains(match, etag) || strings.TrimSpace(match) == "*") {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	model.WriteOK(w, raw)
}
