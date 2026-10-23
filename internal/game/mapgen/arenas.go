package mapgen

import (
	"fmt"
	"strings"

	"battle-squad/internal/game/gamedata"
)

// Files cmd/mapgen writes for the boss arenas: 045 the phase 1 arenas (Arenas). 047 (the boss v2
// arenas, ArenasV2) was generated the same way and is now frozen as applied: 049 moves the
// mound's minion spawns off the crab's body (TestArenasV2Migrations: 047 + 049 = ArenasV2).
const (
	ArenasUpFile       = "migrations/045_boss_arenas.up.sql"
	ArenasDownFile     = "migrations/045_boss_arenas.down.sql"
	ArenasV2UpFile     = "migrations/047_boss_arenas_v2.up.sql"
	ArenaSpawnsFixFile = "migrations/049_boss_placement.up.sql"
)

// ArenaDef is a boss arena (not mirrored unless Build mirrors it). Spawn cells (column, row of
// the platform's top surface): Party (4), Boss, Minions; the map's spawn points are in that order
// (gamedata.BossSpawns). Pillars: the party and minion cells are all pillar tops (spawn set
// pillar): the party spawns on random distinct ones and the minions appear on free ones
// (gamedata.ArenaPillars); the order still works for an editor that drops the sets.
type ArenaDef struct {
	ID          string
	Name        string
	Theme       string
	Wind        [2]float64
	Description string
	Build       func(g *Grid)
	Party       [4][2]int
	Boss        [2]int
	Minions     [][2]int
	Pillars     bool
}

// Arenas are the boss arenas (config_bosses.map_id), one per boss. Same 150 × 70 grid as the
// battle maps; the boss stands ~1400-1800 px from the party, the minions in between. The boss's
// platform is hard (indestructible), so blasts never drop it off the map.
var Arenas = []ArenaDef{
	{
		ID: "arena_steel_robot", Name: "Xưởng thép", Theme: "steel", Wind: [2]float64{0, 2.5},
		Description: "Boss arena of Rô-bốt Thép: a plated deck for the party, a girder bridge for the drones and the titan's hard steel dock on the right.",
		Build: func(g *Grid) {
			deck := func(c0, c1, r int) { // soft plates on a hard girder
				g.Chamfer(c0, r, c1, r+5, 1, 2, Soft)
				g.Paint(c0, r+4, c1, r+5, Hard)
			}
			deck(2, 46, 48)
			deck(64, 98, 46)
			g.Chamfer(104, 42, 148, 54, 1, 3, Hard) // the dock
			g.Rect(50, 28, 53, 31, Hard)            // crates for cover
			g.Rect(88, 24, 91, 27, Hard)
			g.Band(56, 62, 52, -1, Hard) // girders between the decks
		},
		Party:   [4][2]int{{10, 48}, {18, 48}, {26, 48}, {34, 48}},
		Boss:    [2]int{124, 42},
		Minions: [][2]int{{72, 46}, {81, 46}, {90, 46}, {110, 42}},
	},
	{
		ID: "arena_lava_crab", Name: "Miệng núi lửa", Theme: "cave", Wind: [2]float64{0, 2},
		Description: "Boss arena of Cua Dung Nham: a rocky ledge for the party, two crusted islands for the crabs and the crab's hard basalt shelf on the right.",
		Build: func(g *Grid) {
			g.Chamfer(2, 49, 44, 56, 1, 3, Soft) // the ledge, soft rock over basalt
			g.Paint(2, 54, 44, 56, Hard)
			g.Island(60, 76, 47, 6, 3, 3)
			g.Island(82, 98, 45, 6, 3, 3)
			g.Chamfer(104, 41, 148, 55, 1, 4, Hard) // the shelf
			g.Hill(52, 54, 36, 39, Hard)            // a basalt spur for cover
			g.Icicle(30, 0, 8, Hard)                // stalactites at the edges, clear of the arcs
			g.Icicle(140, 0, 9, Hard)
		},
		Party:   [4][2]int{{10, 49}, {18, 49}, {26, 49}, {34, 49}},
		Boss:    [2]int{125, 41},
		Minions: [][2]int{{66, 47}, {72, 47}, {88, 45}, {94, 45}},
	},
	{
		ID: "arena_ice_witch", Name: "Đỉnh tuyết", Theme: "ice", Wind: [2]float64{1, 3.5},
		Description: "Boss arena of Phù Thuỷ Băng: a snow shelf for the party, a frozen bridge for the snowmen and the witch's hard ice cliff on the right, icicles above.",
		Build: func(g *Grid) {
			g.Chamfer(2, 47, 45, 54, 1, 3, Soft)
			g.Paint(2, 52, 45, 54, Hard)
			g.Chamfer(62, 45, 98, 49, 1, 2, Soft) // the frozen bridge
			g.Paint(62, 49, 98, 49, Hard)
			g.Chamfer(104, 40, 148, 56, 1, 4, Hard) // the cliff
			g.Icicle(70, 0, 10, Soft)
			g.Icicle(84, 0, 7, Soft)
			g.Peak(52, 40, 46, Hard) // an ice spire between the shelf and the bridge
		},
		Party:   [4][2]int{{10, 47}, {18, 47}, {26, 47}, {34, 47}},
		Boss:    [2]int{124, 40},
		Minions: [][2]int{{68, 45}, {77, 45}, {86, 45}, {110, 40}},
	},
}

// Boss v2 arenas (migration 047): Cua Dung Nham's mound in the middle of continuous ground and
// Phù Thuỷ Băng's ice pillars. Rô-bốt Thép keeps arena_steel_robot.
const (
	CrabMoundTop   = 40 // row of the mound's flat top (the boss stands on it)
	crabGround     = 50 // row of the ground's surface
	crabSoftRows   = 5  // soft rows over the hard core: craters never reach below them
	witchPillarW   = 9  // cells
	witchPillarGap = 16 // cells from a pillar's left column to the next one's
	witchFirstCol  = 7
	// The mound's slope minion spawns: the slope brick crabSlopeDrop rows under the top in
	// column crabSlopeCol and its mirror (x 936 / 1464, ~250 px from the boss at x 1192: a
	// crab's ~37 px body stands > 40 px clear of the crab boss's ~158 px one).
	crabSlopeCol  = 58
	crabSlopeDrop = 8
)

// WitchPillarTops are the top rows of the ice pillars, left to right.
var WitchPillarTops = [9]int{44, 39, 46, 41, 37, 41, 46, 39, 44}

// witchPillar is the spawn cell on top of pillar i (its middle column).
func witchPillar(i int) [2]int {
	return [2]int{witchFirstCol + witchPillarGap*i + witchPillarW/2, WitchPillarTops[i]}
}

// ArenasV2 are the boss v2 arenas (migration 047).
var ArenasV2 = []ArenaDef{
	{
		ID: "arena_lava_crab_mound", Name: "Gò dung nham", Theme: "cave", Wind: [2]float64{0, 2},
		Description: "Boss arena of Cua Dung Nham v2: continuous volcanic ground from edge to edge (soft rock over a hard basalt core, gentle hills), the crab's hard basalt mound in the middle, two party spawns on each side.",
		Build: func(g *Grid) {
			// Left half, mirrored: the ground (soft over hard, no gap anywhere), low hills, the mound
			g.Rect(0, crabGround, GridW/2-1, GridH-1, Hard)
			g.Paint(0, crabGround, GridW/2-1, crabGround+crabSoftRows-1, Soft)
			g.Hill(3, 5, 48, crabGround-1, Soft)
			g.Hill(38, 41, 47, crabGround-1, Soft)
			g.Hill(67, 82, CrabMoundTop, crabGround-1, Hard)
			g.Mirror()
		},
		Party: [4][2]int{{12, crabGround}, {137, crabGround}, {28, crabGround}, {121, crabGround}},
		Boss:  [2]int{74, CrabMoundTop},
		// the mound's slopes, then its feet (049; 047 had the first two on the top, x 1080 / 1320,
		// inside the crab's drawn body)
		Minions: [][2]int{{crabSlopeCol, CrabMoundTop + crabSlopeDrop}, {GridW - 1 - crabSlopeCol, CrabMoundTop + crabSlopeDrop},
			{54, crabGround}, {95, crabGround}},
	},
	{
		ID: "arena_ice_witch_pillars", Name: "Cột băng", Theme: "ice", Wind: [2]float64{1, 3.5},
		Description: "Boss arena of Phù Thuỷ Băng v2: nine hard ice pillars capped with snow over the abyss, the party on random pillars, snowmen on the free ones, the witch hovering above.",
		Build: func(g *Grid) {
			for i, top := range WitchPillarTops {
				c0 := witchFirstCol + witchPillarGap*i
				g.Chamfer(c0, top, c0+witchPillarW-1, GridH-1, 1, 0, Hard)
				g.Paint(c0, top, c0+witchPillarW-1, top+1, Soft) // the snow cap
			}
		},
		// the outer pillars first (the party's fallback), the boss over the middle one, the rest
		Party:   [4][2]int{witchPillar(0), witchPillar(8), witchPillar(2), witchPillar(6)},
		Boss:    witchPillar(4),
		Minions: [][2]int{witchPillar(1), witchPillar(3), witchPillar(4), witchPillar(5), witchPillar(7)},
		Pillars: true,
	},
}

// AllArenas are every generated arena: Arenas, then ArenasV2.
func AllArenas() []ArenaDef { return append(append([]ArenaDef{}, Arenas...), ArenasV2...) }

// Grid draws the arena's grid (not mirrored).
func (a ArenaDef) Grid() *Grid {
	g := &Grid{}
	a.Build(g)
	return g
}

// Map is the arena's map config: the party's spawns, the boss's, the minions'.
func (a ArenaDef) Map() gamedata.MapConfig {
	spawns := make([]gamedata.SpawnPoint, 0, 5+len(a.Minions))
	pillar := func(s [2]int) gamedata.SpawnPoint {
		sp := SpawnPoint(s[0], s[1])
		if a.Pillars {
			sp.Set = gamedata.SpawnSetPillar
		}
		return sp
	}
	for _, s := range a.Party {
		spawns = append(spawns, pillar(s))
	}
	spawns = append(spawns, SpawnPoint(a.Boss[0], a.Boss[1]))
	for _, s := range a.Minions {
		spawns = append(spawns, pillar(s))
	}
	return gamedata.MapConfig{
		MapID: a.ID, Name: a.Name, Theme: a.Theme, MinRankTier: "bronze", Description: a.Description,
		GridWidth: GridW, GridHeight: GridH, CellSize: CellSize,
		Width: GridW * CellSize, Height: GridH * CellSize,
		DefaultWindPowerRange: []float64{a.Wind[0], a.Wind[1]},
		Tiles:                 a.Grid().Tiles(),
		SpawnPoints:           spawns,
	}
}

// ArenaMaps are the map configs of every arena (AllArenas order).
func ArenaMaps() []gamedata.MapConfig { return arenaMaps(AllArenas()) }

func arenaMaps(defs []ArenaDef) []gamedata.MapConfig {
	out := make([]gamedata.MapConfig, len(defs))
	for i, a := range defs {
		out[i] = a.Map()
	}
	return out
}

// ArenasUpSQL is migration 045: the boss arenas, inserted like 037 inserted the battle maps. A
// row that exists is kept, so an arena edited in the admin map editor survives reruns.
func ArenasUpSQL() string {
	return arenasUpSQL(ArenasUpFile, Arenas, "-- The boss arenas (config_bosses.map_id)")
}

// arenasV2UpSQL is migration 047 as ArenasV2 would render it now: the boss v2 arenas
// (048_boss_mode_v2 points the crab and the witch at them), inserted the same way. 047 itself
// is frozen; 049 updates its spawns.
func arenasV2UpSQL() string {
	return arenasUpSQL(ArenasV2UpFile, ArenasV2, "-- The boss v2 arenas (Cua Dung Nham's mound, Phù Thuỷ Băng's pillars)")
}

func arenasUpSQL(file string, defs []ArenaDef, title string) string {
	var b strings.Builder
	b.WriteString("-- " + file + "\n-- " + generatedNote + "\n")
	fmt.Fprintf(&b, "%s, %d x %d cells of %d px, editable in the admin map editor.\n", title, GridW, GridH, CellSize)
	b.WriteString("-- Spawn points: the party's 4, the boss's, then the minions'. A row that exists is kept.\n\n")
	for _, m := range arenaMaps(defs) {
		fmt.Fprintf(&b, "-- %s (%s, %s)\n", m.MapID, m.Name, m.Theme)
		b.WriteString("INSERT INTO config_maps (map_id, name, theme, width, height, grid_width, grid_height, cell_size, default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier)\n")
		fmt.Fprintf(&b, "VALUES (%s, %s, %s, %d, %d, %d, %d, %d, %s, '[]', %s, %s, %s, %s)\n", sqlString(m.MapID), sqlString(m.Name), sqlString(m.Theme),
			m.Width, m.Height, m.GridWidth, m.GridHeight, m.CellSize, sqlString(windJSON(m.DefaultWindPowerRange)),
			sqlString(SpawnJSON(m.SpawnPoints)), sqlString(TilesJSON(m.Tiles)), sqlString(m.Description), sqlString(m.MinRankTier))
		b.WriteString("ON CONFLICT (map_id) DO NOTHING;\n\n")
	}
	return b.String()
}

// ArenasDownSQL removes the arenas 045 inserted.
func ArenasDownSQL() string { return arenasDownSQL(ArenasDownFile, Arenas) }

func arenasDownSQL(file string, defs []ArenaDef) string {
	ids := make([]string, len(defs))
	for i, a := range defs {
		ids[i] = sqlString(a.ID)
	}
	return "-- " + file + "\n-- " + generatedNote + "\n\n" +
		"DELETE FROM config_maps WHERE map_id IN (" + strings.Join(ids, ", ") + ");\n"
}
