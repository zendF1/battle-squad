package gamedata

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type CharacterConfig struct {
	CharacterID   string `yaml:"characterId"`
	Name          string `yaml:"name"`
	Role          string `yaml:"role"`
	HP            int    `yaml:"hp"`
	Damage        int    `yaml:"damage"`
	Mobility      int    `yaml:"mobility"`
	Defense       int    `yaml:"defense"`
	SkillPower    int    `yaml:"skillPower"`
	TerrainDamage int    `yaml:"terrainDamage"`
	Difficulty    int    `yaml:"difficulty"`
	WeaponID      string `yaml:"weaponId"`
	SkillID       string `yaml:"skillId"`
}

type WeaponConfig struct {
	WeaponID        string  `yaml:"weaponId"`
	Name            string  `yaml:"name"`
	Damage          int     `yaml:"damage"`
	ExplosionRadius int     `yaml:"explosionRadius"`
	TerrainDamage   int     `yaml:"terrainDamage"`
	ProjectileWeight float64 `yaml:"projectileWeight"`
	WindInfluence   float64 `yaml:"windInfluence"`
	MultiHit        int     `yaml:"multiHit"`
}

type SkillConfig struct {
	SkillID          string  `yaml:"skillId"`
	CharacterID      string  `yaml:"characterId"`
	Name             string  `yaml:"name"`
	CooldownTurn     int     `yaml:"cooldownTurn"`
	EffectType       string  `yaml:"effectType"`
	ProjectileCount  int     `yaml:"projectileCount"`
	StatusEffectID   string  `yaml:"statusEffectId,omitempty"`
	DamageMultiplier float64 `yaml:"damageMultiplier"`
}

type ItemConfig struct {
	ItemID         string  `yaml:"itemId"`
	Name           string  `yaml:"name"`
	Type           string  `yaml:"type"`
	TargetType     string  `yaml:"targetType"`
	Value          float64 `yaml:"value"`
	MaxUsePerMatch int     `yaml:"maxUsePerMatch"`
	Cooldown       int     `yaml:"cooldown"`
}

type SpawnPoint struct {
	X float64 `yaml:"x"`
	Y float64 `yaml:"y"`
}

type TerrainLayer struct {
	Type     string `yaml:"type"`
	Hardness int    `yaml:"hardness"`
}

type MapConfig struct {
	MapID                string         `yaml:"mapId"`
	Name                 string         `yaml:"name"`
	Width                int            `yaml:"width"`
	Height               int            `yaml:"height"`
	DefaultWindPowerRange []int          `yaml:"defaultWindPowerRange"`
	TerrainLayers        []TerrainLayer `yaml:"terrainLayers"`
	SpawnPoints          []SpawnPoint   `yaml:"spawnPoints"`
}

type GameData struct {
	Characters map[string]CharacterConfig
	Weapons    map[string]WeaponConfig
	Skills     map[string]SkillConfig
	Items      map[string]ItemConfig
	Maps       map[string]MapConfig
}

var Data *GameData

func LoadGameData(configDir string) error {
	gData := &GameData{
		Characters: make(map[string]CharacterConfig),
		Weapons:    make(map[string]WeaponConfig),
		Skills:     make(map[string]SkillConfig),
		Items:      make(map[string]ItemConfig),
		Maps:       make(map[string]MapConfig),
	}

	// 1. Load characters
	var chars []CharacterConfig
	if err := loadYAMLFile(filepath.Join(configDir, "characters.yaml"), &chars); err != nil {
		return err
	}
	for _, c := range chars {
		gData.Characters[c.CharacterID] = c
	}

	// 2. Load weapons
	var weapons []WeaponConfig
	if err := loadYAMLFile(filepath.Join(configDir, "weapons.yaml"), &weapons); err != nil {
		return err
	}
	for _, w := range weapons {
		gData.Weapons[w.WeaponID] = w
	}

	// 3. Load skills
	var skills []SkillConfig
	if err := loadYAMLFile(filepath.Join(configDir, "skills.yaml"), &skills); err != nil {
		return err
	}
	for _, s := range skills {
		gData.Skills[s.SkillID] = s
	}

	// 4. Load items
	var items []ItemConfig
	if err := loadYAMLFile(filepath.Join(configDir, "items.yaml"), &items); err != nil {
		return err
	}
	for _, i := range items {
		gData.Items[i.ItemID] = i
	}

	// 5. Load maps
	var maps []MapConfig
	if err := loadYAMLFile(filepath.Join(configDir, "maps.yaml"), &maps); err != nil {
		return err
	}
	for _, m := range maps {
		gData.Maps[m.MapID] = m
	}

	// Validate configuration relationships
	if err := gData.validate(); err != nil {
		return fmt.Errorf("game data validation failed: %w", err)
	}

	Data = gData
	return nil
}

func loadYAMLFile(path string, target interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read game config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, target); err != nil {
		return fmt.Errorf("failed to parse game config file %s: %w", path, err)
	}

	return nil
}

func (gd *GameData) validate() error {
	// Validate character config weapon and skill mappings
	for _, char := range gd.Characters {
		if _, exists := gd.Weapons[char.WeaponID]; !exists {
			return fmt.Errorf("character %s refers to non-existent weapon: %s", char.CharacterID, char.WeaponID)
		}
		if _, exists := gd.Skills[char.SkillID]; !exists {
			return fmt.Errorf("character %s refers to non-existent skill: %s", char.CharacterID, char.SkillID)
		}
	}
	return nil
}
