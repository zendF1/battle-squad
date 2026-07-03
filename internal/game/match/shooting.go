package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

const (
	physicsTimeStep = 0.05 // 50ms fixed delta time
	gravityForce    = 200.0 // gravity pulling down (Y-down)
	windScaleFactor = 30.0  // scaling wind force to screen coords
	playerHitRadius = 24.0  // bounding circle of players
)

func SimulateProjectile(
	ownerID string,
	origin Vector2,
	angleDeg float64,
	power float64,
	weapon gamedata.WeaponConfig,
	wind WindState,
	terrain *Terrain,
	players map[string]*BattlePlayerState,
	drillMode bool,
) *ProjectileResult {
	// 1. Calculate initial velocity vector
	// Math angles in standard coordinates where X is right, Y is up, but here Y is down.
	// So we convert angle to radians and negative sin for Y if we want to fire upwards.
	// Firing upwards in Y-down coordinates means Y velocity is negative!
	angleRad := angleDeg * math.Pi / 180.0
	
	// Speed proportional to power
	initialSpeed := power * 6.0
	
	velocity := Vector2{
		X: initialSpeed * math.Cos(angleRad),
		Y: -initialSpeed * math.Sin(angleRad), // Negative because firing up decreases Y
	}

	position := origin
	mass := weapon.ProjectileWeight
	if mass <= 0 {
		mass = 1.0
	}

	// Wind force vector (horizontal wind)
	windForceX := float64(wind.Direction) * float64(wind.Power) * windScaleFactor * weapon.WindInfluence

	result := &ProjectileResult{
		OwnerPlayerID:    ownerID,
		ExplosionRadius:  float64(weapon.ExplosionRadius),
		TerrainDestroyed: false,
		Path:             []ProjectileStep{},
	}

	// 2. Main Simulation Loop
	t := 0.0
	maxDuration := 6.0 // max 6 seconds flight time
	hitPlayerID := ""
	var explosionPoint *Vector2
	terrainPassCount := 0 // tracks how many terrain collisions have been skipped (drill_bomb)

	for t < maxDuration {
		// Record current step
		result.Path = append(result.Path, ProjectileStep{
			Position: position,
			Velocity: velocity,
			Time:     t,
		})

		// Apply physics integration
		velocity.X += (windForceX / mass) * physicsTimeStep
		velocity.Y += gravityForce * physicsTimeStep

		position.X += velocity.X * physicsTimeStep
		position.Y += velocity.Y * physicsTimeStep
		t += physicsTimeStep

		// Check player collisions (excluding owner for first 0.2s of flight to prevent self-collision at origin)
		collisionDetected := false
		for _, p := range players {
			if !p.IsAlive {
				continue
			}
			if p.PlayerID == ownerID && t < 0.2 {
				continue
			}

			// 2D distance check
			dx := position.X - p.Position.X
			dy := position.Y - p.Position.Y
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist <= playerHitRadius {
				hitPlayerID = p.PlayerID
				collisionDetected = true
				expPt := position
				explosionPoint = &expPt
				break
			}
		}

		if collisionDetected {
			break
		}

		// Check terrain collision
		if terrain.IsSolid(position.X, position.Y) {
			if drillMode && terrainPassCount == 0 {
				// drill_bomb: pass through the first terrain hit
				terrainPassCount++
			} else {
				expPt := position
				explosionPoint = &expPt
				break
			}
		}

		// Check out of bounds (left, right, bottom)
		if position.X < -100 || position.X > float64(terrain.Width)+100 || position.Y > float64(terrain.Height)+50 {
			break
		}
	}

	result.HitPlayerID = hitPlayerID
	result.ExplosionPoint = explosionPoint

	return result
}
