package gamedata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// Archived is the payload published under version (gamedata_versions); found is false when
// there is none.
func (r *Repository) Archived(ctx context.Context, version string) (json.RawMessage, bool, error) {
	var payload string
	err := r.db.Pool.QueryRow(ctx, `SELECT payload FROM gamedata_versions WHERE version = $1`, version).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("gamedata_versions: %w", err)
	}
	return json.RawMessage(payload), true, nil
}
