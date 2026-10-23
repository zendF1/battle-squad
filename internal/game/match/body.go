package match

import "math"

// Bodies. A player's body is a circle of physics.player_hit_radius (24 px) around its centre,
// rules.physics shotOriginLift (20 px) above its feet. A boss or minion has BodySize × that:
// radius hitRadius × BodySize around a centre lift × BodySize above the feet (the boss of size 3:
// 72 px around a point 60 px up). Shots hit the body (checkSegmentCollisions), the laser and the
// boss beam cross it (laserHits) and blasts measure to it (blastDistance).

// bodyScale is p's body size, 1 for a player (BodySize 0).
func bodyScale(p *BattlePlayerState) float64 {
	if p.BodySize > 0 {
		return p.BodySize
	}
	return 1
}

// bodyCentre is the centre of p's body: lift × its size above its feet.
func bodyCentre(p *BattlePlayerState, lift float64) Vector2 {
	return Vector2{X: p.Position.X, Y: p.Position.Y - lift*bodyScale(p)}
}

// blastDistance is how far a blast at point is from p's body: from the edge of the circle
// the shells hit (hitRadius × its size around its centre, see checkSegmentCollisions), 0
// inside it. Every unit is measured the same way, whatever its size: two bodies touching the
// blast take the same damage, and a blast on one body reaches the body touching it.
func blastDistance(p *BattlePlayerState, point Vector2, hitRadius, lift float64) float64 {
	c := bodyCentre(p, lift)
	return math.Max(0, math.Hypot(c.X-point.X, c.Y-point.Y)-hitRadius*bodyScale(p))
}

// explosionDamageAt is the damage a target distance px from an explosion takes: baseDamage
// falling off linearly to 0 at the blast radius, reduced by defense with K.
func explosionDamageAt(distance, baseDamage, explosionRadius float64, defense int, k float64) int {
	if distance >= explosionRadius {
		return 0
	}
	mult := math.Max(0, math.Min(1, 1-distance/explosionRadius))
	return int(math.Round(reduceByDefense(baseDamage*mult, defense, k)))
}
