package character

import (
	"encoding/json"
	"errors"
	"net/http"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

// Handler exposes HTTP endpoints for the character progression feature.
type Handler struct {
	service *Service
}

// NewHandler creates a new Handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// GetCharacters handles GET /characters — returns all owned characters for the caller.
func (h *Handler) GetCharacters(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	chars, err := h.service.GetPlayerCharacters(r.Context(), playerID)
	if err != nil {
		var appErr model.AppError
		if errors.As(err, &appErr) {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if chars == nil {
		chars = []PlayerCharacter{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"characters": chars,
	})
}

// AllocateStats handles POST /characters/allocate-stats — spends stat points into bonuses.
func (h *Handler) AllocateStats(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var payload AllocateStatsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if payload.CharacterID == "" {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.AllocateStats(r.Context(), playerID, payload)
	if err != nil {
		var appErr model.AppError
		if errors.As(err, &appErr) {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ResetStats handles POST /characters/reset-stats — charges the reset fee and refunds bonus points.
func (h *Handler) ResetStats(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var payload ResetStatsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if payload.CharacterID == "" {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.ResetStats(r.Context(), playerID, payload.CharacterID)
	if err != nil {
		var appErr model.AppError
		if errors.As(err, &appErr) {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
