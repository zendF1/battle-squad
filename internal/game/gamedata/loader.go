package gamedata

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"

	"github.com/jackc/pgx/v5"
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
	// Display only (admin seed → config_characters; GET /gamedata reads the table): the
	// Vietnamese role label and play style, the gun art kind, the list order.
	RoleName  string `yaml:"roleName,omitempty"`
	Playstyle string `yaml:"playstyle,omitempty"`
	GunKind   string `yaml:"gunKind,omitempty"`
	SortOrder int    `yaml:"sortOrder,omitempty"`
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
	Description      string  `yaml:"description,omitempty"` // the old fixed description (GET /gamedata sends it when there is no template)
	// DescriptionTemplate is the description with {placeholders} for the rank numbers
	// (GET /gamedata skills[].ranks).
	DescriptionTemplate string `yaml:"descriptionTemplate,omitempty"`
	// Params is the effect type's tuning (config_skills.params): a missing or invalid number
	// keeps its default (RainTuningOf, ...).
	Params map[string]float64 `yaml:"params,omitempty"`
}

type ItemConfig struct {
	ItemID         string  `yaml:"itemId"`
	Name           string  `yaml:"name"`
	Type           string  `yaml:"type"`
	TargetType     string  `yaml:"targetType"`
	Value          float64 `yaml:"value"`
	MaxUsePerMatch int     `yaml:"maxUsePerMatch"`
	Cooldown       int     `yaml:"cooldown"`
	DisplayName    string  `yaml:"displayName,omitempty"` // the Vietnamese name (admin seed; GET /gamedata)
	SortOrder      int     `yaml:"sortOrder,omitempty"`
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
	// Theme is how the client draws the map (sky, soft and hard material, bottom); see
	// MapThemes. "" is DefaultMapTheme.
	Theme       string `yaml:"theme,omitempty"`
	Description string `yaml:"description,omitempty"`

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

// PhysicsConfig holds the scalar game_settings rows (ScalarSettings): the shell flight
// (physics.*), the turn and idle times (match.*), walking (move.*), falls (fall.*) and the
// defense constant (combat.defense_k).
type PhysicsConfig struct {
	Gravity                   float64
	ProjectileSpeedMultiplier float64
	WindScale                 float64
	PlayerHitRadius           float64
	TimeStep                  float64
	PathRecordStep            float64
	MaxFlightSeconds          float64
	TurnTimeSeconds           int     // match.turn_time_seconds: a turn's length
	IdleTimeoutMinutes        int     // match.idle_timeout_minutes: a match with no activity this long ends
	MoveStepPixels            float64 // move.step_pixels: a walk of this many px …
	MoveEnergyCostPer2px      float64 // move.energy_cost_per_2px: … costs this much move energy
	FallDamageThreshold       float64 // fall.damage_threshold: falls up to this (px) deal no damage
	FallDamagePerPixel        float64 // fall.damage_per_pixel: HP per px beyond it (no defense)
	// DefenseK is K in damage taken = raw × K / (K + defense)
	// (game_settings combat.defense_k).
	DefenseK float64
}

// PxPerEnergy is how far one move energy walks: move.step_pixels / move.energy_cost_per_2px.
func (p *PhysicsConfig) PxPerEnergy() float64 { return p.MoveStepPixels / p.MoveEnergyCostPer2px }

// EnergyPerPx is what one px of walking costs: move.energy_cost_per_2px / move.step_pixels.
func (p *PhysicsConfig) EnergyPerPx() float64 { return p.MoveEnergyCostPer2px / p.MoveStepPixels }

// DefaultPhysics is the physics used when a game_settings row is missing. The shell physics
// (playtest round 3, migration 032_faster_shells): speed power × 9.6 px/s (was 6), gravity 445
// px/s² (was 200), wind W × 67 × influence / weight px/s² (was 30), at most 4.5 s of flight
// (was 6). A turn lasts 20 s; 2 px of walking cost 1 move energy; falls up to 150 px are free,
// then 3.75 HP per px (the values the match played with while the match.*, move.* and fall.*
// rows were not read; migration 041 corrects the rows to them).
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
		FallDamageThreshold:       150.0,
		FallDamagePerPixel:        3.75,
		DefenseK:                  DefaultDefenseK,
	}
}

// DefaultMoveEnergy is the base move energy per turn of a character missing from the game data
// or configured with mobility <= 0 (rules.movement.defaultMoveEnergy's default).
const DefaultMoveEnergy = 100

// SettingDefenseK is the game_settings key of PhysicsConfig.DefenseK.
const SettingDefenseK = "combat.defense_k"

// DefaultDefenseK is used when combat.defense_k is absent or not positive:
// defense 50 takes 600/650 ≈ 92% of the raw damage, defense 600 half of it.
const DefaultDefenseK = 600.0

// ScalarSetting is a game_settings row holding one number: its admin label, help and valid
// range (inclusive). Whole: it must be a whole number.
type ScalarSetting struct {
	Key, Label, Desc string
	Min, Max         float64
	Whole            bool
}

// ScalarSettings are the one-number game_settings rows the game reads, in admin order.
var ScalarSettings = []ScalarSetting{
	{"physics.gravity", "Trọng lực (px/s²)", "Lực hấp dẫn kéo đạn xuống", 0.1, 100000, false},
	{"physics.projectile_speed_multiplier", "Tốc độ đạn mỗi điểm lực (px/s)", "Tốc độ = lực × giá trị này", 0.01, 1000, false},
	{"physics.wind_scale", "Hệ số gió (px/s²)", "Gió đẩy = hướng × sức gió × giá trị này × ảnh hưởng gió / trọng lượng đạn", 0, 100000, false},
	{"physics.player_hit_radius", "Bán kính va chạm (px)", "Đạn trúng địch trong chừng này px quanh tâm thân", 0, 1000, false},
	{"physics.time_step", "Bước mô phỏng (giây)", "", 0.001, 0.5, false},
	{"physics.path_record_step", "Bước ghi đường bay (giây)", "Khoảng ghi điểm đường bay gửi client", 0.001, 5, false},
	{"physics.max_flight_seconds", "Thời gian bay tối đa (giây)", "", 0.1, 60, false},
	{SettingDefenseK, "Hằng số phòng thủ K", "Sát thương nhận = sát thương × K / (K + phòng thủ)", 1, 10000000, false},
	{"match.turn_time_seconds", "Thời gian mỗi lượt (giây)", "Hết giờ thì tự kết thúc lượt", 1, 600, true},
	{"match.idle_timeout_minutes", "Hủy trận khi không hoạt động (phút)", "", 1, 1440, true},
	{"move.step_pixels", "Bước đi (px)", "Đi chừng này px tốn move.energy_cost_per_2px năng lượng", 0.01, 1000, false},
	{"move.energy_cost_per_2px", "Năng lượng mỗi bước đi", "Năng lượng tốn cho mỗi move.step_pixels px đi được", 0.01, 1000, false},
	{"fall.damage_threshold", "Ngưỡng rơi (px)", "Rơi tới chừng này px không mất máu", 0, 100000, false},
	{"fall.damage_per_pixel", "Sát thương rơi mỗi px", "Máu mất mỗi px rơi vượt ngưỡng (bỏ qua phòng thủ)", 0, 10000, false},
	{"equipment.merge_rate_per_item", "Tỉ lệ ghép mỗi viên (%)", "4 viên = 4 × giá trị này", 0, 100, false},
	{"equipment.milestone_6_bonus", "Bonus mốc +6", "Cộng vào hệ số chỉ số khi đạt +6 (0.1 = 10%)", 0, 100, false},
	{"equipment.milestone_10_bonus", "Bonus mốc +10", "", 0, 100, false},
	{"equipment.milestone_14_bonus", "Bonus mốc +14", "", 0, 100, false},
	{"equipment.milestone_16_bonus", "Bonus mốc +16", "", 0, 100, false},
	{"equipment.sell_rate", "Tỉ lệ hoàn tiền khi bán", "Nhận floor(giá mua × tỉ lệ), trong (0, 1]", 0.0001, 1, false},
	{"equipment.weapon_upgrade_cost_multiplier", "Hệ số đá cường hóa vũ khí", "Vũ khí cần upgrade_cost × hệ số này (>= 1)", 1, 1000, false},
}

// ScalarSettingOf returns the registered scalar row key.
func ScalarSettingOf(key string) (ScalarSetting, bool) {
	for _, s := range ScalarSettings {
		if s.Key == key {
			return s, true
		}
	}
	return ScalarSetting{}, false
}

// Check validates a value for the row.
func (s ScalarSetting) Check(text string) (float64, error) {
	x, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number", s.Label, text)
	}
	if math.IsNaN(x) || math.IsInf(x, 0) || x < s.Min || x > s.Max {
		return 0, fmt.Errorf("%s: %v is outside %v..%v", s.Label, x, s.Min, s.Max)
	}
	if s.Whole && x != math.Trunc(x) {
		return 0, fmt.Errorf("%s: %v is not a whole number", s.Label, x)
	}
	return x, nil
}

// LoadYAML reads the game data from the YAML files in configDir (the fallback when the DB
// cannot be read, and the admin seed's source): the configs with the default physics, rules,
// status effects and rank bands.
func LoadYAML(configDir string) (*Snapshot, error) {
	gData := &GameData{
		Characters: make(map[string]CharacterConfig),
		Weapons:    make(map[string]WeaponConfig),
		Skills:     make(map[string]SkillConfig),
		Items:      make(map[string]ItemConfig),
		Maps:       make(map[string]MapConfig),
	}

	var chars []CharacterConfig
	if err := loadYAMLFile(filepath.Join(configDir, "characters.yaml"), &chars); err != nil {
		return nil, err
	}
	for _, c := range chars {
		gData.Characters[c.CharacterID] = c
	}
	var weapons []WeaponConfig
	if err := loadYAMLFile(filepath.Join(configDir, "weapons.yaml"), &weapons); err != nil {
		return nil, err
	}
	for _, w := range weapons {
		gData.Weapons[w.WeaponID] = w
	}
	var skills []SkillConfig
	if err := loadYAMLFile(filepath.Join(configDir, "skills.yaml"), &skills); err != nil {
		return nil, err
	}
	for _, s := range skills {
		gData.Skills[s.SkillID] = s
	}
	var items []ItemConfig
	if err := loadYAMLFile(filepath.Join(configDir, "items.yaml"), &items); err != nil {
		return nil, err
	}
	for _, i := range items {
		gData.Items[i.ItemID] = i
	}

	// Maps and, when bricks.yaml exists, the brick types their tiles use (the YAML mirror of
	// config_brick_types; without it every brick is a destructible square)
	var maps []MapConfig
	if err := loadYAMLFile(filepath.Join(configDir, "maps.yaml"), &maps); err != nil {
		return nil, err
	}
	for _, m := range maps {
		gData.Maps[m.MapID] = m
	}
	bricks := map[int]BrickTypeConfig{}
	bricksPath := filepath.Join(configDir, "bricks.yaml")
	if _, err := os.Stat(bricksPath); err == nil {
		var list []BrickTypeConfig
		if err := loadYAMLFile(bricksPath, &list); err != nil {
			return nil, err
		}
		for _, b := range list {
			bricks[b.BrickTypeID] = b
		}
	}

	if err := gData.validate(); err != nil {
		return nil, fmt.Errorf("game data validation failed: %w", err)
	}
	s := NewSnapshot(gData)
	s.Bricks = bricks
	s.Source = "yaml"
	return s, nil
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

// LoadFromDB reads every config table and game_settings row in one read-only REPEATABLE READ
// transaction, so the snapshot is consistent even while an admin saves. It fails when a
// table cannot be read, a character, weapon, skill or item table is empty, or a character
// refers to a missing weapon or skill (an invalid map is dropped and logged instead).
func LoadFromDB(ctx context.Context, db *database.PostgresDB) (*Snapshot, error) {
	tx, err := db.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, fmt.Errorf("begin game data read: %w", err)
	}
	defer tx.Rollback(ctx)

	s := &Snapshot{Source: "db", LoadedAt: time.Now()}
	gData := &GameData{}
	gData.fill()
	s.Data = gData

	each := func(table, query string, scan func(rows pgx.Rows) error) error {
		rows, err := tx.Query(ctx, query)
		if err != nil {
			return fmt.Errorf("query %s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return fmt.Errorf("scan %s: %w", table, err)
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read %s: %w", table, err)
		}
		return nil
	}

	if err := tx.QueryRow(ctx, `SELECT revision FROM gamedata_revision WHERE id = 1`).Scan(&s.Revision); err != nil {
		return nil, fmt.Errorf("gamedata_revision: %w", err)
	}

	s.Settings = map[string]string{}
	if err := each("game_settings", `SELECT key, value FROM game_settings`, func(rows pgx.Rows) error {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		s.Settings[k] = v
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_characters", `SELECT character_id, name, role, hp, damage, mobility, defense, skill_power,
		terrain_damage, difficulty, weapon_id, skill_id, role_name, playstyle, gun_kind, sort_order FROM config_characters`,
		func(rows pgx.Rows) error {
			var c CharacterConfig
			if err := rows.Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage, &c.Mobility, &c.Defense, &c.SkillPower,
				&c.TerrainDamage, &c.Difficulty, &c.WeaponID, &c.SkillID, &c.RoleName, &c.Playstyle, &c.GunKind, &c.SortOrder); err != nil {
				return err
			}
			gData.Characters[c.CharacterID] = c
			return nil
		}); err != nil {
		return nil, err
	}

	if err := each("config_weapons", `SELECT weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight,
		wind_influence, multi_hit FROM config_weapons`, func(rows pgx.Rows) error {
		var w WeaponConfig
		if err := rows.Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius, &w.TerrainDamage, &w.ProjectileWeight,
			&w.WindInfluence, &w.MultiHit); err != nil {
			return err
		}
		gData.Weapons[w.WeaponID] = w
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_skills", `SELECT skill_id, character_id, name, cooldown_turn, effect_type, projectile_count,
		COALESCE(status_effect_id, ''), damage_multiplier, COALESCE(description, ''), description_template, params
		FROM config_skills`, func(rows pgx.Rows) error {
		var sk SkillConfig
		var params []byte
		if err := rows.Scan(&sk.SkillID, &sk.CharacterID, &sk.Name, &sk.CooldownTurn, &sk.EffectType, &sk.ProjectileCount,
			&sk.StatusEffectID, &sk.DamageMultiplier, &sk.Description, &sk.DescriptionTemplate, &params); err != nil {
			return err
		}
		sk.Params = parseParams(sk.SkillID, params)
		gData.Skills[sk.SkillID] = sk
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_items", `SELECT item_id, name, type, target_type, value, max_use_per_match, cooldown,
		display_name, sort_order FROM config_items`, func(rows pgx.Rows) error {
		var it ItemConfig
		if err := rows.Scan(&it.ItemID, &it.Name, &it.Type, &it.TargetType, &it.Value, &it.MaxUsePerMatch, &it.Cooldown,
			&it.DisplayName, &it.SortOrder); err != nil {
			return err
		}
		gData.Items[it.ItemID] = it
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_maps", `SELECT map_id, name, grid_width, grid_height, cell_size,
		default_wind_power_range, tiles, spawn_points,
		width, height, terrain_layers, min_rank_tier, theme, description
		FROM config_maps`, func(rows pgx.Rows) error {
		var m MapConfig
		var windRangeJSON, tilesJSON, spawnJSON, terrainJSON []byte
		if err := rows.Scan(&m.MapID, &m.Name, &m.GridWidth, &m.GridHeight, &m.CellSize,
			&windRangeJSON, &tilesJSON, &spawnJSON,
			&m.Width, &m.Height, &terrainJSON, &m.MinRankTier, &m.Theme, &m.Description); err != nil {
			return err
		}
		if err := json.Unmarshal(windRangeJSON, &m.DefaultWindPowerRange); err != nil {
			return fmt.Errorf("wind_power_range of map %s: %w", m.MapID, err)
		}
		if err := json.Unmarshal(tilesJSON, &m.Tiles); err != nil {
			m.Tiles = nil // an empty array is fine
		}
		if err := json.Unmarshal(spawnJSON, &m.SpawnPoints); err != nil {
			return fmt.Errorf("spawn_points of map %s: %w", m.MapID, err)
		}
		if terrainJSON != nil {
			_ = json.Unmarshal(terrainJSON, &m.TerrainLayers)
		}
		gData.Maps[m.MapID] = m
		return nil
	}); err != nil {
		return nil, err
	}

	s.Bricks = map[int]BrickTypeConfig{}
	if err := each("config_brick_types", `SELECT brick_type_id, name, image_id, destructible, border, color FROM config_brick_types`,
		func(rows pgx.Rows) error {
			var bt BrickTypeConfig
			var borderJSON []byte
			if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible, &borderJSON, &bt.Color); err != nil {
				return err
			}
			if borderJSON != nil {
				_ = json.Unmarshal(borderJSON, &bt.Border)
			}
			s.Bricks[bt.BrickTypeID] = bt
			return nil
		}); err != nil {
		return nil, err
	}

	var status []StatusEffectConfig
	if err := each("config_status_effects", `SELECT effect_id, name, marker, banner, sort_order, bad, skips_turn, blocks_move
		FROM config_status_effects`, func(rows pgx.Rows) error {
		var e StatusEffectConfig
		if err := rows.Scan(&e.EffectID, &e.Name, &e.Marker, &e.Banner, &e.SortOrder, &e.Bad, &e.SkipsTurn, &e.BlocksMove); err != nil {
			return err
		}
		status = append(status, e)
		return nil
	}); err != nil {
		return nil, err
	}
	s.Status = withDefaultStatus(status)

	tiers := map[string]RankTierConfig{}
	if err := each("config_rank_tiers", `SELECT tier, name, sort_order, min_rating, divisions, division_size FROM config_rank_tiers`,
		func(rows pgx.Rows) error {
			var t RankTierConfig
			if err := rows.Scan(&t.Tier, &t.Name, &t.SortOrder, &t.MinRating, &t.Divisions, &t.DivisionSize); err != nil {
				return err
			}
			tiers[t.Tier] = t
			return nil
		}); err != nil {
		return nil, err
	}
	s.RankTiers = rankTiersWithDefaults(tiers)

	if err := each("config_equipment_tiers", `SELECT tier, name, kind, id_suffix, sort_order FROM config_equipment_tiers
		ORDER BY sort_order, tier`, func(rows pgx.Rows) error {
		var t EquipmentTierConfig
		if err := rows.Scan(&t.Tier, &t.Name, &t.Kind, &t.IDSuffix, &t.SortOrder); err != nil {
			return err
		}
		s.EquipTiers = append(s.EquipTiers, t)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_game_modes", `SELECT mode_id, kind, name, description, queue, bot, sort_order FROM config_game_modes
		ORDER BY sort_order, mode_id`, func(rows pgx.Rows) error {
		var m GameModeConfig
		if err := rows.Scan(&m.ModeID, &m.Kind, &m.Name, &m.Description, &m.Queue, &m.Bot, &m.SortOrder); err != nil {
			return err
		}
		s.Modes = append(s.Modes, m)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_tips", `SELECT text FROM config_tips WHERE text <> '' ORDER BY sort_order, tip_id`, func(rows pgx.Rows) error {
		var t string
		if err := rows.Scan(&t); err != nil {
			return err
		}
		s.Tips = append(s.Tips, t)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := each("config_gun_kinds", `SELECT gun_kind, name, sort_order FROM config_gun_kinds ORDER BY sort_order, gun_kind`,
		func(rows pgx.Rows) error {
			var g GunKindConfig
			if err := rows.Scan(&g.ID, &g.Name, &g.SortOrder); err != nil {
				return err
			}
			s.GunKinds = append(s.GunKinds, g)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := each("config_equipment_slots", `SELECT slot, name, sort_order FROM config_equipment_slots ORDER BY sort_order, slot`,
		func(rows pgx.Rows) error {
			var e EquipmentSlotConfig
			if err := rows.Scan(&e.ID, &e.Name, &e.SortOrder); err != nil {
				return err
			}
			s.Slots = append(s.Slots, e)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := each("config_item_type_words", `SELECT word, name, gun_kind, sort_order FROM config_item_type_words ORDER BY sort_order, word`,
		func(rows pgx.Rows) error {
			var w TypeWordConfig
			if err := rows.Scan(&w.Word, &w.Name, &w.GunKind, &w.SortOrder); err != nil {
				return err
			}
			s.TypeWords = append(s.TypeWords, w)
			return nil
		}); err != nil {
		return nil, err
	}

	switch {
	case len(gData.Characters) == 0:
		return nil, fmt.Errorf("config_characters table is empty")
	case len(gData.Weapons) == 0:
		return nil, fmt.Errorf("config_weapons table is empty")
	case len(gData.Skills) == 0:
		return nil, fmt.Errorf("config_skills table is empty")
	case len(gData.Items) == 0:
		return nil, fmt.Errorf("config_items table is empty")
	}
	if err := gData.validate(); err != nil {
		return nil, fmt.Errorf("game data validation failed: %w", err)
	}

	s.Physics = PhysicsFromSettings(s.Settings)
	var ruleErrs []error
	s.Rules, ruleErrs = RulesFromSettings(s.Settings)
	for _, err := range ruleErrs {
		observability.Log.Warn().Err(err).Msg("game_settings rule ignored, using its default")
	}
	s.index()
	if ranked.ValidateBands(bandsFromTiers(s.RankTiers)) != nil {
		observability.Log.Warn().Msg("config_rank_tiers bands are not valid, using the default bands")
	}
	return s, nil
}

func bandsFromTiers(tiers []RankTierConfig) []ranked.Band {
	out := make([]ranked.Band, 0, len(tiers))
	for _, t := range tiers {
		out = append(out, ranked.Band{Tier: t.Tier, MinRating: t.MinRating, Divisions: t.Divisions, DivisionSize: t.DivisionSize})
	}
	return out
}

// parseParams reads config_skills.params (a JSON object name → number); anything else is
// logged and ignored (the defaults apply).
func parseParams(skillID string, raw []byte) map[string]float64 {
	if len(raw) == 0 {
		return nil
	}
	out := map[string]float64{}
	if err := json.Unmarshal(raw, &out); err != nil {
		observability.Log.Warn().Err(err).Str("skillId", skillID).Msg("config_skills.params is not an object of numbers, using the defaults")
		return nil
	}
	return out
}

// PhysicsFromSettings maps the scalar game_settings rows (key → value) to a PhysicsConfig; a
// row that is missing or does not parse (or is not positive where it must be) keeps its
// DefaultPhysics() value. A <group>.<name> row is read first, then a bare <name> row.
func PhysicsFromSettings(settings map[string]string) *PhysicsConfig {
	p := DefaultPhysics()
	float := func(dst *float64, ok func(float64) bool, keys ...string) {
		for _, k := range keys {
			if v, found := settings[k]; found {
				if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && ok(f) {
					*dst = f
				}
				return
			}
		}
	}
	whole := func(dst *int, keys ...string) {
		for _, k := range keys {
			if v, found := settings[k]; found {
				if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i > 0 {
					*dst = i
				}
				return
			}
		}
	}
	any := func(float64) bool { return true }
	positive := func(f float64) bool { return f > 0 }
	nonNegative := func(f float64) bool { return f >= 0 }

	float(&p.Gravity, any, "physics.gravity", "gravity")
	float(&p.ProjectileSpeedMultiplier, any, "physics.projectile_speed_multiplier", "projectile_speed_multiplier")
	float(&p.WindScale, any, "physics.wind_scale", "wind_scale")
	float(&p.PlayerHitRadius, any, "physics.player_hit_radius", "player_hit_radius")
	float(&p.TimeStep, positive, "physics.time_step", "time_step")
	float(&p.PathRecordStep, positive, "physics.path_record_step", "path_record_step")
	float(&p.MaxFlightSeconds, any, "physics.max_flight_seconds", "max_flight_seconds")
	whole(&p.TurnTimeSeconds, "match.turn_time_seconds", "turn_time_seconds")
	whole(&p.IdleTimeoutMinutes, "match.idle_timeout_minutes", "idle_timeout_minutes")
	float(&p.MoveStepPixels, positive, "move.step_pixels", "move_step_pixels")
	float(&p.MoveEnergyCostPer2px, positive, "move.energy_cost_per_2px", "move_energy_cost_per_2px")
	float(&p.FallDamageThreshold, nonNegative, "fall.damage_threshold", "fall_damage_threshold")
	float(&p.FallDamagePerPixel, nonNegative, "fall.damage_per_pixel", "fall_damage_per_pixel")
	float(&p.DefenseK, positive, SettingDefenseK)
	return p
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
