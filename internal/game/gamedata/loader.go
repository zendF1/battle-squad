package gamedata

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"

	"gopkg.in/yaml.v3"
)

type CharacterConfig struct {
	CharacterID   string `yaml:"characterId"`
	Name          string `yaml:"name"`
	Role          string `yaml:"role"`
	HP            int    `yaml:"hp"`
	Damage        int    `yaml:"damage"`
	Mobility      int    `yaml:"mobility"` // base move energy per turn, see BaseMoveEnergy
	Defense       int    `yaml:"defense"`
	SkillPower    int    `yaml:"skillPower"`
	TerrainDamage int    `yaml:"terrainDamage"`
	Difficulty    int    `yaml:"difficulty"`
	WeaponID      string `yaml:"weaponId"`
	SkillID       string `yaml:"skillId"`
}

type WeaponConfig struct {
	WeaponID         string  `yaml:"weaponId"`
	Name             string  `yaml:"name"`
	Damage           int     `yaml:"damage"`
	ExplosionRadius  int     `yaml:"explosionRadius"`
	TerrainDamage    int     `yaml:"terrainDamage"`
	ProjectileWeight float64 `yaml:"projectileWeight"`
	WindInfluence    float64 `yaml:"windInfluence"`
	MultiHit         int     `yaml:"multiHit"`
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
	Description      string  `yaml:"description,omitempty"` // shown to players (admin seed); the game server does not read it
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
	YRange   []int  `yaml:"yRange"`
}

type MapConfig struct {
	MapID                 string       `yaml:"mapId"`
	Name                  string       `yaml:"name"`
	GridWidth             int          `yaml:"gridWidth"`
	GridHeight            int          `yaml:"gridHeight"`
	CellSize              int          `yaml:"cellSize"`
	DefaultWindPowerRange []float64    `yaml:"defaultWindPowerRange"`
	Tiles                 [][]int      `yaml:"tiles"` // 0 = air, >0 = brick_type_id
	SpawnPoints           []SpawnPoint `yaml:"spawnPoints"`
	MinRankTier           string       `yaml:"minRankTier"`

	// Legacy fields (for backward compatibility during transition)
	Width         int            `yaml:"width,omitempty"`
	Height        int            `yaml:"height,omitempty"`
	TerrainLayers []TerrainLayer `yaml:"terrainLayers,omitempty"`
}

type GameData struct {
	Characters map[string]CharacterConfig
	Weapons    map[string]WeaponConfig
	Skills     map[string]SkillConfig
	Items      map[string]ItemConfig
	Maps       map[string]MapConfig
}

type BorderPoint struct {
	X float64 `json:"x" yaml:"x"`
	Y float64 `json:"y" yaml:"y"`
}

type BrickBorder struct {
	Top    []BorderPoint `json:"top" yaml:"top"`
	Right  []BorderPoint `json:"right" yaml:"right"`
	Bottom []BorderPoint `json:"bottom" yaml:"bottom"`
	Left   []BorderPoint `json:"left" yaml:"left"`
}

type BrickTypeConfig struct {
	BrickTypeID  int         `yaml:"brickTypeId"`
	Name         string      `yaml:"name"`
	ImageID      string      `yaml:"imageId"`
	Destructible bool        `yaml:"destructible"`
	Border       BrickBorder `yaml:"border"`
	Color        string      `yaml:"color"`
}

// BrickTypes is loaded from config_brick_types table.
var BrickTypes map[int]BrickTypeConfig

// PhysicsConfig holds physics constants loaded from game_settings table
type PhysicsConfig struct {
	Gravity                   float64
	ProjectileSpeedMultiplier float64
	WindScale                 float64
	PlayerHitRadius           float64
	TimeStep                  float64
	PathRecordStep            float64
	MaxFlightSeconds          float64
	TurnTimeSeconds           int
	IdleTimeoutMinutes        int
	MoveStepPixels            int
	MoveEnergyCostPer2px      float64
	FallDamageThreshold       float64
	FallDamagePerPixel        float64
	// DefenseK is K in damage taken = raw × K / (K + defense)
	// (game_settings combat.defense_k).
	DefenseK float64
}

var Data *GameData
var Physics *PhysicsConfig

// DefaultPhysics is the physics used when a game_settings row is missing (and by the match when
// Physics is not loaded). The shell physics (playtest round 3, migration 032_faster_shells):
// speed power × 9.6 px/s (was 6), gravity 445 px/s² (was 200), wind W × 67 × influence /
// weight px/s² (was 30), at most 4.5 s of flight (was 6). The Godot client mirrors them
// (match/aim/trajectory_predictor.gd, aim_aid.gd).
func DefaultPhysics() *PhysicsConfig {
	return &PhysicsConfig{
		Gravity:                   445.0,
		ProjectileSpeedMultiplier: 9.6,
		WindScale:                 67.0,
		PlayerHitRadius:           24.0,
		TimeStep:                  0.02,
		PathRecordStep:            0.05,
		MaxFlightSeconds:          4.5,
		TurnTimeSeconds:           20,
		IdleTimeoutMinutes:        2,
		MoveStepPixels:            2,
		MoveEnergyCostPer2px:      1.0,
		FallDamageThreshold:       50.0,
		FallDamagePerPixel:        0.5,
		DefenseK:                  DefaultDefenseK,
	}
}

// DefaultMoveEnergy is the base move energy per turn of a character missing
// from the game data or configured with mobility <= 0.
const DefaultMoveEnergy = 100

// BaseMoveEnergy is a character's move energy per turn before stat points and
// equipment: its config mobility, or DefaultMoveEnergy.
func BaseMoveEnergy(characterID string) int {
	if Data != nil {
		if c, ok := Data.Characters[characterID]; ok && c.Mobility > 0 {
			return c.Mobility
		}
	}
	return DefaultMoveEnergy
}

// SettingDefenseK is the game_settings key of PhysicsConfig.DefenseK.
const SettingDefenseK = "combat.defense_k"

// DefaultDefenseK is used when combat.defense_k is absent or not positive:
// defense 50 takes 600/650 ≈ 92% of the raw damage, defense 600 half of it.
const DefaultDefenseK = 600.0

// DefenseK returns the loaded combat.defense_k, or DefaultDefenseK.
func DefenseK() float64 {
	if Physics != nil && Physics.DefenseK > 0 {
		return Physics.DefenseK
	}
	return DefaultDefenseK
}

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

// Load loads the game data from the config tables and falls back to the YAML
// files in configDir when that fails. The game server and the API both start
// with it so they see the same character and weapon config.
func Load(db *database.PostgresDB, configDir string) error {
	dbErr := LoadGameDataFromDB(db)
	if dbErr == nil {
		return nil
	}
	observability.Log.Warn().Err(dbErr).Msg("failed to load config from DB, falling back to YAML")
	return LoadGameData(configDir)
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

// LoadGameDataFromDB loads all game config from database tables instead of YAML files.
// Returns error if any config table is empty so caller can fall back to YAML.
func LoadGameDataFromDB(db *database.PostgresDB) error {
	ctx := context.Background()

	gData := &GameData{
		Characters: make(map[string]CharacterConfig),
		Weapons:    make(map[string]WeaponConfig),
		Skills:     make(map[string]SkillConfig),
		Items:      make(map[string]ItemConfig),
		Maps:       make(map[string]MapConfig),
	}

	// 1. Load characters
	rows, err := db.Pool.Query(ctx, `SELECT character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id FROM config_characters`)
	if err != nil {
		return fmt.Errorf("failed to query config_characters: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c CharacterConfig
		if err := rows.Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage, &c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty, &c.WeaponID, &c.SkillID); err != nil {
			return fmt.Errorf("failed to scan config_characters row: %w", err)
		}
		gData.Characters[c.CharacterID] = c
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_characters: %w", err)
	}
	if len(gData.Characters) == 0 {
		return fmt.Errorf("config_characters table is empty")
	}

	// 2. Load weapons
	rows, err = db.Pool.Query(ctx, `SELECT weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit FROM config_weapons`)
	if err != nil {
		return fmt.Errorf("failed to query config_weapons: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var w WeaponConfig
		if err := rows.Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius, &w.TerrainDamage, &w.ProjectileWeight, &w.WindInfluence, &w.MultiHit); err != nil {
			return fmt.Errorf("failed to scan config_weapons row: %w", err)
		}
		gData.Weapons[w.WeaponID] = w
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_weapons: %w", err)
	}
	if len(gData.Weapons) == 0 {
		return fmt.Errorf("config_weapons table is empty")
	}

	// 3. Load skills
	rows, err = db.Pool.Query(ctx, `SELECT skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier FROM config_skills`)
	if err != nil {
		return fmt.Errorf("failed to query config_skills: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s SkillConfig
		if err := rows.Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.CooldownTurn, &s.EffectType, &s.ProjectileCount, &s.StatusEffectID, &s.DamageMultiplier); err != nil {
			return fmt.Errorf("failed to scan config_skills row: %w", err)
		}
		gData.Skills[s.SkillID] = s
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_skills: %w", err)
	}
	if len(gData.Skills) == 0 {
		return fmt.Errorf("config_skills table is empty")
	}

	// 4. Load items
	rows, err = db.Pool.Query(ctx, `SELECT item_id, name, type, target_type, value, max_use_per_match, cooldown FROM config_items`)
	if err != nil {
		return fmt.Errorf("failed to query config_items: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var it ItemConfig
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Type, &it.TargetType, &it.Value, &it.MaxUsePerMatch, &it.Cooldown); err != nil {
			return fmt.Errorf("failed to scan config_items row: %w", err)
		}
		gData.Items[it.ItemID] = it
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_items: %w", err)
	}
	if len(gData.Items) == 0 {
		return fmt.Errorf("config_items table is empty")
	}

	// 5. Load maps (with JSONB fields)
	rows, err = db.Pool.Query(ctx, `SELECT map_id, name, grid_width, grid_height, cell_size,
		default_wind_power_range, tiles, spawn_points,
		width, height, terrain_layers, min_rank_tier
		FROM config_maps`)
	if err != nil {
		return fmt.Errorf("failed to query config_maps: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m MapConfig
		var windRangeJSON, tilesJSON, spawnJSON, terrainJSON []byte
		var legacyWidth, legacyHeight int
		if err := rows.Scan(&m.MapID, &m.Name, &m.GridWidth, &m.GridHeight, &m.CellSize,
			&windRangeJSON, &tilesJSON, &spawnJSON,
			&legacyWidth, &legacyHeight, &terrainJSON, &m.MinRankTier); err != nil {
			return fmt.Errorf("failed to scan config_maps row: %w", err)
		}
		m.Width = legacyWidth
		m.Height = legacyHeight
		if err := json.Unmarshal(windRangeJSON, &m.DefaultWindPowerRange); err != nil {
			return fmt.Errorf("failed to unmarshal wind_power_range for map %s: %w", m.MapID, err)
		}
		if err := json.Unmarshal(tilesJSON, &m.Tiles); err != nil {
			// Tiles might be empty array, that's OK
			m.Tiles = nil
		}
		if err := json.Unmarshal(spawnJSON, &m.SpawnPoints); err != nil {
			return fmt.Errorf("failed to unmarshal spawn_points for map %s: %w", m.MapID, err)
		}
		if terrainJSON != nil {
			json.Unmarshal(terrainJSON, &m.TerrainLayers)
		}
		gData.Maps[m.MapID] = m
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_maps: %w", err)
	}
	if len(gData.Maps) == 0 {
		return fmt.Errorf("config_maps table is empty")
	}

	// 6. Load brick types
	BrickTypes = make(map[int]BrickTypeConfig)
	rows, err = db.Pool.Query(ctx, `SELECT brick_type_id, name, image_id, destructible, border, color FROM config_brick_types`)
	if err != nil {
		return fmt.Errorf("failed to query config_brick_types: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bt BrickTypeConfig
		var borderJSON []byte
		if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible, &borderJSON, &bt.Color); err != nil {
			return fmt.Errorf("failed to scan config_brick_types row: %w", err)
		}
		if borderJSON != nil {
			json.Unmarshal(borderJSON, &bt.Border)
		}
		BrickTypes[bt.BrickTypeID] = bt
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating config_brick_types: %w", err)
	}

	// 7. Load physics settings from game_settings
	physics, err := loadPhysicsFromDB(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to load game_settings: %w", err)
	}

	// Validate configuration relationships
	if err := gData.validate(); err != nil {
		return fmt.Errorf("game data validation failed: %w", err)
	}

	Data = gData
	Physics = physics
	return nil
}

// loadPhysicsFromDB reads game_settings rows and maps them to PhysicsConfig.
// Uses DefaultPhysics() for a key not found in the table.
func loadPhysicsFromDB(ctx context.Context, db *database.PostgresDB) (*PhysicsConfig, error) {
	settings := make(map[string]string)

	rows, err := db.Pool.Query(ctx, `SELECT key, value FROM game_settings`)
	if err != nil {
		return nil, fmt.Errorf("failed to query game_settings: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("failed to scan game_settings row: %w", err)
		}
		settings[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating game_settings: %w", err)
	}

	p := DefaultPhysics()

	if v, ok := physicsSetting(settings, "gravity"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.Gravity = f
		}
	}
	if v, ok := physicsSetting(settings, "projectile_speed_multiplier"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.ProjectileSpeedMultiplier = f
		}
	}
	if v, ok := physicsSetting(settings, "wind_scale"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.WindScale = f
		}
	}
	if v, ok := physicsSetting(settings, "player_hit_radius"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.PlayerHitRadius = f
		}
	}
	if v, ok := physicsSetting(settings, "time_step"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.TimeStep = f
		}
	}
	if v, ok := physicsSetting(settings, "path_record_step"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.PathRecordStep = f
		}
	}
	if v, ok := physicsSetting(settings, "max_flight_seconds"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.MaxFlightSeconds = f
		}
	}
	if v, ok := settings["turn_time_seconds"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			p.TurnTimeSeconds = i
		}
	}
	if v, ok := settings["idle_timeout_minutes"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			p.IdleTimeoutMinutes = i
		}
	}
	if v, ok := settings["move_step_pixels"]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			p.MoveStepPixels = i
		}
	}
	if v, ok := settings["move_energy_cost_per_2px"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.MoveEnergyCostPer2px = f
		}
	}
	if v, ok := settings["fall_damage_threshold"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.FallDamageThreshold = f
		}
	}
	if v, ok := settings["fall_damage_per_pixel"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.FallDamagePerPixel = f
		}
	}
	if v, ok := settings[SettingDefenseK]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			p.DefenseK = f
		}
	}

	return p, nil
}

// physicsSetting is the game_settings row physics.<name> (the key the admin seed and the
// migrations use), or a bare <name> row when there is none.
func physicsSetting(settings map[string]string, name string) (string, bool) {
	if v, ok := settings["physics."+name]; ok {
		return v, true
	}
	v, ok := settings[name]
	return v, ok
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
	// An invalid map is dropped rather than failing the whole load, which would discard every
	// other DB config. Brick IDs are not checked here: NewTerrain fills unknown bricks as squares.
	for id, m := range gd.Maps {
		if err := ValidateMap(m, nil); err != nil {
			observability.Log.Error().Err(err).Str("mapId", id).Msg("skipping invalid map config")
			delete(gd.Maps, id)
		}
	}
	return nil
}
