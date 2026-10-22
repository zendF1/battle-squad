package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

// wallTerrain is 320px wide with a solid wall spanning x ∈ [128, 176) (48px)
// and a second wall at x ∈ [224, 256).
func wallTerrain() *Terrain {
	const cols, rows = 20, 10
	tiles := make([][]int, rows)
	for r := range tiles {
		tiles[r] = make([]int, cols)
		for _, c := range []int{8, 9, 10, 14, 15} {
			tiles[r][c] = 1
		}
	}
	cfg := gamedata.MapConfig{MapID: "walls", GridWidth: cols, GridHeight: rows, CellSize: 16, Tiles: tiles}
	return NewTerrainWithBricks(cfg, map[int]gamedata.BrickTypeConfig{1: {BrickTypeID: 1, Destructible: true}})
}

func fireAtWall(pierce float64) *ProjectileResult {
	weapon := gamedata.WeaponConfig{ExplosionRadius: 20, ProjectileWeight: 1}
	return SimulateProjectile("p1", 1, Vector2{X: 40, Y: 60}, 0, 100, weapon, WindState{}, wallTerrain(),
		map[string]*BattlePlayerState{}, pierce)
}

func TestDrillPiercesTerrainBlock(t *testing.T) {
	plain := fireAtWall(0)
	if plain.ExplosionPoint == nil || plain.ExplosionPoint.X > 132 {
		t.Fatalf("plain shot should explode at the first wall, got %+v", plain.ExplosionPoint)
	}

	drilled := fireAtWall(50)
	if drilled.ExplosionPoint == nil || drilled.ExplosionPoint.X < 224 || drilled.ExplosionPoint.X > 228 {
		t.Fatalf("drill shot should cross the 48px wall and explode at the second one, got %+v", drilled.ExplosionPoint)
	}

	short := fireAtWall(20)
	if short.ExplosionPoint == nil || short.ExplosionPoint.X < 144 || short.ExplosionPoint.X > 156 {
		t.Fatalf("drill with 20px budget should explode ~20px into the wall, got %+v", short.ExplosionPoint)
	}
}

func TestValidShotInput(t *testing.T) {
	cases := []struct {
		angle, power float64
		want         bool
	}{
		{45, 50, true},
		{45, maxShotPower, true},
		{45, maxShotPower + 1, false},
		{45, -1, false},
	}
	for _, c := range cases {
		if got := validShotInput(c.angle, c.power, maxShotPower); got != c.want {
			t.Errorf("validShotInput(%v, %v, maxShotPower) = %v, want %v", c.angle, c.power, got, c.want)
		}
	}
}
