package auth

import "time"

type Account struct {
	AccountID        string     `json:"accountId"`
	AccountType      string     `json:"accountType"`
	Status           string     `json:"status"`
	PrimaryPlayerID  string     `json:"primaryPlayerId"`
	CreatedAt        time.Time  `json:"createdAt"`
	LastLoginAt      time.Time  `json:"lastLoginAt"`
	DeletedAt        *time.Time `json:"deletedAt"`
}

type AuthIdentity struct {
	IdentityID     string    `json:"identityId"`
	AccountID      string    `json:"accountId"`
	Provider       string    `json:"provider"`
	ProviderUserID string    `json:"providerUserId"`
	EmailHash      *string   `json:"emailHash"`
	CreatedAt      time.Time `json:"createdAt"`
	LastUsedAt     time.Time `json:"lastUsedAt"`
}

type Session struct {
	SessionID    string     `json:"sessionId"`
	AccountID    string     `json:"accountId"`
	AccessToken  string     `json:"accessToken"`
	RefreshToken string     `json:"refreshToken"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	RevokedAt    *time.Time `json:"revokedAt"`
}

type GuestLoginRequest struct {
	DeviceInstallID string `json:"deviceInstallId"`
}

type ProviderLoginRequest struct {
	Provider string `json:"provider"` // google, apple
	IDToken  string `json:"idToken"`
}

type LinkProviderRequest struct {
	Provider string `json:"provider"`
	IDToken  string `json:"idToken"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type LoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	PlayerID     string `json:"playerId"`
	DisplayName  string `json:"displayName"`
	Level        int    `json:"level"`
}
