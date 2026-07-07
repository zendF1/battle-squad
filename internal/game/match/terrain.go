package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

type TerrainZone struct {
	Type string  // "lava", "ice", "fragile"
	MinX float64
	MaxX float64
	MinY float64
	MaxY float64
}

type Terrain struct {
	Width  int
	Height int
	Mask   []bool // true = solid, false = empty/destroyed
	Zones  []TerrainZone
}

func NewTerrain(width, height int, mapID string) *Terrain {
	t := &Terrain{
		Width:  width,
		Height: height,
		Mask:   make([]bool, width*height),
	}

	// Generate basic terrain landscape depending on map ID
	// y = 0 is top of map, y = height is bottom of map (standard coordinates)
	// We make solid terrain at the bottom half of the map.
	for x := 0; x < width; x++ {
		var terrainHeight float64
		switch mapID {
		case "frozen_peak":
			// Jagged snowy peaks
			terrainHeight = 500 + 120*math.Sin(float64(x)*0.005) + 60*math.Cos(float64(x)*0.015)
		case "steel_base":
			// Flat base with stepped platforms
			if x > 300 && x < 600 {
				terrainHeight = 400
			} else if x > 1000 && x < 1300 {
				terrainHeight = 400
			} else {
				terrainHeight = 600
			}
		default: // grassland_valley
			// Smooth rolling hills
			terrainHeight = 550 + 100*math.Sin(float64(x)*0.003) + 40*math.Sin(float64(x)*0.01)
		}

		for y := 0; y < height; y++ {
			idx := y*width + x
			if float64(y) >= terrainHeight {
				t.Mask[idx] = true // Solid ground
			} else {
				t.Mask[idx] = false // Sky/Air
			}
		}
	}

	// Build terrain zones from map config for special terrain types
	specialTypes := map[string]bool{"lava": true, "ice": true, "fragile": true}
	if gamedata.Data != nil {
		if mapCfg, ok := gamedata.Data.Maps[mapID]; ok {
			for _, layer := range mapCfg.TerrainLayers {
				if !specialTypes[layer.Type] {
					continue
				}
				if len(layer.YRange) != 2 {
					continue
				}
				zone := TerrainZone{
					Type: layer.Type,
					MinX: 0,
					MaxX: float64(width),
					MinY: float64(layer.YRange[0]),
					MaxY: float64(layer.YRange[1]),
				}
				t.Zones = append(t.Zones, zone)
			}
		}
	}

	return t
}

// GetTerrainTypeAt returns the special terrain type at position (x, y),
// or "normal" if no special zone matches.
func (t *Terrain) GetTerrainTypeAt(x, y float64) string {
	for _, zone := range t.Zones {
		if x >= zone.MinX && x <= zone.MaxX && y >= zone.MinY && y <= zone.MaxY {
			return zone.Type
		}
	}
	return "normal"
}

func (t *Terrain) IsSolid(x, y float64) bool {
	ix := int(math.Round(x))
	iy := int(math.Round(y))

	if ix < 0 || ix >= t.Width {
		return false // Out of boundary
	}
	if iy < 0 {
		return false // sky
	}
	if iy >= t.Height {
		return true // solid floor at bottom boundary
	}

	return t.Mask[iy*t.Width+ix]
}

func (t *Terrain) DestroyCircle(cx, cy float64, radius float64) bool {
	icx := int(math.Round(cx))
	icy := int(math.Round(cy))
	ir := int(math.Round(radius))

	destroyedAny := false
	for y := icy - ir; y <= icy+ir; y++ {
		if y < 0 || y >= t.Height {
			continue
		}
		for x := icx - ir; x <= icx+ir; x++ {
			if x < 0 || x >= t.Width {
				continue
			}

			// Circle formula: (x - cx)^2 + (y - cy)^2 <= r^2
			dx := float64(x - icx)
			dy := float64(y - icy)
			if dx*dx+dy*dy <= radius*radius {
				idx := y*t.Width + x
				if t.Mask[idx] {
					t.Mask[idx] = false
					destroyedAny = true
				}
			}
		}
	}

	return destroyedAny
}

func (t *Terrain) GetLandingY(x, startY float64) float64 {
	ix := int(math.Round(x))
	if ix < 0 || ix >= t.Width {
		return float64(t.Height) // falls off boundary
	}

	iyStart := int(math.Round(startY))
	if iyStart < 0 {
		iyStart = 0
	}

	for y := iyStart; y < t.Height; y++ {
		if t.Mask[y*t.Width+ix] {
			return float64(y)
		}
	}

	return float64(t.Height)
}

// WalkTo simulates pixel-by-pixel horizontal movement with terrain physics.
// The player follows the terrain surface but is blocked by steep upward slopes
// (walls, crater edges). Going downward is always allowed.
func (t *Terrain) WalkTo(startX, startY, targetX float64) (float64, float64) {
	const maxClimbPerStep = 3 // max pixels the player can climb up per 1px horizontal step (~72°)

	curX := int(math.Round(startX))
	curY := int(math.Round(startY))
	endX := int(math.Round(targetX))

	if curX == endX {
		return startX, startY
	}

	dir := 1
	if endX < curX {
		dir = -1
	}

	for curX != endX {
		nextX := curX + dir
		if nextX < 0 || nextX >= t.Width {
			break
		}

		// Find terrain surface at nextX (first solid pixel from top)
		nextY := t.Height
		for y := 0; y < t.Height; y++ {
			if t.Mask[y*t.Width+nextX] {
				nextY = y
				break
			}
		}

		// Going UP: curY > nextY (lower Y = higher position). Block if too steep.
		if curY-nextY > maxClimbPerStep {
			break
		}

		curX = nextX
		curY = nextY
	}

	return float64(curX), float64(curY)
}

