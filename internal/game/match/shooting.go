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

// The shot rules (rules.physics of the terrain's game data): a shot starts shotOriginLift px
// above the feet (20: the body centre, PlayerHeight / 2, the same point projectile hits are
// measured against); each flight step is checked for hits every collisionStepPx (4); a flight
// ends outOfBoundsLeft / outOfBoundsRight px beside the map (100) or outOfBoundsBelow px under
// it (50).

// shotOriginLift is the terrain's game data's shotOriginLift (px).
func shotOriginLift(t *Terrain) float64 { return float64(t.data().Rules.Physics.ShotOriginLift) }

// shotOriginAt returns where a shot fired by a player standing at feet (the solid pixel under
// the feet) starts: the body centre feet − (0, shotOriginLift). When terrain sits in that
// column below the body centre (a low ceiling), it falls back to the highest free point above
// the feet: pixels feet.Y−1, feet.Y−2, … are scanned upwards and the scan stops before the
// first solid one. If feet.Y−1 is already solid the origin is feet itself.
func shotOriginAt(t *Terrain, feet Vector2) Vector2 {
	lift := 0
	for k := 1; k <= t.data().Rules.Physics.ShotOriginLift; k++ {
		if t != nil && t.IsSolid(feet.X, feet.Y-float64(k)) {
			break
		}
		lift = k
	}
	return Vector2{X: feet.X, Y: feet.Y - float64(lift)}
}

// shotOrigin is shotOriginAt for the player's current position.
func shotOrigin(t *Terrain, p *BattlePlayerState) Vector2 {
	return shotOriginAt(t, p.Position)
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
	gd := terrain.data()
	ph, sr := gd.Physics, gd.Rules.Physics
	timeStep, gravity, windScale, hitRadius, recordStep, maxFlight :=
		ph.TimeStep, ph.Gravity, ph.WindScale, ph.PlayerHitRadius, ph.PathRecordStep, ph.MaxFlightSeconds
	lift := float64(sr.ShotOriginLift)

	angleRad := angleDeg * math.Pi / 180.0
	initialSpeed := power * ph.ProjectileSpeedMultiplier

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
		ProjectileID:    generateProjectileID(),
		OwnerPlayerID:   ownerID,
		ExplosionRadius: float64(weapon.ExplosionRadius),
		CraterRadius:    weaponCraterRadius(gd, weapon),
		Path:            []ProjectileStep{},
	}

	t := 0.0
	hitPlayerID := ""
	var explosionPoint *Vector2
	drill := &drillState{remaining: pierceDistance}
	bouncesLeft := weapon.Bounces
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
			prevPos, position, ownerID, ownerTeamID, terrain, players, drill, hitRadius, lift, sr.CollisionStepPx,
		)

		// A bouncing shell's terrain contact (not the floor under the map): it leaves the
		// surface (bounce) and flies on; a unit hit always explodes
		if hit && hitPlayer == "" && bouncesLeft > 0 && hitPos.Y < float64(terrain.Height)-0.5 {
			bouncesLeft--
			tb := bounceTime(prevPos, position, hitPos, t, timeStep)
			at := bounceContact(terrain, prevPos, hitPos)
			out := bounceVelocity(velocity, surfaceNormal(terrain, hitPos, velocity), sr.BounceRestitution, sr.BounceFriction)
			result.Path = append(result.Path,
				ProjectileStep{Position: at, Velocity: velocity, Time: tb},
				ProjectileStep{Position: at, Velocity: out, Time: tb})
			result.Bounces = append(result.Bounces, ProjectileBounce{T: tb, Position: at})
			position, velocity, t, lastRecordedTime = at, out, tb, tb
			continue
		}

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
		if position.X < -sr.OutOfBoundsLeft || position.X > float64(terrain.Width)+sr.OutOfBoundsRight || position.Y > float64(terrain.Height)+sr.OutOfBoundsBelow {
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

// Bounces (054, weapon bounces > 0: Tide's water orb). A shell's first terrain contacts (up to
// its weapon's bounces, never the floor under the map) bounce it instead of exploding it: its
// velocity splits along the surface normal (surfaceNormal) and the surface, leaving with
// −bounceRestitution × the normal part and bounceFriction × the surface part (rules.physics). The
// path records the bounce twice at its time and point (the contact point, the last free pixel
// before the terrain): with the velocity it arrived with, then the one it leaves with, so a client
// replaying the samples (a constant-acceleration arc between two) flies both legs exactly. The
// next contact (terrain or a unit), or any unit hit before it, explodes as usual.

// bounceTime is when the step from prev (time t − step) to curr (time t) met the terrain at hit.
func bounceTime(prev, curr, hit Vector2, t, step float64) float64 {
	seg := math.Hypot(curr.X-prev.X, curr.Y-prev.Y)
	if seg <= 0 {
		return t
	}
	f := math.Min(1, math.Hypot(hit.X-prev.X, hit.Y-prev.Y)/seg)
	return t - step + f*step
}

// bounceContact is the last free point before hit on the way from prev: hit backed off toward
// prev 1 px at a time until it is out of the terrain (prev itself when the whole way is solid).
func bounceContact(t *Terrain, prev, hit Vector2) Vector2 {
	dx, dy := prev.X-hit.X, prev.Y-hit.Y
	d := math.Hypot(dx, dy)
	if d <= 0 {
		return prev
	}
	ux, uy := dx/d, dy/d
	for s := 1.0; s < d; s++ {
		p := Vector2{X: hit.X + ux*s, Y: hit.Y + uy*s}
		if !t.IsSolid(p.X, p.Y) {
			return p
		}
	}
	return prev
}

// surfaceNormal is the unit normal of the terrain at p, pointing out of it: away from the solid
// pixels within bounceNormalRadius of p. Where that cannot tell (none or solid all around, or
// the estimate faces along v), it is −v.
func surfaceNormal(t *Terrain, p, v Vector2) Vector2 {
	const bounceNormalRadius = 6
	sx, sy := 0.0, 0.0
	for dy := -bounceNormalRadius; dy <= bounceNormalRadius; dy++ {
		for dx := -bounceNormalRadius; dx <= bounceNormalRadius; dx++ {
			if dx*dx+dy*dy > bounceNormalRadius*bounceNormalRadius || (dx == 0 && dy == 0) {
				continue
			}
			if t.IsSolid(p.X+float64(dx), p.Y+float64(dy)) {
				sx += float64(dx)
				sy += float64(dy)
			}
		}
	}
	n := math.Hypot(sx, sy)
	speed := math.Hypot(v.X, v.Y)
	if n < 1e-9 || (speed > 0 && (-sx*v.X-sy*v.Y)/n > 0) {
		if speed <= 0 {
			return Vector2{X: 0, Y: -1}
		}
		return Vector2{X: -v.X / speed, Y: -v.Y / speed}
	}
	return Vector2{X: -sx / n, Y: -sy / n}
}

// bounceVelocity is v after a bounce off a surface of normal n: the part along n reversed ×
// restitution, the part along the surface × friction.
func bounceVelocity(v, n Vector2, restitution, friction float64) Vector2 {
	vn := v.X*n.X + v.Y*n.Y
	nx, ny := vn*n.X, vn*n.Y
	tx, ty := v.X-nx, v.Y-ny
	return Vector2{X: tx*friction - nx*restitution, Y: ty*friction - ny*restitution}
}

// drillState tracks a drilling projectile's pass through the first terrain block it meets.
type drillState struct {
	remaining float64 // terrain distance (px) still allowed
	inside    bool    // currently inside the block being drilled
}

// checkSegmentCollisions checks for terrain and player hits along the line from prev to curr,
// sampled every stepPx px; a player is hit within hitRadius of its body centre (lift px above
// its feet), both × its body size (a boss's body is bigger, see body.go).
func checkSegmentCollisions(
	prev, curr Vector2,
	ownerID string,
	ownerTeamID int,
	terrain *Terrain,
	players map[string]*BattlePlayerState,
	drill *drillState,
	hitRadius, lift, stepPx float64,
) (hit bool, hitPos Vector2, hitPlayerID string) {
	dx := curr.X - prev.X
	dy := curr.Y - prev.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	steps := int(math.Ceil(dist / stepPx))
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
			scale := bodyScale(p)
			ddx := px - p.Position.X
			ddy := py - (p.Position.Y - lift*scale) // centre of the player body
			d := math.Sqrt(ddx*ddx + ddy*ddy)
			if d <= hitRadius*scale {
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

// validShotInput reports whether a client-supplied angle/power can be simulated.
// Power is bounded (rules.aim.powerMax) so a single shot cannot monopolise the match loop.
func validShotInput(angle, power, maxPower float64) bool {
	return isFinite(angle) && isFinite(power) && power >= 0 && power <= maxPower
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
