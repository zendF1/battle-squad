package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// Bloom (Flora, skill healing_bloom, effect bloom): no aim. A seed per living ally (Flora too)
// rises with the others out of the top of the map, splits up there and falls straight onto its
// ally, landing at bloomSeedTime: it heals a percent of the ally's max HP (Flora bloomSelfBase +
// bloomPerRank × rank, a teammate bloomAllyBase + bloomPerRank × rank; not scaled by skill
// power), the overflow becomes shield (addShield, capped at shieldCapPercent % of max HP) and
// its bad effects are cleansed.
const (
	bloomSelfBase = 40
	bloomAllyBase = 30
	bloomPerRank  = 5

	// The seeds' flight (visual only: what the bloom does does not depend on it). They rise
	// together above Flora to bloomSkyY (above the top of the map, y 0) for bloomRiseTime, cross
	// over to their ally up there, out of view, for bloomCrossTime, then fall straight down onto
	// the ally's body for bloomFallTime, already falling at up to bloomFallSpeed when they leave
	// the sky. The heal lands at bloomSeedTime; the replay lasts bloomLinger more for the bloom.
	bloomRiseTime  = 0.55
	bloomCrossTime = 0.2
	bloomFallTime  = 0.65
	bloomSeedTime  = bloomRiseTime + bloomCrossTime + bloomFallTime
	bloomSkyY      = -140.0
	bloomFallSpeed = 400.0
	bloomLinger    = 0.9
)

func init() {
	registerSkill(effectBloom, resolveBloom)
	registerBotSkill(effectBloom, botBloom)
}

// The bot blooms when it is below botBloomSelfHP of its max HP, a teammate below
// botBloomAllyHP, or an ally has a bad effect.
const (
	botBloomSelfHP = 0.6
	botBloomAllyHP = 0.5
)

func bloomSelfPercent(rank int) int { return bloomSelfBase + bloomPerRank*rank }
func bloomAllyPercent(rank int) int { return bloomAllyBase + bloomPerRank*rank }

// seedPath is a bloom seed's flight from origin (Flora's gun) to an ally's body at to: up to
// sky above origin (easing out), across to above the ally (out of view), then down onto it,
// landing at bloomSeedTime. Sampled every 0.05 s and at the phase ends, with exact velocities.
func seedPath(origin, sky, to Vector2) []ProjectileStep {
	const step = 0.05
	rise := origin.Y - sky.Y
	cross := to.X - origin.X
	drop := to.Y - sky.Y
	v0 := math.Max(0, math.Min(bloomFallSpeed, drop/bloomFallTime))
	g := 2 * (drop - v0*bloomFallTime) / (bloomFallTime * bloomFallTime)
	at := func(t float64) ProjectileStep {
		switch {
		case t <= bloomRiseTime:
			k := 1 - t/bloomRiseTime
			return ProjectileStep{Position: Vector2{X: origin.X, Y: origin.Y - rise*(1-k*k)},
				Velocity: Vector2{Y: -rise * 2 * k / bloomRiseTime}, Time: t}
		case t <= bloomRiseTime+bloomCrossTime:
			u := (t - bloomRiseTime) / bloomCrossTime
			return ProjectileStep{Position: Vector2{X: origin.X + cross*u*u*(3-2*u), Y: sky.Y},
				Velocity: Vector2{X: cross * 6 * u * (1 - u) / bloomCrossTime}, Time: t}
		}
		tau := t - bloomRiseTime - bloomCrossTime
		return ProjectileStep{Position: Vector2{X: to.X, Y: sky.Y + v0*tau + 0.5*g*tau*tau},
			Velocity: Vector2{Y: v0 + g*tau}, Time: t}
	}
	var path []ProjectileStep
	marks := []float64{bloomRiseTime, bloomRiseTime + bloomCrossTime}
	for i := 0; float64(i)*step < bloomSeedTime-1e-9; i++ {
		t := float64(i) * step
		for len(marks) > 0 && marks[0] <= t+1e-9 {
			if marks[0] < t-1e-9 {
				path = append(path, at(marks[0]))
			}
			marks = marks[1:]
		}
		path = append(path, at(t))
	}
	last := at(bloomSeedTime)
	last.Position, last.Time = to, bloomSeedTime // exactly on the ally
	return append(path, last)
}

// bloom heals every living ally (Flora too) by a percent of their max HP, turns the overflow
// into shield (capped at shieldCapPercent % of max HP) and cleanses their bad effects, when
// the seed lands on them. Not scaled by skill power.
func resolveBloom(m *Match, s *skillShot) {
	player, skillID, shot, tl := s.Player, s.Skill.SkillID, s.Shot, s.TL
	player.ShotsFired++
	origin := shotOrigin(m.Terrain, player)
	var allies []*BattlePlayerState
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID == player.TeamID {
			allies = append(allies, p)
		}
	}
	sky := Vector2{X: origin.X, Y: bloomSkyY}
	t := bloomSeedTime
	for _, p := range allies {
		to := Vector2{X: p.Position.X, Y: p.Position.Y - ShotOriginLift}
		shot.Projectiles = append(shot.Projectiles, &ProjectileResult{
			ProjectileID: generateProjectileID(), OwnerPlayerID: player.PlayerID, SkillID: skillID, Kind: "seed",
			Path: seedPath(origin, sky, to), HitPlayerID: p.PlayerID,
		})

		percent := bloomAllyPercent(player.SkillRank)
		if p.PlayerID == player.PlayerID {
			percent = bloomSelfPercent(player.SkillRank)
		}
		heal := int(math.Round(float64(p.MaxHP) * float64(percent) / 100))
		gained := healPlayer(p, heal)
		tl.add(ShotEvent{T: t, Type: "heal", PlayerID: p.PlayerID, Amount: gained, HP: intPtr(p.HP)})
		if shield := addShield(p, heal-gained); shield > 0 {
			tl.add(ShotEvent{T: t, Type: "shield", PlayerID: p.PlayerID, Amount: shield, Shield: intPtr(p.Shield)})
		}
		cleanseEvent(tl, t, p)
	}
	tl.reach(t + bloomLinger) // the blooms play before the turn moves on
}

// botBloom: bloom (no aim) when wantsBloom.
func botBloom(_ *BotBrain, m *Match, bot *BattlePlayerState, _ gamedata.WeaponConfig) (float64, float64, bool) {
	return NoAimAngle, NoAimPower, wantsBloom(m, bot)
}

// wantsBloom: the bot is below botBloomSelfHP of its max HP, a teammate below botBloomAllyHP,
// or an ally has an effect bloom cleanses.
func wantsBloom(m *Match, bot *BattlePlayerState) bool {
	for _, p := range m.State.Players {
		if !p.IsAlive || p.TeamID != bot.TeamID || p.MaxHP <= 0 {
			continue
		}
		limit := botBloomAllyHP
		if p.PlayerID == bot.PlayerID {
			limit = botBloomSelfHP
		}
		if float64(p.HP)/float64(p.MaxHP) < limit {
			return true
		}
		for _, e := range p.StatusEffects {
			if badEffects[e.EffectID] && e.DurationTurn > 0 {
				return true
			}
		}
	}
	return false
}
