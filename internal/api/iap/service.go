package iap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
)

var (
	ErrMissingParams       = errors.New("missing parameters for verification")
	ErrProductInactive     = errors.New("product not active or does not exist")
	ErrStoreNotConfigured  = errors.New("store receipt verification is not configured")
	ErrReceiptInvalid      = errors.New("receipt verification failed with store")
	ErrTransactionNotOwned = errors.New("store transaction belongs to another player")
	ErrTransactionConflict = errors.New("store transaction was already used for a different purchase")
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
	mockStore   bool
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB, mockStore bool) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
		mockStore:   mockStore,
	}
}

func (s *Service) GetActiveProducts(ctx context.Context) ([]IAPProduct, error) {
	return s.repo.GetActiveProducts(ctx)
}

func (s *Service) VerifyReceipt(ctx context.Context, playerID, productID, platform, purchaseToken string) (*PaymentTransaction, error) {
	if productID == "" || platform == "" || purchaseToken == "" {
		return nil, ErrMissingParams
	}

	// 1. Fetch product config
	product, err := s.repo.GetProductByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if product == nil || !product.IsActive {
		return nil, ErrProductInactive
	}

	// 2. Verify with the store and get a unique store transaction ID
	storeTransactionID, err := s.verifyWithStore(ctx, platform, purchaseToken)
	if err != nil {
		return nil, err
	}

	// 3. Start DB transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 4. Claim the store transaction ID; the PK makes a replay a no-op
	grantedGems := product.GemAmount + product.BonusGemAmount
	now := time.Now()
	paymentTrans := &PaymentTransaction{
		TransactionID: storeTransactionID,
		PlayerID:      playerID,
		ProductID:     productID,
		Platform:      platform,
		PurchaseToken: purchaseToken,
		AmountUSD:     product.PriceUSD,
		GemGranted:    grantedGems,
		Status:        "verified",
		VerifiedAt:    &now,
	}
	inserted, err := s.repo.CreateTransactionTx(ctx, tx, paymentTrans)
	if err != nil {
		return nil, fmt.Errorf("failed to record payment transaction: %w", err)
	}
	if !inserted {
		existingTrans, err := s.repo.FindTransactionByIDTx(ctx, tx, storeTransactionID)
		if err != nil {
			return nil, err
		}
		if existingTrans == nil {
			return nil, errors.New("payment transaction conflict, please retry")
		}
		if existingTrans.PlayerID != playerID {
			return nil, ErrTransactionNotOwned
		}
		if existingTrans.ProductID != productID {
			return nil, ErrTransactionConflict
		}
		if existingTrans.Status == "verified" {
			// Already verified, return the existing verified transaction details (idempotent success)
			return existingTrans, nil
		}
		return nil, fmt.Errorf("%w: status %s", ErrTransactionConflict, existingTrans.Status)
	}

	// 5. Credit player balance with Gems inside the same transaction
	_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "gem", grantedGems, "iap", storeTransactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to credit gems to player: %w", err)
	}

	// 6. Commit
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit payment: %w", err)
	}

	return paymentTrans, nil
}

// verifyWithStore returns the store's transaction ID for purchaseToken. Real
// Google Play / App Store verification is not implemented yet, so only the
// mock (IAP_MOCK_ENABLED) can succeed.
func (s *Service) verifyWithStore(ctx context.Context, platform, purchaseToken string) (string, error) {
	if !s.mockStore {
		return "", ErrStoreNotConfigured
	}
	id, err := mockStoreVerification(platform, purchaseToken)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrReceiptInvalid, err)
	}
	return id, nil
}

func mockStoreVerification(platform, purchaseToken string) (string, error) {
	if purchaseToken == "fail_token" {
		return "", errors.New("invalid signature or token from store")
	}

	// Hash the whole token: distinct tokens must map to distinct order IDs
	sum := sha256.Sum256([]byte(purchaseToken))
	h := hex.EncodeToString(sum[:])

	if platform == "android" {
		return fmt.Sprintf("GPA.mock-%s", h), nil
	} else if platform == "ios" {
		return fmt.Sprintf("ios_trans_%s", h), nil
	}

	return "", errors.New("unknown platform")
}
