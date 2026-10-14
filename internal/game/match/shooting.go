package match

import (
	"crypto/rand"
	"encoding/hex"
	"math"

	"battle-squad/internal/game/gamedata"
)

func generateProjectileID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "proj-fallback"
	}
	return "proj-" + hex.EncodeToString(b)
}

// getPhysics returns physics constants from gamedata.Physics if loaded, otherwise defaults.
func getPhysics() (timeStep, gravity, windScale, hitRadius, recordStep, maxFlight float64) {
	if gamedata.Physics != nil {
		return gamedata.Physics.TimeStep, gamedata.Physics.Gravity, gamedata.Physics.WindScale,
			gamedata.Physics.PlayerHitRadius, gamedata.Physics.PathRecordStep, gamedata.Physics.MaxFlightSeconds
	}
	return 0.02, 200.0, 30.0, 24.0, 0.05, 6.0
}

func SimulateProjectile(
	ownerID string,
	ownerTeamID int,
	origin Vector2,
	angleDeg float64,
	power float64,
	weapon gamedata.WeaponConfig,
	wind WindState,
	terrain *Terrain,
	players map[string]*BattlePlayerState,
	pierceDistance float64, // terrain (px) the projectile may drill through; 0 = none
) *ProjectileResult {
	timeStep, gravity, windScale, hitRadius, recordStep, maxFlight := getPhysics()

	angleRad := angleDeg * math.Pi / 180.0
	initialSpeed := power * 6.0

	velocity := Vector2{
		X: initialSpeed * math.Cos(angleRad),
		Y: -initialSpeed * math.Sin(angleRad),
	}

	position := origin
	mass := weapon.ProjectileWeight
	if mass <= 0 {
		mass = 1.0
	}

	windForceX := float64(wind.Direction) * wind.Power * windScale * weapon.WindInfluence

	result := &ProjectileResult{
		ProjectileID:     generateProjectileID(),
		OwnerPlayerID:    ownerID,
		ExplosionRadius:  float64(weapon.ExplosionRadius),
		TerrainDestroyed: false,
		Path:             []ProjectileStep{},
	}

	t := 0.0
	hitPlayerID := ""
	var explosionPoint *Vector2
	drill := &drillState{remaining: pierceDistance}
	lastRecordedTime := -recordStep // force first record

	for t < maxFlight {
		// Record path at intervals for client animation
		if t-lastRecordedTime >= recordStep {
			result.Path = append(result.Path, ProjectileStep{
				Position: position,
				Velocity: velocity,
				Time:     t,
			})
			lastRecordedTime = t
		}

		prevPos := position

		// Apply physics
		velocity.X += (windForceX / mass) * timeStep
		velocity.Y += gravity * timeStep
		position.X += velocity.X * timeStep
		position.Y += velocity.Y * timeStep
		t += timeStep

		// --- Sub-step collision along movement segment ---
		hit, hitPos, hitPlayer := checkSegmentCollisions(
			prevPos, position, ownerID, ownerTeamID, t, terrain, players, drill, hitRadius,
		)

		if hit {
			if hitPlayer != "" {
				hitPlayerID = hitPlayer
			}
			ep := hitPos
			explosionPoint = &ep
			// Record final position
			result.Path = append(result.Path, ProjectileStep{
				Position: hitPos,
				Velocity: velocity,
				Time:     t,
			})
			break
		}

		// Check out of bounds
		if position.X < -100 || position.X > float64(terrain.Width)+100 || position.Y > float64(terrain.Height)+50 {
			break
		}
	}

	// Record final position if not already recorded
	if len(result.Path) == 0 || result.Path[len(result.Path)-1].Time < t-timeStep {
		result.Path = append(result.Path, ProjectileStep{
			Position: position,
			Velocity: velocity,
			Time:     t,
		})
	}

	result.HitPlayerID = hitPlayerID
	result.ExplosionPoint = explosionPoint

	return result
}

// drillState tracks a drilling projectile's pass through the first terrain block it meets.
type drillState struct {
	remaining float64 // terrain distance (px) still allowed
	inside    bool    // currently inside the block being drilled
}

// checkSegmentCollisions checks for terrain and player hits along the line from prev to curr.
// Uses sub-sampling with 4-pixel steps for precision.
func checkSegmentCollisions(
	prev, curr Vector2,
	ownerID string,
	ownerTeamID int,
	t float64,
	terrain *Terrain,
	players map[string]*BattlePlayerState,
	drill *drillState,
	hitRadius float64,
) (hit bool, hitPos Vector2, hitPlayerID string) {
	dx := curr.X - prev.X
	dy := curr.Y - prev.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	steps := int(math.Ceil(dist / 4.0)) // check every 4 pixels
	if steps < 1 {
		steps = 1
	}
	stepLen := dist / float64(steps)

	for i := 1; i <= steps; i++ {
		frac := float64(i) / float64(steps)
		px := prev.X + dx*frac
		py := prev.Y + dy*frac

		// Check player collisions (skip teammates — no friendly fire)
		for _, p := range players {
			if !p.IsAlive {
				continue
			}
			if p.TeamID == ownerTeamID {
				continue // skip all teammates (including self)
			}
			ddx := px - p.Position.X
			ddy := py - (p.Position.Y - 20) // center of player body (sprite is 48px tall, anchor bottom)
			d := math.Sqrt(ddx*ddx + ddy*ddy)
			if d <= hitRadius {
				return true, Vector2{X: px, Y: py}, p.PlayerID
			}
		}

		// Check terrain collision
		if terrain.IsSolid(px, py) {
			if drill.remaining > 0 {
				drill.inside = true
				drill.remaining -= stepLen
				continue
			}
			return true, Vector2{X: px, Y: py}, ""
		}
		if drill.inside {
			// Exited the drilled block: the next terrain hit explodes
			drill.inside = false
			drill.remaining = 0
		}
	}

	return false, Vector2{}, ""
}

const (
	maxShotPower    = 100.0
	drillPierceDist = 50.0 // default drill_bomb pierce distance when not configured
)

// validShotInput reports whether a client-supplied angle/power can be simulated.
// Power is bounded so a single shot cannot monopolise the match loop.
func validShotInput(angle, power float64) bool {
	return isFinite(angle) && isFinite(power) && power >= 0 && power <= maxShotPower
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
