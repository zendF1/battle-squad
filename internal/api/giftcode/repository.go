package giftcode

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

func (r *Repository) GetGiftCode(ctx context.Context, code string) (*GiftCode, error) {
	query := `
		SELECT code, reward_coin, reward_gem, reward_items, max_uses, used_count, expired_at, is_active
		FROM gift_codes
		WHERE code = $1
	`
	var gc GiftCode
	var itemsBytes []byte
	var expiredAt sql.NullTime
	err := r.db.Pool.QueryRow(ctx, query, code).Scan(
		&gc.Code,
		&gc.RewardCoin,
		&gc.RewardGem,
		&itemsBytes,
		&gc.MaxUses,
		&gc.UsedCount,
		&expiredAt,
		&gc.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(itemsBytes, &gc.RewardItems); err != nil {
		return nil, err
	}
	if expiredAt.Valid {
		gc.ExpiredAt = &expiredAt.Time
	}
	return &gc, nil
}

func (r *Repository) HasRedeemed(ctx context.Context, playerID, code string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM gift_code_redemptions
			WHERE player_id = $1 AND code = $2
		)
	`
	var exists bool
	err := r.db.Pool.QueryRow(ctx, query, playerID, code).Scan(&exists)
	return exists, err
}

func (r *Repository) RedeemTx(ctx context.Context, tx pgx.Tx, playerID, code string) error {
	// 1. Increment used count
	queryInc := `
		UPDATE gift_codes
		SET used_count = used_count + 1
		WHERE code = $1
	`
	_, err := tx.Exec(ctx, queryInc, code)
	if err != nil {
		return err
	}

	// 2. Insert redemption record
	queryInsert := `
		INSERT INTO gift_code_redemptions (player_id, code)
		VALUES ($1, $2)
	`
	_, err = tx.Exec(ctx, queryInsert, playerID, code)
	return err
}
