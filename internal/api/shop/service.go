package shop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/idempotency"
)

type Service struct {
	repo         *Repository
	economyRepo  *economy.Repository
	inventoryRepo *inventory.Repository
	db           *database.PostgresDB
	idempotency  *idempotency.Manager
}

func NewService(
	repo *Repository,
	economyRepo *economy.Repository,
	inventoryRepo *inventory.Repository,
	db *database.PostgresDB,
	idempotency *idempotency.Manager,
) *Service {
	return &Service{
		repo:          repo,
		economyRepo:   economyRepo,
		inventoryRepo: inventoryRepo,
		db:            db,
		idempotency:   idempotency,
	}
}

func (s *Service) GetActiveOffers(ctx context.Context) ([]ShopOffer, error) {
	return s.repo.GetActiveOffers(ctx)
}

func (s *Service) Purchase(ctx context.Context, playerID, offerID, idempotencyKey string) error {
	if offerID == "" {
		return errors.New("offer ID is required")
	}

	// 1. Handle idempotency check
	if idempotencyKey != "" {
		key := fmt.Sprintf("shop_purchase:%s:%s", playerID, idempotencyKey)
		duplicate, err := s.idempotency.CheckAndSet(ctx, key, 24*time.Hour)
		if err != nil {
			return fmt.Errorf("idempotency check error: %w", err)
		}
		if duplicate {
			return nil // Duplicate request - return success (idempotent)
		}
		defer func() {
			s.idempotency.Complete(ctx, key, 24*time.Hour)
		}()
	}

	// 2. Start PostgreSQL transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 3. Fetch offer details
	offer, err := s.repo.GetOfferByID(ctx, offerID)
	if err != nil {
		return err
	}
	if offer == nil || !offer.IsActive {
		return errors.New("offer is not active or does not exist")
	}

	// Check offer date validation
	now := time.Now()
	if offer.StartsAt != nil && now.Before(*offer.StartsAt) {
		return errors.New("offer has not started yet")
	}
	if offer.EndsAt != nil && now.After(*offer.EndsAt) {
		return errors.New("offer has expired")
	}

	// 4. Check player purchase limit
	if offer.LimitPerPlayer != nil {
		purchasedCount, err := s.repo.GetPlayerPurchaseCount(ctx, playerID, offerID)
		if err != nil {
			return err
		}
		if purchasedCount >= *offer.LimitPerPlayer {
			return errors.New("purchase limit exceeded for this offer")
		}
	}

	// 5. Debit currency balance
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, offer.PriceCurrency, offer.PriceAmount, "shop_purchase", offerID, false)
	if err != nil {
		return err // Balance check failure (e.g. Insufficient Balance)
	}

	// 6. Grant item/character depending on offerType
	if offer.OfferType == "item" || offer.OfferType == "bundle" {
		err = s.inventoryRepo.AddItemsTx(ctx, tx, playerID, offer.ItemID, offer.Quantity, "shop_purchase", nil)
		if err != nil {
			return fmt.Errorf("failed to grant item: %w", err)
		}
	} else if offer.OfferType == "character_unlock" {
		// Insert character unlock
		queryUnlock := `
			INSERT INTO player_characters (player_id, character_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`
		_, err = tx.Exec(ctx, queryUnlock, playerID, offer.ItemID)
		if err != nil {
			return fmt.Errorf("failed to unlock character: %w", err)
		}
	} else {
		return fmt.Errorf("unsupported offer type: %s", offer.OfferType)
	}

	// 7. Write purchase record
	purchaseID := generateID()
	purchaseRecord := &ShopPurchase{
		PurchaseID:      purchaseID,
		PlayerID:        playerID,
		OfferID:         offerID,
		PriceCurrency:   offer.PriceCurrency,
		PriceAmount:     offer.PriceAmount,
		QuantityGranted: offer.Quantity,
		Status:          "completed",
	}
	err = s.repo.CreateShopPurchaseTx(ctx, tx, purchaseRecord)
	if err != nil {
		return fmt.Errorf("failed to record purchase: %w", err)
	}

	// 8. Commit transaction
	return tx.Commit(ctx)
}

func (s *Service) GetPlayerPurchases(ctx context.Context, playerID string) ([]ShopPurchase, error) {
	return s.repo.GetPlayerPurchases(ctx, playerID)
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}
