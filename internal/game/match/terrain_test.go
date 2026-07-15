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

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_map": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	}

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

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_destroy": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	}

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

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"grassland_valley": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = nil

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

	gamedata.BrickTypes = map[int]gamedata.BrickTypeConfig{
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
	}

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
