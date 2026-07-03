package economy

import "time"

type EconomyTransaction struct {
	TransactionID string    `json:"transactionId"`
	PlayerID      string    `json:"playerId"`
	Currency      string    `json:"currency"` // coin, gem
	Amount        int       `json:"amount"`
	BalanceAfter  int       `json:"balanceAfter"`
	Source        string    `json:"source"` // match_reward, mission, iap, gift_code, shop_purchase, refund
	RefID         string    `json:"refId,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}
