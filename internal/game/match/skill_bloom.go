package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// Bloom (Flora, skill healing_bloom, effect bloom): no aim. A seed per living ally (Flora too)
// rises with the others to a shared apex, splits and lands on the ally at bloomSeedTime: it
// heals a percent of the ally's max HP (Flora bloomSelfBase + bloomPerRank × rank, a teammate
// bloomAllyBase + bloomPerRank × rank; not scaled by skill power), the overflow becomes shield
// (addShield, capped at shieldCapPercent % of max HP) and its bad effects are cleansed.
const (
	bloomSelfBase = 40
	bloomAllyBase = 30
	bloomPerRank  = 5
	bloomSeedTime = 1.1   // s a seed flies from Flora to an ally; the heal lands then
	bloomSeedApex = 220.0 // px the seeds rise above the highest ally
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

// seedPath is a bloom seed's flight from origin to an ally's body at to: up to apex (shared by
// every seed of the bloom, so they rise as one and split there), then falling onto the ally,
// landing at bloomSeedTime.
func seedPath(origin, apex, to Vector2) []ProjectileStep {
	const up, step = 0.45, 0.05
	down := bloomSeedTime - up
	rise := origin.Y - apex.Y
	vx := (to.X - apex.X) / down
	g := 2 * (to.Y - apex.Y) / (down * down)
	at := func(t float64) ProjectileStep {
		if t <= up {
			k := 1 - t/up
			x := origin.X + (apex.X-origin.X)*(1-k*k)
			return ProjectileStep{
				Position: Vector2{X: x, Y: origin.Y - rise*(1-k*k)},
				Velocity: Vector2{X: (apex.X - origin.X) * 2 * k / up, Y: -rise * 2 * k / up},
				Time:     t,
			}
		}
		tau := t - up
		return ProjectileStep{
			Position: Vector2{X: apex.X + vx*tau, Y: apex.Y + 0.5*g*tau*tau},
			Velocity: Vector2{X: vx, Y: g * tau},
			Time:     t,
		}
	}
	var path []ProjectileStep
	for i := 0; float64(i)*step < bloomSeedTime-1e-9; i++ {
		path = append(path, at(float64(i)*step))
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
	top := origin.Y
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID == player.TeamID {
			allies = append(allies, p)
			top = math.Min(top, p.Position.Y-ShotOriginLift)
		}
	}
	apex := Vector2{X: origin.X, Y: top - bloomSeedApex}
	t := bloomSeedTime
	for _, p := range allies {
		to := Vector2{X: p.Position.X, Y: p.Position.Y - ShotOriginLift}
		shot.Projectiles = append(shot.Projectiles, &ProjectileResult{
			ProjectileID: generateProjectileID(), OwnerPlayerID: player.PlayerID, SkillID: skillID, Kind: "seed",
			Path: seedPath(origin, apex, to), HitPlayerID: p.PlayerID,
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
