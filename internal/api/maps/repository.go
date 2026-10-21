package maps

import (
	"context"
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

const mapColumns = `map_id, name, theme, width, height, grid_width, grid_height, cell_size,
	min_rank_tier, default_wind_power_range, updated_at`

func scanMap(row pgx.Row, extra ...any) (*mapRow, error) {
	var m mapRow
	dest := append([]any{&m.MapID, &m.Name, &m.Theme, &m.LegacyWidth, &m.LegacyHeight, &m.GridWidth, &m.GridHeight,
		&m.CellSize, &m.MinRankTier, &m.WindJSON, &m.UpdatedAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListMaps returns every config_maps row (without tiles), ordered by map id. The JSON
// array length tells tile maps from legacy ones.
func (r *Repository) ListMaps(ctx context.Context) ([]mapRow, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT `+mapColumns+`, jsonb_typeof(tiles) = 'array' AND jsonb_array_length(tiles) > 0
		FROM config_maps ORDER BY map_id`)
	if err != nil {
		return nil, fmt.Errorf("query maps: %w", err)
	}
	defer rows.Close()
	var out []mapRow
	for rows.Next() {
		var hasTiles bool
		m, err := scanMap(rows, &hasTiles)
		if err != nil {
			return nil, fmt.Errorf("scan map: %w", err)
		}
		m.HasTiles = hasTiles
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetMap returns one map with its tiles and spawn points; nil when there is no such map.
func (r *Repository) GetMap(ctx context.Context, mapID string) (*mapRow, error) {
	var tiles, spawns []byte
	m, err := scanMap(r.db.Pool.QueryRow(ctx, `SELECT `+mapColumns+`, tiles, spawn_points
		FROM config_maps WHERE map_id = $1`, mapID), &tiles, &spawns)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get map %s: %w", mapID, err)
	}
	m.TilesJSON, m.SpawnJSON = tiles, spawns
	return m, nil
}

// ListBrickTypes returns config_brick_types ordered by id.
func (r *Repository) ListBrickTypes(ctx context.Context) ([]BrickType, error) {
	rows, err := r.db.Pool.Query(ctx, `SELECT brick_type_id, name, destructible, color, image_id
		FROM config_brick_types ORDER BY brick_type_id`)
	if err != nil {
		return nil, fmt.Errorf("query brick types: %w", err)
	}
	defer rows.Close()
	out := []BrickType{}
	for rows.Next() {
		var b BrickType
		if err := rows.Scan(&b.BrickTypeID, &b.Name, &b.Destructible, &b.Color, &b.ImageID); err != nil {
			return nil, fmt.Errorf("scan brick type: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
