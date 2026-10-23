package match

import (
	"math"
	"math/rand"

	"battle-squad/internal/game/gamedata"
)

// Missile rain (Rookie, skill triple_shot, effect missile_rain; the numbers are the skill's
// gamedata.RainTuning): a flare fired like a normal shot; delay s after it lands, baseCount +
// countPerRank × rank missiles fall straight down interval s apart, over the flare point ±
// (spreadBase + spreadPerRank × rank) px, denser near it (focus), at columns from the match +
// turn seed (skillRNG). Each missile: damage × base, blast × the weapon's, crater ×; the whole
// rain deals one target at most cap × base (before defense). Every enemy damaged burns.
//
// The defaults (playtest round 6): 12 + 4 × rank missiles over ± (90 + 15 × rank) px. focus 2
// packs the columns toward the flare: column offset = spread × |v|^focus for v spread evenly
// over -1..1, so half of them fall within ± spread / 4. A well placed flare deals its target
// ~1.4 × base at rank 0 (12 missiles) and the cap at rank 4 (28) on grassland_valley
// (TestMissileRainOnRealMap), the rest still covers ~100 px around. The missiles start 60 px
// above the map at 1500 px/s (~0.37 s down to the ground), 0.25 × base each, blast 0.7 ×, crater
// 1 × (migration 039), cap 2 × base, burn 1 turn.

func init() {
	registerSkill(effectMissileRain, resolveMissileRain)
	registerBotSkill(effectMissileRain, botMissileRain)
}

// rainDropPower is the power that fires a missile down at the tuning's dropSpeed.
func rainDropPower(t gamedata.RainTuning, ph *gamedata.PhysicsConfig) float64 {
	return t.DropSpeed / ph.ProjectileSpeedMultiplier
}

func rainCount(t gamedata.RainTuning, rank int) int { return t.BaseCount + t.CountPerRank*rank }
func rainSpread(t gamedata.RainTuning, rank int) float64 {
	return t.SpreadBase + t.SpreadPerRank*float64(rank)
}

// rainColumns returns n drop columns over center ± spread: one per equal slice of -1..1,
// jittered inside it, pulled toward the center by focus (offset spread × |v|^focus),
// clamped to the map, in a random falling order.
func rainColumns(rng *rand.Rand, n int, center, spread float64, width int, focus float64) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		v := -1 + 2*(float64(i)+rng.Float64())/float64(n)
		x := center + math.Copysign(math.Pow(math.Abs(v), focus), v)*spread
		xs[i] = math.Max(2, math.Min(float64(width)-2, x))
	}
	rng.Shuffle(n, func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	return xs
}

// missileRain: the flare flies like a normal shot without exploding; delay s after it lands
// the missiles fall straight down around it, each launched interval s after the previous.
// They are resolved in the order they land, each seeing the craters of those before it.
func resolveMissileRain(m *Match, s *skillShot) {
	player, action, skillID, weapon, base, shot, tl := s.Player, s.Action, s.Skill.SkillID, s.Weapon, s.Base, s.Shot, s.TL
	tu := gamedata.RainTuningOf(s.Skill)
	flare := m.fireProjectile(player, shotOrigin(m.Terrain, player), action.Angle, action.Power, weapon, m.State.Wind, 0)
	flare.SkillID, flare.Kind = skillID, "flare"
	flare.ExplosionRadius, flare.CraterRadius = 0, 0
	shot.Projectiles = append(shot.Projectiles, flare)
	if flare.ExplosionPoint == nil || flare.ExplosionPoint.Y >= float64(m.Terrain.Height) {
		return // the flare left the map: nothing falls
	}
	landed, at := flightEnd(flare), *flare.ExplosionPoint
	tl.add(ShotEvent{T: landed, Type: "flare", ProjectileID: flare.ProjectileID, Position: vecPtr(at)})

	n, spread := rainCount(tu, player.SkillRank), rainSpread(tu, player.SkillRank)
	xs := rainColumns(m.skillRNG(), n, at.X, spread, m.Terrain.Width, tu.Focus)
	start := landed + tu.Delay
	tl.add(ShotEvent{T: start, Type: "rain", Position: vecPtr(at), Radius: spread, Count: n})

	drop := weapon
	drop.WindInfluence = 0
	launch := func(k int) float64 { return start + tu.Interval*float64(k) }
	limit := newSkillCap(m.capOfBase(player, tu.Cap*base))
	dropPower := rainDropPower(tu, m.gd.Physics)
	burned := map[string]bool{}
	left := make([]int, n)
	for k := range left {
		left[k] = k
	}
	for len(left) > 0 {
		// The missile landing first, on the terrain as the earlier ones left it
		pick, bestEnd := -1, math.MaxFloat64
		var best *ProjectileResult
		for i, k := range left {
			r := SimulateProjectile(player.PlayerID, player.TeamID, Vector2{X: xs[k], Y: tu.StartY}, 270, dropPower,
				drop, WindState{}, m.Terrain, m.State.Players, 0)
			if end := launch(k) + flightEnd(r); end < bestEnd {
				pick, best, bestEnd = i, r, end
			}
		}
		k := left[pick]
		left = append(left[:pick], left[pick+1:]...)

		player.ShotsFired++
		delayPath(best, launch(k))
		best.SkillID, best.Kind = skillID, "missile"
		best.ExplosionRadius = float64(weapon.ExplosionRadius) * tu.Blast
		best.CraterRadius = weaponCraterRadius(m.gd, weapon) * tu.Crater
		shot.Projectiles = append(shot.Projectiles, best)
		if best.ExplosionPoint == nil {
			continue
		}
		hit := m.explodeCapped(player, best, base*tu.Damage, tl, limit)
		m.applyOnce(tl, flightEnd(best), player, hit, burned, "burn", tu.BurnTurns)
		m.settleFalls(player, flightEnd(best), tl)
	}
}

// botMissileRain: aimed like the bot's best normal shot at an enemy (aimedSkill).
func botMissileRain(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	return b.aimedSkill(m, bot, weapon)
}
