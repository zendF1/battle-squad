package iap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetActiveProducts(ctx context.Context) ([]IAPProduct, error) {
	query := `
		SELECT product_id, platform_sku_id, gem_amount, bonus_gem_amount, price_usd, is_active
		FROM iap_products
		WHERE is_active = TRUE
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []IAPProduct
	for rows.Next() {
		var p IAPProduct
		var skuBytes []byte
		err := rows.Scan(
			&p.ProductID,
			&skuBytes,
			&p.GemAmount,
			&p.BonusGemAmount,
			&p.PriceUSD,
			&p.IsActive,
		)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(skuBytes, &p.PlatformSkuID); err != nil {
			return nil, err
		}
		products = append(products, p)
	}

	return products, nil
}

func (r *Repository) GetProductByID(ctx context.Context, productID string) (*IAPProduct, error) {
	query := `
		SELECT product_id, platform_sku_id, gem_amount, bonus_gem_amount, price_usd, is_active
		FROM iap_products
		WHERE product_id = $1
	`
	var p IAPProduct
	var skuBytes []byte
	err := r.db.Pool.QueryRow(ctx, query, productID).Scan(
		&p.ProductID,
		&skuBytes,
		&p.GemAmount,
		&p.BonusGemAmount,
		&p.PriceUSD,
		&p.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(skuBytes, &p.PlatformSkuID); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) FindTransactionByID(ctx context.Context, transactionID string) (*PaymentTransaction, error) {
	query := `
		SELECT transaction_id, player_id, product_id, platform, purchase_token, amount_usd, gem_granted, status, created_at, verified_at, refunded_at
		FROM payment_transactions
		WHERE transaction_id = $1
	`
	var t PaymentTransaction
	var verifiedAt, refundedAt sql.NullTime
	err := r.db.Pool.QueryRow(ctx, query, transactionID).Scan(
		&t.TransactionID,
		&t.PlayerID,
		&t.ProductID,
		&t.Platform,
		&t.PurchaseToken,
		&t.AmountUSD,
		&t.GemGranted,
		&t.Status,
		&t.CreatedAt,
		&verifiedAt,
		&refundedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if verifiedAt.Valid {
		t.VerifiedAt = &verifiedAt.Time
	}
	if refundedAt.Valid {
		t.RefundedAt = &refundedAt.Time
	}
	return &t, nil
}

func (r *Repository) CreateTransactionTx(ctx context.Context, tx pgx.Tx, t *PaymentTransaction) error {
	query := `
		INSERT INTO payment_transactions (transaction_id, player_id, product_id, platform, purchase_token, amount_usd, gem_granted, status, verified_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(
		ctx,
		query,
		t.TransactionID,
		t.PlayerID,
		t.ProductID,
		t.Platform,
		t.PurchaseToken,
		t.AmountUSD,
		t.GemGranted,
		t.Status,
		t.VerifiedAt,
	)
	return err
}
