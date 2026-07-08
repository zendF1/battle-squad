package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type settingDef struct {
	Key         string
	Value       string
	ValueType   string
	Description string
	Category    string
}

var defaultSettings = []settingDef{
	{"physics.gravity", "200", "number", "Lực hấp dẫn kéo đạn xuống. Tăng = đạn rơi nhanh, tầm bắn ngắn", "physics"},
	{"physics.projectile_speed_multiplier", "6.0", "number", "Hệ số tốc độ đạn = power × giá trị này. Tăng = đạn bay xa hơn", "physics"},
	{"physics.wind_scale", "30.0", "number", "Hệ số ảnh hưởng gió lên đạn. Tăng = gió đẩy đạn lệch nhiều hơn", "physics"},
	{"physics.player_hit_radius", "24.0", "number", "Bán kính va chạm player (pixels). Tăng = dễ bắn trúng hơn", "physics"},
	{"physics.time_step", "0.02", "number", "Bước thời gian mô phỏng vật lý (giây). Giảm = chính xác hơn, tốn CPU hơn", "physics"},
	{"physics.path_record_step", "0.05", "number", "Khoảng thời gian ghi path đạn cho animation client", "physics"},
	{"physics.max_flight_seconds", "6.0", "number", "Thời gian bay tối đa của đạn trước khi biến mất", "physics"},
	{"match.turn_time_seconds", "20", "number", "Thời gian mỗi lượt (giây). Hết = tự động kết thúc lượt", "match"},
	{"match.idle_timeout_minutes", "2", "number", "Phút không hoạt động trước khi match bị hủy", "match"},
	{"move.step_pixels", "10", "number", "Số pixel di chuyển mỗi tick khi giữ nút move", "movement"},
	{"move.energy_cost_per_2px", "0.5", "number", "Năng lượng tiêu hao mỗi 2 pixel di chuyển", "movement"},
	{"fall.damage_threshold", "30", "number", "Khoảng rơi tối thiểu (pixels) trước khi nhận fall damage", "physics"},
	{"fall.damage_per_pixel", "0.5", "number", "Damage mỗi pixel rơi vượt ngưỡng", "physics"},
}

// SeedAll seeds both game_settings and config tables from YAML files.
func SeedAll(ctx context.Context, db *database.PostgresDB, configDir string) error {
	if err := SeedSettings(ctx, db); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	if err := SeedConfigFromYAML(ctx, db, configDir); err != nil {
		return fmt.Errorf("seed config from YAML: %w", err)
	}
	return nil
}

// SeedSettings inserts default physics/match/movement settings into game_settings.
// Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedSettings(ctx context.Context, db *database.PostgresDB) error {
	query := `INSERT INTO game_settings (key, value, value_type, description, category)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) DO NOTHING`

	for _, s := range defaultSettings {
		if _, err := db.Pool.Exec(ctx, query, s.Key, s.Value, s.ValueType, s.Description, s.Category); err != nil {
			return fmt.Errorf("insert setting %s: %w", s.Key, err)
		}
	}

	observability.Log.Info().Int("count", len(defaultSettings)).Msg("seeded game_settings")
	return nil
}

// SeedConfigFromYAML loads YAML config files via gamedata.LoadGameData and inserts
// the data into config_* tables. Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedConfigFromYAML(ctx context.Context, db *database.PostgresDB, configDir string) error {
	if err := gamedata.LoadGameData(configDir); err != nil {
		return fmt.Errorf("load game data: %w", err)
	}
	data := gamedata.Data

	// Seed characters
	charQuery := `INSERT INTO config_characters
		(character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (character_id) DO NOTHING`
	for _, c := range data.Characters {
		if _, err := db.Pool.Exec(ctx, charQuery,
			c.CharacterID, c.Name, c.Role, c.HP, c.Damage, c.Mobility,
			c.Defense, c.SkillPower, c.TerrainDamage, c.Difficulty, c.WeaponID, c.SkillID,
		); err != nil {
			return fmt.Errorf("insert character %s: %w", c.CharacterID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Characters)).Msg("seeded config_characters")

	// Seed weapons
	weaponQuery := `INSERT INTO config_weapons
		(weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (weapon_id) DO NOTHING`
	for _, w := range data.Weapons {
		if _, err := db.Pool.Exec(ctx, weaponQuery,
			w.WeaponID, w.Name, w.Damage, w.ExplosionRadius, w.TerrainDamage,
			w.ProjectileWeight, w.WindInfluence, w.MultiHit,
		); err != nil {
			return fmt.Errorf("insert weapon %s: %w", w.WeaponID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Weapons)).Msg("seeded config_weapons")

	// Seed skills
	skillQuery := `INSERT INTO config_skills
		(skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (skill_id) DO NOTHING`
	for _, s := range data.Skills {
		if _, err := db.Pool.Exec(ctx, skillQuery,
			s.SkillID, s.CharacterID, s.Name, s.CooldownTurn, s.EffectType,
			s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier,
		); err != nil {
			return fmt.Errorf("insert skill %s: %w", s.SkillID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Skills)).Msg("seeded config_skills")

	// Seed items
	itemQuery := `INSERT INTO config_items
		(item_id, name, type, target_type, value, max_use_per_match, cooldown)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (item_id) DO NOTHING`
	for _, i := range data.Items {
		if _, err := db.Pool.Exec(ctx, itemQuery,
			i.ItemID, i.Name, i.Type, i.TargetType, i.Value,
			i.MaxUsePerMatch, i.Cooldown,
		); err != nil {
			return fmt.Errorf("insert item %s: %w", i.ItemID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Items)).Msg("seeded config_items")

	// Seed brick types
	brickQuery := `INSERT INTO config_brick_types (brick_type_id, name, destructible)
		VALUES ($1, $2, $3)
		ON CONFLICT (brick_type_id) DO NOTHING`
	defaultBricks := []struct {
		ID           string
		Name         string
		Destructible bool
	}{
		{"dirt", "Dirt", true},
		{"rock", "Rock", false},
		{"ice", "Ice", true},
		{"lava", "Lava", false},
		{"fragile", "Fragile", true},
	}
	for _, b := range defaultBricks {
		if _, err := db.Pool.Exec(ctx, brickQuery, b.ID, b.Name, b.Destructible); err != nil {
			return fmt.Errorf("insert brick type %s: %w", b.ID, err)
		}
	}
	observability.Log.Info().Int("count", len(defaultBricks)).Msg("seeded config_brick_types")

	// Seed maps (JSONB fields need marshaling)
	mapQuery := `INSERT INTO config_maps
		(map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points,
		 grid_width, grid_height, cell_size, tiles)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (map_id) DO NOTHING`
	for _, m := range data.Maps {
		windRange, err := json.Marshal(m.DefaultWindPowerRange)
		if err != nil {
			return fmt.Errorf("marshal wind range for map %s: %w", m.MapID, err)
		}
		terrainLayers, err := json.Marshal(m.TerrainLayers)
		if err != nil {
			return fmt.Errorf("marshal terrain layers for map %s: %w", m.MapID, err)
		}
		spawnPoints, err := json.Marshal(m.SpawnPoints)
		if err != nil {
			return fmt.Errorf("marshal spawn points for map %s: %w", m.MapID, err)
		}
		tiles, err := json.Marshal(m.Tiles)
		if err != nil {
			tiles = []byte("[]")
		}
		gridWidth := m.GridWidth
		if gridWidth == 0 {
			gridWidth = 100
		}
		gridHeight := m.GridHeight
		if gridHeight == 0 {
			gridHeight = 56
		}
		cellSize := m.CellSize
		if cellSize == 0 {
			cellSize = 16
		}
		if _, err := db.Pool.Exec(ctx, mapQuery,
			m.MapID, m.Name, m.Width, m.Height, windRange, terrainLayers, spawnPoints,
			gridWidth, gridHeight, cellSize, tiles,
		); err != nil {
			return fmt.Errorf("insert map %s: %w", m.MapID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Maps)).Msg("seeded config_maps")

	return nil
}
