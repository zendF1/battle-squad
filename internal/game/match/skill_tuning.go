package match

import (
	"strings"

	"battle-squad/internal/game/gamedata"
)

// The shared skill rules come from the match's game data (rules.skill_energy, rules.combat,
// rules.skill_ranks, config_status_effects); each skill's own numbers are its
// config_skills.params (gamedata.RainTuningOf, ChainTuningOf, LaserTuningOf, BloomTuningOf).
//
// "base" is a skill's base damage (skillBaseDamage): (weapon damage + damageBonus) ×
// power_shot (when armed) × the skill's config damage_multiplier (≤ 0 counts as 1) ×
// (1 + skillPowerBonus/100). "rank" is the shooter's SkillRank: the equipped weapon's tier
// rank (weaponSkillRank, 0..4).
//
// Skill energy: +perTurn at the end of each own turn, +onHit once per enemy action that takes
// HP or shield from the player (grantHitEnergy); usable at max. Burn: at the start of the
// target's turn it loses burnMaxHpPercent % of its max HP (shield first, no defense); it can
// kill. Stun and freeze skip the target's whole turn: the server ends it after
// skipTurnDelaySec. A player's shield never exceeds shieldCapPercent % of its max HP.

// weaponSkillRank is the skill rank of the weapon item a player brought: the tier word at the
// end of its id (<character>_weapon_lvN / normal_weapon_lvN: lv10 iron 1, lv20 steel 2, lv30
// mithril 3, lv40 dragon 4; crafted_weapon_<tier>: silver 2, gold 3, titan 4, diamond 4) in
// rules.skill_ranks, its default for no weapon (bots) and an unknown word. The +upgrade level
// does not count.
func weaponSkillRank(gd *gamedata.Snapshot, weaponItemID string) int {
	ranks := gd.Rules.SkillRanks
	if weaponItemID == "" {
		return ranks.Default
	}
	parts := strings.Split(weaponItemID, "_")
	if r, ok := ranks.ByTierWord[parts[len(parts)-1]]; ok {
		return r
	}
	return ranks.Default
}

// WeaponSkillRank is weaponSkillRank with the current game data.
func WeaponSkillRank(weaponItemID string) int {
	return weaponSkillRank(gamedata.Current(), weaponItemID)
}
