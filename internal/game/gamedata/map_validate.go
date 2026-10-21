package gamedata

import "fmt"

// Map size limits. Terrain masks are allocated per match from these dimensions.
const (
	MinCellSize  = 4
	MaxCellSize  = 64
	MaxMapWidth  = 4096 // pixels
	MaxMapHeight = 2048 // pixels

	// Size of the legacy terrain used when a map is missing or has no size configured.
	DefaultMapWidth  = 1600
	DefaultMapHeight = 900

	// MinSpawnPoints is one spawn per team; TeamSpawns gives the first half to team 1.
	MinSpawnPoints = 2
)

// HasTiles reports whether the map uses tile terrain. Only an empty matrix means legacy
// terrain; a matrix with any rows is tile terrain and must match the grid.
func (m MapConfig) HasTiles() bool {
	return len(m.Tiles) > 0
}

// PixelSize returns the terrain size in pixels, as built by match.NewTerrain.
func (m MapConfig) PixelSize() (width, height int) {
	if m.HasTiles() {
		return m.GridWidth * m.CellSize, m.GridHeight * m.CellSize
	}
	width, height = m.Width, m.Height
	if width == 0 {
		width = m.GridWidth * m.CellSize
	}
	if height == 0 {
		height = m.GridHeight * m.CellSize
	}
	return width, height
}

// TeamSpawns splits spawn points between the teams: the first half to team 1, the rest to
// team 2. A team left without spawns gets a fallback inside the map, near its own side.
func (m MapConfig) TeamSpawns() (team1, team2 []SpawnPoint) {
	mid := len(m.SpawnPoints) / 2
	team1 = append(team1, m.SpawnPoints[:mid]...)
	team2 = append(team2, m.SpawnPoints[mid:]...)

	width, _ := m.PixelSize()
	if width <= 0 {
		width = DefaultMapWidth
	}
	if len(team1) == 0 {
		team1 = append(team1, SpawnPoint{X: float64(width) / 8, Y: 0})
	}
	if len(team2) == 0 {
		team2 = append(team2, SpawnPoint{X: float64(width) * 7 / 8, Y: 0})
	}
	return team1, team2
}

// ValidateMap checks a map config can be built into terrain safely.
// brickTypes may be nil, in which case brick IDs are not checked against known types.
func ValidateMap(m MapConfig, brickTypes map[int]BrickTypeConfig) error {
	if m.MapID == "" {
		return fmt.Errorf("map id is required")
	}

	width, height := m.PixelSize()
	if m.HasTiles() {
		if m.CellSize < MinCellSize || m.CellSize > MaxCellSize {
			return fmt.Errorf("map %s: cellSize %d out of range [%d, %d]", m.MapID, m.CellSize, MinCellSize, MaxCellSize)
		}
		if m.GridWidth <= 0 || m.GridHeight <= 0 {
			return fmt.Errorf("map %s: grid size %dx%d must be positive", m.MapID, m.GridWidth, m.GridHeight)
		}
		if m.GridWidth > MaxMapWidth/m.CellSize || m.GridHeight > MaxMapHeight/m.CellSize {
			return fmt.Errorf("map %s: %dx%d cells of %dpx exceeds %dx%dpx limit", m.MapID, m.GridWidth, m.GridHeight, m.CellSize, MaxMapWidth, MaxMapHeight)
		}

		if len(m.Tiles) != m.GridHeight {
			return fmt.Errorf("map %s: %d tile rows, gridHeight is %d", m.MapID, len(m.Tiles), m.GridHeight)
		}
		for row, cols := range m.Tiles {
			if len(cols) != m.GridWidth {
				return fmt.Errorf("map %s: tile row %d has %d columns, gridWidth is %d", m.MapID, row, len(cols), m.GridWidth)
			}
			for col, id := range cols {
				if id < 0 {
					return fmt.Errorf("map %s: negative brick id %d at row %d col %d", m.MapID, id, row, col)
				}
				if id == 0 || brickTypes == nil {
					continue
				}
				if _, ok := brickTypes[id]; !ok {
					return fmt.Errorf("map %s: unknown brick id %d at row %d col %d", m.MapID, id, row, col)
				}
			}
		}
	} else if width <= 0 || height <= 0 || width > MaxMapWidth || height > MaxMapHeight {
		return fmt.Errorf("map %s: legacy size %dx%d out of range (max %dx%d)", m.MapID, width, height, MaxMapWidth, MaxMapHeight)
	}

	for i, sp := range m.SpawnPoints {
		if sp.X < 0 || sp.X >= float64(width) || sp.Y < 0 || sp.Y >= float64(height) {
			return fmt.Errorf("map %s: spawn point %d (%v,%v) outside %dx%d map", m.MapID, i, sp.X, sp.Y, width, height)
		}
	}

	if r := m.DefaultWindPowerRange; len(r) != 0 {
		if len(r) != 2 || r[0] < 0 || r[0] > r[1] {
			return fmt.Errorf("map %s: wind power range %v must be [min, max] with 0 <= min <= max", m.MapID, r)
		}
	}

	return nil
}

// DefaultMapTheme is the theme of a map without one (today's grassland look).
const DefaultMapTheme = "grassland"

// MapThemes are the themes the client draws (docs/technical-documentation.md, Maps). Each is a
// sky / background, a soft (destructible) and a hard (indestructible) material, and a bottom:
// water (beach, ice, sky, temple, city_night, steel) or an abyss (the others). Either way a
// player below the map bottom is out of the map.
var MapThemes = []string{
	"grassland", "ice", "beach", "graveyard", "desert", "sky", "temple", "steel", "city_night", "stone", "cave",
}

// KnownMapTheme reports whether theme is one of MapThemes.
func KnownMapTheme(theme string) bool {
	for _, t := range MapThemes {
		if t == theme {
			return true
		}
	}
	return false
}

// ThemeOrDefault is the map's theme, or DefaultMapTheme when it has none or one the client
// does not know.
func (m MapConfig) ThemeOrDefault() string {
	if KnownMapTheme(m.Theme) {
		return m.Theme
	}
	return DefaultMapTheme
}

// MapDisplay is the name and theme clients show for mapID (MatchStarted mapName / mapTheme,
// MatchFound): the loaded map's, else the id itself and DefaultMapTheme.
func MapDisplay(mapID string) (name, theme string) {
	if Data != nil {
		if m, ok := Data.Maps[mapID]; ok {
			name = m.Name
			if name == "" {
				name = mapID
			}
			return name, m.ThemeOrDefault()
		}
	}
	return mapID, DefaultMapTheme
}
