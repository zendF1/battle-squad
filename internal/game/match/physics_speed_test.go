package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// SimulateProjectile launches at power × physics.projectile_speed_multiplier (it was a fixed
// power × 6 whatever the setting) and flies at most physics.max_flight_seconds.
func TestSimulateProjectileUsesThePhysicsSettings(t *testing.T) {
	prev := gamedata.Physics
	t.Cleanup(func() { gamedata.Physics = prev })
	terrain := emptyTerrain(400, 300)
	weapon := gamedata.WeaponConfig{ExplosionRadius: 20, ProjectileWeight: 1, WindInfluence: 1}
	for _, mult := range []float64{6, 9.6, 12} {
		p := *gamedata.DefaultPhysics()
		p.ProjectileSpeedMultiplier = mult
		gamedata.Physics = &p
		r := SimulateProjectile("p", 1, Vector2{X: 200, Y: 100}, 0, 50, weapon, WindState{}, terrain, nil, 0)
		if v := r.Path[0].Velocity.X; math.Abs(v-50*mult) > 1e-9 {
			t.Fatalf("multiplier %v: launch speed %v, want %v", mult, v, 50*mult)
		}
	}
	gamedata.Physics = nil
	d := gamedata.DefaultPhysics()
	if d.ProjectileSpeedMultiplier != 9.6 || d.Gravity != 445 || d.WindScale != 67 || d.MaxFlightSeconds != 4.5 {
		t.Fatalf("defaults %+v", d)
	}
	// Straight up at power 100 into an empty sky: the flight times out at MaxFlightSeconds
	r := SimulateProjectile("p", 1, Vector2{X: 200, Y: 100}, 90, 100, weapon, WindState{}, emptyTerrain(400, 20000), nil, 0)
	if r.ExplosionPoint != nil || math.Abs(flightEnd(r)-d.MaxFlightSeconds) > d.TimeStep+1e-9 {
		t.Fatalf("flight %.2f s, want %v", flightEnd(r), d.MaxFlightSeconds)
	}
}

func emptyTerrain(w, h int) *Terrain {
	return &Terrain{Width: w, Height: h, Mask: make([]uint64, maskSize(w, h)), DestructibleMask: make([]uint64, maskSize(w, h))}
}
