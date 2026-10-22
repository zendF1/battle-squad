package match

import "battle-squad/internal/game/gamedata"

// Bot skill use. With full skill energy the bot asks the planner registered for its
// character's skill effect (each skill file registers one in its init); a planner returns the
// Shoot angle / power and true to use the skill now, or false to keep the energy and act
// normally. skillAction fills in ActionMode "skill" and the timestamp.
type botSkillPlanner func(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (angle, power float64, use bool)

var botSkillPlanners = map[string]botSkillPlanner{}

// registerBotSkill makes the bot plan effectType's skill with p.
func registerBotSkill(effectType string, p botSkillPlanner) { botSkillPlanners[effectType] = p }

// botSkillEffect is the effect type of the bot's character skill, "" when none. A skill that
// ignores the aim (bloom; the laser ignores the power) is sent with rules.aim noAimAngle /
// noAimPower: the server still validates them.
func botSkillEffect(gd *gamedata.Snapshot, bot *BattlePlayerState) string {
	char, ok := gd.Data.Characters[bot.CharacterID]
	if !ok {
		return ""
	}
	return gd.Data.Skills[char.SkillID].EffectType
}

// skillAction asks the bot's skill planner whether and how to use the skill now.
func (b *BotBrain) skillAction(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig, now int64) (ShootAction, bool) {
	plan, ok := botSkillPlanners[botSkillEffect(m.gd, bot)]
	if !ok {
		return ShootAction{}, false
	}
	angle, power, use := plan(b, m, bot, weapon)
	if !use {
		return ShootAction{}, false
	}
	return ShootAction{Angle: angle, Power: power, ActionMode: "skill", ClientTimestamp: now}, true
}

// aimedSkill plans a skill fired like the bot's best normal shot at an enemy, missing by the
// tier's hit chance like it (nearMiss); false when no trajectory damages an enemy.
func (b *BotBrain) aimedSkill(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	best := b.bestShot(m, bot, bot.Position, weapon)
	if !best.found || best.damage <= 0 {
		return 0, 0, false
	}
	shot := best
	if !b.rollHit() {
		if miss, ok := b.nearMiss(m, bot, best, weapon); ok {
			shot = miss
		}
	}
	return shot.angle, shot.power, true
}
