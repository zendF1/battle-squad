package moderation

import "time"

type PlayerReport struct {
	ReportID         string     `json:"reportId"`
	ReporterPlayerID string     `json:"reporterPlayerId"`
	TargetPlayerID   string     `json:"targetPlayerId"`
	MatchID          *string    `json:"matchId,omitempty"`
	Category         string     `json:"category"` // cheat, abuse, afk, payment_fraud, other
	Description      *string    `json:"description,omitempty"`
	Status           string     `json:"status"` // open, reviewing, actioned, rejected
	CreatedAt        time.Time  `json:"createdAt"`
	ReviewedAt       *time.Time `json:"reviewedAt,omitempty"`
	ReviewedBy       *string    `json:"reviewedBy,omitempty"`
}

type AccountBan struct {
	BanID         string     `json:"banId"`
	AccountID     string     `json:"accountId"`
	PlayerID      string     `json:"playerId"`
	ReasonCode    string     `json:"reasonCode"`
	ReasonText    string     `json:"reasonText"`
	Source        string     `json:"source"` // anti_cheat, moderator, payment_fraud, system
	StartsAt      time.Time  `json:"startsAt"`
	EndsAt        *time.Time `json:"endsAt,omitempty"` // null for permanent
	Status        string     `json:"status"` // active, expired, revoked
	EvidenceRefID *string    `json:"evidenceRefId,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

type ReportRequest struct {
	TargetPlayerID string  `json:"targetPlayerId"`
	MatchID        *string `json:"matchId,omitempty"`
	Category       string  `json:"category"`
	Description    *string `json:"description,omitempty"`
}

type BanRequest struct {
	PlayerID      string `json:"playerId"`
	ReasonCode    string `json:"reasonCode"`
	ReasonText    string `json:"reasonText"`
	DurationHours *int   `json:"durationHours,omitempty"` // empty for permanent
}

type RevokeBanRequest struct {
	PlayerID string `json:"playerId"`
}
