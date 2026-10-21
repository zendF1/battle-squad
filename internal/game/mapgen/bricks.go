// Package mapgen builds the battle maps' tile grids from hand-written primitives (rects,
// chamfered islands, 45° bands, hills) and renders them as the config_maps / config_brick_types
// migrations and the configs/*.yaml mirror. cmd/mapgen writes the files; the layouts are our
// own, authored by hand from the reference maps' platform positions only.
package mapgen

import "battle-squad/internal/game/gamedata"

// Material of a brick: soft is destructible, hard is not.
type Material int

const (
	Soft Material = iota
	Hard
)

// Shape of a brick inside its cell. Slopes are 45°: SlopeR rises to the right (solid lower
// right, ◢), SlopeL rises to the left (◣); CeilR / CeilL are the solid upper right (◥) / upper
// left (◤), the undersides of islands and bands.
type Shape int

const (
	Full Shape = iota
	SlopeR
	SlopeL
	CeilR
	CeilL
	HalfBottom
	HalfTop
	Plank
	numShapes
)

// Brick ids. 1..5 are the admin seed's defaults (Dirt and Rock are the soft and hard full
// blocks); 6..19 are the shapes, soft then hard, inserted by migration 036 with fixed ids.
const (
	BrickDirt    = 1
	BrickRock    = 2
	BrickIce     = 3
	BrickLava    = 4
	BrickFragile = 5
	firstShaped  = 6
	LastBrickID  = firstShaped + 2*(int(numShapes)-1) - 1
)

// BrickID is the brick type of a material and shape.
func BrickID(m Material, s Shape) int {
	if s == Full {
		if m == Hard {
			return BrickRock
		}
		return BrickDirt
	}
	id := firstShaped + int(s) - 1
	if m == Hard {
		id += int(numShapes) - 1
	}
	return id
}

// brickOf is the material and shape of a brick id (unknown ids: a soft full block).
func brickOf(id int) (Material, Shape) {
	switch {
	case id == BrickRock || id == BrickLava:
		return Hard, Full
	case id >= firstShaped && id <= LastBrickID:
		k := id - firstShaped
		m := Soft
		if k >= int(numShapes)-1 {
			m = Hard
			k -= int(numShapes) - 1
		}
		return m, Shape(k + 1)
	}
	return Soft, Full
}

var shapeNames = map[Shape]string{
	SlopeR: "Slope Up-Right", SlopeL: "Slope Up-Left", CeilR: "Ceiling Right", CeilL: "Ceiling Left",
	HalfBottom: "Half Bottom", HalfTop: "Half Top", Plank: "Plank",
}

// shapePolygon is the shape's outline as 4 corners in brick border coordinates (0..16, origin
// bottom left, y up) chained bottom → right → top → left like the default square; a triangle
// repeats one corner.
func shapePolygon(s Shape) [4]gamedata.BorderPoint {
	p := func(x, y float64) gamedata.BorderPoint { return gamedata.BorderPoint{X: x, Y: y} }
	switch s {
	case SlopeR:
		return [4]gamedata.BorderPoint{p(0, 0), p(16, 0), p(16, 16), p(16, 16)}
	case SlopeL:
		return [4]gamedata.BorderPoint{p(0, 0), p(16, 0), p(0, 16), p(0, 16)}
	case CeilR:
		return [4]gamedata.BorderPoint{p(16, 0), p(16, 0), p(16, 16), p(0, 16)}
	case CeilL:
		return [4]gamedata.BorderPoint{p(0, 0), p(0, 0), p(16, 16), p(0, 16)}
	case HalfBottom:
		return [4]gamedata.BorderPoint{p(0, 0), p(16, 0), p(16, 8), p(0, 8)}
	case HalfTop:
		return [4]gamedata.BorderPoint{p(0, 8), p(16, 8), p(16, 16), p(0, 16)}
	case Plank:
		return [4]gamedata.BorderPoint{p(0, 10), p(16, 10), p(16, 16), p(0, 16)}
	}
	return [4]gamedata.BorderPoint{p(0, 0), p(16, 0), p(16, 16), p(0, 16)}
}

func borderOf(s Shape) gamedata.BrickBorder {
	c := shapePolygon(s)
	return gamedata.BrickBorder{
		Bottom: []gamedata.BorderPoint{c[0], c[1]},
		Right:  []gamedata.BorderPoint{c[1], c[2]},
		Top:    []gamedata.BorderPoint{c[2], c[3]},
		Left:   []gamedata.BorderPoint{c[3], c[0]},
	}
}

// BrickTypes is every brick type the maps use, ids 1..LastBrickID: the 5 seed defaults (as the
// admin seed inserts them) and the soft / hard shapes.
func BrickTypes() []gamedata.BrickTypeConfig {
	full := borderOf(Full)
	out := []gamedata.BrickTypeConfig{
		{BrickTypeID: BrickDirt, Name: "Dirt", Destructible: true, Border: full, Color: "#8B4513"},
		{BrickTypeID: BrickRock, Name: "Rock", Destructible: false, Border: full, Color: "#808080"},
		{BrickTypeID: BrickIce, Name: "Ice", Destructible: true, Border: full, Color: "#87CEEB"},
		{BrickTypeID: BrickLava, Name: "Lava", Destructible: false, Border: full, Color: "#FF4500"},
		{BrickTypeID: BrickFragile, Name: "Fragile", Destructible: true, Border: full, Color: "#DEB887"},
	}
	for _, m := range []Material{Soft, Hard} {
		for s := SlopeR; s < numShapes; s++ {
			name, color := "Dirt ", "#8B4513"
			if m == Hard {
				name, color = "Rock ", "#808080"
			}
			out = append(out, gamedata.BrickTypeConfig{
				BrickTypeID: BrickID(m, s), Name: name + shapeNames[s], Destructible: m == Soft,
				Border: borderOf(s), Color: color,
			})
		}
	}
	return out
}

// BrickTypeMap is BrickTypes by id.
func BrickTypeMap() map[int]gamedata.BrickTypeConfig {
	m := map[int]gamedata.BrickTypeConfig{}
	for _, b := range BrickTypes() {
		m[b.BrickTypeID] = b
	}
	return m
}
