package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

var benchTerrain *Terrain
var benchMapConfig = gamedata.MapConfig{
	MapID:  "grassland_valley",
	Width:  1600,
	Height: 900,
}

func setupBenchTerrain() *Terrain {
	if benchTerrain == nil {
		benchTerrain = NewTerrain(benchMapConfig)
	}
	return benchTerrain
}

func BenchmarkNewTerrainLegacy(b *testing.B) {
	cfg := gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTerrain(cfg)
	}
}

func BenchmarkNewTerrainGridV2(b *testing.B) {
	tiles := make([][]int, 56)
	for r := 0; r < 56; r++ {
		tiles[r] = make([]int, 100)
		if r > 30 {
			for c := 0; c < 100; c++ {
				tiles[r][c] = 1
			}
		}
	}
	cfg := gamedata.MapConfig{
		MapID:      "bench_grid",
		GridWidth:  100,
		GridHeight: 56,
		CellSize:   16,
		Tiles:      tiles,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTerrain(cfg)
	}
}

func BenchmarkIsSolid(b *testing.B) {
	t := setupBenchTerrain()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.IsSolid(float64(i%1600), float64(400+i%500))
	}
}

func BenchmarkDestroyCircle(b *testing.B) {
	cfg := gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := NewTerrain(cfg)
		t.DestroyCircle(800, 600, 30)
	}
}

func BenchmarkWalkTo(b *testing.B) {
	t := setupBenchTerrain()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.WalkTo(400, 550, 600)
	}
}

func BenchmarkSimulateProjectile(b *testing.B) {
	t := setupBenchTerrain()
	weapon := gamedata.WeaponConfig{
		WeaponID:         "basic_rocket",
		Damage:           60,
		ExplosionRadius:  40,
		TerrainDamage:    50,
		ProjectileWeight: 1.0,
		WindInfluence:    1.0,
		MultiHit:         1,
	}
	wind := WindState{Direction: 1, Power: 2.0}
	players := map[string]*BattlePlayerState{
		"p1": {PlayerID: "p1", TeamID: 1, Position: Vector2{X: 400, Y: 500}, IsAlive: true},
		"p2": {PlayerID: "p2", TeamID: 2, Position: Vector2{X: 1200, Y: 500}, IsAlive: true},
	}
	origin := Vector2{X: 400, Y: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SimulateProjectile("p1", 1, origin, 45.0, 80.0, weapon, wind, t, players, false)
	}
}
