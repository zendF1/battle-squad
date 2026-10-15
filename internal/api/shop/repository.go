package shop

import (
	"context"
	"database/sql"
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

func (r *Repository) GetActiveOffers(ctx context.Context) ([]ShopOffer, error) {
	query := `
		SELECT offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, starts_at, ends_at, is_active
		FROM shop_offers
		WHERE is_active = TRUE 
		  AND (starts_at IS NULL OR starts_at <= CURRENT_TIMESTAMP)
		  AND (ends_at IS NULL OR ends_at >= CURRENT_TIMESTAMP)
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var offers []ShopOffer
	for rows.Next() {
		var o ShopOffer
		var limit sql.NullInt32
		var startsAt, endsAt sql.NullTime
		err := rows.Scan(
			&o.OfferID,
			&o.ItemID,
			&o.OfferType,
			&o.PriceCurrency,
			&o.PriceAmount,
			&o.Quantity,
			&limit,
			&startsAt,
			&endsAt,
			&o.IsActive,
		)
		if err != nil {
			return nil, err
		}
		if limit.Valid {
			val := int(limit.Int32)
			o.LimitPerPlayer = &val
		}
		if startsAt.Valid {
			o.StartsAt = &startsAt.Time
		}
		if endsAt.Valid {
			o.EndsAt = &endsAt.Time
		}
		offers = append(offers, o)
	}

	return offers, nil
}

// LockPlayerTx serialises purchases per player: limit checks and idempotency
// lookups after this lock see every previously committed purchase.
func (r *Repository) LockPlayerTx(ctx context.Context, tx pgx.Tx, playerID string) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("player profile not found")
	}
	return err
}

func (r *Repository) GetOfferByIDTx(ctx context.Context, tx pgx.Tx, offerID string) (*ShopOffer, error) {
	query := `
		SELECT offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, starts_at, ends_at, is_active
		FROM shop_offers
		WHERE offer_id = $1
	`
	var o ShopOffer
	var limit sql.NullInt32
	var startsAt, endsAt sql.NullTime
	err := tx.QueryRow(ctx, query, offerID).Scan(
		&o.OfferID,
		&o.ItemID,
		&o.OfferType,
		&o.PriceCurrency,
		&o.PriceAmount,
		&o.Quantity,
		&limit,
		&startsAt,
		&endsAt,
		&o.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if limit.Valid {
		val := int(limit.Int32)
		o.LimitPerPlayer = &val
	}
	if startsAt.Valid {
		o.StartsAt = &startsAt.Time
	}
	if endsAt.Valid {
		o.EndsAt = &endsAt.Time
	}
	return &o, nil
}

func (r *Repository) GetPlayerPurchaseCountTx(ctx context.Context, tx pgx.Tx, playerID, offerID string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM shop_purchases
		WHERE player_id = $1 AND offer_id = $2 AND status = 'completed'
	`
	var count int
	err := tx.QueryRow(ctx, query, playerID, offerID).Scan(&count)
	return count, err
}

func (r *Repository) FindPurchaseByIdempotencyKeyTx(ctx context.Context, tx pgx.Tx, playerID, idempotencyKey string) (*ShopPurchase, error) {
	query := `
		SELECT purchase_id, player_id, offer_id, price_currency, price_amount, quantity_granted, status, created_at
		FROM shop_purchases
		WHERE player_id = $1 AND idempotency_key = $2
	`
	var p ShopPurchase
	err := tx.QueryRow(ctx, query, playerID, idempotencyKey).Scan(
		&p.PurchaseID,
		&p.PlayerID,
		&p.OfferID,
		&p.PriceCurrency,
		&p.PriceAmount,
		&p.QuantityGranted,
		&p.Status,
		&p.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) CreateShopPurchaseTx(ctx context.Context, tx pgx.Tx, purchase *ShopPurchase, idempotencyKey string) error {
	query := `
		INSERT INTO shop_purchases (purchase_id, player_id, offer_id, price_currency, price_amount, quantity_granted, status, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''))
		RETURNING created_at
	`
	return tx.QueryRow(
		ctx,
		query,
		purchase.PurchaseID,
		purchase.PlayerID,
		purchase.OfferID,
		purchase.PriceCurrency,
		purchase.PriceAmount,
		purchase.QuantityGranted,
		purchase.Status,
		idempotencyKey,
	).Scan(&purchase.CreatedAt)
}

func (r *Repository) GetPlayerPurchases(ctx context.Context, playerID string) ([]ShopPurchase, error) {
	query := `
		SELECT purchase_id, player_id, offer_id, price_currency, price_amount, quantity_granted, status, created_at
		FROM shop_purchases
		WHERE player_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var purchases []ShopPurchase
	for rows.Next() {
		var p ShopPurchase
		err := rows.Scan(
			&p.PurchaseID,
			&p.PlayerID,
			&p.OfferID,
			&p.PriceCurrency,
			&p.PriceAmount,
			&p.QuantityGranted,
			&p.Status,
			&p.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		purchases = append(purchases, p)
	}

	return purchases, nil
}

// UnlockCharacterTx returns false when the player already owns the character.
func (r *Repository) UnlockCharacterTx(ctx context.Context, tx pgx.Tx, playerID, characterID string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO player_characters (player_id, character_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, playerID, characterID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
