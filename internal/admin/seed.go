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
	if err := SeedEquipmentConfig(ctx, db); err != nil {
		return fmt.Errorf("seed equipment config: %w", err)
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

	// Seed brick types (v2 — SERIAL PK, auto-increment)
	var brickCount int
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_brick_types`).Scan(&brickCount)
	if brickCount == 0 {
		brickQuery := `INSERT INTO config_brick_types (name, image_id, destructible, color)
			VALUES ($1, $2, $3, $4)`
		defaultBricks := []struct {
			Name         string
			Destructible bool
			Color        string
		}{
			{"Dirt", true, "#8B4513"},
			{"Rock", false, "#808080"},
			{"Ice", true, "#87CEEB"},
			{"Lava", false, "#FF4500"},
			{"Fragile", true, "#DEB887"},
		}
		for _, b := range defaultBricks {
			if _, err := db.Pool.Exec(ctx, brickQuery, b.Name, "", b.Destructible, b.Color); err != nil {
				return fmt.Errorf("insert brick type %s: %w", b.Name, err)
			}
		}
		observability.Log.Info().Int("count", len(defaultBricks)).Msg("seeded config_brick_types")
	}

	// Seed maps (JSONB fields need marshaling)
	mapQuery := `INSERT INTO config_maps
		(map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points,
		 grid_width, grid_height, cell_size, tiles, min_rank_tier)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
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
		minRankTier := m.MinRankTier
		if minRankTier == "" {
			minRankTier = "bronze"
		}
		if _, err := db.Pool.Exec(ctx, mapQuery,
			m.MapID, m.Name, m.Width, m.Height, windRange, terrainLayers, spawnPoints,
			gridWidth, gridHeight, cellSize, tiles, minRankTier,
		); err != nil {
			return fmt.Errorf("insert map %s: %w", m.MapID, err)
		}
	}
	observability.Log.Info().Int("count", len(data.Maps)).Msg("seeded config_maps")

	return nil
}

// SeedEquipmentConfig inserts default equipment configuration rows into
// config_upgrade_rates, config_stones, config_gems, and config_set_bonuses.
// Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedEquipmentConfig(ctx context.Context, db *database.PostgresDB) error {
	// ── Upgrade rates ─────────────────────────────────────────────────────────
	type upgradeRate struct {
		fromLevel    int
		toLevel      int
		upgradeCost  int
		maxPercent   float64
		failResetTo  int
	}
	upgradeRates := []upgradeRate{
		{0, 1, 1, 80, 0},
		{1, 2, 2, 76, 1},
		{2, 3, 6, 72, 2},
		{3, 4, 18, 68, 3},
		{4, 5, 40, 64, 4},
		{5, 6, 80, 60, 5},
		{6, 7, 180, 56, 6},
		{7, 8, 500, 52, 6},
		{8, 9, 1200, 45, 6},
		{9, 10, 3000, 40, 6},
		{10, 11, 6000, 35, 10},
		{11, 12, 15000, 30, 10},
		{12, 13, 25000, 25, 10},
		{13, 14, 50000, 20, 10},
		{14, 15, 80000, 15, 14},
		{15, 16, 200000, 10, 14},
	}
	upgradeRateQuery := `INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (from_level, to_level) DO NOTHING`
	for _, r := range upgradeRates {
		if _, err := db.Pool.Exec(ctx, upgradeRateQuery, r.fromLevel, r.toLevel, r.upgradeCost, r.maxPercent, r.failResetTo); err != nil {
			return fmt.Errorf("insert upgrade rate %d->%d: %w", r.fromLevel, r.toLevel, err)
		}
	}
	observability.Log.Info().Int("count", len(upgradeRates)).Msg("seeded config_upgrade_rates")

	// ── Stone configs ──────────────────────────────────────────────────────────
	type stoneConfig struct {
		stoneLevel int
		power      int
		priceCoin  int
		priceGem   int
		source     string
	}
	stones := []stoneConfig{
		{1, 1, 100, 0, "coin_shop"},
		{2, 3, 250, 0, "coin_shop"},
		{3, 9, 700, 0, "coin_shop"},
		{4, 27, 2000, 0, "coin_shop"},
		{5, 81, 5500, 0, "coin_shop"},
		{6, 243, 15000, 0, "coin_shop"},
		{7, 729, 0, 50, "gem_shop"},
		{8, 2187, 0, 140, "gem_shop"},
		{9, 6561, 0, 400, "gem_shop"},
		{10, 19683, 0, 1100, "gem_shop"},
		{11, 59049, 0, 0, "merge_only"},
		{12, 177147, 0, 0, "merge_only"},
	}
	stoneQuery := `INSERT INTO config_stones (stone_level, power, price_coin, price_gem, source)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (stone_level) DO NOTHING`
	for _, s := range stones {
		if _, err := db.Pool.Exec(ctx, stoneQuery, s.stoneLevel, s.power, s.priceCoin, s.priceGem, s.source); err != nil {
			return fmt.Errorf("insert stone level %d: %w", s.stoneLevel, err)
		}
	}
	observability.Log.Info().Int("count", len(stones)).Msg("seeded config_stones")

	// ── Gem configs ────────────────────────────────────────────────────────────
	type gemConfig struct {
		gemType  string
		gemLevel int
		statValue float64
	}
	hpValues := []float64{20, 40, 70, 110, 160, 220, 300, 400, 520, 660}
	damageValues := []float64{3, 6, 10, 15, 22, 30, 40, 52, 67, 85}
	defenseValues := []float64{2, 4, 7, 11, 16, 22, 30, 40, 52, 66}
	criticalValues := []float64{1, 2, 3, 5, 7, 9, 12, 15, 18, 22}

	var gems []gemConfig
	for i, v := range hpValues {
		gems = append(gems, gemConfig{"hp", i + 1, v})
	}
	for i, v := range damageValues {
		gems = append(gems, gemConfig{"damage", i + 1, v})
	}
	for i, v := range defenseValues {
		gems = append(gems, gemConfig{"defense", i + 1, v})
	}
	for i, v := range criticalValues {
		gems = append(gems, gemConfig{"critical", i + 1, v})
	}

	gemQuery := `INSERT INTO config_gems (gem_type, gem_level, stat_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (gem_type, gem_level) DO NOTHING`
	for _, g := range gems {
		if _, err := db.Pool.Exec(ctx, gemQuery, g.gemType, g.gemLevel, g.statValue); err != nil {
			return fmt.Errorf("insert gem %s level %d: %w", g.gemType, g.gemLevel, err)
		}
	}
	observability.Log.Info().Int("count", len(gems)).Msg("seeded config_gems")

	// ── Set bonuses ────────────────────────────────────────────────────────────
	type setBonus struct {
		tier           string
		piecesRequired int
		bonusHP        float64
		bonusDmg       float64
		bonusDef       float64
		bonusCrit      float64
	}
	setBonuses := []setBonus{
		// silver
		{"silver", 2, 3, 0, 0, 0},
		{"silver", 4, 0, 0, 3, 0},
		{"silver", 6, 5, 0, 5, 0},
		// gold
		{"gold", 2, 5, 0, 0, 0},
		{"gold", 4, 0, 5, 0, 0},
		{"gold", 6, 8, 8, 5, 0},
		// titan
		{"titan", 2, 8, 0, 0, 0},
		{"titan", 4, 0, 8, 0, 0},
		{"titan", 6, 12, 12, 8, 5},
		// diamond
		{"diamond", 2, 10, 0, 0, 0},
		{"diamond", 4, 0, 10, 0, 0},
		{"diamond", 6, 15, 15, 12, 10},
	}
	setBonusQuery := `INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tier, pieces_required) DO NOTHING`
	for _, b := range setBonuses {
		if _, err := db.Pool.Exec(ctx, setBonusQuery, b.tier, b.piecesRequired, b.bonusHP, b.bonusDmg, b.bonusDef, b.bonusCrit); err != nil {
			return fmt.Errorf("insert set bonus %s %dpc: %w", b.tier, b.piecesRequired, err)
		}
	}
	observability.Log.Info().Int("count", len(setBonuses)).Msg("seeded config_set_bonuses")

	return nil
}
