package match

import "strings"

// Shared skill tunables (the status effects, shield and skill energy every skill uses). Each
// skill's own numbers live at the top of its file (skill_rain.go, skill_bombchain.go,
// skill_laser.go, skill_bloom.go).
//
// "base" is a skill's base damage (skillBaseDamage): (weapon damage + damageBonus) ×
// power_shot 1.5 (when armed) × the skill's config damage_multiplier (≤ 0 counts as 1) ×
// (1 + skillPowerBonus/100). "rank" is the shooter's SkillRank: the equipped weapon's tier
// rank (WeaponSkillRank, 0..4).
const (
	// Skill energy: +skillEnergyPerTurn at the end of each own turn, +skillEnergyOnHit once per
	// enemy action that takes HP or shield from the player (grantHitEnergy); usable at
	// skillEnergyMax.
	skillEnergyMax     = 100
	skillEnergyPerTurn = 20
	skillEnergyOnHit   = 10

	// Burn: at the start of the target's turn it loses burnMaxHPPercent % of its max HP
	// (shield first, no defense); it can kill.
	burnMaxHPPercent = 10
	// Stun and freeze skip the target's whole turn: the server ends it after skipTurnDelay s.
	skipTurnDelay = 2.0
	// A player's shield never exceeds shieldCapPercent % of its max HP (addShield).
	shieldCapPercent = 50
)

// badEffects are the effects Cleanse removes: control and damage over time put on a player by
// an enemy. The player's own shot modifiers (power_shot, ...) stay.
var badEffects = map[string]bool{"burn": true, "root": true, "stun": true, "net": true, "freeze": true}

// weaponTierRanks are the skill ranks of the weapon tiers (normal lv10..lv40 and crafted).
var weaponTierRanks = map[string]int{
	"lv10": 1, "lv20": 2, "lv30": 3, "lv40": 4,
	"iron": 1, "steel": 2, "mithril": 3, "dragon": 4,
	"silver": 2, "gold": 3, "titan": 4, "diamond": 4,
}

// WeaponSkillRank is the skill rank of the weapon item a player brought: the tier word at the
// end of its id (<character>_weapon_lvN / normal_weapon_lvN: lv10 iron 1, lv20 steel 2, lv30
// mithril 3, lv40 dragon 4; crafted_weapon_<tier>: silver 2, gold 3, titan 4, diamond 4), 0
// for no weapon (bots) and an unknown id. The +upgrade level does not count.
func WeaponSkillRank(weaponItemID string) int {
	if weaponItemID == "" {
		return 0
	}
	parts := strings.Split(weaponItemID, "_")
	return weaponTierRanks[parts[len(parts)-1]]
}
