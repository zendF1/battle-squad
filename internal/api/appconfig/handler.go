package appconfig

import (
	"net/http"

	"battle-squad/internal/shared/model"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) GetVersionPolicy(w http.ResponseWriter, r *http.Request) {
	platform := r.URL.Query().Get("platform")
	if platform == "" {
		platform = "android"
	}

	policy, err := h.service.GetVersionPolicy(r.Context(), platform)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	model.WriteOK(w, policy)
}

func (h *Handler) GetRemoteConfig(w http.ResponseWriter, r *http.Request) {
	config, err := h.service.GetRemoteConfig(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	model.WriteOK(w, config)
}
