package maps

import (
	"encoding/json"
	"time"
)

// Map is one map of GET /maps: what the client needs to list it, name it and draw its theme
// before a match (the terrain itself comes with MatchStarted). Version is updated_at in Unix ms:
// it changes whenever the map is saved (migration, admin map editor).
type Map struct {
	MapID       string     `json:"mapId"`
	Name        string     `json:"name"`
	Theme       string     `json:"theme"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	CellSize    int        `json:"cellSize"`
	GridWidth   int        `json:"gridWidth"`
	GridHeight  int        `json:"gridHeight"`
	MinRankTier string     `json:"minRankTier"`
	WindRange   [2]float64 `json:"windRange"`
	HasTiles    bool       `json:"hasTiles"` // false: a legacy heightmap map (no tile grid to preview)
	Version     int64      `json:"version"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// MapDetail is GET /maps/{mapId}: the map with its tile grid (config_brick_types ids, 0 = air,
// GridHeight rows of GridWidth; [] for a legacy map) and spawn points, for a preview.
type MapDetail struct {
	Map
	Tiles       [][]int      `json:"tiles"`
	SpawnPoints []SpawnPoint `json:"spawnPoints"`
}

type SpawnPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// BrickType is a config_brick_types row: destructible = soft terrain, else hard.
type BrickType struct {
	BrickTypeID  int    `json:"brickTypeId"`
	Name         string `json:"name"`
	Destructible bool   `json:"destructible"`
	Color        string `json:"color"`
	ImageID      string `json:"imageId"`
}

// MapList is GET /maps. Version is a hash of the whole list (also the ETag): a client with it
// cached sends If-None-Match and gets 304 while nothing changed.
type MapList struct {
	Version    string      `json:"version"`
	Maps       []Map       `json:"maps"`
	BrickTypes []BrickType `json:"brickTypes"`
}

// mapRow is a config_maps row as the repository reads it.
type mapRow struct {
	Map
	LegacyWidth, LegacyHeight int
	WindJSON, TilesJSON       []byte
	SpawnJSON                 json.RawMessage
}
