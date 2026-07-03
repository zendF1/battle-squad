package player

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

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	profile, err := h.service.GetProfile(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(profile)
}

func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.UpdateDisplayName(r.Context(), playerID, req.DisplayName)
	if err != nil {
		errResponse := model.AppError{
			Code:    "PLAYER_UPDATE_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) RequestAccountDeletion(w http.ResponseWriter, r *http.Request) {
	accID, ok := r.Context().Value("accountId").(string)
	if !ok || accID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	deletedAt, err := h.service.RequestAccountDeletion(r.Context(), accID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "pending_deletion",
		"deletedAt": deletedAt,
	})
}

func (h *Handler) CancelAccountDeletion(w http.ResponseWriter, r *http.Request) {
	accID, ok := r.Context().Value("accountId").(string)
	if !ok || accID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	err := h.service.CancelAccountDeletion(r.Context(), accID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"active"}`))
}

func (h *Handler) GetAccountDeletionStatus(w http.ResponseWriter, r *http.Request) {
	accID, ok := r.Context().Value("accountId").(string)
	if !ok || accID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	status, deletedAt, err := h.service.GetAccountDeletionStatus(r.Context(), accID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	resp := AccountDeletionStatusResponse{
		Status:    status,
		DeletedAt: deletedAt,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}
