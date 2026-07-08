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
		Tiles: [][]string{
			{"", "", "", ""},
			{"", "dirt", "dirt", ""},
			{"rock", "rock", "rock", "rock"},
		},
	}

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_map": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[string]gamedata.BrickTypeConfig{
		"dirt": {BrickTypeID: "dirt", Destructible: true},
		"rock": {BrickTypeID: "rock", Destructible: false},
	}

	terrain := NewTerrain(cfg)

	if terrain.IsSolid(0, 0) {
		t.Error("expected air at (0,0)")
	}
	if terrain.IsSolid(16, 8) {
		t.Error("expected air at (16,8)")
	}

	if !terrain.IsSolid(20, 20) {
		t.Error("expected solid at (20,20) — dirt cell")
	}

	if terrain.IsSolid(8, 20) {
		t.Error("expected air at (8,20)")
	}

	if !terrain.IsSolid(0, 40) {
		t.Error("expected solid at (0,40) — rock cell")
	}
}

func TestDestroyCircleRespectsDestructible(t *testing.T) {
	cfg := gamedata.MapConfig{
		MapID:      "test_destroy",
		GridWidth:  4,
		GridHeight: 2,
		CellSize:   16,
		Tiles: [][]string{
			{"dirt", "dirt", "rock", "rock"},
			{"dirt", "dirt", "rock", "rock"},
		},
	}

	gamedata.Data = &gamedata.GameData{
		Maps:       map[string]gamedata.MapConfig{"test_destroy": cfg},
		Characters: map[string]gamedata.CharacterConfig{},
		Weapons:    map[string]gamedata.WeaponConfig{},
		Skills:     map[string]gamedata.SkillConfig{},
		Items:      map[string]gamedata.ItemConfig{},
	}
	gamedata.BrickTypes = map[string]gamedata.BrickTypeConfig{
		"dirt": {BrickTypeID: "dirt", Destructible: true},
		"rock": {BrickTypeID: "rock", Destructible: false},
	}

	terrain := NewTerrain(cfg)

	terrain.DestroyCircle(32, 16, 50)

	if terrain.IsSolid(8, 8) {
		t.Error("expected dirt at (8,8) to be destroyed")
	}

	if !terrain.IsSolid(56, 8) {
		t.Error("expected rock at (56,8) to remain solid")
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
