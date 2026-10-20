package player

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetProfile(ctx context.Context, playerID string) (*PlayerProfile, error) {
	// The equipped title counts only while it is owned and active
	query := `
		SELECT p.player_id, p.account_id, p.display_name, p.level, p.exp, p.coin, p.gem, p.created_at, p.last_login_at,
		       t.title_id, t.name, t.rarity
		FROM player_profiles p
		LEFT JOIN player_titles pt ON pt.player_id = p.player_id AND pt.title_id = p.equipped_title_id
		LEFT JOIN config_titles t ON t.title_id = pt.title_id AND t.is_active = TRUE
		WHERE p.player_id = $1
	`
	var p PlayerProfile
	var titleID, titleName, titleRarity *string
	err := r.db.Pool.QueryRow(ctx, query, playerID).Scan(
		&p.PlayerID,
		&p.AccountID,
		&p.DisplayName,
		&p.Level,
		&p.Exp,
		&p.Coin,
		&p.Gem,
		&p.CreatedAt,
		&p.LastLoginAt,
		&titleID,
		&titleName,
		&titleRarity,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if titleID != nil && titleName != nil && titleRarity != nil {
		p.EquippedTitle = &EquippedTitle{ID: *titleID, Name: *titleName, Rarity: *titleRarity}
	}
	return &p, nil
}

func (r *Repository) UpdateDisplayName(ctx context.Context, playerID, displayName string) error {
	query := `
		UPDATE player_profiles
		SET display_name = $1
		WHERE player_id = $2
	`
	_, err := r.db.Pool.Exec(ctx, query, displayName, playerID)
	return err
}

func (r *Repository) RequestAccountDeletion(ctx context.Context, accountID string, gracePeriod time.Duration) (time.Time, error) {
	targetTime := time.Now().Add(gracePeriod)
	query := `
		UPDATE accounts
		SET status = 'pending_deletion', deleted_at = $1
		WHERE account_id = $2
	`
	_, err := r.db.Pool.Exec(ctx, query, targetTime, accountID)
	if err != nil {
		return time.Time{}, err
	}
	return targetTime, nil
}

func (r *Repository) CancelAccountDeletion(ctx context.Context, accountID string) error {
	query := `
		UPDATE accounts
		SET status = 'active', deleted_at = NULL
		WHERE account_id = $1
	`
	_, err := r.db.Pool.Exec(ctx, query, accountID)
	return err
}

func (r *Repository) GetAccountDeletionStatus(ctx context.Context, accountID string) (string, *time.Time, error) {
	query := `
		SELECT status, deleted_at
		FROM accounts
		WHERE account_id = $1
	`
	var status string
	var deletedAt sql.NullTime
	err := r.db.Pool.QueryRow(ctx, query, accountID).Scan(&status, &deletedAt)
	if err != nil {
		return "", nil, err
	}

	var dAt *time.Time
	if deletedAt.Valid {
		dAt = &deletedAt.Time
	}
	return status, dAt, nil
}
