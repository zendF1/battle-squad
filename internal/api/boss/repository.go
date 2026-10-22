package boss

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

// RunsOn returns the player's boss runs on day: boss id → runs (player_boss_runs).
func (r *Repository) RunsOn(ctx context.Context, playerID, day string) (map[string]int, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT boss_id, runs FROM player_boss_runs WHERE player_id = $1 AND day = $2`, playerID, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var runs int
		if err := rows.Scan(&id, &runs); err != nil {
			return nil, err
		}
		out[id] = runs
	}
	return out, rows.Err()
}
