package mission

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

// GetDailyMissions answers GET /missions/daily with the DailyState.
func (h *Handler) GetDailyMissions(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	st, err := h.service.Daily(r.Context(), playerID)
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, st)
}

// ClaimMission answers POST /missions/claim {"missionId"} with {activity, state}.
func (h *Handler) ClaimMission(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req ClaimMissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	res, err := h.service.ClaimMission(r.Context(), playerID, req.MissionID)
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, res)
}

// ClaimChest answers POST /missions/chests/claim {"chestId"} with {rewards, state}.
func (h *Handler) ClaimChest(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req ClaimChestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	res, err := h.service.ClaimChest(r.Context(), playerID, req.ChestID)
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	model.WriteOK(w, res)
}

func (h *Handler) GetAchievements(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	progresses, err := h.service.GetMissions(r.Context(), playerID, "achievement")
	if err != nil {
		model.WriteServiceError(w, r, err)
		return
	}
	if progresses == nil {
		progresses = []MissionProgress{}
	}
	model.WriteOK(w, progresses)
}
