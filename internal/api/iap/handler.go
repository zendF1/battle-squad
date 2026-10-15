package iap

import (
	"encoding/json"
	"errors"
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

	model.WriteOK(w, products)
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
		model.WriteServiceError(w, r, verifyError(err))
		return
	}

	model.WriteOK(w, trans)
}

// verifyError maps known service errors to API errors; anything else is
// returned unchanged so WriteServiceError logs it and answers 500.
func verifyError(err error) error {
	switch {
	case errors.Is(err, ErrMissingParams):
		return model.AppError{Code: model.ErrBadRequest.Code, Message: err.Error(), Status: http.StatusBadRequest}
	case errors.Is(err, ErrProductInactive):
		return model.ErrIAPProductInactive
	case errors.Is(err, ErrStoreNotConfigured):
		return model.ErrIAPStoreNotConfigured
	case errors.Is(err, ErrReceiptInvalid):
		return model.ErrIAPReceiptInvalid
	case errors.Is(err, ErrTransactionNotOwned), errors.Is(err, ErrTransactionConflict):
		return model.ErrIAPTransactionConflict
	default:
		return err
	}
}
