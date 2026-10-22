package match

import (
	"math"
	"math/big"
	"strconv"

	"battle-squad/internal/game/gamedata"
)

// Bloom (Flora, skill healing_bloom, effect bloom; the numbers are the skill's
// gamedata.BloomTuning): no aim. A seed per living ally (Flora too) rises with the others out
// of the top of the map, splits up there and falls straight onto its ally, landing at the seed
// time: it heals a percent of the ally's max HP (Flora selfBase + perRank × rank, 40 + 5r, a
// teammate allyBase + perRank × rank, 30 + 5r; not scaled by skill power), the overflow becomes
// shield (addShield, capped at rules.combat shieldCapPercent % of max HP) and its bad effects
// are cleansed.
//
// The seeds' flight (visual only: what the bloom does does not depend on it). They rise
// together above Flora to skyY (-140, above the top of the map, y 0) for riseTime (0.55 s),
// cross over to their ally up there, out of view, for crossTime (0.2 s), then fall straight
// down onto the ally's body for fallTime (0.65 s), already falling at up to fallSpeed (400 px/s)
// when they leave the sky. The heal lands at the seed time (their sum); the replay lasts
// linger (0.9 s) more for the bloom.

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

func bloomSelfPercent(t gamedata.BloomTuning, rank int) int { return t.SelfBase + t.PerRank*rank }
func bloomAllyPercent(t gamedata.BloomTuning, rank int) int { return t.AllyBase + t.PerRank*rank }

// seedPath is a bloom seed's flight from origin (Flora's gun) to an ally's body at to: up to
// sky above origin (easing out), across to above the ally (out of view), then down onto it,
// landing at the seed time. Sampled every 0.05 s and at the phase ends, with exact velocities.
func seedPath(tu gamedata.BloomTuning, origin, sky, to Vector2) []ProjectileStep {
	const step = 0.05
	bloomRiseTime, bloomCrossTime, bloomFallTime, bloomFallSpeed := tu.RiseTime, tu.CrossTime, tu.FallTime, tu.FallSpeed
	bloomSeedTime := tu.SeedTime()
	rise := origin.Y - sky.Y
	cross := to.X - origin.X
	drop := to.Y - sky.Y
	v0 := math.Max(0, math.Min(bloomFallSpeed, drop/bloomFallTime))
	g := 2 * (drop - v0*bloomFallTime) / decimalSquare(bloomFallTime)
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

// decimalSquare is x² computed on x's shortest decimal form and rounded once, as Go folds a
// constant expression: the seeds' paths stay bit for bit what they were while the bloom's
// times were constants (0.65 × 0.65 rounded once differs from the float product in the last
// bit).
func decimalSquare(x float64) float64 {
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(x, 'g', -1, 64))
	if !ok {
		return x * x
	}
	sq, _ := r.Mul(r, r).Float64()
	return sq
}

// bloom heals every living ally (Flora too) by a percent of their max HP, turns the overflow
// into shield (capped at shieldCapPercent % of max HP) and cleanses their bad effects, when
// the seed lands on them. Not scaled by skill power.
func resolveBloom(m *Match, s *skillShot) {
	player, skillID, shot, tl := s.Player, s.Skill.SkillID, s.Shot, s.TL
	tu := gamedata.BloomTuningOf(s.Skill)
	lift, shieldCap := shotOriginLift(m.Terrain), m.gd.Rules.Combat.ShieldCapPercent
	player.ShotsFired++
	origin := shotOrigin(m.Terrain, player)
	var allies []*BattlePlayerState
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID == player.TeamID {
			allies = append(allies, p)
		}
	}
	sky := Vector2{X: origin.X, Y: tu.SkyY}
	t := tu.SeedTime()
	for _, p := range allies {
		to := Vector2{X: p.Position.X, Y: p.Position.Y - lift}
		shot.Projectiles = append(shot.Projectiles, &ProjectileResult{
			ProjectileID: generateProjectileID(), OwnerPlayerID: player.PlayerID, SkillID: skillID, Kind: "seed",
			Path: seedPath(tu, origin, sky, to), HitPlayerID: p.PlayerID,
		})

		percent := bloomAllyPercent(tu, player.SkillRank)
		if p.PlayerID == player.PlayerID {
			percent = bloomSelfPercent(tu, player.SkillRank)
		}
		heal := int(math.Round(float64(p.MaxHP) * float64(percent) / 100))
		gained := healPlayer(p, heal)
		tl.add(ShotEvent{T: t, Type: "heal", PlayerID: p.PlayerID, Amount: gained, HP: intPtr(p.HP)})
		if shield := addShield(p, heal-gained, shieldCap); shield > 0 {
			tl.add(ShotEvent{T: t, Type: "shield", PlayerID: p.PlayerID, Amount: shield, Shield: intPtr(p.Shield)})
		}
		cleanseEvent(m.gd, tl, t, p)
	}
	tl.reach(t + tu.Linger) // the blooms play before the turn moves on
}

// botBloom: bloom (no aim) when wantsBloom.
func botBloom(_ *BotBrain, m *Match, bot *BattlePlayerState, _ gamedata.WeaponConfig) (float64, float64, bool) {
	aim := m.gd.Rules.Aim
	return aim.NoAimAngle, aim.NoAimPower, wantsBloom(m, bot)
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
			if m.gd.StatusRule(e.EffectID).Bad && e.DurationTurn > 0 {
				return true
			}
		}
	}
	return false
}
