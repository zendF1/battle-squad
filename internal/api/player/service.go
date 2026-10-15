package player

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"battle-squad/internal/shared/model"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetProfile(ctx context.Context, playerID string) (*PlayerProfile, error) {
	profile, err := s.repo.GetProfile(ctx, playerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, errors.New("profile not found")
	}
	return profile, nil
}

// UpdateDisplayName stores the name with surrounding whitespace trimmed; it
// must be 1 to MaxDisplayNameLength characters.
func (s *Service) UpdateDisplayName(ctx context.Context, playerID, displayName string) error {
	name, err := normalizeDisplayName(displayName)
	if err != nil {
		return err
	}
	return s.repo.UpdateDisplayName(ctx, playerID, name)
}

func normalizeDisplayName(displayName string) (string, error) {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return "", model.ErrDisplayNameEmpty
	}
	if utf8.RuneCountInString(name) > MaxDisplayNameLength {
		return "", model.ErrDisplayNameTooLong
	}
	return name, nil
}

func (s *Service) RequestAccountDeletion(ctx context.Context, accountID string) (time.Time, error) {
	// Grace period is 7 days as standard in MVP/Launch Readiness Spec
	return s.repo.RequestAccountDeletion(ctx, accountID, 7*24*time.Hour)
}

func (s *Service) CancelAccountDeletion(ctx context.Context, accountID string) error {
	return s.repo.CancelAccountDeletion(ctx, accountID)
}

func (s *Service) GetAccountDeletionStatus(ctx context.Context, accountID string) (string, *time.Time, error) {
	return s.repo.GetAccountDeletionStatus(ctx, accountID)
}
