package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"battle-squad/internal/api/milestone"
	"battle-squad/internal/api/title"
	"battle-squad/internal/game/gamedata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is what the seed writes through: a pgx.Tx, or a pool in tests.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Beginner opens the seed's transaction: a *pgxpool.Pool or *pgxpool.Conn.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// counting adds up the rows every Exec changed.
type counting struct {
	Execer
	rows int64
}

func (c *counting) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tag, err := c.Execer.Exec(ctx, sql, args...)
	c.rows += tag.RowsAffected()
	return tag, err
}

type settingDef struct {
	Key         string
	Value       string
	ValueType   string
	Description string
	Category    string
}

// DefaultSettings are the scalar game_settings rows the seed inserts.
var DefaultSettings = []settingDef{
	{"physics.gravity", "445", "number", "Lực hấp dẫn kéo đạn xuống. Tăng = đạn rơi nhanh, tầm bắn ngắn", "physics"},
	{"physics.projectile_speed_multiplier", "9.6", "number", "Hệ số tốc độ đạn = power × giá trị này. Tăng = đạn bay xa hơn", "physics"},
	{"physics.wind_scale", "67", "number", "Hệ số ảnh hưởng gió lên đạn. Tăng = gió đẩy đạn lệch nhiều hơn", "physics"},
	{"physics.player_hit_radius", "24.0", "number", "Bán kính va chạm player (pixels). Tăng = dễ bắn trúng hơn", "physics"},
	{"physics.time_step", "0.02", "number", "Bước thời gian mô phỏng vật lý (giây). Giảm = chính xác hơn, tốn CPU hơn", "physics"},
	{"physics.path_record_step", "0.05", "number", "Khoảng thời gian ghi path đạn cho animation client", "physics"},
	{"physics.max_flight_seconds", "4.5", "number", "Thời gian bay tối đa của đạn trước khi biến mất", "physics"},
	{"match.turn_time_seconds", "20", "number", "Thời gian mỗi lượt (giây). Hết = tự động kết thúc lượt", "match"},
	{"match.idle_timeout_minutes", "2", "number", "Phút không hoạt động trước khi match bị hủy", "match"},
	{"move.step_pixels", "2", "number", "Đi chừng này px tốn move.energy_cost_per_2px năng lượng", "movement"},
	{"move.energy_cost_per_2px", "1", "number", "Năng lượng tốn cho mỗi move.step_pixels px đi được", "movement"},
	{"fall.damage_threshold", "150", "number", "Rơi tới chừng này px không mất máu", "physics"},
	{"fall.damage_per_pixel", "3.75", "number", "Máu mất mỗi px rơi vượt ngưỡng (bỏ qua phòng thủ)", "physics"},
	{"combat.defense_k", "600", "number", "Hằng số phòng thủ K: sát thương nhận = sát thương × K / (K + phòng thủ)", "combat"},
	{"equipment.merge_rate_per_item", "25", "number", "Tỷ lệ ghép mỗi viên (%). 4 viên = 4 × giá trị này", "equipment"},
	{"equipment.milestone_6_bonus", "0.10", "number", "Bonus stat khi trang bị đạt +6 (10%)", "equipment"},
	{"equipment.milestone_10_bonus", "0.20", "number", "Bonus stat khi trang bị đạt +10 (20%)", "equipment"},
	{"equipment.milestone_14_bonus", "0.40", "number", "Bonus stat khi trang bị đạt +14 (40%)", "equipment"},
	{"equipment.milestone_16_bonus", "1.00", "number", "Bonus stat khi trang bị đạt +16 (100%)", "equipment"},
	{"equipment.sell_rate", "0.25", "number", "Tỉ lệ hoàn tiền khi bán trang bị: nhận floor(giá mua × tỉ lệ), trong (0, 1]", "equipment"},
	{"equipment.weapon_upgrade_cost_multiplier", "3", "number", "Vũ khí cần upgrade_cost × hệ số này sức mạnh đá (>= 1)", "equipment"},
}

// SeedAll inserts the missing reference game data rows (game_settings, the config tables from
// the YAML in configs, equipment, titles, milestones) in one transaction and returns how many
// it inserted. Every insert is ON CONFLICT DO NOTHING, so admin edits are never overwritten.
// When nothing was missing the transaction rolls back: the config tables' statement triggers
// then leave gamedata_revision alone, and a rerun changes nothing.
func SeedAll(ctx context.Context, db Beginner, configs fs.FS) (int64, error) {
	return seed(ctx, db, configs, false)
}

// SeedMissing is how many rows SeedAll would insert (it rolls back).
func SeedMissing(ctx context.Context, db Beginner, configs fs.FS) (int64, error) {
	return seed(ctx, db, configs, true)
}

func seed(ctx context.Context, db Beginner, configs fs.FS, dryRun bool) (int64, error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	c := &counting{Execer: tx}
	if err := seedAll(ctx, c, configs); err != nil {
		return 0, err
	}
	if c.rows == 0 || dryRun {
		return c.rows, nil
	}
	return c.rows, tx.Commit(ctx)
}

func seedAll(ctx context.Context, q Execer, configs fs.FS) error {
	if err := SeedSettings(ctx, q); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	if err := SeedConfigFromYAML(ctx, q, configs); err != nil {
		return fmt.Errorf("seed config from YAML: %w", err)
	}
	if err := SeedEquipmentConfig(ctx, q); err != nil {
		return fmt.Errorf("seed equipment config: %w", err)
	}
	if err := SeedTitles(ctx, q); err != nil {
		return fmt.Errorf("seed titles: %w", err)
	}
	if err := SeedMilestones(ctx, q); err != nil {
		return fmt.Errorf("seed milestones: %w", err)
	}
	return nil
}

// SeedTitles inserts the default titles (title.Defaults, as migrations/027) into
// config_titles. ON CONFLICT DO NOTHING keeps edited rows.
func SeedTitles(ctx context.Context, db Execer) error {
	query := `INSERT INTO config_titles (title_id, name, rarity, sort_order, description) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (title_id) DO NOTHING`
	for _, d := range title.Defaults {
		if _, err := db.Exec(ctx, query, d.ID, d.Name, d.Rarity, d.SortOrder, d.Description); err != nil {
			return fmt.Errorf("insert title %s: %w", d.ID, err)
		}
	}
	return nil
}

// SeedMilestones inserts the default milestones (milestone.Defaults, as migrations/026) into
// missions with type 'milestone'. ON CONFLICT DO NOTHING keeps edited rows; a row seeded
// before titles gets its title item (milestone.TitleRewards) appended, as migrations/027.
func SeedMilestones(ctx context.Context, db Execer) error {
	query := `INSERT INTO missions (mission_id, type, track, target, required_value, sort_order, reward_coin, reward_gem, reward_items, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10)
		ON CONFLICT (mission_id) DO NOTHING`
	for _, d := range milestone.Defaults {
		items := d.Items
		if items == nil {
			items = []milestone.RewardItem{}
		}
		raw, err := json.Marshal(items)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, query, d.ID, milestone.MissionType, d.Track, d.Target, d.Required, d.SortOrder,
			d.Coin, d.Gem, string(raw), d.Description); err != nil {
			return fmt.Errorf("insert milestone %s: %w", d.ID, err)
		}
	}
	appendTitle := `UPDATE missions
		SET reward_items = (CASE WHEN jsonb_typeof(reward_items) = 'array' THEN reward_items ELSE '[]'::jsonb END) || $2::jsonb
		WHERE mission_id = $1
		  AND NOT (CASE WHEN jsonb_typeof(reward_items) = 'array' THEN reward_items ELSE '[]'::jsonb END) @> $2::jsonb`
	for id, titleID := range milestone.TitleRewards {
		raw, err := json.Marshal([]milestone.RewardItem{{Kind: milestone.RewardKindTitle, ID: titleID}})
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, appendTitle, id, string(raw)); err != nil {
			return fmt.Errorf("attach title %s to milestone %s: %w", titleID, id, err)
		}
	}
	return nil
}

// SeedSettings inserts default physics/match/movement settings into game_settings.
// Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedSettings(ctx context.Context, db Execer) error {
	query := `INSERT INTO game_settings (key, value, value_type, description, category)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) DO NOTHING`

	for _, s := range DefaultSettings {
		if _, err := db.Exec(ctx, query, s.Key, s.Value, s.ValueType, s.Description, s.Category); err != nil {
			return fmt.Errorf("insert setting %s: %w", s.Key, err)
		}
	}
	// The rules rows (as migrations/041): every group with its defaults
	rules := gamedata.DefaultRules()
	for _, g := range gamedata.RuleGroups {
		if _, err := db.Exec(ctx, query, g.Key, g.Encode(rules), "json", g.Title, "rules"); err != nil {
			return fmt.Errorf("insert setting %s: %w", g.Key, err)
		}
	}

	return nil
}

// SeedConfigFromYAML loads the YAML files in configs (gamedata.LoadYAMLFS) and inserts
// the data into config_* tables. Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedConfigFromYAML(ctx context.Context, db Execer, configs fs.FS) error {
	snap, err := gamedata.LoadYAMLFS(configs)
	if err != nil {
		return fmt.Errorf("load game data: %w", err)
	}
	data := snap.Data

	// Seed characters
	charQuery := `INSERT INTO config_characters
		(character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id,
		 role_name, playstyle, gun_kind, sort_order, element)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (character_id) DO NOTHING`
	for _, c := range data.Characters {
		if _, err := db.Exec(ctx, charQuery,
			c.CharacterID, c.Name, c.Role, c.HP, c.Damage, c.Mobility,
			c.Defense, c.SkillPower, c.TerrainDamage, c.Difficulty, c.WeaponID, c.SkillID,
			c.RoleName, c.Playstyle, c.GunKind, c.SortOrder, c.Element,
		); err != nil {
			return fmt.Errorf("insert character %s: %w", c.CharacterID, err)
		}
	}

	// Seed weapons
	weaponQuery := `INSERT INTO config_weapons
		(weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit, bounces)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (weapon_id) DO NOTHING`
	for _, w := range data.Weapons {
		if _, err := db.Exec(ctx, weaponQuery,
			w.WeaponID, w.Name, w.Damage, w.ExplosionRadius, w.TerrainDamage,
			w.ProjectileWeight, w.WindInfluence, w.MultiHit, w.Bounces,
		); err != nil {
			return fmt.Errorf("insert weapon %s: %w", w.WeaponID, err)
		}
	}

	// Seed skills
	skillQuery := `INSERT INTO config_skills
		(skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier, description,
		 description_template, params)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb)
		ON CONFLICT (skill_id) DO NOTHING`
	for _, s := range data.Skills {
		params := s.Params
		if params == nil {
			params = gamedata.DefaultSkillParams(s.EffectType)
		}
		if params == nil {
			params = map[string]float64{}
		}
		rawParams, err := json.Marshal(params)
		if err != nil {
			return err
		}
		if _, err := db.Exec(ctx, skillQuery,
			s.SkillID, s.CharacterID, s.Name, s.CooldownTurn, s.EffectType,
			s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier, s.Description, s.DescriptionTemplate, string(rawParams),
		); err != nil {
			return fmt.Errorf("insert skill %s: %w", s.SkillID, err)
		}
	}

	// Seed items
	itemQuery := `INSERT INTO config_items
		(item_id, name, type, target_type, value, max_use_per_match, cooldown, display_name, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (item_id) DO NOTHING`
	for _, i := range data.Items {
		if _, err := db.Exec(ctx, itemQuery,
			i.ItemID, i.Name, i.Type, i.TargetType, i.Value,
			i.MaxUsePerMatch, i.Cooldown, i.DisplayName, i.SortOrder,
		); err != nil {
			return fmt.Errorf("insert item %s: %w", i.ItemID, err)
		}
	}

	// Seed brick types (v2 — SERIAL PK, auto-increment)
	var brickCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM config_brick_types`).Scan(&brickCount); err != nil {
		return fmt.Errorf("count brick types: %w", err)
	}
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
			if _, err := db.Exec(ctx, brickQuery, b.Name, "", b.Destructible, b.Color); err != nil {
				return fmt.Errorf("insert brick type %s: %w", b.Name, err)
			}
		}
	}

	// Seed maps (JSONB fields need marshaling)
	mapQuery := `INSERT INTO config_maps
		(map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points,
		 grid_width, grid_height, cell_size, tiles, min_rank_tier, theme, description)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
		if _, err := db.Exec(ctx, mapQuery,
			m.MapID, m.Name, m.Width, m.Height, windRange, terrainLayers, spawnPoints,
			gridWidth, gridHeight, cellSize, tiles, minRankTier, m.ThemeOrDefault(), m.Description,
		); err != nil {
			return fmt.Errorf("insert map %s: %w", m.MapID, err)
		}
	}

	return nil
}

// SeedEquipmentConfig inserts default equipment configuration rows into
// config_upgrade_rates, config_stones, config_gems, and config_set_bonuses.
// Uses ON CONFLICT DO NOTHING so re-running is safe.
func SeedEquipmentConfig(ctx context.Context, db Execer) error {
	// ── Upgrade rates ─────────────────────────────────────────────────────────
	type upgradeRate struct {
		fromLevel   int
		toLevel     int
		upgradeCost int
		maxPercent  float64
		failResetTo int
		carefulGem  int     // careful-upgrade gem price, as migrations/025
		carefulPct  float64 // careful-upgrade relative bonus %, as migrations/025
	}
	upgradeRates := []upgradeRate{
		{0, 1, 1, 80, 0, 2, 30},
		{1, 2, 2, 76, 1, 2, 30},
		{2, 3, 6, 72, 2, 3, 30},
		{3, 4, 18, 68, 3, 5, 30},
		{4, 5, 40, 64, 4, 5, 30},
		{5, 6, 80, 60, 5, 10, 30},
		{6, 7, 180, 56, 6, 10, 30},
		{7, 8, 500, 52, 6, 10, 30},
		{8, 9, 1200, 45, 6, 15, 30},
		{9, 10, 3000, 40, 6, 20, 30},
		{10, 11, 6000, 35, 10, 30, 40},
		{11, 12, 15000, 30, 10, 60, 40},
		{12, 13, 25000, 25, 10, 80, 40},
		{13, 14, 50000, 20, 10, 120, 40},
		{14, 15, 80000, 15, 14, 180, 50},
		{15, 16, 200000, 10, 14, 250, 50},
	}
	upgradeRateQuery := `INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to, careful_gem_cost, careful_bonus_percent)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (from_level, to_level) DO NOTHING`
	for _, r := range upgradeRates {
		if _, err := db.Exec(ctx, upgradeRateQuery, r.fromLevel, r.toLevel, r.upgradeCost, r.maxPercent, r.failResetTo, r.carefulGem, r.carefulPct); err != nil {
			return fmt.Errorf("insert upgrade rate %d->%d: %w", r.fromLevel, r.toLevel, err)
		}
	}

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
		if _, err := db.Exec(ctx, stoneQuery, s.stoneLevel, s.power, s.priceCoin, s.priceGem, s.source); err != nil {
			return fmt.Errorf("insert stone level %d: %w", s.stoneLevel, err)
		}
	}

	// ── Gem configs ────────────────────────────────────────────────────────────
	type gemConfig struct {
		gemType   string
		gemLevel  int
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
		if _, err := db.Exec(ctx, gemQuery, g.gemType, g.gemLevel, g.statValue); err != nil {
			return fmt.Errorf("insert gem %s level %d: %w", g.gemType, g.gemLevel, err)
		}
	}

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
		if _, err := db.Exec(ctx, setBonusQuery, b.tier, b.piecesRequired, b.bonusHP, b.bonusDmg, b.bonusDef, b.bonusCrit); err != nil {
			return fmt.Errorf("insert set bonus %s %dpc: %w", b.tier, b.piecesRequired, err)
		}
	}

	// ── Normal equipment items ─────────────────────────────────────────────────
	// Same rows as migrations/016 + 019 + 020. Required levels 10/20/30/40 (the id
	// suffix). Weapons are per character (characterWeapons below); the generic
	// weapons stay for pieces players already own but are inactive (not sold).
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
		{"normal_weapon_lv10", "Iron Blaster", "weapon", 10, 0, 8, 0, 0, 0, 500},
		{"normal_weapon_lv20", "Steel Blaster", "weapon", 20, 0, 15, 0, 1, 0, 1500},
		{"normal_weapon_lv30", "Mithril Blaster", "weapon", 30, 0, 25, 0, 2, 0, 4000},
		{"normal_weapon_lv40", "Dragon Blaster", "weapon", 40, 0, 38, 0, 3, 0, 10000},
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
		 stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem, is_active)
		VALUES ($1, $2, $3, 'normal', NULL, $4, $5, 1, $6, $7, $8, $9, $10, $11, 0, $12)
		ON CONFLICT (item_id) DO NOTHING`
	var normalWeapons []equipItem
	for _, it := range normalItems {
		if it.slot == "weapon" {
			normalWeapons = append(normalWeapons, it)
		}
		if _, err := db.Exec(ctx, equipItemQuery,
			it.itemID, it.name, it.slot, it.level, nil,
			it.statHP, it.statDamage, it.statDefense, it.statCrit, it.statMoveEnergy,
			it.priceCoin, it.slot != "weapon",
		); err != nil {
			return fmt.Errorf("insert normal equipment item %s: %w", it.itemID, err)
		}
	}
	// Per-character guns (migrations 019 + 020, Tide's 055): each generic weapon tier's
	// stats and price, named "<tier word> <gun>" (Iron Bazooka ... Dragon Bazooka).
	characterWeapons := []struct{ characterID, gun string }{
		{"rookie", "Bazooka"}, {"tanko", "Cannon"}, {"spark", "Shock Rifle"}, {"flora", "Seed Launcher"}, {"tide", "Water Gun"},
	}
	for _, c := range characterWeapons {
		for _, w := range normalWeapons {
			itemID := fmt.Sprintf("%s_weapon_lv%d", c.characterID, w.level)
			tierWord := strings.Fields(w.name)[0] // Iron, Steel, Mithril, Dragon
			if _, err := db.Exec(ctx, equipItemQuery,
				itemID, tierWord+" "+c.gun, "weapon", w.level, c.characterID,
				w.statHP, w.statDamage, w.statDefense, w.statCrit, w.statMoveEnergy,
				w.priceCoin, true,
			); err != nil {
				return fmt.Errorf("insert character weapon %s: %w", itemID, err)
			}
		}
	}

	// ── Crafted equipment items ────────────────────────────────────────────────
	// Multipliers: silver=1.3×Lv10, gold=1.3×Lv30, titan=1.5×Lv40, diamond=2.0×Lv40
	// Required levels silver/gold/titan/diamond = 15/35/55/75.
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
		{"crafted_weapon_silver", "Silver Blaster", "weapon", "silver", 15, 0, 10, 0, 0, 0},
		{"crafted_weapon_gold", "Gold Blaster", "weapon", "gold", 35, 0, 33, 0, 3, 0},
		{"crafted_weapon_titan", "Titan Blaster", "weapon", "titan", 55, 0, 57, 0, 5, 0},
		{"crafted_weapon_diamond", "Diamond Blaster", "weapon", "diamond", 75, 0, 76, 0, 6, 0},
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
		if _, err := db.Exec(ctx, craftedItemQuery,
			it.itemID, it.name, it.slot, it.tier, it.level,
			it.statHP, it.statDamage, it.statDefense, it.statCrit, it.statMoveEnergy,
		); err != nil {
			return fmt.Errorf("insert crafted equipment item %s: %w", it.itemID, err)
		}
	}

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
		if _, err := db.Exec(ctx, materialQuery, m.materialID, m.name, m.source, m.tier, m.priceGem); err != nil {
			return fmt.Errorf("insert material %s: %w", m.materialID, err)
		}
	}

	// ── Crafting recipes ───────────────────────────────────────────────────────
	type matQty struct {
		MaterialID string `json:"materialId"`
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
		if _, err := db.Exec(ctx, recipeQuery, r.recipeID, r.resultItemID, matsJSON); err != nil {
			return fmt.Errorf("insert crafting recipe %s: %w", r.recipeID, err)
		}
	}

	return nil
}
