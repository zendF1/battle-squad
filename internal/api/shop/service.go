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
	"battle-squad/internal/api/mission"
	"battle-squad/internal/shared/database"
)

var (
	ErrOfferRequired          = errors.New("offer ID is required")
	ErrOfferInactive          = errors.New("offer is not active or does not exist")
	ErrOfferNotStarted        = errors.New("offer has not started yet")
	ErrOfferExpired           = errors.New("offer has expired")
	ErrLimitExceeded          = errors.New("purchase limit exceeded for this offer")
	ErrCharacterAlreadyOwned  = errors.New("character already owned")
	ErrIdempotencyKeyMismatch = errors.New("idempotency key was already used for a different offer")
	ErrIdempotencyKeyTooLong  = errors.New("idempotency key is too long")
	// ErrCoinPackMisconfigured is a catalog error (500): a coin pack must be
	// itemId "coin" paid in gem, so coin can never buy coin.
	ErrCoinPackMisconfigured = errors.New("coin_pack offer must have itemId coin and priceCurrency gem")
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

func (s *Service) GetActiveOffers(ctx context.Context) ([]ShopOffer, error) {
	return s.repo.GetActiveOffers(ctx)
}

// Purchase debits the player and grants the offer in one transaction. A retry
// with the same idempotencyKey returns the original purchase without charging
// again; a failed attempt leaves no record, so retrying it runs it afresh.
func (s *Service) Purchase(ctx context.Context, playerID, offerID, idempotencyKey string) (*ShopPurchase, error) {
	if offerID == "" {
		return nil, ErrOfferRequired
	}
	if len(idempotencyKey) > 64 {
		return nil, ErrIdempotencyKeyTooLong
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 1. Lock the player first so concurrent purchases (same key or not) run one at a time
	if err := s.repo.LockPlayerTx(ctx, tx, playerID); err != nil {
		return nil, err
	}

	// 2. Idempotency: a committed purchase with this key is the answer
	if idempotencyKey != "" {
		existing, err := s.repo.FindPurchaseByIdempotencyKeyTx(ctx, tx, playerID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.OfferID != offerID {
				return nil, ErrIdempotencyKeyMismatch
			}
			return existing, nil
		}
	}

	// 3. Fetch offer details
	offer, err := s.repo.GetOfferByIDTx(ctx, tx, offerID)
	if err != nil {
		return nil, err
	}
	if offer == nil || !offer.IsActive {
		return nil, ErrOfferInactive
	}

	now := time.Now()
	if offer.StartsAt != nil && now.Before(*offer.StartsAt) {
		return nil, ErrOfferNotStarted
	}
	if offer.EndsAt != nil && now.After(*offer.EndsAt) {
		return nil, ErrOfferExpired
	}

	// 4. Check player purchase limit (under the player lock)
	if offer.LimitPerPlayer != nil {
		purchasedCount, err := s.repo.GetPlayerPurchaseCountTx(ctx, tx, playerID, offerID)
		if err != nil {
			return nil, err
		}
		if purchasedCount >= *offer.LimitPerPlayer {
			return nil, ErrLimitExceeded
		}
	}

	if offer.OfferType == "coin_pack" && (offer.ItemID != "coin" || offer.PriceCurrency != "gem" || offer.Quantity <= 0) {
		return nil, fmt.Errorf("offer %s: %w", offerID, ErrCoinPackMisconfigured)
	}

	// 5. Debit currency balance
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, offer.PriceCurrency, offer.PriceAmount, "shop_purchase", offerID, false)
	if err != nil {
		return nil, err // Balance check failure (e.g. Insufficient Balance)
	}

	// 6. Grant item/character depending on offerType
	switch offer.OfferType {
	case "item", "bundle":
		err = s.inventoryRepo.AddItemsTx(ctx, tx, playerID, offer.ItemID, offer.Quantity, "shop_purchase", nil)
		if err != nil {
			return nil, fmt.Errorf("failed to grant item: %w", err)
		}
	case "character_unlock":
		unlocked, err := s.repo.UnlockCharacterTx(ctx, tx, playerID, offer.ItemID)
		if err != nil {
			return nil, fmt.Errorf("failed to unlock character: %w", err)
		}
		if !unlocked {
			return nil, ErrCharacterAlreadyOwned // rollback undoes the debit
		}
	case "coin_pack":
		// Gem -> coin exchange: credit in the same tx as the gem debit
		if _, err := s.economyRepo.CreditTx(ctx, tx, playerID, "coin", offer.Quantity, "shop_purchase", offerID); err != nil {
			return nil, fmt.Errorf("failed to credit coin: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported offer type: %s", offer.OfferType)
	}

	// 7. Write purchase record
	purchase := &ShopPurchase{
		PurchaseID:      generateID(),
		PlayerID:        playerID,
		OfferID:         offerID,
		PriceCurrency:   offer.PriceCurrency,
		PriceAmount:     offer.PriceAmount,
		QuantityGranted: offer.Quantity,
		Status:          "completed",
	}
	if err := s.repo.CreateShopPurchaseTx(ctx, tx, purchase, idempotencyKey); err != nil {
		return nil, fmt.Errorf("failed to record purchase: %w", err)
	}
	if err := mission.RecordShopPurchaseTx(ctx, tx, playerID, now); err != nil {
		return nil, err
	}

	// 8. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return purchase, nil
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
