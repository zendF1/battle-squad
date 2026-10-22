package mapgen

import (
	"fmt"
	"strings"

	"battle-squad/internal/game/gamedata"
)

// Files cmd/mapgen writes for the boss arenas.
const (
	ArenasUpFile   = "migrations/045_boss_arenas.up.sql"
	ArenasDownFile = "migrations/045_boss_arenas.down.sql"
)

// ArenaDef is a boss arena: not mirrored, the party's side on the left, the boss's big hard
// platform on the right and a middle ground for the minions. Spawn cells (column, row of the
// platform's top surface): Party (4), Boss, Minions; the map's spawn points are in that order
// (gamedata.BossSpawns).
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

// Grid draws the arena's grid (not mirrored).
func (a ArenaDef) Grid() *Grid {
	g := &Grid{}
	a.Build(g)
	return g
}

// Map is the arena's map config: the party's spawns, the boss's, the minions'.
func (a ArenaDef) Map() gamedata.MapConfig {
	spawns := make([]gamedata.SpawnPoint, 0, 5+len(a.Minions))
	for _, s := range a.Party {
		spawns = append(spawns, SpawnPoint(s[0], s[1]))
	}
	spawns = append(spawns, SpawnPoint(a.Boss[0], a.Boss[1]))
	for _, s := range a.Minions {
		spawns = append(spawns, SpawnPoint(s[0], s[1]))
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

// ArenaMaps are the arena map configs, in Arenas order.
func ArenaMaps() []gamedata.MapConfig {
	out := make([]gamedata.MapConfig, len(Arenas))
	for i, a := range Arenas {
		out[i] = a.Map()
	}
	return out
}

// ArenasUpSQL is migration 045: the boss arenas, inserted like 037 inserted the battle maps. A
// row that exists is kept, so an arena edited in the admin map editor survives reruns.
func ArenasUpSQL() string {
	var b strings.Builder
	b.WriteString("-- migrations/045_boss_arenas.up.sql\n-- " + generatedNote + "\n")
	fmt.Fprintf(&b, "-- The boss arenas (config_bosses.map_id), %d x %d cells of %d px, editable in the admin map editor.\n", GridW, GridH, CellSize)
	b.WriteString("-- Spawn points: the party's 4, the boss's, then the minions'. A row that exists is kept.\n\n")
	for _, m := range ArenaMaps() {
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
func ArenasDownSQL() string {
	ids := make([]string, len(Arenas))
	for i, a := range Arenas {
		ids[i] = sqlString(a.ID)
	}
	return "-- migrations/045_boss_arenas.down.sql\n-- " + generatedNote + "\n\n" +
		"DELETE FROM config_maps WHERE map_id IN (" + strings.Join(ids, ", ") + ");\n"
}
