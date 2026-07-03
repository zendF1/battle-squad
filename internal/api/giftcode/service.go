package giftcode

import (
	"context"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/shared/database"
)

type Service struct {
	repo          *Repository
	economyRepo   *economy.Repository
	inventoryRepo *inventory.Repository
	db            *database.PostgresDB
}

func NewService(
	repo *Repository,
	economyRepo *economy.Repository,
	inventoryRepo *inventory.Repository,
	db *database.PostgresDB,
) *Service {
	return &Service{
		repo:          repo,
		economyRepo:   economyRepo,
		inventoryRepo: inventoryRepo,
		db:            db,
	}
}

func (s *Service) RedeemCode(ctx context.Context, playerID, code string) error {
	if code == "" {
		return errors.New("code is required")
	}

	// 1. Fetch gift code config
	gc, err := s.repo.GetGiftCode(ctx, code)
	if err != nil {
		return err
	}
	if gc == nil || !gc.IsActive {
		return errors.New("invalid or inactive gift code")
	}

	// Check expiration
	if gc.ExpiredAt != nil && time.Now().After(*gc.ExpiredAt) {
		return errors.New("gift code has expired")
	}

	// Check maximum usage count
	if gc.UsedCount >= gc.MaxUses {
		return errors.New("gift code usage limit reached")
	}

	// 2. Check if player has already redeemed this code
	alreadyRedeemed, err := s.repo.HasRedeemed(ctx, playerID, code)
	if err != nil {
		return err
	}
	if alreadyRedeemed {
		return errors.New("you have already redeemed this code")
	}

	// 3. Start Postgres transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 4. Grant rewards inside transaction
	// Coin reward
	if gc.RewardCoin > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "coin", gc.RewardCoin, "gift_code", code)
		if err != nil {
			return fmt.Errorf("failed to credit coins: %w", err)
		}
	}

	// Gem reward
	if gc.RewardGem > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "gem", gc.RewardGem, "gift_code", code)
		if err != nil {
			return fmt.Errorf("failed to credit gems: %w", err)
		}
	}

	// Item rewards
	for _, item := range gc.RewardItems {
		if item.Quantity > 0 {
			err = s.inventoryRepo.AddItemsTx(ctx, tx, playerID, item.ItemID, item.Quantity, "gift_code", nil)
			if err != nil {
				return fmt.Errorf("failed to credit item %s: %w", item.ItemID, err)
			}
		}
	}

	// 5. Update used counts and log redemption
	err = s.repo.RedeemTx(ctx, tx, playerID, code)
	if err != nil {
		return fmt.Errorf("failed to process gift code redemption: %w", err)
	}

	// 6. Commit
	return tx.Commit(ctx)
}
