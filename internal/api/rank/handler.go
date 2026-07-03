package rank

import (
	"encoding/json"
	"net/http"
	"strconv"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetRankMe(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	pr, err := h.service.GetPlayerRank(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(pr)
}

func (h *Handler) GetLeaderboard(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil {
			page = p
		}
	}

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil {
			limit = l
		}
	}

	resp, err := h.service.GetLeaderboard(r.Context(), page, limit)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) GetCurrentSeason(w http.ResponseWriter, r *http.Request) {
	season, err := h.service.GetCurrentSeason(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(season)
}

func (h *Handler) ClaimReward(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req ClaimSeasonRewardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	coin, gem, err := h.service.ClaimSeasonReward(r.Context(), playerID, req.SeasonID)
	if err != nil {
		errResponse := model.AppError{
			Code:    "SEASON_REWARD_CLAIM_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"rewards": map[string]interface{}{
			"coin": coin,
			"gem":  gem,
		},
	})
}
