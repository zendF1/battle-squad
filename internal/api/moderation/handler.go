package moderation

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

func (h *Handler) CreateReport(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.CreateReport(r.Context(), playerID, req.TargetPlayerID, req.MatchID, &req.Category, req.Description)
	if err != nil {
		errResponse := model.AppError{
			Code:    "REPORT_SUBMIT_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) BanPlayer(w http.ResponseWriter, r *http.Request) {
	role, ok := r.Context().Value(observability.RoleKey).(string)
	if !ok {
		observability.Log.Warn().Msg("role not found in context - possible middleware misconfiguration")
	}
	if role != "admin" {
		model.WriteError(w, r, model.ErrAdminRequired)
		return
	}

	var req BanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.BanPlayer(r.Context(), req.PlayerID, req.ReasonCode, req.ReasonText, req.DurationHours)
	if err != nil {
		errResponse := model.AppError{
			Code:    "BAN_ACTION_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) RevokeBan(w http.ResponseWriter, r *http.Request) {
	role, ok := r.Context().Value(observability.RoleKey).(string)
	if !ok {
		observability.Log.Warn().Msg("role not found in context - possible middleware misconfiguration")
	}
	if role != "admin" {
		model.WriteError(w, r, model.ErrAdminRequired)
		return
	}

	var req RevokeBanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.RevokeBan(r.Context(), req.PlayerID)
	if err != nil {
		errResponse := model.AppError{
			Code:    "BAN_REVOKE_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}
