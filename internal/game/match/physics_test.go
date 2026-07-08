package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

func TestSimulateProjectile(t *testing.T) {
	ownerID := "p1"
	ownerTeamID := 1
	origin := Vector2{X: 100, Y: 500}
	angleDeg := 45.0
	power := 50.0

	weapon := gamedata.WeaponConfig{
		WeaponID:        "basic_rocket",
		Name:            "Basic Rocket",
		Damage:          60,
		ExplosionRadius: 40,
		TerrainDamage:   50,
		ProjectileWeight: 1.0,
		WindInfluence:   1.0,
		MultiHit:        1,
	}

	wind := WindState{
		Direction: 1, // wind blows right
		Power:     2,
	}

	terrain := NewTerrain(gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	})

	players := map[string]*BattlePlayerState{
		"p1": {
			PlayerID:   "p1",
			TeamID:     1,
			Position:   origin,
			IsAlive:    true,
			MoveEnergy: 100,
		},
		"p2": {
			PlayerID:   "p2",
			TeamID:     2,
			Position:   Vector2{X: 400, Y: 500},
			IsAlive:    true,
			MoveEnergy: 100,
		},
	}

	// Run simulation
	result := SimulateProjectile(ownerID, ownerTeamID, origin, angleDeg, power, weapon, wind, terrain, players, false)

	if len(result.Path) == 0 {
		t.Fatal("Expected projectile path to contain simulated steps, got 0")
	}

	// Verify that the flight steps are simulated correctly
	firstStep := result.Path[0]
	if firstStep.Position.X != origin.X || firstStep.Position.Y != origin.Y {
		t.Errorf("Expected path to start at origin, got %+v", firstStep.Position)
	}

	// The projectile should move rightwards (positive X)
	lastStep := result.Path[len(result.Path)-1]
	if lastStep.Position.X <= origin.X {
		t.Errorf("Expected projectile to move right, got final X: %f", lastStep.Position.X)
	}
}
