package match

import (
	"context"
	"hash/fnv"
	"math"
	"math/rand"
	"strconv"

	"battle-squad/internal/game/gamedata"
)

// Character skills: config_skills.effect_type picks the resolver registered for it (one file
// per skill: skill_rain.go, skill_bombchain.go, skill_laser.go, skill_bloom.go). An effect
// type without a resolver fires one normal projectile with the skill's damage.
const (
	effectMissileRain = "missile_rain" // Rookie, triple_shot
	effectBombChain   = "bomb_chain"   // Tanko, heavy_bomb
	effectLaser       = "laser"        // Spark, shock_field
	effectBloom       = "bloom"        // Flora, healing_bloom
)

// skillShot is what a skill resolver works with: the shooter, its Shoot action, the skill's
// config, the shooter's weapon, the base damage (skillBaseDamage), and the shot and timeline it
// fills (projectiles, events). shootSkill already checked the energy; after the resolver it
// resets the energy and finishes the shot (hit energy, broadcast, win check, end of turn).
type skillShot struct {
	Player *BattlePlayerState
	Action ShootAction
	Skill  gamedata.SkillConfig
	Weapon gamedata.WeaponConfig
	Base   float64
	Shot   *ShotResolved
	TL     *shotTimeline
}

// skillResolver resolves one skill shot on the match state (craters, damage, effects) and
// records it in s.Shot / s.TL.
type skillResolver func(m *Match, s *skillShot)

var skillResolvers = map[string]skillResolver{}

// registerSkill makes effectType resolve with r (called from each skill file's init).
func registerSkill(effectType string, r skillResolver) { skillResolvers[effectType] = r }

// shootSkill fires the character's skill at full skill energy and resets the energy.
func (m *Match) shootSkill(ctx context.Context, player *BattlePlayerState, action ShootAction, skillID string, weapon gamedata.WeaponConfig) {
	skill, ok := gamedata.Data.Skills[skillID]
	if !ok || player.SkillEnergy < skillEnergyMax {
		return
	}
	s := &skillShot{Player: player, Action: action, Skill: skill, Weapon: weapon, Base: skillBaseDamage(player, skill, weapon),
		Shot: &ShotResolved{ShooterID: player.PlayerID, ActionMode: "skill", SkillID: skillID}, TL: &shotTimeline{}}
	if resolve, ok := skillResolvers[skill.EffectType]; ok {
		resolve(m, s)
	} else {
		resolveSingleProjectile(m, s)
	}
	player.SkillEnergy = 0
	player.SkillUses++
	m.finishShot(ctx, s.Shot, s.TL)
}

// resolveSingleProjectile fires one normal projectile with the skill's base damage: the
// fallback for an effect type without a resolver.
func resolveSingleProjectile(m *Match, s *skillShot) {
	p := s.Player
	r := m.fireProjectile(p, shotOrigin(m.Terrain, p), s.Action.Angle, s.Action.Power, s.Weapon, m.State.Wind, drillPierce(p))
	r.SkillID = s.Skill.SkillID
	s.Shot.Projectiles = append(s.Shot.Projectiles, r)
	if r.ExplosionPoint != nil {
		m.explodeProjectile(p, r, s.Base, s.TL)
		m.settleFalls(p, flightEnd(r), s.TL)
	}
}

// skillBaseDamage is the skill's base damage: (weapon damage + damage bonus) × power_shot ×
// the config damage_multiplier (≤ 0 counts as 1) × skill power.
func skillBaseDamage(player *BattlePlayerState, skill gamedata.SkillConfig, weapon gamedata.WeaponConfig) float64 {
	factor := 1.0
	if HasEffect(player, "power_shot") {
		factor = powerShotFactor
	}
	if skill.DamageMultiplier > 0 {
		factor *= skill.DamageMultiplier
	}
	return float64(weapon.Damage+player.DamageBonus) * factor * skillPowerFactor(player)
}

// fireProjectile simulates one projectile of the player's and counts it as fired.
func (m *Match) fireProjectile(player *BattlePlayerState, origin Vector2, angle, power float64, weapon gamedata.WeaponConfig, wind WindState, pierce float64) *ProjectileResult {
	player.ShotsFired++
	return SimulateProjectile(player.PlayerID, player.TeamID, origin, angle, power, weapon, wind, m.Terrain, m.State.Players, pierce)
}

// delayPath makes r leave delay s after the shot: its path times are shifted by delay.
func delayPath(r *ProjectileResult, delay float64) {
	for i := range r.Path {
		r.Path[i].Time += delay
	}
	r.LaunchDelay = delay
}

// applyOnce applies effectID for turns to every target still alive that was not given it yet
// by this skill (seen), at time t.
func applyOnce(tl *shotTimeline, t float64, source *BattlePlayerState, targets []*BattlePlayerState, seen map[string]bool, effectID string, turns int) {
	for _, p := range targets {
		if !p.IsAlive || seen[p.PlayerID] {
			continue
		}
		seen[p.PlayerID] = true
		applyShotStatus(tl, t, source, p, effectID, turns)
	}
}

// skillRNG is the seeded random source of this turn's skill: the same match and turn give the
// same numbers (the missile rain columns), so a replay of the event log is deterministic.
func (m *Match) skillRNG() *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(m.State.MatchID + ":" + strconv.Itoa(m.State.TurnIndex)))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// angleDir is the unit direction of a Shoot angle (degrees, y down).
func angleDir(angle float64) Vector2 {
	rad := angle * math.Pi / 180
	return Vector2{X: math.Cos(rad), Y: -math.Sin(rad)}
}

// addShield adds amount shield to p, capped at shieldCapPercent % of its max HP (the excess
// is lost), and returns the shield gained.
func addShield(p *BattlePlayerState, amount int) int {
	shield := min(p.Shield+max(amount, 0), p.MaxHP*shieldCapPercent/100)
	if shield <= p.Shield {
		return 0
	}
	gained := shield - p.Shield
	p.Shield = shield
	return gained
}

// healPlayer heals p by amount up to its max HP and returns the HP gained; the rest is the
// overflow (amount − gained).
func healPlayer(p *BattlePlayerState, amount int) int {
	gained := min(max(amount, 0), max(p.MaxHP-p.HP, 0))
	p.HP += gained
	return gained
}

// cleanseEvent removes p's bad effects (Cleanse) and records a cleanse event at t when any
// were removed.
func cleanseEvent(tl *shotTimeline, t float64, p *BattlePlayerState) {
	if removed := Cleanse(p); len(removed) > 0 {
		tl.add(ShotEvent{T: t, Type: "cleanse", PlayerID: p.PlayerID, EffectIDs: removed})
	}
}
