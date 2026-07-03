package auth

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/auth"
)

type Service struct {
	repo         *Repository
	redis        *database.RedisClient
	jwtAccess    *auth.JWTManager
	jwtRefresh   *auth.JWTManager
}

func NewService(repo *Repository, redis *database.RedisClient, jwtAccess, jwtRefresh *auth.JWTManager) *Service {
	return &Service{
		repo:       repo,
		redis:      redis,
		jwtAccess:  jwtAccess,
		jwtRefresh: jwtRefresh,
	}
}

func (s *Service) GuestLogin(ctx context.Context, deviceInstallID string) (*LoginResponse, error) {
	if deviceInstallID == "" {
		return nil, errors.New("device install ID is required")
	}

	// 1. Check if guest identity exists
	ident, err := s.repo.FindIdentity(ctx, "guest", deviceInstallID)
	if err != nil {
		return nil, err
	}

	var accountID string
	var playerID string
	var displayName string
	var level int

	if ident != nil {
		accountID = ident.AccountID
		// Get player details
		pID, dName, lvl, err := s.repo.GetPlayerProfileByAccountID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		playerID = pID
		displayName = dName
		level = lvl
		
		// Update login time
		s.repo.UpdateLastLogin(ctx, accountID)
	} else {
		// Create new guest account
		acc, _, pID, err := s.repo.CreateGuestAccount(ctx, deviceInstallID)
		if err != nil {
			return nil, err
		}
		accountID = acc.AccountID
		playerID = pID
		displayName = "Rookie_" + deviceInstallID
		if len(displayName) > 20 {
			displayName = displayName[:20]
		}
		level = 1
	}

	// Double check if account is banned
	acc, err := s.repo.FindAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if acc.Status == "banned" {
		return nil, errors.New("banned")
	}

	return s.generateLoginTokens(ctx, accountID, playerID, displayName, level)
}

func (s *Service) ProviderLogin(ctx context.Context, provider, idToken string) (*LoginResponse, error) {
	if provider != "google" && provider != "apple" {
		return nil, errors.New("unsupported provider")
	}

	// Parse / verify ID token (for MVP, we parse details from mock format or verify cleanly)
	providerUserID, email, err := s.verifyToken(idToken, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to verify ID token: %w", err)
	}

	// 1. Check if identity exists
	ident, err := s.repo.FindIdentity(ctx, provider, providerUserID)
	if err != nil {
		return nil, err
	}

	var accountID string
	var playerID string
	var displayName string
	var level int

	if ident != nil {
		accountID = ident.AccountID
		pID, dName, lvl, err := s.repo.GetPlayerProfileByAccountID(ctx, accountID)
		if err != nil {
			return nil, err
		}
		playerID = pID
		displayName = dName
		level = lvl

		s.repo.UpdateLastLogin(ctx, accountID)
	} else {
		// Create new provider account
		acc, _, pID, err := s.repo.CreateProviderAccount(ctx, provider, providerUserID, email)
		if err != nil {
			return nil, err
		}
		accountID = acc.AccountID
		playerID = pID
		displayName = "SquadPlayer"
		level = 1
	}

	// Ban check
	acc, err := s.repo.FindAccountByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if acc.Status == "banned" {
		return nil, errors.New("banned")
	}

	return s.generateLoginTokens(ctx, accountID, playerID, displayName, level)
}

func (s *Service) LinkProvider(ctx context.Context, accountID, provider, idToken string) error {
	if provider != "google" && provider != "apple" {
		return errors.New("unsupported provider")
	}

	providerUserID, email, err := s.verifyToken(idToken, provider)
	if err != nil {
		return err
	}

	return s.repo.LinkIdentity(ctx, accountID, provider, providerUserID, email)
}

func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// Verify refresh token structure
	claims, err := s.jwtRefresh.Verify(refreshToken)
	if err != nil {
		return nil, err
	}

	// Check if refresh token is in Redis (revocation check)
	redisKey := "session:" + refreshToken
	_, err = s.redis.Client.Get(ctx, redisKey).Result()
	if err != nil {
		return nil, errors.New("invalid or expired session")
	}

	// Get player details
	playerID, displayName, level, err := s.repo.GetPlayerProfileByAccountID(ctx, claims.AccountID)
	if err != nil {
		return nil, err
	}

	// Revoke old session and generate new tokens
	s.redis.Client.Del(ctx, redisKey)

	return s.generateLoginTokens(ctx, claims.AccountID, playerID, displayName, level)
}

func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	redisKey := "session:" + refreshToken
	return s.redis.Client.Del(ctx, redisKey).Err()
}

func (s *Service) generateLoginTokens(ctx context.Context, accountID, playerID, displayName string, level int) (*LoginResponse, error) {
	accessToken, err := s.jwtAccess.Generate(accountID, playerID)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.jwtRefresh.Generate(accountID, playerID)
	if err != nil {
		return nil, err
	}

	// Store refresh token in Redis with 30 days expiration
	redisKey := "session:" + refreshToken
	val := fmt.Sprintf("%s:%s", accountID, playerID)
	err = s.redis.Client.Set(ctx, redisKey, val, 30*24*time.Hour).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to save session in cache: %w", err)
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		PlayerID:     playerID,
		DisplayName:  displayName,
		Level:        level,
	}, nil
}

func (s *Service) verifyToken(token, provider string) (string, string, error) {
	// MVP mock verification: if token is simple string or starts with "mock_", we extract it
	// In production, parse/validate Apple Client ID or Google OAuth verification
	if token == "" {
		return "", "", errors.New("empty ID token")
	}

	// Let's generate consistent mock sub from token value
	mockSub := "mock_sub_" + hex.EncodeToString([]byte(token))
	if len(mockSub) > 50 {
		mockSub = mockSub[:50]
	}
	mockEmail := provider + "@example.com"

	return mockSub, mockEmail, nil
}
