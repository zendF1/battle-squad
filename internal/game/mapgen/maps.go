package mapgen

import "battle-squad/internal/game/gamedata"

// Def is one generated map. Build draws team 1's (left) half and the centre, then the grid is
// mirrored so team 2 gets the same terrain; Spawns are team 1's two spawn cells (column, row of
// the platform's top surface), team 2's are their mirror images. The 1v1 spawn is Spawns[0].
type Def struct {
	ID          string
	Name        string // Vietnamese, shown by the client (mapName)
	Theme       string // gamedata.MapThemes
	Tier        string // minRankTier
	Wind        [2]float64
	Description string
	Build       func(g *Grid)
	Spawns      [2][2]int
}

// Defs are the generated maps, in the reference's order. Layouts: platform positions read off
// the reference thumbnails by eye, laid out on our 150 × 70 grid (2400 × 1120, close to the
// references' 2.1:1) and made symmetric; hard (indestructible) where the reference shows metal or
// stone. The extra width puts the teams 1300-1900 px apart, the extra height gives higher
// platforms and deeper drops; every spawn stands on at least 14 cells of walkable ground. A
// spawn below its teammate stands nearer the centre, so the platform above it does not cover
// its shots (a full-power shot carries ~2070 px on the flat, less uphill).
var Defs = []Def{
	{
		ID: "ice_bridge", Name: "Cầu băng", Theme: "ice", Tier: "bronze", Wind: [2]float64{1.5, 4},
		Description: "Bridge of four wide ice decks over the water, joined by three tall hard ice arches; ice floes float low under the arches.",
		Build: func(g *Grid) {
			// Decks 5 rows thick at row 36; arches rise 6 rows to a flat at row 30.
			g.Chamfer(0, 36, 22, 40, 0, 2, Soft)
			g.Chamfer(41, 36, 62, 40, 0, 2, Soft)
			g.Band(23, 28, 35, -1, Hard)
			g.Rect(29, 30, 34, 30, Hard)
			g.Band(35, 40, 30, 1, Hard)
			g.Band(63, 68, 35, -1, Hard) // the centre arch, mirrored into its right half
			g.Rect(69, 30, 74, 30, Hard)
			// floes under the arches, low over the water
			g.Chamfer(25, 58, 38, 61, 1, 2, Soft)
			g.Chamfer(65, 58, 84, 61, 1, 2, Soft)
		},
		Spawns: [2][2]int{{31, 30}, {15, 36}},
	},
	{
		ID: "coconut_isles", Name: "Đảo dừa", Theme: "beach", Tier: "bronze", Wind: [2]float64{0.5, 3.5},
		Description: "A chain of seven wide sandy islets over the sea with narrow gaps, a palm on the middle islet and rocks low in the water.",
		Build: func(g *Grid) {
			for _, k := range [][2]int{{-2, 17}, {21, 39}, {43, 61}, {65, 84}} {
				g.Chamfer(k[0], 35, k[1], 39, 1, 3, Soft)
			}
			for _, c := range []int{12, 50} { // dunes
				g.Put(c, 34, Soft, SlopeR)
				g.Put(c+1, 34, Soft, SlopeL)
			}
			// the palm on the middle islet: a trunk and its canopy, cover for low shots
			g.Rect(74, 26, 74, 34, Soft)
			g.Chamfer(67, 20, 82, 25, 4, 3, Soft)
			// rocks under the gaps
			for _, c := range []int{15, 37, 59} {
				g.Chamfer(c, 56, c+8, 58, 1, 2, Hard)
			}
		},
		Spawns: [2][2]int{{32, 35}, {15, 35}},
	},
	{
		ID: "graveyard", Name: "Nghĩa trang", Theme: "graveyard", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Graveyard ground over the abyss: a wide raised field with a mound, a sunken middle with an open grave, hard tombstones, pumpkins and a hanging block.",
		Build: func(g *Grid) {
			g.Rect(0, 49, 44, 62, Soft)
			g.Hill(37, 40, 46, 48, Soft) // the raised mound
			// down into the sunken middle
			for i, c := 0, 45; c <= 50; i, c = i+1, c+1 {
				g.Put(c, 49+i, Soft, SlopeL)
				g.Rect(c, 50+i, c, 62, Soft)
			}
			g.Rect(51, 55, 74, 62, Soft)
			g.Clear(71, 55, 74, 62) // the grave, open into the abyss (cols 71..78)
			// tombstones (hard) as cover
			for _, t := range [][2]int{{4, 47}, {57, 53}} {
				g.Put(t[0], t[1], Hard, SlopeR)
				g.Put(t[0]+1, t[1], Hard, SlopeL)
				g.Rect(t[0], t[1]+1, t[0]+1, t[1]+1, Hard)
			}
			// pumpkins
			g.Chamfer(18, 9, 28, 17, 3, 3, Soft)
			g.Chamfer(68, 33, 81, 40, 3, 3, Soft)
			// the block hanging from the top
			g.Chamfer(52, 0, 97, 11, 0, 3, Soft)
		},
		Spawns: [2][2]int{{30, 49}, {15, 49}},
	},
	{
		ID: "pyramid", Name: "Kim tự tháp", Theme: "desert", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "A big hollow pyramid of hard stone in the middle of the sand, doorways at its base and a dune inside; lob over its apex or fight under its roof.",
		Build: func(g *Grid) {
			g.Rect(0, 57, 74, 61, Soft) // the sand
			g.Rect(0, 62, 74, 63, Hard)
			// the left wall: up from col 36 at row 56 to the apex at col 74, row 18
			for i, c := 0, 36; c <= 74; i, c = i+1, c+1 {
				r := 56 - i
				for k, s := range []Shape{SlopeR, Full, CeilL} {
					if r+k < 50 || c > 44 { // rows 50..56 under cols 36..44 stay open: the doorway
						g.Put(c, r+k, Hard, s)
					}
				}
			}
			g.Peak(74, 51, 56, Soft)     // the dune inside
			g.Rect(62, 38, 74, 39, Soft) // a ledge under the roof
			g.Put(74, 17, Soft, SlopeR)  // sand on the apex
		},
		Spawns: [2][2]int{{26, 57}, {15, 57}},
	},
	{
		ID: "floating_isle", Name: "Đảo nổi", Theme: "sky", Tier: "bronze", Wind: [2]float64{2, 4},
		Description: "Wide grassy islands with hard rock undersides scattered over the sky.",
		Build: func(g *Grid) {
			g.Island(66, 83, 3, 5, 3, 2)
			g.Island(18, 35, 17, 6, 3, 2)
			g.Island(54, 95, 33, 7, 3, 2)
			g.Island(30, 45, 45, 6, 3, 2)
			g.Island(68, 81, 56, 5, 3, 2)
			g.Island(4, 22, 61, 6, 3, 2)
		},
		Spawns: [2][2]int{{36, 45}, {25, 17}},
	},
	{
		ID: "frozen_land", Name: "Băng giá", Theme: "ice", Tier: "bronze", Wind: [2]float64{1.5, 4},
		Description: "Wide ice shelves over the water under hanging icicles; the icicles in the middle are hard.",
		Build: func(g *Grid) {
			g.Chamfer(36, 34, 57, 39, 1, 2, Soft)
			g.Icicle(41, 40, 3, Soft)
			g.Icicle(52, 40, 4, Soft)
			g.Chamfer(3, 56, 25, GridH-1, 1, 0, Soft)
			g.Chamfer(62, 54, 87, GridH-1, 1, 0, Soft)
			// hanging icicles: soft at the sides, hard in the middle
			g.Icicle(8, 14, 11, Soft)
			g.Icicle(13, 24, 10, Soft)
			g.Icicle(38, 4, 12, Soft)
			g.Icicle(60, 0, 11, Hard)
			g.Icicle(66, 0, 9, Hard)
			g.Icicle(72, 0, 12, Hard)
		},
		Spawns: [2][2]int{{40, 34}, {15, 56}},
	},
	{
		ID: "sky_temple", Name: "Đền tế", Theme: "temple", Tier: "silver", Wind: [2]float64{0.5, 3},
		Description: "Temple planks and boats above the lava: a long plank roof, hard pillars, two wide decks, a centre boat and two boats low at the sides.",
		Build: func(g *Grid) {
			g.Rect(16, 3, 64, 4, Soft) // the roof plank, open in the middle
			g.Rect(44, 0, 46, 9, Hard) // pillars through it
			g.Rect(50, 22, 52, 28, Hard)
			g.Rect(10, 22, 12, 26, Hard)
			g.Chamfer(30, 37, 54, 41, 1, 2, Soft) // the deck, a hard keel under 4 soft rows
			g.Paint(30, 41, 54, 41, Hard)
			g.Chamfer(64, 51, 85, 53, 1, 2, Soft) // the centre boat
			g.Chamfer(4, 57, 24, 61, 1, 2, Soft)  // the side boat
			g.Paint(4, 60, 24, 61, Hard)
		},
		Spawns: [2][2]int{{38, 37}, {15, 57}},
	},
	{
		ID: "escape", Name: "Trốn thoát", Theme: "city_night", Tier: "gold", Wind: [2]float64{0, 3},
		Description: "Rows of hard steel crates floating over the night city; nothing here breaks.",
		Build: func(g *Grid) {
			crates := func(c, r, n int) { g.Rect(c, r, c+4*n-1, r+3, Hard) }
			crates(67, 4, 2)  // top centre (cols 67..82)
			crates(14, 18, 1) // high singles
			crates(58, 31, 5) // the long middle row (58..91)
			crates(40, 33, 1)
			crates(28, 41, 4) // team 1's upper row
			crates(0, 37, 1)
			crates(0, 41, 1)
			crates(46, 49, 3)
			crates(6, 54, 4) // team 1's lower row
		},
		Spawns: [2][2]int{{34, 41}, {15, 54}},
	},
	{
		ID: "iron_bars", Name: "Song sắt", Theme: "steel", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Long hard steel bars and crates stacked over a soft floor above the water.",
		Build: func(g *Grid) {
			plank := func(c0, c1, r int) {
				for c := c0; c <= c1; c++ {
					g.Put(c, r, Hard, Plank)
				}
			}
			crate := func(c, r int) { g.Rect(c, r, c+2, r+2, Hard) }
			crate(34, 3)
			plank(37, 64, 6)
			plank(16, 42, 18)
			crate(50, 16)
			plank(60, 74, 18)
			plank(8, 28, 30)
			crate(10, 34)
			g.Rect(64, 31, 74, 33, Hard) // the crate pile in the middle
			g.Rect(69, 34, 74, 36, Hard)
			plank(30, 58, 43)
			crate(26, 50)
			plank(40, 74, 52)
			g.Rect(0, 58, 74, 62, Soft) // the floor
		},
		Spawns: [2][2]int{{33, 43}, {15, 58}},
	},
	{
		ID: "barricade", Name: "Rào cản", Theme: "steel", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "A lattice of hard steel studs over a wide soft floor: shoot through the gaps.",
		Build: func(g *Grid) {
			stud := func(c, r int) { g.Chamfer(c, r, c+1, r+2, 1, 1, Hard) }
			for _, r := range []int{18, 33, 48} {
				for c := 74; c >= 0; c -= 15 {
					stud(c, r)
				}
			}
			for _, r := range []int{25, 40} {
				for c := 67; c >= 0; c -= 15 {
					stud(c, r)
				}
			}
			g.Rect(0, 58, 74, 62, Soft) // the floor
		},
		Spawns: [2][2]int{{32, 58}, {15, 58}},
	},
	{
		ID: "iron_city", Name: "Thành phố sắt", Theme: "city_night", Tier: "gold", Wind: [2]float64{0, 2.5},
		Description: "Hard steel girders over the night city's water: ramps down from the corners to wide plated landings, a zigzag in the middle.",
		Build: func(g *Grid) {
			landing := func(c0, c1, r int) { // a hard girder at row r, plated soft on top
				g.Rect(c0, r, c1, r, Hard)
				g.Put(c0, r-1, Soft, SlopeR)
				g.Rect(c0+1, r-1, c1-1, r-1, Soft)
				g.Put(c1, r-1, Soft, SlopeL)
			}
			// from the top-left corner down to landing A
			g.Rect(0, 11, 5, 11, Hard)
			g.Band(6, 18, 11, 1, Hard)
			landing(19, 33, 24)
			// up from the water at the left edge to landing B
			g.Rect(0, 58, 9, 58, Hard)
			g.Band(10, 21, 57, -1, Hard)
			landing(22, 38, 46)
			// the zigzag
			g.Band(50, 55, 22, 1, Hard)
			g.Band(56, 61, 27, -1, Hard)
			g.Band(62, 67, 22, 1, Hard)
			g.Band(68, 73, 27, -1, Hard)
			g.Rect(74, 22, 74, 22, Hard)
			g.Rect(40, 6, 47, 6, Hard)
			g.Rect(70, 37, 74, 37, Hard)
			g.Rect(52, 40, 58, 40, Hard)
			g.Chamfer(12, 62, 19, 63, 1, 1, Soft)
			g.Chamfer(40, 62, 47, 63, 1, 0, Soft)
			g.Chamfer(62, 62, 68, 63, 1, 0, Soft)
		},
		Spawns: [2][2]int{{33, 45}, {26, 23}},
	},
	{
		ID: "stone_forest", Name: "Rừng đá", Theme: "stone", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Hard stone shapes over a wide grassy floor with a hard base, two pits into the abyss and stone spikes.",
		Build: func(g *Grid) {
			g.Rect(0, 55, 74, 57, Soft)
			g.Rect(0, 58, 74, 61, Hard)
			g.Clear(46, 55, 48, 61) // a pit
			g.Peak(24, 52, 54, Hard)
			g.Peak(74, 50, 54, Hard) // the centre spike
			g.Peak(28, 9, 12, Hard)  // a stone roof in the air
			// the house: roof and walls
			g.Hill(56, 57, 15, 18, Hard)
			g.Chamfer(52, 19, 61, 30, 0, 1, Hard)
			// a stone wedge and a shelf
			for i := 0; i <= 6; i++ {
				g.Rect(12, 22+i, 12+i-1, 22+i, Hard)
				g.Put(12+i, 22+i, Hard, SlopeL)
			}
			g.Chamfer(34, 32, 46, 36, 0, 3, Hard)
			g.Paint(34, 32, 46, 32, Soft)
			for i := 0; i <= 4; i++ {
				g.Rect(0, 36+i, i-1, 36+i, Hard)
				g.Put(i, 36+i, Hard, SlopeL)
			}
		},
		Spawns: [2][2]int{{34, 55}, {15, 55}},
	},
	{
		ID: "stone_cave", Name: "Hang đá", Theme: "cave", Tier: "silver", Wind: [2]float64{0, 2},
		Description: "Wide boulder bowls (hard, rubble on top) and wooden sticks floating in a cave.",
		Build: func(g *Grid) {
			bowl := func(c0, c1, r0, r1 int) {
				g.Chamfer(c0, r0, c1, r1, 1, 3, Hard)
				g.Paint(c0, r0, c1, r0+1, Soft)
			}
			bowl(28, 42, 0, 4)
			bowl(48, 64, 17, 22)
			bowl(16, 32, 28, 33)
			bowl(56, 72, 38, 43)
			bowl(2, 14, 44, 49)
			bowl(26, 52, 50, 57)
			bowl(8, 24, 63, GridH-1)
			bowl(64, 85, 63, GridH-1)
			// sticks
			g.Band(12, 16, 10, 1, Soft)
			g.Band(42, 46, 32, -1, Soft)
			g.Band(58, 62, 55, 1, Soft)
		},
		Spawns: [2][2]int{{34, 50}, {22, 28}},
	},
	{
		ID: "sky_islands", Name: "Đảo bay", Theme: "sky", Tier: "silver", Wind: [2]float64{2, 4},
		Description: "Many floating islets, grass over hard rock, with a long strip low over the water.",
		Build: func(g *Grid) {
			g.Island(68, 81, 2, 4, 3, 2)
			g.Island(44, 55, 12, 5, 3, 2)
			g.Island(58, 69, 23, 5, 3, 2)
			g.Island(6, 23, 24, 5, 3, 2)
			g.Island(25, 41, 36, 5, 3, 2)
			g.Island(48, 59, 38, 5, 3, 2)
			g.Island(66, 83, 46, 5, 3, 2)
			g.Island(10, 22, 50, 5, 3, 2)
			g.Island(48, 60, 56, 5, 3, 2)
			g.Chamfer(34, 65, 115, GridH-1, 1, 2, Soft)
		},
		Spawns: [2][2]int{{33, 36}, {15, 24}},
	},
	{
		ID: "iron_bridge", Name: "Cầu sắt", Theme: "steel", Tier: "bronze", Wind: [2]float64{0.5, 3.5},
		Description: "Three long steel decks (soft plates on a hard girder) over the water, hard trusses at their ends, hard crates above and in the gaps.",
		Build: func(g *Grid) {
			deck := func(c0, c1 int) {
				g.Chamfer(c0, 44, c1, 48, 0, 2, Soft)
				g.Paint(c0, 48, c1, 48, Hard)
			}
			deck(12, 46)
			deck(58, 91)
			truss := func(c int) { g.Peak(c+1, 42, 43, Hard) }
			truss(12)
			truss(43)
			truss(58)
			crate := func(c, r int) { g.Rect(c, r, c+3, r+3, Hard) }
			crate(40, 14)
			crate(24, 26)
			crate(54, 26)
			g.Rect(72, 12, 74, 17, Hard) // the middle crate and its roof
			g.Peak(74, 9, 11, Hard)
			g.Rect(8, 54, 11, 61, Hard)
			crate(48, 56)
		},
		Spawns: [2][2]int{{32, 44}, {20, 44}},
	},
	{
		ID: "steel_base", Name: "Căn cứ thép", Theme: "steel", Tier: "gold", Wind: [2]float64{0, 2.5},
		Description: "Hard steel girders and blocks over the water, wide plated landings for each team.",
		Build: func(g *Grid) {
			landing := func(c0, c1, r int) { // a hard girder at row r (2 thick), plated soft on top
				g.Rect(c0, r, c1, r+1, Hard)
				g.Put(c0, r-1, Soft, SlopeR)
				g.Rect(c0+1, r-1, c1-1, r-1, Soft)
				g.Put(c1, r-1, Soft, SlopeL)
			}
			g.Rect(0, 14, 9, 14, Hard)
			g.Rect(40, 5, 48, 5, Hard)
			g.Band(49, 56, 5, 1, Hard)
			g.Rect(62, 21, 70, 21, Hard)
			g.Rect(56, 30, 63, 30, Hard)
			landing(6, 20, 35) // the upper landing
			g.Band(21, 28, 35, 1, Hard)
			g.Band(66, 74, 45, 1, Hard) // the V in the middle
			g.Rect(50, 50, 57, 50, Hard)
			g.Band(14, 25, 64, -1, Hard)
			landing(26, 41, 53) // the lower landing, low over the water
		},
		Spawns: [2][2]int{{33, 52}, {15, 34}},
	},
}

// SpawnPoint is the pixel spawn of a spawn cell: its column's centre, just above the
// platform's top surface (the match drops it onto the surface, Terrain.SpawnY).
func SpawnPoint(col, row int) gamedata.SpawnPoint {
	return gamedata.SpawnPoint{X: float64(col*CellSize + CellSize/2), Y: float64(row*CellSize - 4)}
}

// Build draws a map's grid (mirrored).
func (d Def) Grid() *Grid {
	g := &Grid{}
	d.Build(g)
	g.Mirror()
	return g
}

// Map is the generated map config: team 1's spawns, then team 2's (TeamSpawns splits halves).
func (d Def) Map() gamedata.MapConfig {
	spawns := make([]gamedata.SpawnPoint, 0, 4)
	for _, s := range d.Spawns {
		spawns = append(spawns, SpawnPoint(s[0], s[1]))
	}
	for _, s := range d.Spawns {
		spawns = append(spawns, SpawnPoint(GridW-1-s[0], s[1]))
	}
	return gamedata.MapConfig{
		MapID: d.ID, Name: d.Name, Theme: d.Theme, MinRankTier: d.Tier, Description: d.Description,
		GridWidth: GridW, GridHeight: GridH, CellSize: CellSize,
		Width: GridW * CellSize, Height: GridH * CellSize,
		DefaultWindPowerRange: []float64{d.Wind[0], d.Wind[1]},
		Tiles:                 d.Grid().Tiles(),
		SpawnPoints:           spawns,
	}
}

// Maps are all generated map configs, in Defs order.
func Maps() []gamedata.MapConfig {
	out := make([]gamedata.MapConfig, len(Defs))
	for i, d := range Defs {
		out[i] = d.Map()
	}
	return out
}
