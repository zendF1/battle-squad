package title

import (
	"context"
	"errors"
	"time"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

// querier is a pool or a tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// Titles returns the active titles by sort order then id with the player's ownership.
func (r *Repository) Titles(ctx context.Context, q querier, playerID string) ([]Title, error) {
	rows, err := q.Query(ctx, `
		SELECT t.title_id, t.name, t.rarity, t.description, pt.obtained_at
		FROM config_titles t
		LEFT JOIN player_titles pt ON pt.title_id = t.title_id AND pt.player_id = $1
		WHERE t.is_active = TRUE
		ORDER BY t.sort_order, t.title_id`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Title{}
	for rows.Next() {
		var t Title
		var obtained *time.Time
		if err := rows.Scan(&t.ID, &t.Name, &t.Rarity, &t.Description, &obtained); err != nil {
			return nil, err
		}
		if obtained != nil {
			s := obtained.UTC().Format(time.RFC3339)
			t.Owned, t.ObtainedAt = true, &s
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Equipped returns the player's equipped title when it is active and owned, else nil.
func (r *Repository) Equipped(ctx context.Context, q querier, playerID string) (*Ref, error) {
	var ref Ref
	err := q.QueryRow(ctx, `
		SELECT t.title_id, t.name, t.rarity
		FROM player_profiles p
		JOIN player_titles pt ON pt.player_id = p.player_id AND pt.title_id = p.equipped_title_id
		JOIN config_titles t ON t.title_id = pt.title_id AND t.is_active = TRUE
		WHERE p.player_id = $1`, playerID).Scan(&ref.ID, &ref.Name, &ref.Rarity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// OwnedTx returns the title when the player owns it and it is active, else nil.
func (r *Repository) OwnedTx(ctx context.Context, tx pgx.Tx, playerID, titleID string) (*Ref, error) {
	var ref Ref
	err := tx.QueryRow(ctx, `
		SELECT t.title_id, t.name, t.rarity
		FROM player_titles pt
		JOIN config_titles t ON t.title_id = pt.title_id AND t.is_active = TRUE
		WHERE pt.player_id = $1 AND pt.title_id = $2`, playerID, titleID).Scan(&ref.ID, &ref.Name, &ref.Rarity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// LockProfileTx locks the player's profile row; false when there is none.
func (r *Repository) LockProfileTx(ctx context.Context, tx pgx.Tx, playerID string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// SetEquippedTx writes player_profiles.equipped_title_id ("" = NULL); false when no row
// was updated.
func (r *Repository) SetEquippedTx(ctx context.Context, tx pgx.Tx, playerID, titleID string) (bool, error) {
	var id *string
	if titleID != "" {
		id = &titleID
	}
	tag, err := tx.Exec(ctx, `UPDATE player_profiles SET equipped_title_id = $2 WHERE player_id = $1`, playerID, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
