package inventory

import "time"

type InventoryItem struct {
	PlayerID   string     `json:"playerId"`
	ItemID     string     `json:"itemId"`
	Quantity   int        `json:"quantity"`
	Source     string     `json:"source"`
	AcquiredAt time.Time  `json:"acquiredAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type InventoryReservation struct {
	ReservationID string    `json:"reservationId"`
	PlayerID      string    `json:"playerId"`
	MatchID       string    `json:"matchId"`
	ItemID        string    `json:"itemId"`
	Quantity      int       `json:"quantity"`
	Status        string    `json:"status"` // reserved, consumed, released
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}
