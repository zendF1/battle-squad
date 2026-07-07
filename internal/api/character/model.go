package character

import (
	"encoding/json"
	"time"
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
}

// AllocateStatsPayload is the request body for allocating stat points.
type AllocateStatsPayload struct {
	CharacterID  string `json:"characterId"`
	HP           int    `json:"hp"`
	Damage       int    `json:"damage"`
	Mobility     int    `json:"mobility"`
	Defense      int    `json:"defense"`
	SkillPower   int    `json:"skillPower"`
	TerrainDamage int   `json:"terrainDamage"`
}

// ResetStatsPayload is the request body for resetting stat points.
type ResetStatsPayload struct {
	CharacterID string `json:"characterId"`
}

// ProgressionConfig holds server-side rules for character progression.
type ProgressionConfig struct {
	PointsPerLevel     int            `json:"pointsPerLevel"`
	ResetCostCurrency  string         `json:"resetCostCurrency"`
	ResetCostAmount    int            `json:"resetCostAmount"`
	StatMultipliers    map[string]int `json:"statMultipliers"`
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

	// Determine nextLevelExp
	isMaxLevel = true
	for _, entry := range levels {
		if entry.Level == level+1 {
			next := entry.ExpRequired
			nextLevelExp = &next
			isMaxLevel = false
			break
		}
	}
	return level, nextLevelExp, isMaxLevel
}
