package iap

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

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

func (s *Service) GetActiveProducts(ctx context.Context) ([]IAPProduct, error) {
	return s.repo.GetActiveProducts(ctx)
}

func (s *Service) VerifyReceipt(ctx context.Context, playerID, productID, platform, purchaseToken string) (*PaymentTransaction, error) {
	if productID == "" || platform == "" || purchaseToken == "" {
		return nil, errors.New("missing parameters for verification")
	}

	// 1. Fetch product config
	product, err := s.repo.GetProductByID(ctx, productID)
	if err != nil {
		return nil, err
	}
	if product == nil || !product.IsActive {
		return nil, errors.New("product not active or does not exist")
	}

	// 2. Perform mock Store verification and get a unique Store transaction ID
	storeTransactionID, err := s.mockStoreVerification(ctx, platform, purchaseToken)
	if err != nil {
		return nil, fmt.Errorf("receipt verification failed with store: %w", err)
	}

	// 3. Start DB transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 4. Idempotency check: verify if transaction ID has already been processed
	existingTrans, err := s.repo.FindTransactionByID(ctx, storeTransactionID)
	if err != nil {
		return nil, err
	}
	if existingTrans != nil {
		if existingTrans.Status == "verified" {
			// Already verified, return the existing verified transaction details (idempotent success)
			return existingTrans, nil
		}
		return nil, errors.New("transaction was already processed with status: " + existingTrans.Status)
	}

	// 5. Calculate granted gems
	grantedGems := product.GemAmount + product.BonusGemAmount

	// 6. Credit player balance with Gems inside database transaction
	_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "gem", grantedGems, "iap", storeTransactionID)
	if err != nil {
		return nil, fmt.Errorf("failed to credit gems to player: %w", err)
	}

	// 7. Insert payment transaction log
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

	err = s.repo.CreateTransactionTx(ctx, tx, paymentTrans)
	if err != nil {
		return nil, fmt.Errorf("failed to record payment transaction: %w", err)
	}

	// 8. Commit
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit payment: %w", err)
	}

	return paymentTrans, nil
}

func (s *Service) mockStoreVerification(ctx context.Context, platform, purchaseToken string) (string, error) {
	// For testing, generate a deterministic or pseudo-random unique store ID based on token
	// Example token: "mock_token_abc" -> orderId: "GPA.1234-5678-90123" or Apple trans id
	if purchaseToken == "fail_token" {
		return "", errors.New("invalid signature or token from store")
	}

	// Generate a unique store order reference
	h := hex.EncodeToString([]byte(purchaseToken))
	if len(h) > 20 {
		h = h[:20]
	}

	if platform == "android" {
		return fmt.Sprintf("GPA.mock-%s", h), nil
	} else if platform == "ios" {
		return fmt.Sprintf("ios_trans_%s", h), nil
	}

	return "", errors.New("unknown platform")
}
