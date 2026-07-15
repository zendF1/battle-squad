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
	{"equipment.merge_rate_per_item", "25", "number", "Tỷ lệ ghép mỗi viên (%). 4 viên = 4 × giá trị này", "equipment"},
	{"equipment.milestone_6_bonus", "0.10", "number", "Bonus stat khi trang bị đạt +6 (10%)", "equipment"},
	{"equipment.milestone_10_bonus", "0.20", "number", "Bonus stat khi trang bị đạt +10 (20%)", "equipment"},
	{"equipment.milestone_14_bonus", "0.40", "number", "Bonus stat khi trang bị đạt +14 (40%)", "equipment"},
	{"equipment.milestone_16_bonus", "1.00", "number", "Bonus stat khi trang bị đạt +16 (100%)", "equipment"},
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

	// ── Normal equipment items ─────────────────────────────────────────────────
	type equipItem struct {
		itemID         string
		name           string
		slot           string
		level          int
		statHP         int
		statDamage     int
		statDefense    int
		statCrit       float64
		statMoveEnergy int
		priceCoin      int
	}
	normalItems := []equipItem{
		// weapon
		{"normal_weapon_lv10", "Iron Sword", "weapon", 10, 0, 8, 0, 0, 0, 500},
		{"normal_weapon_lv20", "Steel Sword", "weapon", 20, 0, 15, 0, 1, 0, 1500},
		{"normal_weapon_lv30", "Mithril Sword", "weapon", 30, 0, 25, 0, 2, 0, 4000},
		{"normal_weapon_lv40", "Dragon Sword", "weapon", 40, 0, 38, 0, 3, 0, 10000},
		// armor
		{"normal_armor_lv10", "Iron Vest", "armor", 10, 30, 0, 3, 0, 0, 400},
		{"normal_armor_lv20", "Steel Vest", "armor", 20, 60, 0, 6, 0, 0, 1200},
		{"normal_armor_lv30", "Mithril Vest", "armor", 30, 100, 0, 10, 0, 0, 3500},
		{"normal_armor_lv40", "Dragon Vest", "armor", 40, 150, 0, 15, 0, 0, 8000},
		// helmet
		{"normal_helmet_lv10", "Iron Cap", "helmet", 10, 20, 0, 2, 0, 0, 300},
		{"normal_helmet_lv20", "Steel Cap", "helmet", 20, 40, 0, 4, 0, 0, 900},
		{"normal_helmet_lv30", "Mithril Cap", "helmet", 30, 70, 0, 7, 0, 0, 2500},
		{"normal_helmet_lv40", "Dragon Cap", "helmet", 40, 110, 0, 11, 0, 0, 6000},
		// pants
		{"normal_pants_lv10", "Iron Pants", "pants", 10, 20, 0, 2, 0, 0, 350},
		{"normal_pants_lv20", "Steel Pants", "pants", 20, 45, 0, 5, 0, 0, 1000},
		{"normal_pants_lv30", "Mithril Pants", "pants", 30, 75, 0, 8, 0, 0, 3000},
		{"normal_pants_lv40", "Dragon Pants", "pants", 40, 120, 0, 12, 0, 0, 7000},
		// boots
		{"normal_boots_lv10", "Iron Boots", "boots", 10, 10, 0, 1, 0, 5, 300},
		{"normal_boots_lv20", "Steel Boots", "boots", 20, 20, 0, 2, 0, 10, 800},
		{"normal_boots_lv30", "Mithril Boots", "boots", 30, 35, 0, 4, 0, 15, 2200},
		{"normal_boots_lv40", "Dragon Boots", "boots", 40, 55, 0, 6, 0, 20, 5500},
		// gloves
		{"normal_gloves_lv10", "Iron Gloves", "gloves", 10, 0, 2, 0, 2, 0, 250},
		{"normal_gloves_lv20", "Steel Gloves", "gloves", 20, 0, 4, 0, 4, 0, 750},
		{"normal_gloves_lv30", "Mithril Gloves", "gloves", 30, 0, 7, 0, 6, 0, 2000},
		{"normal_gloves_lv40", "Dragon Gloves", "gloves", 40, 0, 12, 0, 8, 0, 5000},
	}
	equipItemQuery := `INSERT INTO config_equipment_items
		(item_id, name, slot, category, tier, required_level, character_id, gem_slots,
		 stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem)
		VALUES ($1, $2, $3, 'normal', NULL, $4, NULL, 1, $5, $6, $7, $8, $9, $10, 0)
		ON CONFLICT (item_id) DO NOTHING`
	for _, it := range normalItems {
		if _, err := db.Pool.Exec(ctx, equipItemQuery,
			it.itemID, it.name, it.slot, it.level,
			it.statHP, it.statDamage, it.statDefense, it.statCrit, it.statMoveEnergy,
			it.priceCoin,
		); err != nil {
			return fmt.Errorf("insert normal equipment item %s: %w", it.itemID, err)
		}
	}
	observability.Log.Info().Int("count", len(normalItems)).Msg("seeded normal config_equipment_items")

	// ── Crafted equipment items ────────────────────────────────────────────────
	// Multipliers: silver=1.3×Lv10, gold=1.3×Lv30, titan=1.5×Lv40, diamond=2.0×Lv40
	type craftedItem struct {
		itemID         string
		name           string
		slot           string
		tier           string
		level          int
		statHP         int
		statDamage     int
		statDefense    int
		statCrit       float64
		statMoveEnergy int
	}
	craftedItems := []craftedItem{
		// weapon: base DMG {8,25,38,38}, Crit {0,2,3,3}
		{"crafted_weapon_silver", "Silver Sword", "weapon", "silver", 15, 0, 10, 0, 0, 0},
		{"crafted_weapon_gold", "Gold Sword", "weapon", "gold", 35, 0, 33, 0, 3, 0},
		{"crafted_weapon_titan", "Titan Sword", "weapon", "titan", 55, 0, 57, 0, 5, 0},
		{"crafted_weapon_diamond", "Diamond Sword", "weapon", "diamond", 75, 0, 76, 0, 6, 0},
		// armor: base HP {30,100,150,150}, DEF {3,10,15,15}
		{"crafted_armor_silver", "Silver Vest", "armor", "silver", 15, 39, 0, 4, 0, 0},
		{"crafted_armor_gold", "Gold Vest", "armor", "gold", 35, 130, 0, 13, 0, 0},
		{"crafted_armor_titan", "Titan Vest", "armor", "titan", 55, 225, 0, 23, 0, 0},
		{"crafted_armor_diamond", "Diamond Vest", "armor", "diamond", 75, 300, 0, 30, 0, 0},
		// helmet: base HP {20,70,110,110}, DEF {2,7,11,11}
		{"crafted_helmet_silver", "Silver Cap", "helmet", "silver", 15, 26, 0, 3, 0, 0},
		{"crafted_helmet_gold", "Gold Cap", "helmet", "gold", 35, 91, 0, 9, 0, 0},
		{"crafted_helmet_titan", "Titan Cap", "helmet", "titan", 55, 165, 0, 17, 0, 0},
		{"crafted_helmet_diamond", "Diamond Cap", "helmet", "diamond", 75, 220, 0, 22, 0, 0},
		// pants: base HP {20,75,120,120}, DEF {2,8,12,12}
		{"crafted_pants_silver", "Silver Pants", "pants", "silver", 15, 26, 0, 3, 0, 0},
		{"crafted_pants_gold", "Gold Pants", "pants", "gold", 35, 98, 0, 10, 0, 0},
		{"crafted_pants_titan", "Titan Pants", "pants", "titan", 55, 180, 0, 18, 0, 0},
		{"crafted_pants_diamond", "Diamond Pants", "pants", "diamond", 75, 240, 0, 24, 0, 0},
		// boots: base HP {10,35,55,55}, DEF {1,4,6,6}, MoveEnergy {5,15,20,20}
		{"crafted_boots_silver", "Silver Boots", "boots", "silver", 15, 13, 0, 1, 0, 7},
		{"crafted_boots_gold", "Gold Boots", "boots", "gold", 35, 46, 0, 5, 0, 20},
		{"crafted_boots_titan", "Titan Boots", "boots", "titan", 55, 83, 0, 9, 0, 30},
		{"crafted_boots_diamond", "Diamond Boots", "boots", "diamond", 75, 110, 0, 12, 0, 40},
		// gloves: base DMG {2,7,12,12}, Crit {2,6,8,8}
		{"crafted_gloves_silver", "Silver Gloves", "gloves", "silver", 15, 0, 3, 0, 3, 0},
		{"crafted_gloves_gold", "Gold Gloves", "gloves", "gold", 35, 0, 9, 0, 8, 0},
		{"crafted_gloves_titan", "Titan Gloves", "gloves", "titan", 55, 0, 18, 0, 12, 0},
		{"crafted_gloves_diamond", "Diamond Gloves", "gloves", "diamond", 75, 0, 24, 0, 16, 0},
	}
	craftedItemQuery := `INSERT INTO config_equipment_items
		(item_id, name, slot, category, tier, required_level, character_id, gem_slots,
		 stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem)
		VALUES ($1, $2, $3, 'crafted', $4, $5, NULL, 2, $6, $7, $8, $9, $10, 0, 0)
		ON CONFLICT (item_id) DO NOTHING`
	for _, it := range craftedItems {
		if _, err := db.Pool.Exec(ctx, craftedItemQuery,
			it.itemID, it.name, it.slot, it.tier, it.level,
			it.statHP, it.statDamage, it.statDefense, it.statCrit, it.statMoveEnergy,
		); err != nil {
			return fmt.Errorf("insert crafted equipment item %s: %w", it.itemID, err)
		}
	}
	observability.Log.Info().Int("count", len(craftedItems)).Msg("seeded crafted config_equipment_items")

	// ── Materials ──────────────────────────────────────────────────────────────
	type material struct {
		materialID string
		name       string
		source     string
		tier       string
		priceGem   int
	}
	materials := []material{
		// silver tier
		{"silver_ore", "Silver Ore", "drop", "silver", 5},
		{"woven_thread", "Woven Thread", "drop", "silver", 5},
		{"beast_hide", "Beast Hide", "drop", "silver", 5},
		{"crystal_powder", "Crystal Powder", "gem_shop", "silver", 10},
		{"silver_blueprint", "Silver Blueprint", "gem_shop", "silver", 20},
		// gold tier
		{"gold_ore", "Gold Ore", "drop", "gold", 15},
		{"enchanted_silk", "Enchanted Silk", "drop", "gold", 15},
		{"small_dragon_scale", "Small Dragon Scale", "drop", "gold", 15},
		{"magic_essence", "Magic Essence", "gem_shop", "gold", 30},
		{"gold_blueprint", "Gold Blueprint", "gem_shop", "gold", 50},
		// titan tier
		{"titan_ore", "Titan Ore", "drop", "titan", 40},
		{"titan_fiber", "Titan Fiber", "drop", "titan", 40},
		{"energy_core", "Energy Core", "drop", "titan", 40},
		{"titan_solvent", "Titan Solvent", "gem_shop", "titan", 80},
		{"titan_blueprint", "Titan Blueprint", "gem_shop", "titan", 120},
		// diamond tier
		{"raw_diamond", "Raw Diamond", "drop", "diamond", 100},
		{"light_weave", "Light Weave", "drop", "diamond", 100},
		{"boss_soul_shard", "Boss Soul Shard", "drop", "diamond", 100},
		{"phoenix_tear", "Phoenix Tear", "gem_shop", "diamond", 200},
		{"diamond_blueprint", "Diamond Blueprint", "gem_shop", "diamond", 300},
	}
	materialQuery := `INSERT INTO config_materials (material_id, name, source, tier, price_gem)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (material_id) DO NOTHING`
	for _, m := range materials {
		if _, err := db.Pool.Exec(ctx, materialQuery, m.materialID, m.name, m.source, m.tier, m.priceGem); err != nil {
			return fmt.Errorf("insert material %s: %w", m.materialID, err)
		}
	}
	observability.Log.Info().Int("count", len(materials)).Msg("seeded config_materials")

	// ── Crafting recipes ───────────────────────────────────────────────────────
	type matQty struct {
		MaterialID string `json:"material_id"`
		Quantity   int    `json:"quantity"`
	}
	type recipe struct {
		recipeID     string
		resultItemID string
		materials    []matQty
	}

	silverMats := func(qty int) []matQty {
		return []matQty{
			{"silver_ore", qty}, {"woven_thread", qty}, {"beast_hide", qty},
			{"crystal_powder", qty}, {"silver_blueprint", qty},
		}
	}
	goldMats := func(qty int) []matQty {
		return []matQty{
			{"gold_ore", qty}, {"enchanted_silk", qty}, {"small_dragon_scale", qty},
			{"magic_essence", qty}, {"gold_blueprint", qty},
		}
	}
	titanMats := func(qty int) []matQty {
		return []matQty{
			{"titan_ore", qty}, {"titan_fiber", qty}, {"energy_core", qty},
			{"titan_solvent", qty}, {"titan_blueprint", qty},
		}
	}
	diamondMats := func(qty int) []matQty {
		return []matQty{
			{"raw_diamond", qty}, {"light_weave", qty}, {"boss_soul_shard", qty},
			{"phoenix_tear", qty}, {"diamond_blueprint", qty},
		}
	}

	type tierDef struct {
		name    string
		baseQty int
		weapQty int
		matsFn  func(int) []matQty
	}
	recipeTiers := []tierDef{
		{"silver", 10, 15, silverMats},
		{"gold", 20, 30, goldMats},
		{"titan", 30, 45, titanMats},
		{"diamond", 40, 60, diamondMats},
	}
	recipeSlots := []string{"weapon", "armor", "helmet", "pants", "boots", "gloves"}

	var recipes []recipe
	for _, tier := range recipeTiers {
		for _, slot := range recipeSlots {
			qty := tier.baseQty
			if slot == "weapon" {
				qty = tier.weapQty
			}
			recipes = append(recipes, recipe{
				recipeID:     fmt.Sprintf("recipe_%s_%s", slot, tier.name),
				resultItemID: fmt.Sprintf("crafted_%s_%s", slot, tier.name),
				materials:    tier.matsFn(qty),
			})
		}
	}

	recipeQuery := `INSERT INTO config_crafting_recipes (recipe_id, result_item_id, materials)
		VALUES ($1, $2, $3)
		ON CONFLICT (recipe_id) DO NOTHING`
	for _, r := range recipes {
		matsJSON, err := json.Marshal(r.materials)
		if err != nil {
			return fmt.Errorf("marshal materials for recipe %s: %w", r.recipeID, err)
		}
		if _, err := db.Pool.Exec(ctx, recipeQuery, r.recipeID, r.resultItemID, matsJSON); err != nil {
			return fmt.Errorf("insert crafting recipe %s: %w", r.recipeID, err)
		}
	}
	observability.Log.Info().Int("count", len(recipes)).Msg("seeded config_crafting_recipes")

	return nil
}
