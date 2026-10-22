package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

func TestNewTerrainFromTiles(t *testing.T) {
	cfg := gamedata.MapConfig{
		MapID:      "test_map",
		GridWidth:  4,
		GridHeight: 3,
		CellSize:   16,
		Tiles: [][]int{
			{0, 0, 0, 0},
			{0, 1, 1, 0},
			{2, 2, 2, 2},
		},
	}

	useData(t, &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_map": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	})
	useBricks(t, map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	})

	terrain := NewTerrain(cfg)

	if terrain.IsSolid(0, 0) {
		t.Error("expected air at (0,0)")
	}
	if terrain.IsSolid(16, 8) {
		t.Error("expected air at (16,8)")
	}
	if !terrain.IsSolid(20, 20) {
		t.Error("expected solid at (20,20) — brick 1 cell")
	}
	if terrain.IsSolid(8, 20) {
		t.Error("expected air at (8,20)")
	}
	if !terrain.IsSolid(0, 40) {
		t.Error("expected solid at (0,40) — brick 2 cell")
	}
}

func TestDestroyCircleRespectsDestructible(t *testing.T) {
	cfg := gamedata.MapConfig{
		MapID:      "test_destroy",
		GridWidth:  4,
		GridHeight: 2,
		CellSize:   16,
		Tiles: [][]int{
			{1, 1, 2, 2},
			{1, 1, 2, 2},
		},
	}

	useData(t, &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_destroy": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	})
	useBricks(t, map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	})

	terrain := NewTerrain(cfg)
	terrain.DestroyCircle(32, 16, 50)

	if terrain.IsSolid(8, 8) {
		t.Error("expected brick 1 at (8,8) to be destroyed")
	}
	if !terrain.IsSolid(56, 8) {
		t.Error("expected brick 2 at (56,8) to remain solid")
	}
}

func TestNewTerrainLegacyFallback(t *testing.T) {
	cfg := gamedata.MapConfig{
		MapID:      "grassland_valley",
		GridWidth:  100,
		GridHeight: 56,
		CellSize:   16,
		Tiles:      nil,
		Width:      1600,
		Height:     900,
	}

	useData(t, &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"grassland_valley": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	})
	useBricks(t, nil)

	terrain := NewTerrain(cfg)

	if !terrain.IsSolid(800, 850) {
		t.Error("expected solid near bottom of map with legacy generation")
	}
	if terrain.IsSolid(800, 100) {
		t.Error("expected air near top of map with legacy generation")
	}
}

func TestNewTerrainPolygonBorder(t *testing.T) {
	// Full square brick with explicit border — should fill fully
	cfg := gamedata.MapConfig{
		MapID:      "test_poly",
		GridWidth:  1,
		GridHeight: 1,
		CellSize:   16,
		Tiles:      [][]int{{1}},
	}

	useBricks(t, map[int]gamedata.BrickTypeConfig{
		1: {
			BrickTypeID:  1,
			Destructible: true,
			Border: gamedata.BrickBorder{
				Bottom: []gamedata.BorderPoint{{X: 0, Y: 0}, {X: 16, Y: 0}},
				Right:  []gamedata.BorderPoint{{X: 16, Y: 0}, {X: 16, Y: 16}},
				Top:    []gamedata.BorderPoint{{X: 16, Y: 16}, {X: 0, Y: 16}},
				Left:   []gamedata.BorderPoint{{X: 0, Y: 16}, {X: 0, Y: 0}},
			},
		},
	})

	terrain := NewTerrain(cfg)

	if !terrain.IsSolid(8, 8) {
		t.Error("expected solid at center (8,8)")
	}
	if !terrain.IsSolid(1, 1) {
		t.Error("expected solid at (1,1)")
	}
	if !terrain.IsSolid(14, 14) {
		t.Error("expected solid at (14,14)")
	}
}

func squareBorder() gamedata.BrickBorder {
	return gamedata.BrickBorder{
		Bottom: []gamedata.BorderPoint{{X: 0, Y: 0}, {X: 16, Y: 0}},
		Right:  []gamedata.BorderPoint{{X: 16, Y: 0}, {X: 16, Y: 16}},
		Top:    []gamedata.BorderPoint{{X: 16, Y: 16}, {X: 0, Y: 16}},
		Left:   []gamedata.BorderPoint{{X: 0, Y: 16}, {X: 0, Y: 0}},
	}
}

func TestPolygonBorderScalesWithCellSize(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true, Border: squareBorder()},
	})
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_scale", GridWidth: 1, GridHeight: 1, CellSize: 32,
		Tiles: [][]int{{1}},
	})

	for _, p := range [][2]float64{{1, 1}, {30, 1}, {1, 30}, {30, 30}} {
		if !terrain.IsSolid(p[0], p[1]) {
			t.Errorf("expected solid at (%v,%v) for 32px square brick", p[0], p[1])
		}
	}
}

func TestNewTerrainIgnoresTilesOutsideGrid(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true, Border: squareBorder()},
	})
	// GridHeight=1 but two rows of tiles: the second row must not be written past the mask.
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_oob", GridWidth: 2, GridHeight: 1, CellSize: 16,
		Tiles: [][]int{{0, 0}, {1, 1}, {1}},
	})
	if terrain.Height != 16 || terrain.Width != 32 {
		t.Fatalf("unexpected size %dx%d", terrain.Width, terrain.Height)
	}
	if getBit(terrain.Mask, 8, 8, terrain.Width) {
		t.Error("expected air in the only grid row")
	}
}

func TestSpawnYKeepsLowerFloor(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
	// Platform at row 1, floor at row 4.
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_spawn", GridWidth: 2, GridHeight: 5, CellSize: 16,
		Tiles: [][]int{{0, 0}, {1, 1}, {0, 0}, {0, 0}, {1, 1}},
	})

	if got := terrain.SpawnY(8, 0); got != 16 {
		t.Errorf("spawn above platform: got %v, want 16", got)
	}
	if got := terrain.SpawnY(8, 40); got != 64 {
		t.Errorf("spawn below platform: got %v, want 64", got)
	}
	// Spawn embedded in the floor is lifted to its surface.
	if got := terrain.SpawnY(8, 70); got != 64 {
		t.Errorf("spawn inside floor: got %v, want 64", got)
	}
}

func TestWalkToUnderRoof(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
	// Roof at row 1, floor at row 5: a 48px tall tunnel, enough for the player.
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_cave", GridWidth: 4, GridHeight: 6, CellSize: 16,
		Tiles: [][]int{{0, 0, 0, 0}, {1, 1, 1, 1}, {0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 0, 0}, {1, 1, 1, 1}},
	})

	x, y := terrain.WalkTo(8, 80, 56)
	if x != 56 || y != 80 {
		t.Errorf("walk under roof: got (%v,%v), want (56,80)", x, y)
	}
}

func TestWalkToBlockedByLowCeiling(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
	// Open area on the left; from column 2 a roof at row 2 leaves a 16px gap above the floor.
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_low", GridWidth: 4, GridHeight: 5, CellSize: 16,
		Tiles: [][]int{{0, 0, 0, 0}, {0, 0, 0, 0}, {0, 0, 1, 1}, {0, 0, 0, 0}, {1, 1, 1, 1}},
	})

	x, y := terrain.WalkTo(8, 64, 56)
	if x != 31 || y != 64 {
		t.Errorf("walk into low gap: got (%v,%v), want (31,64)", x, y)
	}
}

func TestCanStandAt(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
	// Column 0: floor only. Column 1: nothing. Column 2: floor under a low roof.
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_stand", GridWidth: 3, GridHeight: 5, CellSize: 16,
		Tiles: [][]int{{0, 0, 0}, {0, 0, 0}, {0, 0, 1}, {0, 0, 0}, {1, 0, 1}},
	})

	if !terrain.CanStandAt(8, 0) {
		t.Error("expected spawn above floor to be standable")
	}
	if terrain.CanStandAt(24, 0) {
		t.Error("expected spawn over a bottomless column not to be standable")
	}
	if terrain.CanStandAt(40, 50) {
		t.Error("expected spawn under a low roof not to be standable")
	}
}

func TestWalkToBlockedByWall(t *testing.T) {
	useBricks(t, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_wall", GridWidth: 3, GridHeight: 3, CellSize: 16,
		Tiles: [][]int{{0, 0, 0}, {0, 0, 1}, {1, 1, 1}},
	})

	x, y := terrain.WalkTo(8, 32, 40)
	if x != 31 || y != 32 {
		t.Errorf("walk into wall: got (%v,%v), want (31,32)", x, y)
	}
	// Walking off a ledge drops to the lower surface.
	x, y = terrain.WalkTo(40, 16, 8)
	if x != 8 || y != 32 {
		t.Errorf("walk off ledge: got (%v,%v), want (8,32)", x, y)
	}
}
