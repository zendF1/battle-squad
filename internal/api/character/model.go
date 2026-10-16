package character

import (
	"encoding/json"
	"time"

	"battle-squad/internal/game/battlestats"
)

// PlayerCharacter represents a character owned by a player with progression state.
type PlayerCharacter struct {
	PlayerID        string    `json:"playerId"`
	CharacterID     string    `json:"characterId"`
	Level           int       `json:"level"`
	Exp             int       `json:"exp"`
	NextLevelExp    *int      `json:"nextLevelExp"`
	IsMaxLevel      bool      `json:"isMaxLevel"`
	StatPoints      int       `json:"statPoints"`
	BonusHP         int       `json:"bonusHp"`
	BonusDamage     int       `json:"bonusDamage"`
	BonusMobility   int       `json:"bonusMobility"`
	BonusDefense    int       `json:"bonusDefense"`
	BonusSkillPower int       `json:"bonusSkillPower"`
	BonusTerrainDmg int       `json:"bonusTerrainDmg"`
	UnlockedAt      time.Time `json:"unlockedAt"`
	// Stats are the numbers this character brings into a match.
	Stats *CharacterStats `json:"stats,omitempty"`
}

// Points returns the allocated bonus points.
func (c PlayerCharacter) Points() battlestats.Points {
	return battlestats.Points{
		HP: c.BonusHP, Damage: c.BonusDamage, Mobility: c.BonusMobility,
		Defense: c.BonusDefense, SkillPower: c.BonusSkillPower, TerrainDamage: c.BonusTerrainDmg,
	}
}

// StatValues are battle stats as the client shows them. In a total moveEnergy
// is the per-turn energy (100 + bonuses); in a part (fromPoints,
// fromEquipment) every field is what that part adds.
type StatValues struct {
	MaxHP              int     `json:"maxHp"`
	Defense            int     `json:"defense"`
	DamageBonus        int     `json:"damageBonus"`        // flat, added to weapon damage
	MoveEnergy         int     `json:"moveEnergy"`         // per turn
	SkillPowerBonus    int     `json:"skillPowerBonus"`    // percent
	TerrainDamageBonus int     `json:"terrainDamageBonus"` // percent
	CritChance         float64 `json:"critChance,omitempty"`
}

// BaseStats are a character's stats with no points or equipment.
type BaseStats struct {
	StatValues
	WeaponDamage int `json:"weaponDamage"`
}

// CharacterStats are an owned character's battle stats and their parts:
// total = base + fromPoints + fromEquipment. Equipment adds stat points like
// allocated points do: EquipmentPoints are those points, FromEquipment is them
// converted with statMultipliers (crit is a percent and is not converted).
type CharacterStats struct {
	StatValues
	WeaponDamage    int                         `json:"weaponDamage"`
	Base            StatValues                  `json:"base"`
	FromPoints      StatValues                  `json:"fromPoints"`
	FromEquipment   StatValues                  `json:"fromEquipment"`
	EquipmentPoints battlestats.EquipmentPoints `json:"equipmentPoints"`
}

// statValues converts battle stats; total adds BaseMoveEnergy to the move energy.
func statValues(s battlestats.Stats, total bool) StatValues {
	moveEnergy := s.MoveEnergyBonus
	if total {
		moveEnergy += battlestats.BaseMoveEnergy
	}
	return StatValues{
		MaxHP: s.HP, Defense: s.Defense, DamageBonus: s.DamageBonus, MoveEnergy: moveEnergy,
		SkillPowerBonus: s.SkillPowerBonus, TerrainDamageBonus: s.TerrainDamageBonus, CritChance: s.CritChance,
	}
}

// newCharacterStats converts a battlestats breakdown.
func newCharacterStats(characterID string, b battlestats.Breakdown) *CharacterStats {
	return &CharacterStats{
		StatValues:      statValues(b.Total, true),
		WeaponDamage:    battlestats.WeaponDamage(characterID),
		Base:            statValues(b.Base, true),
		FromPoints:      statValues(b.FromPoints, false),
		FromEquipment:   statValues(b.FromEquipment, false),
		EquipmentPoints: b.EquipmentPoints,
	}
}

// AllocateStatsPayload is the request body for allocating stat points.
type AllocateStatsPayload struct {
	CharacterID   string `json:"characterId"`
	HP            int    `json:"hp"`
	Damage        int    `json:"damage"`
	Mobility      int    `json:"mobility"`
	Defense       int    `json:"defense"`
	SkillPower    int    `json:"skillPower"`
	TerrainDamage int    `json:"terrainDamage"`
}

// ResetStatsCost is what POST /player/character/reset-stats charges (the
// character_progression resetCostCurrency / resetCostAmount).
type ResetStatsCost struct {
	Currency string `json:"currency"`
	Amount   int    `json:"amount"`
}

// CharactersResponse is the GET /player/characters data.
type CharactersResponse struct {
	Characters     []PlayerCharacter `json:"characters"`
	ResetStatsCost ResetStatsCost    `json:"resetStatsCost"`
	// BaseStats has every configured character (owned or not), by characterId.
	BaseStats map[string]BaseStats `json:"baseStats"`
	// StatMultipliers is what one allocated point adds in battle.
	StatMultipliers battlestats.Multipliers `json:"statMultipliers"`
}

// ResetStatsPayload is the request body for resetting stat points.
type ResetStatsPayload struct {
	CharacterID string `json:"characterId"`
}

// game_settings keys; must match migrations/006 and the admin config routes.
const (
	SettingsKeyProgression = "character_progression"
	SettingsKeyLevels      = "character_levels"
)

// statMultipliers keys: how much one allocated point adds in battle.
const (
	StatHP            = "hp"             // +N max HP
	StatDamage        = "damage"         // +N weapon damage
	StatMobility      = "mobility"       // +N max move energy
	StatDefense       = "defense"        // +N defense
	StatSkillPower    = "skill_power"    // +N% skill damage and healing
	StatTerrainDamage = "terrain_damage" // +N% crater radius
)

// DefaultStatMultipliers fill keys missing from (or zero in) the configured
// statMultipliers; they match the migrations/006 seed.
var DefaultStatMultipliers = map[string]int{
	StatHP:            50,
	StatDamage:        5,
	StatMobility:      3,
	StatDefense:       5,
	StatSkillPower:    5,
	StatTerrainDamage: 3,
}

// StatMultiplier returns the configured per-point value for stat, or its default.
func (c ProgressionConfig) StatMultiplier(stat string) int {
	if v := c.StatMultipliers[stat]; v > 0 {
		return v
	}
	return DefaultStatMultipliers[stat]
}

// Multipliers returns the per-point battle values, defaults filled in.
func (c ProgressionConfig) Multipliers() battlestats.Multipliers {
	return battlestats.Multipliers{
		HP:            c.StatMultiplier(StatHP),
		Damage:        c.StatMultiplier(StatDamage),
		Mobility:      c.StatMultiplier(StatMobility),
		Defense:       c.StatMultiplier(StatDefense),
		SkillPower:    c.StatMultiplier(StatSkillPower),
		TerrainDamage: c.StatMultiplier(StatTerrainDamage),
	}
}

// ProgressionConfig holds server-side rules for character progression.
type ProgressionConfig struct {
	PointsPerLevel    int            `json:"pointsPerLevel"`
	ResetCostCurrency string         `json:"resetCostCurrency"`
	ResetCostAmount   int            `json:"resetCostAmount"`
	StatMultipliers   map[string]int `json:"statMultipliers"`
}

// ResetStatsCost returns the configured reset fee.
func (c ProgressionConfig) ResetStatsCost() ResetStatsCost {
	return ResetStatsCost{Currency: c.ResetCostCurrency, Amount: c.ResetCostAmount}
}

// LevelEntry maps an XP threshold to a level.
type LevelEntry struct {
	Level       int `json:"level"`
	ExpRequired int `json:"expRequired"`
}

// CharacterLevelsConfig is the full level curve loaded from game_settings.
type CharacterLevelsConfig struct {
	Levels []LevelEntry `json:"levels"`
}

// LoadProgressionConfig unmarshals a JSON string from game_settings with safe defaults.
func LoadProgressionConfig(value string) ProgressionConfig {
	cfg := ProgressionConfig{
		PointsPerLevel:    3,
		ResetCostCurrency: "gem",
		ResetCostAmount:   50,
		StatMultipliers:   map[string]int{},
	}
	if value != "" {
		_ = json.Unmarshal([]byte(value), &cfg)
	}
	if cfg.StatMultipliers == nil {
		cfg.StatMultipliers = map[string]int{}
	}
	return cfg
}

// LoadLevelsConfig unmarshals a JSON string from game_settings.
func LoadLevelsConfig(value string) CharacterLevelsConfig {
	var cfg CharacterLevelsConfig
	if value != "" {
		_ = json.Unmarshal([]byte(value), &cfg)
	}
	return cfg
}

// CalcLevelFromExp iterates the level curve and returns the current level,
// the EXP required for the next level (nil if max), and whether the character
// is at max level.
func CalcLevelFromExp(exp int, levels []LevelEntry) (level int, nextLevelExp *int, isMaxLevel bool) {
	level = 1
	for _, entry := range levels {
		if exp >= entry.ExpRequired {
			level = entry.Level
		}
	}
	nextLevelExp, isMaxLevel = NextLevelExp(level, levels)
	return level, nextLevelExp, isMaxLevel
}

// NextLevelExp returns the EXP required to reach level+1 (nil if level is max).
func NextLevelExp(level int, levels []LevelEntry) (nextLevelExp *int, isMaxLevel bool) {
	for _, entry := range levels {
		if entry.Level == level+1 {
			next := entry.ExpRequired
			return &next, false
		}
	}
	return nil, true
}
