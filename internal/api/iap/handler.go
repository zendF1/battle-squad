package iap

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

func (h *Handler) GetProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.service.GetActiveProducts(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if products == nil {
		products = []IAPProduct{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(products)
}

func (h *Handler) VerifyReceipt(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req VerifyReceiptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	trans, err := h.service.VerifyReceipt(r.Context(), playerID, req.ProductID, req.Platform, req.PurchaseToken)
	if err != nil {
		errResponse := model.AppError{
			Code:    "IAP_VERIFICATION_FAILED",
			Message: err.Error(),
			Status:  http.StatusBadRequest,
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(trans)
}
