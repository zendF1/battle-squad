package auth

import (
	"encoding/json"
	"net/http"

	"battle-squad/internal/shared/model"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GuestLogin(w http.ResponseWriter, r *http.Request) {
	var req GuestLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	resp, err := h.service.GuestLogin(r.Context(), req.DeviceInstallID)
	if err != nil {
		if err.Error() == "banned" {
			model.WriteError(w, r, model.ErrBanned)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ProviderLogin(w http.ResponseWriter, r *http.Request) {
	var req ProviderLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	resp, err := h.service.ProviderLogin(r.Context(), req.Provider, req.IDToken)
	if err != nil {
		if err.Error() == "banned" {
			model.WriteError(w, r, model.ErrBanned)
			return
		}
		errResponse := model.AppError{
			Code:    "AUTH_INVALID_CREDENTIALS",
			Message: err.Error(),
			Status:  http.StatusUnauthorized,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) LinkProvider(w http.ResponseWriter, r *http.Request) {
	accIDVal := r.Context().Value("accountId")
	accID, ok := accIDVal.(string)
	if !ok || accID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req LinkProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.LinkProvider(r.Context(), accID, req.Provider, req.IDToken)
	if err != nil {
		errResponse := model.AppError{
			Code:    "AUTH_LINK_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	resp, err := h.service.RefreshToken(r.Context(), req.RefreshToken)
	if err != nil {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.Logout(r.Context(), req.RefreshToken)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}
