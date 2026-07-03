package iap

import "time"

type IAPProduct struct {
	ProductID      string            `json:"productId"`
	PlatformSkuID  map[string]string `json:"platformSkuId"` // {"android": "com.battlesquad.gem.small", "ios": "com.battlesquad.gem.small"}
	GemAmount      int               `json:"gemAmount"`
	BonusGemAmount int               `json:"bonusGemAmount"`
	PriceUSD       float64           `json:"priceUsd"`
	IsActive       bool              `json:"isActive"`
}

type PaymentTransaction struct {
	TransactionID string     `json:"transactionId"`
	PlayerID      string     `json:"playerId"`
	ProductID     string     `json:"productId"`
	Platform      string     `json:"platform"` // android, ios
	PurchaseToken string     `json:"purchaseToken"`
	AmountUSD     float64    `json:"amountUsd"`
	GemGranted    int        `json:"gemGranted"`
	Status        string     `json:"status"` // pending, verified, failed, refunded
	CreatedAt     time.Time  `json:"createdAt"`
	VerifiedAt    *time.Time `json:"verifiedAt,omitempty"`
	RefundedAt    *time.Time `json:"refundedAt,omitempty"`
}

type VerifyReceiptRequest struct {
	ProductID     string `json:"productId"`
	Platform      string `json:"platform"` // android, ios
	PurchaseToken string `json:"purchaseToken"`
}
