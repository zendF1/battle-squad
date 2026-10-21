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
// the reference thumbnails by eye, adapted to our 100 × 56 grid (the references are ~2.1:1, ours
// 1.79:1) and made symmetric; hard (indestructible) where the reference shows metal or stone.
var Defs = []Def{
	{
		ID: "ice_bridge", Name: "Cầu băng", Theme: "ice", Tier: "bronze", Wind: [2]float64{1.5, 4},
		Description: "Bridge of four ice decks over the water, joined by three hard ice arches.",
		Build: func(g *Grid) {
			// Decks 5 rows thick at row 30; the water under them is the map bottom.
			g.Chamfer(0, 30, 14, 34, 0, 2, Soft)
			g.Chamfer(28, 30, 42, 34, 0, 2, Soft)
			// Arches: up 4 rows, a flat top, down again; hard, one cell thick, over the gaps.
			arch := func(c0, flatEnd int) {
				g.Band(c0, c0+3, 29, -1, Hard)
				g.Rect(c0+4, 26, flatEnd, 26, Hard)
			}
			arch(15, 23)
			g.Band(24, 27, 26, 1, Hard)
			arch(43, 49) // the centre arch, mirrored into its right half
		},
		Spawns: [2][2]int{{21, 26}, {7, 30}},
	},
	{
		ID: "coconut_isles", Name: "Đảo dừa", Theme: "beach", Tier: "bronze", Wind: [2]float64{0.5, 3.5},
		Description: "A chain of seven sandy islets over the sea, two-cell gaps between them.",
		Build: func(g *Grid) {
			for _, k := range [][2]int{{2, 4}, {16, 18}, {30, 35}, {44, 49}} {
				g.Chamfer(k[0], 30, k[0]+11, 34, 1, 3, Soft)
				// a dune on each islet (the middle one's is mirrored into a 2-cell peak)
				g.Put(k[1], 29, Soft, SlopeR)
				g.Put(k[1]+1, 29, Soft, SlopeL)
			}
		},
		Spawns: [2][2]int{{22, 30}, {8, 30}},
	},
	{
		ID: "graveyard", Name: "Nghĩa trang", Theme: "graveyard", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Graveyard ground over the abyss: raised mounds, a sunken middle with an open grave, hard tombstones, pumpkins and a hanging block.",
		Build: func(g *Grid) {
			g.Rect(0, 40, 28, 50, Soft)
			g.Hill(24, 26, 38, 39, Soft) // the raised mound
			// down into the sunken middle
			for i, c := 0, 29; c <= 32; i, c = i+1, c+1 {
				g.Put(c, 40+i, Soft, SlopeL)
				g.Rect(c, 41+i, c, 50, Soft)
			}
			g.Rect(33, 44, 47, 50, Soft)
			// tombstones (hard) as cover
			for _, t := range [][2]int{{3, 38}, {37, 42}} {
				g.Put(t[0], t[1], Hard, SlopeR)
				g.Put(t[0]+1, t[1], Hard, SlopeL)
				g.Rect(t[0], t[1]+1, t[0]+1, t[1]+1, Hard)
			}
			// pumpkins
			g.Chamfer(12, 9, 17, 14, 2, 2, Soft)
			g.Chamfer(47, 27, 52, 32, 2, 2, Soft)
			// the block hanging from the top
			g.Chamfer(37, 0, 62, 9, 0, 2, Soft)
			// cols 48..51 stay open under the ground: the grave, down into the abyss
		},
		Spawns: [2][2]int{{25, 38}, {9, 40}},
	},
	{
		ID: "pyramid", Name: "Kim tự tháp", Theme: "desert", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "A hollow pyramid of hard stone on the sand, doorways at its base and a dune inside; climb its sides or fight under its roof.",
		Build: func(g *Grid) {
			g.Rect(0, 46, 49, 51, Soft) // the sand
			g.Rect(18, 50, 49, 51, Hard)
			// the left wall: up from col 16 at row 45 to the apex at col 49, row 12
			for i, c := 0, 16; c <= 49; i, c = i+1, c+1 {
				r := 45 - i
				for k, s := range []Shape{SlopeR, Full, CeilL} {
					if r+k < 42 || c > 23 { // rows 42..45 under cols 16..23 stay open: the doorway
						g.Put(c, r+k, Hard, s)
					}
				}
			}
			g.Hill(46, 49, 42, 45, Soft) // the dune inside
			g.Put(49, 11, Soft, SlopeR)  // sand on the apex
		},
		Spawns: [2][2]int{{30, 46}, {8, 46}},
	},
	{
		ID: "floating_isle", Name: "Đảo nổi", Theme: "sky", Tier: "bronze", Wind: [2]float64{2, 4},
		Description: "Grassy islands with hard rock undersides scattered over the sky.",
		Build: func(g *Grid) {
			g.Island(46, 53, 1, 5, 3, 2)
			g.Island(22, 30, 12, 6, 3, 2)
			g.Island(40, 59, 27, 6, 3, 2)
			g.Island(29, 37, 36, 6, 3, 2)
			g.Island(45, 54, 44, 6, 3, 2)
			g.Island(13, 26, 50, 6, 3, 2)
		},
		Spawns: [2][2]int{{26, 12}, {19, 50}},
	},
	{
		ID: "frozen_land", Name: "Băng giá", Theme: "ice", Tier: "bronze", Wind: [2]float64{1.5, 4},
		Description: "Ice shelves over the water under hanging icicles; the four in the middle are hard.",
		Build: func(g *Grid) {
			g.Chamfer(21, 29, 36, 34, 1, 2, Soft)
			g.Icicle(24, 35, 2, Soft)
			g.Icicle(31, 35, 3, Soft)
			g.Chamfer(3, 49, 19, GridH-1, 1, 0, Soft)
			g.Chamfer(41, 49, 58, GridH-1, 1, 0, Soft)
			// hanging icicles: soft at the sides, hard in the middle
			g.Icicle(7, 14, 9, Soft)
			g.Icicle(10, 21, 9, Soft)
			g.Icicle(24, 4, 10, Soft)
			g.Icicle(41, 0, 9, Hard)
			g.Icicle(46, 0, 8, Hard)
		},
		Spawns: [2][2]int{{28, 29}, {10, 49}},
	},
	{
		ID: "sky_temple", Name: "Đền tế", Theme: "temple", Tier: "silver", Wind: [2]float64{0.5, 3},
		Description: "Temple planks and boats above the lava: a long plank roof, hard pillars, two decks, a low centre plank.",
		Build: func(g *Grid) {
			g.Rect(12, 3, 46, 4, Soft) // the roof plank, open in the middle
			g.Rect(30, 0, 31, 8, Hard) // pillars through it
			g.Rect(30, 14, 31, 23, Hard)
			g.Rect(8, 18, 10, 21, Hard)
			g.Chamfer(17, 29, 40, 32, 1, 2, Soft) // the deck
			g.Chamfer(43, 42, 56, 43, 1, 0, Soft) // centre plank
			g.Chamfer(12, 50, 29, 54, 1, 2, Soft) // the boat
			g.Paint(12, 53, 29, 54, Hard)
		},
		Spawns: [2][2]int{{28, 29}, {20, 50}},
	},
	{
		ID: "escape", Name: "Trốn thoát", Theme: "city_night", Tier: "gold", Wind: [2]float64{0, 3},
		Description: "Hard steel crates floating in arcs over the night city; nothing here breaks.",
		Build: func(g *Grid) {
			for _, k := range [][2]int{
				{44, 3}, {48, 3},
				{10, 15},
				{28, 26}, {40, 26}, {44, 26}, {48, 26},
				{44, 30}, {48, 30},
				{34, 31}, {19, 31},
				{0, 31}, {0, 35},
				{24, 36}, {28, 36}, {38, 36},
				{15, 36}, {15, 41},
				{5, 48}, {9, 48},
			} {
				g.Rect(k[0], k[1], k[0]+3, k[1]+3, Hard)
			}
		},
		Spawns: [2][2]int{{20, 31}, {9, 48}},
	},
	{
		ID: "iron_bars", Name: "Song sắt", Theme: "steel", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Hard steel bars and crates stacked over a soft floor above the water.",
		Build: func(g *Grid) {
			plank := func(c0, c1, r int) {
				for c := c0; c <= c1; c++ {
					g.Put(c, r, Hard, Plank)
				}
			}
			crate := func(c, r int) { g.Rect(c, r, c+2, r+2, Hard) }
			crate(24, 1)
			plank(27, 44, 4)
			plank(14, 32, 14)
			plank(40, 49, 14)
			crate(36, 15)
			plank(16, 34, 23)
			crate(44, 26)
			crate(47, 26)
			crate(45, 29)
			g.Rect(48, 29, 49, 31, Hard)
			crate(8, 28)
			plank(20, 42, 34)
			crate(17, 41)
			plank(21, 40, 41)
			g.Rect(0, 46, 49, 49, Soft) // the floor
		},
		Spawns: [2][2]int{{25, 23}, {4, 46}},
	},
	{
		ID: "barricade", Name: "Rào cản", Theme: "steel", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "A lattice of hard steel studs over a soft floor: shoot through the gaps.",
		Build: func(g *Grid) {
			stud := func(c, r int) { g.Chamfer(c, r, c+1, r+2, 1, 1, Hard) }
			for _, r := range []int{15, 27, 39} {
				for c := 4; c < 50; c += 10 {
					stud(c, r)
				}
			}
			for _, r := range []int{21, 33} {
				for c := 9; c < 50; c += 10 {
					stud(c, r)
				}
			}
			g.Rect(0, 46, 49, 50, Soft)
		},
		Spawns: [2][2]int{{27, 46}, {7, 46}},
	},
	{
		ID: "iron_city", Name: "Thành phố sắt", Theme: "city_night", Tier: "gold", Wind: [2]float64{0, 2.5},
		Description: "Hard steel girders over the night city's water: ramps down from the corners, a zigzag in the middle, plated landings.",
		Build: func(g *Grid) {
			// from the top-left corner down to landing A (plated)
			g.Rect(0, 8, 3, 8, Hard)
			g.Band(4, 15, 8, 1, Hard)
			g.Rect(16, 20, 23, 20, Hard)
			g.Put(16, 19, Soft, SlopeR)
			g.Rect(17, 19, 22, 19, Soft)
			g.Put(23, 19, Soft, SlopeL)
			// up from the water at the left edge to landing B (plated)
			g.Rect(0, 44, 5, 44, Hard)
			g.Band(6, 13, 43, -1, Hard)
			g.Rect(14, 36, 19, 36, Hard)
			g.Put(14, 35, Soft, SlopeR)
			g.Rect(15, 35, 18, 35, Soft)
			g.Put(19, 35, Soft, SlopeL)
			g.Band(34, 38, 18, 1, Hard)  // the zigzag
			g.Band(39, 43, 22, -1, Hard)
			g.Band(44, 49, 18, 1, Hard)
			g.Rect(30, 5, 33, 5, Hard)
			g.Rect(47, 29, 49, 29, Hard)
			g.Rect(37, 33, 40, 33, Hard)
			g.Rect(38, 34, 39, 34, Hard)
			g.Chamfer(8, 48, 13, 49, 1, 1, Soft)
			g.Chamfer(23, 48, 27, 49, 1, 0, Soft)
			g.Chamfer(41, 49, 45, 50, 1, 0, Soft)
		},
		Spawns: [2][2]int{{20, 19}, {17, 35}},
	},
	{
		ID: "stone_forest", Name: "Rừng đá", Theme: "stone", Tier: "bronze", Wind: [2]float64{0, 3},
		Description: "Hard stone shapes over a grassy floor with a hard base, two pits into the abyss and stone spikes.",
		Build: func(g *Grid) {
			g.Rect(0, 44, 49, 45, Soft)
			g.Rect(0, 46, 49, 48, Hard)
			g.Clear(30, 44, 31, 48) // a pit
			g.Peak(17, 41, 43, Hard)
			g.Peak(49, 40, 43, Hard) // the centre spike
			g.Peak(19, 7, 10, Hard)  // a stone roof in the air
			// the house: roof and walls
			g.Hill(36, 37, 12, 14, Hard)
			g.Chamfer(33, 15, 40, 22, 0, 1, Hard)
			// a stone wedge, a ledge and a shelf on the wall
			for i := 0; i <= 4; i++ {
				g.Rect(9, 17+i, 9+i-1, 17+i, Hard)
				g.Put(9+i, 17+i, Hard, SlopeL)
			}
			g.Chamfer(20, 26, 27, 29, 0, 3, Hard)
			g.Paint(20, 26, 27, 26, Soft)
			for i := 0; i <= 3; i++ {
				g.Rect(0, 24+i, i-1, 24+i, Hard)
				g.Put(i, 24+i, Hard, SlopeL)
			}
		},
		Spawns: [2][2]int{{24, 44}, {6, 44}},
	},
	{
		ID: "stone_cave", Name: "Hang đá", Theme: "cave", Tier: "silver", Wind: [2]float64{0, 2},
		Description: "Boulder bowls (hard, rubble on top) and wooden sticks floating in a cave.",
		Build: func(g *Grid) {
			bowl := func(c0, c1, r0, r1 int) {
				g.Chamfer(c0, r0, c1, r1, 1, 3, Hard)
				g.Paint(c0, r0, c1, r0+1, Soft)
			}
			bowl(18, 28, 0, 3)
			bowl(33, 43, 14, 18)
			bowl(15, 22, 23, 27)
			bowl(44, 55, 29, 33)
			bowl(5, 12, 33, 37)
			bowl(21, 39, 39, 44)
			bowl(8, 20, 49, 54)
			bowl(46, 53, 50, GridH-1)
			// sticks
			g.Band(10, 13, 8, 1, Soft)
			g.Band(29, 32, 22, -1, Soft)
			g.Band(40, 43, 39, 1, Soft)
		},
		Spawns: [2][2]int{{27, 39}, {18, 23}},
	},
	{
		ID: "sky_islands", Name: "Đảo bay", Theme: "sky", Tier: "silver", Wind: [2]float64{2, 4},
		Description: "Many small floating islets, grass over hard rock, with a long strip low over the water.",
		Build: func(g *Grid) {
			g.Island(46, 53, 1, 4, 3, 2)
			g.Island(34, 40, 9, 5, 3, 2)
			g.Island(41, 47, 21, 5, 3, 2)
			g.Island(26, 32, 28, 5, 3, 2)
			g.Island(45, 54, 36, 5, 3, 2)
			g.Island(32, 38, 42, 5, 3, 2)
			g.Island(12, 19, 30, 5, 3, 2)
			g.Island(5, 10, 44, 5, 3, 2)
			g.Chamfer(24, 51, 41, GridH-1, 1, 2, Soft)
		},
		Spawns: [2][2]int{{15, 30}, {25, 51}},
	},
	{
		ID: "iron_bridge", Name: "Cầu sắt", Theme: "steel", Tier: "bronze", Wind: [2]float64{0.5, 3.5},
		Description: "Three steel decks (soft plates on a hard girder) over the water, hard trusses at their ends, hard crates above and in the gaps.",
		Build: func(g *Grid) {
			deck := func(c0, c1 int) {
				g.Chamfer(c0, 36, c1, 40, 0, 2, Soft)
				g.Paint(c0, 40, c1, 40, Hard)
			}
			deck(6, 29)
			deck(38, 61)
			truss := func(c int) { g.Peak(c+1, 34, 35, Hard) }
			truss(6)
			truss(26)
			truss(38)
			crate := func(c, r int) { g.Rect(c, r, c+2, r+2, Hard) }
			crate(18, 20)
			crate(27, 12)
			crate(37, 20)
			g.Rect(48, 12, 49, 15, Hard) // the middle crate and its roof
			g.Peak(49, 10, 11, Hard)
			crate(8, 44)
			crate(32, 44)
			crate(35, 44)
		},
		Spawns: [2][2]int{{22, 36}, {12, 36}},
	},
	{
		ID: "steel_base", Name: "Căn cứ thép", Theme: "steel", Tier: "gold", Wind: [2]float64{0, 2.5},
		Description: "Hard steel girders and blocks over the water, plated landings for each team.",
		Build: func(g *Grid) {
			g.Rect(0, 10, 7, 10, Hard)
			g.Rect(26, 5, 29, 5, Hard)
			g.Band(30, 40, 5, 1, Hard)
			g.Rect(43, 15, 46, 15, Hard)
			g.Rect(42, 22, 45, 22, Hard)
			g.Rect(0, 30, 6, 30, Hard)
			g.Rect(22, 28, 32, 28, Hard) // the upper landing, plated
			g.Put(22, 27, Soft, SlopeR)
			g.Rect(23, 27, 31, 27, Soft)
			g.Put(32, 27, Soft, SlopeL)
			g.Band(33, 40, 28, 1, Hard)
			g.Band(46, 49, 46, 1, Hard) // the V in the middle
			g.Rect(35, 44, 39, 44, Hard)
			g.Band(0, 4, 55, -1, Hard)
			g.Rect(5, 51, 12, 52, Hard) // the lower landing, plated, low over the water
			g.Put(5, 50, Soft, SlopeR)
			g.Rect(6, 50, 11, 50, Soft)
			g.Put(12, 50, Soft, SlopeL)
		},
		Spawns: [2][2]int{{27, 27}, {9, 50}},
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
