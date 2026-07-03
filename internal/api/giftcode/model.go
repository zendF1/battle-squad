package giftcode

import "time"

type RewardItem struct {
	ItemID   string `json:"itemId"`
	Quantity int    `json:"quantity"`
}

type GiftCode struct {
	Code        string       `json:"code"`
	RewardCoin  int          `json:"rewardCoin"`
	RewardGem   int          `json:"rewardGem"`
	RewardItems []RewardItem `json:"rewardItems"`
	MaxUses     int          `json:"maxUses"`
	UsedCount   int          `json:"usedCount"`
	ExpiredAt   *time.Time   `json:"expiredAt,omitempty"`
	IsActive    bool         `json:"isActive"`
}

type GiftCodeRedemption struct {
	PlayerID   string    `json:"playerId"`
	Code       string    `json:"code"`
	RedeemedAt time.Time `json:"redeemedAt"`
}

type RedeemRequest struct {
	Code string `json:"code"`
}
