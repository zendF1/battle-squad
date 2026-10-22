// Package battlestats computes the stats a player brings into a match from the
// character config, allocated stat points and equipped items. The game server
// (room) and the API (GET /player/characters) both use it, so the numbers the
// profile shows are the numbers the battle uses.
package battlestats

import (
	"math"
	"sort"

	"battle-squad/internal/game/gamedata"
)

// Default base stats for a character missing from the game data.
const (
	defaultHP      = 1500
	defaultDefense = 50
)

// Stats are battle stats, or a part of them (from points, from equipment).
type Stats struct {
	HP                 int
	Defense            int
	DamageBonus        int     // flat, added to weapon damage
	MoveEnergy         int     // the character's base move energy per turn (gamedata.BaseMoveEnergy); only Base sets it
	MoveEnergyBonus    int     // flat, added to MoveEnergy each turn
	CritChance         float64 // percent
	SkillPowerBonus    int     // percent, scales skill damage and healing
	TerrainDamageBonus int     // percent, grows the crater radius
}

// Add returns s + o field by field.
func (s Stats) Add(o Stats) Stats {
	return Stats{
		HP:                 s.HP + o.HP,
		Defense:            s.Defense + o.Defense,
		DamageBonus:        s.DamageBonus + o.DamageBonus,
		MoveEnergy:         s.MoveEnergy + o.MoveEnergy,
		MoveEnergyBonus:    s.MoveEnergyBonus + o.MoveEnergyBonus,
		CritChance:         s.CritChance + o.CritChance,
		SkillPowerBonus:    s.SkillPowerBonus + o.SkillPowerBonus,
		TerrainDamageBonus: s.TerrainDamageBonus + o.TerrainDamageBonus,
	}
}

// TurnMoveEnergy is the move energy a player with these stats gets each turn:
// the base plus the bonuses.
func (s Stats) TurnMoveEnergy() int {
	return s.MoveEnergy + s.MoveEnergyBonus
}

// Points are the points a player allocated to a character
// (player_characters bonus_* columns).
type Points struct {
	HP, Damage, Mobility, Defense, SkillPower, TerrainDamage int
}

// Multipliers are what one allocated point adds in battle
// (character_progression statMultipliers, with defaults filled in).
type Multipliers struct {
	HP            int `json:"hp"`
	Damage        int `json:"damage"`
	Mobility      int `json:"mobility"`
	Defense       int `json:"defense"`
	SkillPower    int `json:"skillPower"`
	TerrainDamage int `json:"terrainDamage"`
}

// Base returns a character's config HP, defense and move energy with no bonuses (the current
// game data).
func Base(characterID string) Stats { return BaseIn(gamedata.Current(), characterID) }

// BaseIn is Base with the game data gd.
func BaseIn(gd *gamedata.Snapshot, characterID string) Stats {
	s := Stats{HP: defaultHP, Defense: defaultDefense, MoveEnergy: gd.BaseMoveEnergy(characterID)}
	if c, ok := gd.Data.Characters[characterID]; ok {
		s.HP, s.Defense = c.HP, c.Defense
	}
	return s
}

// WeaponDamage returns the damage of the character's weapon (0 when unknown).
func WeaponDamage(characterID string) int {
	gd := gamedata.Current()
	c, ok := gd.Data.Characters[characterID]
	if !ok {
		return 0
	}
	return gd.Data.Weapons[c.WeaponID].Damage
}

// FromPoints converts allocated points to battle stats.
func FromPoints(p Points, m Multipliers) Stats {
	return Stats{
		HP:                 p.HP * m.HP,
		DamageBonus:        p.Damage * m.Damage,
		MoveEnergyBonus:    p.Mobility * m.Mobility,
		Defense:            p.Defense * m.Defense,
		SkillPowerBonus:    p.SkillPower * m.SkillPower,
		TerrainDamageBonus: p.TerrainDamage * m.TerrainDamage,
	}
}

// Gem is a gem socketed into an equipped item.
type Gem struct {
	Type  string // hp, damage, defense, critical
	Value float64
}

// EquippedItem is one item a character wears, with its config stats.
type EquippedItem struct {
	ItemID              string
	Slot                string // weapon, armor, helmet, pants, boots, gloves
	HP, Damage, Defense int
	Crit                float64
	MoveEnergy          int
	UpgradeLevel        int
	Category            string
	Tier                string // "" when the item has no tier
	Gems                []Gem
}

// SetBonus is a config_set_bonuses row: percent bonuses for wearing
// PiecesRequired crafted items of one tier.
type SetBonus struct {
	PiecesRequired                 int
	HPPct, DMGPct, DEFPct, CritPct float64
}

// Milestones are the extra upgrade multipliers reached at +6, +10, +14 and +16.
type Milestones struct {
	Bonus6, Bonus10, Bonus14, Bonus16 float64
}

// DefaultMilestones are used for game_settings keys that are absent.
func DefaultMilestones() Milestones {
	return Milestones{Bonus6: 0.10, Bonus10: 0.20, Bonus14: 0.40, Bonus16: 1.00}
}

// UpgradeStepPerLevel is what each +level of a piece adds to its stat multiplier
// (rules.equipment upgradeStepPerLevel of the current game data, 2 %).
func UpgradeStepPerLevel() float64 { return gamedata.Current().Rules.Equipment.UpgradeStepPerLevel }

// upgradeMultiplier is 1 + UpgradeStepPerLevel per upgrade level + reached milestone bonuses.
func (ms Milestones) upgradeMultiplier(level int) float64 {
	mul := 1.0 + float64(level)*UpgradeStepPerLevel()
	if level >= 6 {
		mul += ms.Bonus6
	}
	if level >= 10 {
		mul += ms.Bonus10
	}
	if level >= 14 {
		mul += ms.Bonus14
	}
	if level >= 16 {
		mul += ms.Bonus16
	}
	return mul
}

// EquipmentPoints are the stat points a character's equipped items add, in the
// same unit as allocated points: they become battle stats through the same
// statMultipliers (see FromEquipment). Crit has no multiplier and is a crit
// chance percent.
type EquipmentPoints struct {
	HP       int     `json:"hp"`
	Damage   int     `json:"damage"`
	Defense  int     `json:"defense"`
	Mobility int     `json:"mobility"` // from stat_move_energy
	Crit     float64 `json:"crit"`     // percent
}

// SumEquipment sums equipped items' points: each item stat × its upgrade
// multiplier, rounded per item, plus gem values (truncated per gem), then each
// crafted tier's reached set bonuses (percentages summed per tier, applied
// once to the point totals and rounded; set crit is added).
func SumEquipment(items []EquippedItem, setBonuses map[string][]SetBonus, ms Milestones) EquipmentPoints {
	var e EquipmentPoints
	craftedTierCount := make(map[string]int)
	for _, it := range items {
		mul := ms.upgradeMultiplier(it.UpgradeLevel)
		e.HP += int(math.Round(float64(it.HP) * mul))
		e.Damage += int(math.Round(float64(it.Damage) * mul))
		e.Defense += int(math.Round(float64(it.Defense) * mul))
		e.Crit += it.Crit * mul
		e.Mobility += int(math.Round(float64(it.MoveEnergy) * mul))

		if it.Category == "crafted" && it.Tier != "" {
			craftedTierCount[it.Tier]++
		}
		for _, g := range it.Gems {
			switch g.Type {
			case "hp":
				e.HP += int(g.Value)
			case "damage":
				e.Damage += int(g.Value)
			case "defense":
				e.Defense += int(g.Value)
			case "critical":
				e.Crit += g.Value
			}
		}
	}

	tiers := make([]string, 0, len(craftedTierCount))
	for t := range craftedTierCount {
		tiers = append(tiers, t)
	}
	sort.Strings(tiers) // rounding depends on order when several tiers apply
	for _, t := range tiers {
		var hpPct, dmgPct, defPct, critPct float64
		for _, b := range setBonuses[t] {
			if craftedTierCount[t] >= b.PiecesRequired {
				hpPct += b.HPPct
				dmgPct += b.DMGPct
				defPct += b.DEFPct
				critPct += b.CritPct
			}
		}
		if hpPct > 0 {
			e.HP = int(math.Round(float64(e.HP) * (1 + hpPct/100)))
		}
		if dmgPct > 0 {
			e.Damage = int(math.Round(float64(e.Damage) * (1 + dmgPct/100)))
		}
		if defPct > 0 {
			e.Defense = int(math.Round(float64(e.Defense) * (1 + defPct/100)))
		}
		e.Crit += critPct
	}
	return e
}

// FromEquipment converts equipment points to battle stats with the per-point
// multipliers allocated points use: hp → max HP, damage → damage bonus,
// defense → defense, mobility → move energy. Crit is already a percent.
func FromEquipment(e EquipmentPoints, m Multipliers) Stats {
	return Stats{
		HP:              e.HP * m.HP,
		DamageBonus:     e.Damage * m.Damage,
		Defense:         e.Defense * m.Defense,
		MoveEnergyBonus: e.Mobility * m.Mobility,
		CritChance:      e.Crit,
	}
}

// Breakdown is a character's battle stats and where they come from:
// Total = Base + FromPoints + FromEquipment. EquipmentPoints are the points
// FromEquipment was converted from.
type Breakdown struct {
	Base, FromPoints, FromEquipment, Total Stats
	EquipmentPoints                        EquipmentPoints
}

// Compute builds the breakdown for an owned character; allocated points and
// equipment points are converted with the same multipliers.
func Compute(characterID string, p Points, m Multipliers, equipment EquipmentPoints) Breakdown {
	b := Breakdown{
		Base: Base(characterID), FromPoints: FromPoints(p, m),
		FromEquipment: FromEquipment(equipment, m), EquipmentPoints: equipment,
	}
	b.Total = b.Base.Add(b.FromPoints).Add(b.FromEquipment)
	return b
}
