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
	// EquippedTitle is the title the player wears (active and owned), null when none.
	EquippedTitle *EquippedTitle `json:"equippedTitle"`
}

// EquippedTitle is a title plate: id, Vietnamese name and rarity (blue, orange, purple, red).
type EquippedTitle struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Rarity string `json:"rarity"`
}

// MaxDisplayNameLength is the longest display name, in characters (runes).
const MaxDisplayNameLength = 20

type UpdateProfileRequest struct {
	DisplayName string `json:"displayName"`
}

type AccountDeletionStatusResponse struct {
	Status    string     `json:"status"`              // active, pending_deletion, deleted
	DeletedAt *time.Time `json:"deletedAt,omitempty"` // when grace period ends
}
