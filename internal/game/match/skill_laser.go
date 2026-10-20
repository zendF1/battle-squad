package match

import (
	"math"
	"sort"

	"battle-squad/internal/game/gamedata"
)

// Laser (Spark, skill shock_field, effect laser): a straight beam from the shot origin along
// the aim to the map edge, through terrain (no gravity, wind, crater, knockback or falls;
// power ignored); it hits every enemy whose body is within laserHalfWidth + the player hit
// radius of it, nearest first: (laserFirstBase + laserFirstPerRank × rank) × base, each next
// one × laserDecay of the previous. Every enemy hit is stunned.
const (
	laserHalfWidth    = 10.0
	laserFirstBase    = 1.5
	laserFirstPerRank = 0.1
	laserDecay        = 0.8
	laserBeamTime     = 0.6  // s the beam shows (the beam event's duration)
	laserHitDelay     = 0.12 // s after the shot of the first hit
	laserHitGap       = 0.05 // s between hits, nearest first
	stunTurns         = 1
)

func init() {
	registerSkill(effectLaser, resolveLaser)
	registerBotSkill(effectLaser, botLaser)
}

// botLaserMiss is how far (degrees) the bot turns the beam off when its hit roll misses.
const botLaserMiss = 10.0

func laserFirst(rank int) float64 { return laserFirstBase + laserFirstPerRank*float64(rank) }

// beamEnd is where a beam from origin along dir leaves the map rectangle [0, width] × [0, height].
func beamEnd(origin, dir Vector2, width, height int) Vector2 {
	s := math.MaxFloat64
	if dir.X > 1e-9 {
		s = math.Min(s, (float64(width)-origin.X)/dir.X)
	} else if dir.X < -1e-9 {
		s = math.Min(s, -origin.X/dir.X)
	}
	if dir.Y > 1e-9 {
		s = math.Min(s, (float64(height)-origin.Y)/dir.Y)
	} else if dir.Y < -1e-9 {
		s = math.Min(s, -origin.Y/dir.Y)
	}
	s = math.Max(s, 0)
	return Vector2{X: origin.X + dir.X*s, Y: origin.Y + dir.Y*s}
}

// laserHits returns the living enemies of shooter whose body the beam from origin to end
// crosses (within laserHalfWidth + the player hit radius of it), nearest to origin first.
// Terrain does not stop the beam.
func laserHits(players map[string]*BattlePlayerState, shooter *BattlePlayerState, origin, end Vector2) []*BattlePlayerState {
	_, _, _, hitRadius, _, _ := getPhysics()
	dx, dy := end.X-origin.X, end.Y-origin.Y
	length := math.Hypot(dx, dy)
	if length <= 0 {
		return nil
	}
	ux, uy := dx/length, dy/length
	reach := laserHalfWidth + hitRadius
	type hit struct {
		p *BattlePlayerState
		s float64
	}
	var hits []hit
	for _, p := range players {
		if !p.IsAlive || p.TeamID == shooter.TeamID {
			continue
		}
		cx, cy := p.Position.X-origin.X, p.Position.Y-ShotOriginLift-origin.Y
		s := cx*ux + cy*uy // along the beam
		if s < -reach || s > length+reach {
			continue
		}
		if math.Abs(cx*uy-cy*ux) > reach { // off the beam
			continue
		}
		hits = append(hits, hit{p, s})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s < hits[j].s
		}
		return hits[i].p.PlayerID < hits[j].p.PlayerID
	})
	out := make([]*BattlePlayerState, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}

// laser: the beam from the shot origin along the aim to the map edge. No gravity, wind,
// terrain, crater, knockback or falls; power is ignored. The nearest enemy takes laserFirst ×
// base, each next one laserDecay × the previous; every enemy hit is stunned.
func resolveLaser(m *Match, s *skillShot) {
	player, action, base, tl := s.Player, s.Action, s.Base, s.TL
	player.ShotsFired++
	origin := shotOrigin(m.Terrain, player)
	end := beamEnd(origin, angleDir(action.Angle), m.Terrain.Width, m.Terrain.Height)
	tl.add(ShotEvent{T: 0, Type: "beam", PlayerID: player.PlayerID, From: vecPtr(origin), To: vecPtr(end), Duration: laserBeamTime})

	raw := laserFirst(player.SkillRank) * base
	for k, p := range laserHits(m.State.Players, player, origin, end) {
		t := laserHitDelay + laserHitGap*float64(k)
		damage := int(math.Round(ReduceByDefense(raw, p.Defense)))
		damage, critical := ApplyCritical(damage, player.CritChance)
		raw *= laserDecay
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		player.DamageDealt += damage
		player.ShotsHit++
		e := damageEvent(t, p, damage, shieldBefore, "laser")
		e.Critical = critical
		tl.add(e)
		if killed {
			player.KillCount++
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "laser"})
			continue
		}
		applyShotStatus(tl, t, player, p, "stun", stunTurns)
	}
}

// botLaser aims the beam through as many enemies as it can line up (bestLaser); a missed hit
// roll turns it off them. False when no line hits an enemy.
func botLaser(b *BotBrain, m *Match, bot *BattlePlayerState, _ gamedata.WeaponConfig) (float64, float64, bool) {
	angle, hits := bestLaser(m, bot)
	if hits == 0 {
		return 0, 0, false
	}
	if !b.rollHit() {
		for _, off := range []float64{botLaserMiss, -botLaserMiss, 2 * botLaserMiss, -2 * botLaserMiss} {
			if laserHitCount(m, bot, angle+off) == 0 {
				angle += off
				break
			}
		}
	}
	return angle, NoAimPower, true
}

// laserHitCount is how many enemies the bot's laser fired at angle would hit.
func laserHitCount(m *Match, bot *BattlePlayerState, angle float64) int {
	origin := shotOrigin(m.Terrain, bot)
	end := beamEnd(origin, angleDir(angle), m.Terrain.Width, m.Terrain.Height)
	return len(laserHits(m.State.Players, bot, origin, end))
}

// bestLaser returns the laser angle hitting the most enemies (lines through two preferred)
// and that count: every whole degree, ties broken by the angle closest to straight at an
// enemy's body (the beam's centre on it).
func bestLaser(m *Match, bot *BattlePlayerState) (float64, int) {
	origin := shotOrigin(m.Terrain, bot)
	var aims []float64 // straight at each enemy's body
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID != bot.TeamID {
			dx, dy := p.Position.X-origin.X, p.Position.Y-ShotOriginLift-origin.Y
			aims = append(aims, math.Atan2(-dy, dx)*180/math.Pi)
		}
	}
	offAim := func(angle float64) float64 {
		d := math.MaxFloat64
		for _, a := range aims {
			d = math.Min(d, math.Abs(math.Remainder(angle-a, 360)))
		}
		return d
	}
	candidates := append([]float64(nil), aims...)
	for a := 0.0; a < 360; a++ {
		candidates = append(candidates, a)
	}
	bestAngle, bestHits := 0.0, 0
	for _, a := range candidates {
		h := laserHitCount(m, bot, a)
		if h > bestHits || (h == bestHits && h > 0 && offAim(a) < offAim(bestAngle)) {
			bestAngle, bestHits = a, h
		}
	}
	return bestAngle, bestHits
}
