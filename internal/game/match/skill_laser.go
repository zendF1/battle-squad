package match

import (
	"math"
	"sort"

	"battle-squad/internal/game/gamedata"
)

// Laser (Spark, skill shock_field, effect laser; the numbers are the skill's
// gamedata.LaserTuning): a straight beam from the shot origin along the aim to the map edge,
// through terrain (no gravity, wind, crater, knockback or falls; power ignored); it hits every
// enemy whose body is within halfWidth (10) + the player hit radius of it, nearest first:
// (firstBase + firstPerRank × rank) × base (1.5 + 0.1 × rank), each next one × decay (0.8) of
// the previous. Every enemy hit is stunned.
// Timeline: the gun charges for chargeTime (0.3 s), then the beam (event at chargeTime, shown
// beamTime, 0.6 s) sweeps out from the muzzle at sweepSpeed (12000 px/s); each enemy is hit
// when its front reaches them (the client draws the same sweep).

func init() {
	registerSkill(effectLaser, resolveLaser)
	registerBotSkill(effectLaser, botLaser)
}

// botLaserMiss is how far (degrees) the bot turns the beam off when its hit roll misses.
const botLaserMiss = 10.0

func laserFirst(t gamedata.LaserTuning, rank int) float64 {
	return t.FirstBase + t.FirstPerRank*float64(rank)
}

// laserTuningOf is the tuning of p's character skill when it is a laser (the defaults
// otherwise).
func (m *Match) laserTuningOf(p *BattlePlayerState) gamedata.LaserTuning {
	if c, ok := m.gd.Data.Characters[p.CharacterID]; ok {
		return gamedata.LaserTuningOf(m.gd.Data.Skills[c.SkillID])
	}
	return gamedata.DefaultLaserTuning()
}

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

// laserHit is an enemy the beam crosses and how far along the beam (px from the origin).
type laserHit struct {
	p *BattlePlayerState
	s float64
}

// laserHits returns the living enemies of shooter whose body (lift px above the feet) the beam
// from origin to end crosses (within halfWidth + the player hit radius of it), nearest to origin
// first. Terrain does not stop the beam.
func (m *Match) laserHits(shooter *BattlePlayerState, halfWidth float64, origin, end Vector2) []laserHit {
	players, lift := m.State.Players, shotOriginLift(m.Terrain)
	dx, dy := end.X-origin.X, end.Y-origin.Y
	length := math.Hypot(dx, dy)
	if length <= 0 {
		return nil
	}
	ux, uy := dx/length, dy/length
	var hits []laserHit
	for _, p := range players {
		if !p.IsAlive || p.TeamID == shooter.TeamID {
			continue
		}
		reach := halfWidth + m.gd.Physics.PlayerHitRadius*bodyScale(p)
		centre := bodyCentre(p, lift)
		cx, cy := centre.X-origin.X, centre.Y-origin.Y
		s := cx*ux + cy*uy // along the beam
		if s < -reach || s > length+reach {
			continue
		}
		if math.Abs(cx*uy-cy*ux) > reach { // off the beam
			continue
		}
		hits = append(hits, laserHit{p, s})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s < hits[j].s
		}
		return hits[i].p.PlayerID < hits[j].p.PlayerID
	})
	return hits
}

// laserHitTime is when the beam's front reaches a body s px along it (s since the shot).
func laserHitTime(t gamedata.LaserTuning, s float64) float64 {
	return t.ChargeTime + math.Max(s, 0)/t.SweepSpeed
}

// laser: the beam from the shot origin along the aim to the map edge. No gravity, wind,
// terrain, crater, knockback or falls; power is ignored. The nearest enemy takes laserFirst ×
// base, each next one decay × the previous (also past a kill); every enemy hit is stunned.
func resolveLaser(m *Match, s *skillShot) {
	player, action, base, tl := s.Player, s.Action, s.Base, s.TL
	tu := gamedata.LaserTuningOf(s.Skill)
	k, crit := m.gd.Physics.DefenseKOrDefault(), m.gd.Rules.Combat.CritMultiplier
	player.ShotsFired++
	origin := shotOrigin(m.Terrain, player)
	end := beamEnd(origin, angleDir(action.Angle), m.Terrain.Width, m.Terrain.Height)
	tl.add(ShotEvent{T: tu.ChargeTime, Type: "beam", PlayerID: player.PlayerID, From: vecPtr(origin), To: vecPtr(end), Duration: tu.BeamTime})

	raw := laserFirst(tu, player.SkillRank) * base
	for _, h := range m.laserHits(player, tu.HalfWidth, origin, end) {
		p, t := h.p, laserHitTime(tu, h.s)
		damage := int(math.Round(reduceByDefense(raw, p.Defense, k)))
		damage, critical := applyCritical(damage, player.CritChance, crit)
		raw *= tu.Decay
		damage = m.shieldedDamage(p, damage)
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		player.DamageDealt += damage
		m.noteBossSideDamage(player, p, damage)
		player.ShotsHit++
		e := damageEvent(t, p, damage, shieldBefore, "laser")
		e.Critical = critical
		tl.add(e)
		if killed {
			player.KillCount++
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "laser"})
			continue
		}
		m.applyShotStatus(tl, t, player, p, "stun", tu.StunTurns)
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
	return angle, m.gd.Rules.Aim.NoAimPower, true
}

// laserHitCount is how many enemies the bot's laser fired at angle would hit.
func laserHitCount(m *Match, bot *BattlePlayerState, angle float64) int {
	origin := shotOrigin(m.Terrain, bot)
	end := beamEnd(origin, angleDir(angle), m.Terrain.Width, m.Terrain.Height)
	return len(m.laserHits(bot, m.laserTuningOf(bot).HalfWidth, origin, end))
}

// bestLaser returns the laser angle hitting the most enemies (lines through two preferred)
// and that count: every whole degree, ties broken by the angle closest to straight at an
// enemy's body (the beam's centre on it).
func bestLaser(m *Match, bot *BattlePlayerState) (float64, int) {
	origin, lift := shotOrigin(m.Terrain, bot), shotOriginLift(m.Terrain)
	var aims []float64 // straight at each enemy's body
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID != bot.TeamID {
			dx, dy := p.Position.X-origin.X, p.Position.Y-lift-origin.Y
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
