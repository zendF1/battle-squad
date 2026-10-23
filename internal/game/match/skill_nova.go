package match

import (
	"math"
	"sort"

	"battle-squad/internal/game/gamedata"
)

// Nova (Tide, skill snow_nova, effect nova, 054; the numbers are the skill's gamedata.NovaTuning):
// no aim, no projectile, no crater, no falls. castTime (0.35 s) after the shot a snowflake blast
// goes off at Tide's body centre (a nova event: position, radius, duration = radius / waveSpeed);
// its frost ring travels out at waveSpeed (1400 px/s). Every living enemy unit (players, minions,
// the boss, flying ones too) whose body is within radius (500 px; blastDistance, to the body's
// edge) takes novaFactor × base: (centreBase + centrePerRank × rank) (1.3 + 0.1 × rank) at the
// centre, falling linearly to edgeFactor (0.4) at the radius, × the element factor, reduced by
// defense, the critical roll and the boss's minion shield; when the ring reaches it (castTime +
// distance / waveSpeed) a damage event (cause nova), then, if it lives, it is frozen for
// freezeTurns (1) (a status event). Tide, her teammates and allied units are never touched.

func init() {
	registerSkill(effectNova, resolveNova)
	registerBotSkill(effectNova, botNova)
}

// CauseNova is the damage / die cause of the nova.
const CauseNova = "nova"

// novaCentre is the nova's centre factor at rank (× base).
func novaCentre(t gamedata.NovaTuning, rank int) float64 {
	return t.CentreBase + t.CentrePerRank*float64(rank)
}

// novaFactor is the nova's damage factor (× base) d px from its centre at rank: the centre
// factor at 0 down to edgeFactor at the radius, 0 beyond.
func novaFactor(t gamedata.NovaTuning, rank int, d float64) float64 {
	if d > t.Radius || t.Radius <= 0 {
		return 0
	}
	c := novaCentre(t, rank)
	return c + (t.EdgeFactor-c)*math.Max(d, 0)/t.Radius
}

// novaHitTime is when the nova's ring reaches a body d px from its centre (s since the shot).
func novaHitTime(t gamedata.NovaTuning, d float64) float64 {
	return t.CastTime + math.Max(d, 0)/t.WaveSpeed
}

// novaHit is an enemy in the nova and its distance from the centre (to its body's edge).
type novaHit struct {
	p *BattlePlayerState
	d float64
}

// novaHits are the living enemies of p within the nova's radius of its centre, nearest first.
func (m *Match) novaHits(p *BattlePlayerState, t gamedata.NovaTuning) (Vector2, []novaHit) {
	hitRadius, lift := m.gd.Physics.PlayerHitRadius, shotOriginLift(m.Terrain)
	centre := bodyCentre(p, lift)
	var hits []novaHit
	for _, q := range m.playersByID() {
		if !q.IsAlive || q.TeamID == p.TeamID {
			continue
		}
		if d := blastDistance(q, centre, hitRadius, lift); d <= t.Radius {
			hits = append(hits, novaHit{q, d})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].d < hits[j].d })
	return centre, hits
}

// novaTuningOf is the tuning of p's character skill when it is a nova (the defaults otherwise).
func (m *Match) novaTuningOf(p *BattlePlayerState) gamedata.NovaTuning {
	if c, ok := m.gd.Data.Characters[p.CharacterID]; ok {
		return gamedata.NovaTuningOf(m.gd.Data.Skills[c.SkillID])
	}
	return gamedata.DefaultNovaTuning()
}

// resolveNova: the snowflake blast at the caster's body centre (see the top of the file).
func resolveNova(m *Match, s *skillShot) {
	player, base, tl := s.Player, s.Base, s.TL
	tu := gamedata.NovaTuningOf(s.Skill)
	k, crit := m.gd.Physics.DefenseKOrDefault(), m.gd.Rules.Combat.CritMultiplier
	player.ShotsFired++
	centre, hits := m.novaHits(player, tu)
	tl.add(ShotEvent{T: tu.CastTime, Type: "nova", PlayerID: player.PlayerID, Position: vecPtr(centre), Radius: tu.Radius,
		Duration: tu.Radius / tu.WaveSpeed})
	for _, h := range hits {
		p, t := h.p, novaHitTime(tu, h.d)
		ef, effect := m.elementFactor(player, p)
		damage := int(math.Round(reduceByDefense(novaFactor(tu, player.SkillRank, h.d)*base*ef, p.Defense, k)))
		damage, critical := applyCritical(damage, player.CritChance, crit)
		damage = m.shieldedDamage(p, damage)
		if damage > 0 {
			shieldBefore := p.Shield
			killed := hurtPlayer(p, damage)
			player.DamageDealt += damage
			m.noteBossSideDamage(player, p, damage)
			player.ShotsHit++
			e := damageEvent(t, p, damage, shieldBefore, CauseNova)
			e.Critical, e.ElementEffect = critical, effect
			tl.add(e)
			if killed {
				player.KillCount++
				tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: CauseNova})
				continue
			}
		}
		if tu.FreezeTurns > 0 {
			m.applyShotStatus(tl, t, player, p, "freeze", tu.FreezeTurns)
		}
	}
}

// botNova: the bot casts the nova when two or more enemies are inside it, or one is and the
// nova's expected damage beats its normal shot's (its best shot's damage × its hit chance).
func botNova(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	char := m.gd.Data.Characters[bot.CharacterID]
	skill := m.gd.Data.Skills[char.SkillID]
	tu := gamedata.NovaTuningOf(skill)
	_, hits := m.novaHits(bot, tu)
	aim := m.gd.Rules.Aim
	switch {
	case len(hits) == 0:
		return 0, 0, false
	case len(hits) >= 2:
		return aim.NoAimAngle, aim.NoAimPower, true
	}
	base := skillBaseDamage(bot, skill, weapon, m.gd.PowerShotFactor())
	k := m.gd.Physics.DefenseKOrDefault()
	nova := 0.0
	for _, h := range hits {
		ef, _ := m.elementFactor(bot, h.p)
		nova += float64(m.shieldedDamage(h.p, int(math.Round(reduceByDefense(novaFactor(tu, bot.SkillRank, h.d)*base*ef, h.p.Defense, k)))))
	}
	best := b.bestShot(m, bot, bot.Position, weapon)
	shot := 0.0
	if best.found && best.damage > 0 {
		shot = best.damage * b.hitChance()
	}
	if nova > shot {
		return aim.NoAimAngle, aim.NoAimPower, true
	}
	return 0, 0, false
}
