package match

import (
	"math"
	"math/rand"

	"battle-squad/internal/game/gamedata"
)

// Missile rain (Rookie, skill triple_shot, effect missile_rain): a flare fired like a normal
// shot; rainDelay s after it lands, rainBaseCount + rainCountPerRank × rank missiles fall
// straight down rainInterval s apart, over the flare point ± (rainSpreadBase +
// rainSpreadPerRank × rank) px, at columns from the match + turn seed (skillRNG). Each
// missile: rainDamage × base, blast rainBlast × the weapon's, crater rainCrater ×; the whole
// rain deals one target at most rainCap × base (before defense). Every enemy damaged burns.
const (
	rainBaseCount     = 6
	rainCountPerRank  = 2
	rainSpreadBase    = 90.0
	rainSpreadPerRank = 15.0
	rainDelay         = 0.5
	rainInterval      = 0.1
	rainStartY        = -60.0 // missiles start above the top of the map
	rainDropPower     = 50.0  // initial downward speed: power × 6 px/s
	rainDamage        = 0.35
	rainBlast         = 0.7
	rainCrater        = 0.6
	rainCap           = 2.0
	burnTurns         = 1
)

func init() {
	registerSkill(effectMissileRain, resolveMissileRain)
	registerBotSkill(effectMissileRain, botMissileRain)
}

func rainCount(rank int) int      { return rainBaseCount + rainCountPerRank*rank }
func rainSpread(rank int) float64 { return rainSpreadBase + rainSpreadPerRank*float64(rank) }

// rainColumns returns n drop columns over center ± spread: one per equal slice of the spread,
// jittered inside it, clamped to the map, in a random falling order.
func rainColumns(rng *rand.Rand, n int, center, spread float64, width int) []float64 {
	xs := make([]float64, n)
	slice := 2 * spread / float64(n)
	for i := range xs {
		x := center - spread + slice*(float64(i)+rng.Float64())
		xs[i] = math.Max(2, math.Min(float64(width)-2, x))
	}
	rng.Shuffle(n, func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	return xs
}

// missileRain: the flare flies like a normal shot without exploding; rainDelay s after it lands
// the missiles fall straight down around it, each launched rainInterval s after the previous.
// They are resolved in the order they land, each seeing the craters of those before it.
func resolveMissileRain(m *Match, s *skillShot) {
	player, action, skillID, weapon, base, shot, tl := s.Player, s.Action, s.Skill.SkillID, s.Weapon, s.Base, s.Shot, s.TL
	flare := m.fireProjectile(player, shotOrigin(m.Terrain, player), action.Angle, action.Power, weapon, m.State.Wind, 0)
	flare.SkillID, flare.Kind = skillID, "flare"
	flare.ExplosionRadius, flare.CraterRadius = 0, 0
	shot.Projectiles = append(shot.Projectiles, flare)
	if flare.ExplosionPoint == nil || flare.ExplosionPoint.Y >= float64(m.Terrain.Height) {
		return // the flare left the map: nothing falls
	}
	landed, at := flightEnd(flare), *flare.ExplosionPoint
	tl.add(ShotEvent{T: landed, Type: "flare", ProjectileID: flare.ProjectileID, Position: vecPtr(at)})

	n, spread := rainCount(player.SkillRank), rainSpread(player.SkillRank)
	xs := rainColumns(m.skillRNG(), n, at.X, spread, m.Terrain.Width)
	start := landed + rainDelay
	tl.add(ShotEvent{T: start, Type: "rain", Position: vecPtr(at), Radius: spread, Count: n})

	drop := weapon
	drop.WindInfluence = 0
	launch := func(k int) float64 { return start + rainInterval*float64(k) }
	limit := newSkillCap(capOfBase(rainCap * base))
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
			r := SimulateProjectile(player.PlayerID, player.TeamID, Vector2{X: xs[k], Y: rainStartY}, 270, rainDropPower,
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
		best.ExplosionRadius = float64(weapon.ExplosionRadius) * rainBlast
		best.CraterRadius = weaponCraterRadius(weapon) * rainCrater
		shot.Projectiles = append(shot.Projectiles, best)
		if best.ExplosionPoint == nil {
			continue
		}
		hit := m.explodeCapped(player, best, base*rainDamage, tl, limit)
		applyOnce(tl, flightEnd(best), player, hit, burned, "burn", burnTurns)
		m.settleFalls(player, flightEnd(best), tl)
	}
}

// botMissileRain: aimed like the bot's best normal shot at an enemy (aimedSkill).
func botMissileRain(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	return b.aimedSkill(m, bot, weapon)
}
