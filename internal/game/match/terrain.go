package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// The terrain rules (rules.terrain of the game data the terrain was built with): the clearance
// a player needs above the surface (playerHeight, the character body drawn by the client, ~40
// px), the most it climbs per 1 px step (maxClimbPerStep, 3: ~72°), the crater bowl's depth and
// ceiling (bowlDepth, bowlCeiling).

type TerrainZone struct {
	Type string // "lava", "ice", "fragile"
	MinX float64
	MaxX float64
	MinY float64
	MaxY float64
}

type Terrain struct {
	Width            int
	Height           int
	Mask             []uint64
	DestructibleMask []uint64
	Zones            []TerrainZone
	Version          uint64 // incremented by every crater (CarveBowl, DestroyCircle); clients use it to skip operations already in a snapshot
	// gd is the game data the terrain plays by (the match's snapshot); nil (a Terrain built by
	// hand in a test) uses gamedata.Current().
	gd *gamedata.Snapshot
}

// data is the game data the terrain plays by.
func (t *Terrain) data() *gamedata.Snapshot {
	if t != nil && t.gd != nil {
		return t.gd
	}
	return gamedata.Current()
}

// rules are the terrain rules the terrain plays by.
func (t *Terrain) rules() gamedata.TerrainRules { return t.data().Rules.Terrain }

func maskSize(width, height int) int {
	return (width*height + 63) / 64
}

func bitIndex(x, y, width int) (int, uint64) {
	idx := y*width + x
	return idx / 64, uint64(1) << (idx % 64)
}

func getBit(mask []uint64, x, y, width int) bool {
	word, bit := bitIndex(x, y, width)
	return mask[word]&bit != 0
}

func setBit(mask []uint64, x, y, width int) {
	word, bit := bitIndex(x, y, width)
	mask[word] |= bit
}

func clearBit(mask []uint64, x, y, width int) {
	word, bit := bitIndex(x, y, width)
	mask[word] &^= bit
}

// NewTerrain builds the terrain of mapCfg with the current game data's brick types and rules.
func NewTerrain(mapCfg gamedata.MapConfig) *Terrain {
	gd := gamedata.Current()
	return newTerrain(gd, mapCfg, gd.Bricks)
}

// NewTerrainFor builds the terrain of mapCfg with gd's brick types, playing by gd's rules.
func NewTerrainFor(gd *gamedata.Snapshot, mapCfg gamedata.MapConfig) *Terrain {
	return newTerrain(gd, mapCfg, gd.Bricks)
}

// NewTerrainWithBricks builds terrain using the given brick types instead of the loaded ones
// (and the current game data's rules). Unknown brick IDs fill their whole cell and are
// destructible.
func NewTerrainWithBricks(mapCfg gamedata.MapConfig, bricks map[int]gamedata.BrickTypeConfig) *Terrain {
	return newTerrain(gamedata.Current(), mapCfg, bricks)
}

func newTerrain(gd *gamedata.Snapshot, mapCfg gamedata.MapConfig, bricks map[int]gamedata.BrickTypeConfig) *Terrain {
	hasTiles := mapCfg.HasTiles()
	width, height := mapCfg.PixelSize()

	if width <= 0 || height <= 0 {
		width, height = gamedata.DefaultMapWidth, gamedata.DefaultMapHeight
	}

	t := &Terrain{
		Width:            width,
		Height:           height,
		Mask:             make([]uint64, maskSize(width, height)),
		DestructibleMask: make([]uint64, maskSize(width, height)),
		gd:               gd,
	}

	if hasTiles {
		cs := mapCfg.CellSize
		// Tiles beyond GridWidth/GridHeight lie outside the mask and are ignored.
		for row := 0; row < len(mapCfg.Tiles) && row < mapCfg.GridHeight; row++ {
			for col := 0; col < len(mapCfg.Tiles[row]) && col < mapCfg.GridWidth; col++ {
				brickID := mapCfg.Tiles[row][col]
				if brickID == 0 {
					continue
				}
				destructible := true
				var border gamedata.BrickBorder
				hasBorder := false
				if bt, ok := bricks[brickID]; ok {
					destructible = bt.Destructible
					border = bt.Border
					hasBorder = len(border.Top) > 0
				}

				offsetX := col * cs
				offsetY := row * cs

				if hasBorder {
					poly := polygonFromBorder(border, cs)
					scanlineFillPolygon(t.Mask, t.DestructibleMask, width, height, poly, offsetX, offsetY, cs, destructible)
				} else {
					// Fallback: fill full square
					for py := offsetY; py < offsetY+cs && py < height; py++ {
						for px := offsetX; px < offsetX+cs && px < width; px++ {
							setBit(t.Mask, px, py, width)
							if destructible {
								setBit(t.DestructibleMask, px, py, width)
							}
						}
					}
				}
			}
		}
	} else {
		t.generateLegacyTerrain(mapCfg.MapID)
	}

	// Build terrain zones from legacy terrain layers (backward compat)
	if !hasTiles {
		specialTypes := map[string]bool{"lava": true, "ice": true, "fragile": true}
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

	return t
}

func (t *Terrain) generateLegacyTerrain(mapID string) {
	for x := 0; x < t.Width; x++ {
		var terrainHeight float64
		switch mapID {
		case "frozen_peak":
			terrainHeight = 500 + 120*math.Sin(float64(x)*0.005) + 60*math.Cos(float64(x)*0.015)
		case "steel_base":
			if x > 300 && x < 600 {
				terrainHeight = 400
			} else if x > 1000 && x < 1300 {
				terrainHeight = 400
			} else {
				terrainHeight = 600
			}
		default:
			terrainHeight = 550 + 100*math.Sin(float64(x)*0.003) + 40*math.Sin(float64(x)*0.01)
		}

		for y := 0; y < t.Height; y++ {
			if float64(y) >= terrainHeight {
				setBit(t.Mask, x, y, t.Width)
				setBit(t.DestructibleMask, x, y, t.Width)
			}
		}
	}
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

	return getBit(t.Mask, ix, iy, t.Width)
}

// IsDestructible reports whether pixel (x, y) is solid soft terrain (explosions carve it);
// solid pixels that are not are hard. Outside the map it is false.
func (t *Terrain) IsDestructible(x, y int) bool {
	if x < 0 || x >= t.Width || y < 0 || y >= t.Height {
		return false
	}
	return getBit(t.Mask, x, y, t.Width) && getBit(t.DestructibleMask, x, y, t.Width)
}

// DestroyCircle clears the destructible solid pixels within radius of (cx, cy). No explosion
// carves it any more (every one is a bowl, CarveBowl); it stays for the tests and the client's
// parity fixture (crater "circle").
func (t *Terrain) DestroyCircle(cx, cy float64, radius float64) bool {
	t.Version++
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

			dx := float64(x - icx)
			dy := float64(y - icy)
			if dx*dx+dy*dy <= radius*radius {
				if getBit(t.Mask, x, y, t.Width) && getBit(t.DestructibleMask, x, y, t.Width) {
					clearBit(t.Mask, x, y, t.Width)
					destroyedAny = true
				}
			}
		}
	}

	return destroyedAny
}

// Crater shapes (the explode event's crater).
const (
	CraterBowl   = "bowl"   // CarveBowl: every explosion, the bomb chain's too
	CraterCircle = "circle" // DestroyCircle: no explosion carves it now (the bomb chain did before playtest 5)
)

// bowlCeiling (rules.terrain, 1.5) is how far above the impact point, in crater radii, a bowl
// carves (its flat top). High enough that on an uphill slope up to ~40° the floor meets the
// surface below it, so the uphill rim leaves no overhang.
//
// bowlDepth (rules.terrain, 0.72; playtest 5, was 0.5) is how deep the bowl's floor lies under
// the impact point, in crater radii.

// Carve cuts the crater of the given shape (CraterBowl or CraterCircle; "" is CraterBowl).
func (t *Terrain) Carve(shape string, cx, cy, radius float64) bool {
	if shape == CraterCircle {
		return t.DestroyCircle(cx, cy, radius)
	}
	return t.CarveBowl(cx, cy, radius)
}

// CarveBowl clears the destructible solid pixels of a bowl around (cx, cy). With r the crater
// radius, k = bowlDepth and (dx, dy) a pixel's offset from the rounded centre (y down), a
// pixel is carved when
//
//	dx² + (r/k)·dy <= r²  (above the floor: a parabola k·r under the centre, r to each side at its height)
//	dy >= -bowlCeiling·r  (below the flat top)
//
// On flat ground the bowl is k·r deep and 2r wide at the impact's height, its walls at most
// atan(2k) = 55° (1.44:1) at the rim; on a 30° slope the uphill wall rises at most ~65°
// (2.1:1) to meet the surface ~1.48r away. Every pixel step is <= 3 px, the climb of a player
// (WalkTo), so a player in the crater can walk out. Mirrored bit for bit by the client's
// TerrainMask.carve_bowl.
func (t *Terrain) CarveBowl(cx, cy, radius float64) bool {
	t.Version++
	if radius <= 0 {
		return false
	}
	tr := t.rules()
	bowlCeiling, bowlDepth := tr.BowlCeiling, tr.BowlDepth

	icx := int(math.Round(cx))
	icy := int(math.Round(cy))
	reach := int(math.Ceil(2*radius)) + 1 // past where the floor meets the top (r·√(1 + bowlCeiling/k) < 2r)
	top := -bowlCeiling * radius
	r2 := radius * radius
	lean := radius / bowlDepth // r/k: 2r at k = 0.5

	destroyedAny := false
	for y := icy - int(math.Ceil(bowlCeiling*radius)) - 1; y <= icy+int(math.Ceil(bowlDepth*radius))+1; y++ {
		if y < 0 || y >= t.Height {
			continue
		}
		dy := float64(y - icy)
		if dy < top {
			continue
		}
		lift := float64(lean * dy) // explicit conversion: no fused multiply-add, so the client matches
		for x := icx - reach; x <= icx+reach; x++ {
			if x < 0 || x >= t.Width {
				continue
			}
			dx := x - icx
			if float64(dx*dx)+lift <= r2 {
				if getBit(t.Mask, x, y, t.Width) && getBit(t.DestructibleMask, x, y, t.Width) {
					clearBit(t.Mask, x, y, t.Width)
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
		if getBit(t.Mask, ix, y, t.Width) {
			return float64(y)
		}
	}

	return float64(t.Height)
}

// SpawnY returns the surface Y for a spawn point at (x, y). Unlike GetLandingY(x, 0) it keeps
// the configured floor: a spawn in open air drops to the first surface below it, and a spawn
// embedded in solid ground is lifted to the top of that ground.
func (t *Terrain) SpawnY(x, y float64) float64 {
	ix := int(math.Round(x))
	if ix < 0 || ix >= t.Width {
		return float64(t.Height)
	}

	iy := int(math.Round(y))
	if iy < 0 {
		iy = 0
	}
	if iy >= t.Height {
		return float64(t.Height)
	}

	if !getBit(t.Mask, ix, iy, t.Width) {
		return t.GetLandingY(x, float64(iy))
	}
	for iy > 0 && getBit(t.Mask, ix, iy-1, t.Width) {
		iy--
	}
	return float64(iy)
}

// WalkTo simulates pixel-by-pixel horizontal movement with terrain physics.
// The player follows the terrain surface but is blocked by steep upward slopes
// (walls, crater edges). Going downward is always allowed. The surface is searched
// locally around the current height, so roofs and platforms above the player are ignored.
// A step over a hole that reaches the bottom of the map ends the walk there, with y =
// Terrain.Height: the player falls out of the map.
func (t *Terrain) WalkTo(startX, startY, targetX float64) (float64, float64) {

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

	maxClimb := t.rules().MaxClimbPerStep
	for curX != endX {
		nextX := curX + dir
		if nextX < 0 || nextX >= t.Width {
			break
		}

		nextY, ok := t.stepSurface(nextX, curY, maxClimb)
		if !ok {
			break
		}

		curX = nextX
		curY = nextY
		if curY >= t.Height {
			break // fell out of the map
		}
	}

	return float64(curX), float64(curY)
}

// CanStandAt reports whether a player spawned at (x, y) lands on solid ground inside the map
// with enough headroom.
func (t *Terrain) CanStandAt(x, y float64) bool {
	sy := int(t.SpawnY(x, y))
	if sy >= t.Height {
		return false
	}
	return t.hasHeadroom(int(math.Round(x)), sy)
}

// hasHeadroom reports whether the playerHeight pixels above surfaceY in column x are free.
// Pixels above the top of the map count as free.
func (t *Terrain) hasHeadroom(x, surfaceY int) bool {
	top := surfaceY - t.rules().PlayerHeight
	if top < 0 {
		top = 0
	}
	for y := top; y < surfaceY && y < t.Height; y++ {
		if getBit(t.Mask, x, y, t.Width) {
			return false
		}
	}
	return true
}

// stepSurface finds the surface the player stands on after stepping into column x from
// height curY (Y of the solid pixel under the feet). ok is false when the step is a wall
// taller than maxClimb, or the column has no room for the player's body.
func (t *Terrain) stepSurface(x, curY, maxClimb int) (int, bool) {
	feet := curY - 1 // lowest pixel occupied by the player's body
	if feet >= t.Height {
		return t.Height, true
	}
	if feet < 0 || !getBit(t.Mask, x, feet, t.Width) {
		// Flat or downhill: the body moves across at the current height, then drops to the
		// first solid pixel below.
		if !t.hasHeadroom(x, curY) {
			return 0, false
		}
		return int(t.GetLandingY(float64(x), float64(feet))), true
	}

	// Uphill: climb to the top of the solid run, up to maxClimb pixels.
	y := feet
	for y > 0 && getBit(t.Mask, x, y-1, t.Width) {
		y--
		if curY-y > maxClimb {
			return 0, false
		}
	}
	if curY-y > maxClimb || !t.hasHeadroom(x, y) {
		return 0, false
	}
	return y, true
}
