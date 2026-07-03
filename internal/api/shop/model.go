package shop

import "time"

type ShopOffer struct {
	OfferID       string     `json:"offerId"`
	ItemID        string     `json:"itemId"`
	OfferType     string     `json:"offerType"` // item, character_unlock, bundle
	PriceCurrency string     `json:"priceCurrency"` // coin, gem
	PriceAmount   int        `json:"priceAmount"`
	Quantity      int        `json:"quantity"`
	LimitPerPlayer *int       `json:"limitPerPlayer,omitempty"`
	StartsAt      *time.Time `json:"startsAt,omitempty"`
	EndsAt        *time.Time `json:"endsAt,omitempty"`
	IsActive      bool       `json:"isActive"`
}

type ShopPurchase struct {
	PurchaseID      string    `json:"purchaseId"`
	PlayerID        string    `json:"playerId"`
	OfferID         string    `json:"offerId"`
	PriceCurrency   string    `json:"priceCurrency"`
	PriceAmount     int       `json:"priceAmount"`
	QuantityGranted int       `json:"quantityGranted"`
	Status          string    `json:"status"` // completed, failed, refunded
	CreatedAt       time.Time `json:"createdAt"`
}

type PurchaseRequest struct {
	OfferID        string `json:"offerId"`
	IdempotencyKey string `json:"idempotencyKey"`
}
