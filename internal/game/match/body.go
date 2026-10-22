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

// blastDistance is how far a blast at point is from p: from its feet, as always, for a body
// of size 1 or less; for a bigger body (a boss) the nearer of its feet and the edge of its
// body beyond a normal one (the centre distance minus (size − 1) × hitRadius), so a blast on
// the top of a boss is as close as a blast at a player's body.
func blastDistance(p *BattlePlayerState, point Vector2, hitRadius, lift float64) float64 {
	feet := math.Hypot(p.Position.X-point.X, p.Position.Y-point.Y)
	scale := bodyScale(p)
	if scale <= 1 {
		return feet
	}
	c := bodyCentre(p, lift)
	edge := math.Max(0, math.Hypot(c.X-point.X, c.Y-point.Y)-(scale-1)*hitRadius)
	return math.Min(feet, edge)
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
