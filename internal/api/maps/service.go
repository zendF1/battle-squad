package maps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/model"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// finish fills the derived fields of a row: the theme the client knows, the pixel size (the
// grid's for a tile map, the legacy columns otherwise), the wind range, the version.
func finish(m *mapRow, hasTiles bool) Map {
	out := m.Map
	out.HasTiles = hasTiles
	out.Theme = gamedata.MapConfig{Theme: m.Theme}.ThemeOrDefault()
	if hasTiles {
		out.Width, out.Height = m.GridWidth*m.CellSize, m.GridHeight*m.CellSize
	} else {
		out.Width, out.Height = m.LegacyWidth, m.LegacyHeight
	}
	var wind []float64
	if json.Unmarshal(m.WindJSON, &wind) == nil && len(wind) == 2 {
		out.WindRange = [2]float64{wind[0], wind[1]}
	}
	out.Version = m.UpdatedAt.UnixMilli()
	return out
}

// List is every map and brick type, with the list's version (a hash of its content).
func (s *Service) List(ctx context.Context) (*MapList, error) {
	rows, err := s.repo.ListMaps(ctx)
	if err != nil {
		return nil, err
	}
	bricks, err := s.repo.ListBrickTypes(ctx)
	if err != nil {
		return nil, err
	}
	out := &MapList{Maps: make([]Map, 0, len(rows)), BrickTypes: bricks}
	for i := range rows {
		out.Maps = append(out.Maps, finish(&rows[i], rows[i].HasTiles))
	}
	body, _ := json.Marshal(struct {
		Maps       []Map
		BrickTypes []BrickType
	}{out.Maps, out.BrickTypes})
	sum := sha256.Sum256(body)
	out.Version = hex.EncodeToString(sum[:8])
	return out, nil
}

// Get is one map with its tiles and spawn points (MAP_NOT_FOUND when there is none).
func (s *Service) Get(ctx context.Context, mapID string) (*MapDetail, error) {
	m, err := s.repo.GetMap(ctx, strings.TrimSpace(mapID))
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, model.ErrMapNotFound
	}
	var tiles [][]int
	_ = json.Unmarshal(m.TilesJSON, &tiles)
	if tiles == nil {
		tiles = [][]int{}
	}
	var spawns []SpawnPoint // the editor writes {x, y}, the admin seed {X, Y}: both decode
	_ = json.Unmarshal(m.SpawnJSON, &spawns)
	if spawns == nil {
		spawns = []SpawnPoint{}
	}
	return &MapDetail{Map: finish(m, len(tiles) > 0), Tiles: tiles, SpawnPoints: spawns}, nil
}
