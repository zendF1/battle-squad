package milestone

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

// List: GET /milestones.
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

// Summary: GET /milestones/summary.
func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	pid := playerID(r)
	if pid == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	out, err := h.service.Summary(r.Context(), pid)
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, out)
}

// Claim: POST /milestones/claim {id}.
func (h *Handler) Claim(w http.ResponseWriter, r *http.Request) {
	pid := playerID(r)
	if pid == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	out, err := h.service.Claim(r.Context(), pid, req.ID)
	if err != nil {
		h.writeClaimError(w, r, pid, req.ID, err)
		return
	}
	log := observability.FromContext(r.Context())
	log.Info().Str("playerId", pid).Strs("milestones", out.Claimed).Msg("milestone claimed")
	model.WriteOK(w, out)
}

// ClaimAll: POST /milestones/claim-all.
func (h *Handler) ClaimAll(w http.ResponseWriter, r *http.Request) {
	pid := playerID(r)
	if pid == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	out, err := h.service.ClaimAll(r.Context(), pid)
	if err != nil {
		h.writeClaimError(w, r, pid, "", err)
		return
	}
	log := observability.FromContext(r.Context())
	log.Info().Str("playerId", pid).Strs("milestones", out.Claimed).Msg("milestones claimed")
	model.WriteOK(w, out)
}

func (h *Handler) writeClaimError(w http.ResponseWriter, r *http.Request, pid, id string, err error) {
	log := observability.FromContext(r.Context())
	log.Warn().Err(err).Str("playerId", pid).Str("milestone", id).Msg("milestone claim refused")
	model.WriteServiceError(w, r, err)
}
