package player

import "time"

type PlayerProfile struct {
	PlayerID    string    `json:"playerId"`
	AccountID   string    `json:"accountId"`
	DisplayName string    `json:"displayName"`
	Level       int       `json:"level"`
	Exp         int       `json:"exp"`
	Coin        int       `json:"coin"`
	Gem         int       `json:"gem"`
	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

type UpdateProfileRequest struct {
	DisplayName string `json:"displayName"`
}

type AccountDeletionStatusResponse struct {
	Status    string     `json:"status"`    // active, pending_deletion, deleted
	DeletedAt *time.Time `json:"deletedAt,omitempty"` // when grace period ends
}
