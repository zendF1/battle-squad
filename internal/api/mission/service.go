package mission

import (
	"context"
	"errors"
	"fmt"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
	}
}

func (s *Service) GetMissions(ctx context.Context, playerID string, missionType string) ([]MissionProgress, error) {
	return s.repo.GetPlayerMissions(ctx, playerID, missionType)
}

func (s *Service) ClaimReward(ctx context.Context, playerID, missionID string) error {
	// 1. Fetch current progress
	progress, err := s.repo.GetMissionProgress(ctx, playerID, missionID)
	if err != nil {
		return err
	}
	if progress == nil {
		return errors.New("mission not found")
	}

	// 2. Validate claim criteria
	if progress.IsClaimed {
		return errors.New("reward already claimed")
	}
	if progress.CurrentValue < progress.RequiredValue {
		return errors.New("mission criteria not completed yet")
	}

	// 3. Start transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 4. Grant rewards
	// Coin
	if progress.RewardCoin > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "coin", progress.RewardCoin, "mission", missionID)
		if err != nil {
			return fmt.Errorf("failed to credit coins: %w", err)
		}
	}

	// Gem
	if progress.RewardGem > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "gem", progress.RewardGem, "mission", missionID)
		if err != nil {
			return fmt.Errorf("failed to credit gems: %w", err)
		}
	}

	// 5. Update claim status
	err = s.repo.MarkClaimedTx(ctx, tx, playerID, missionID)
	if err != nil {
		return fmt.Errorf("failed to mark mission as claimed: %w", err)
	}

	// 6. Commit
	return tx.Commit(ctx)
}
