package shop

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

func (h *Handler) GetOffers(w http.ResponseWriter, r *http.Request) {
	offers, err := h.service.GetActiveOffers(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if offers == nil {
		offers = []ShopOffer{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(offers)
}

func (h *Handler) Purchase(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var req PurchaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	err := h.service.Purchase(r.Context(), playerID, req.OfferID, req.IdempotencyKey)
	if err != nil {
		// Map errors appropriately
		var errResponse model.AppError
		if err.Error() == "insufficient coin balance" {
			errResponse = model.ErrInsufficientBalance
		} else if err.Error() == "insufficient gem balance" {
			errResponse = model.ErrInsufficientBalance
		} else if err.Error() == "offer is not active or does not exist" {
			errResponse = model.AppError{
				Code:    "SHOP_OFFER_INACTIVE",
				Message: err.Error(),
				Status:  http.StatusNotFound,
			}
		} else if err.Error() == "purchase limit exceeded for this offer" {
			errResponse = model.ErrItemOutOfStock
		} else {
			errResponse = model.AppError{
				Code:    "SHOP_PURCHASE_FAILED",
				Message: err.Error(),
				Status:  http.StatusBadRequest,
			}
		}
		model.WriteError(w, r, errResponse)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) GetPurchases(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	purchases, err := h.service.GetPlayerPurchases(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if purchases == nil {
		purchases = []ShopPurchase{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(purchases)
}
