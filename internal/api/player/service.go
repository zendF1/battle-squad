package player

import (
	"context"
	"errors"
	"time"
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

func (s *Service) UpdateDisplayName(ctx context.Context, playerID, displayName string) error {
	if displayName == "" {
		return errors.New("display name cannot be empty")
	}
	if len(displayName) > 50 {
		return errors.New("display name is too long")
	}
	return s.repo.UpdateDisplayName(ctx, playerID, displayName)
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
