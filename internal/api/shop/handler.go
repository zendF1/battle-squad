package shop

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

func (h *Handler) GetOffers(w http.ResponseWriter, r *http.Request) {
	offers, err := h.service.GetActiveOffers(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if offers == nil {
		offers = []ShopOffer{}
	}

	model.WriteOK(w, offers)
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

	purchase, err := h.service.Purchase(r.Context(), playerID, req.OfferID, req.IdempotencyKey)
	if err != nil {
		model.WriteServiceError(w, r, purchaseError(err))
		return
	}

	model.WriteOK(w, purchase)
}

// purchaseError maps known service errors to API errors; anything else is
// returned unchanged so WriteServiceError logs it and answers 500.
func purchaseError(err error) error {
	switch {
	case err.Error() == "insufficient coin balance", err.Error() == "insufficient gem balance":
		return model.ErrInsufficientBalance
	case errors.Is(err, ErrOfferRequired), errors.Is(err, ErrIdempotencyKeyTooLong):
		return model.AppError{Code: model.ErrBadRequest.Code, Message: err.Error(), Status: http.StatusBadRequest}
	case errors.Is(err, ErrOfferInactive), errors.Is(err, ErrOfferNotStarted), errors.Is(err, ErrOfferExpired):
		return model.AppError{Code: "SHOP_OFFER_INACTIVE", Message: err.Error(), Status: http.StatusNotFound}
	case errors.Is(err, ErrLimitExceeded):
		return model.ErrItemOutOfStock
	case errors.Is(err, ErrCharacterAlreadyOwned):
		return model.ErrShopAlreadyOwned
	case errors.Is(err, ErrIdempotencyKeyMismatch):
		return model.ErrIdempotencyKeyReused
	default:
		return err
	}
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

	model.WriteOK(w, purchases)
}
