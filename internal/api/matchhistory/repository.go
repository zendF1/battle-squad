package matchhistory

import (
	"context"

	"battle-squad/internal/shared/database"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetPlayerHistory(ctx context.Context, playerID string, limit, offset int) ([]MatchHistoryEntry, error) {
	query := `
		SELECT match_id, mode, map_id, result, exp_gained, coin_gained, rating_change, played_at
		FROM match_histories
		WHERE player_id = $1
		ORDER BY played_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []MatchHistoryEntry
	for rows.Next() {
		var e MatchHistoryEntry
		err := rows.Scan(
			&e.MatchID,
			&e.Mode,
			&e.MapID,
			&e.Result,
			&e.ExpGained,
			&e.CoinGained,
			&e.RatingChange,
			&e.PlayedAt,
		)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []MatchHistoryEntry{}
	}

	return entries, nil
}
