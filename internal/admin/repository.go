package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"battle-squad/internal/shared/database"
)

// Repository provides all database queries for the admin dashboard.
type Repository struct {
	db    *database.PostgresDB
	redis *database.RedisClient
}

// NewRepository creates a new admin Repository.
func NewRepository(db *database.PostgresDB, redis *database.RedisClient) *Repository {
	return &Repository{db: db, redis: redis}
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// Dashboard Stats
// ---------------------------------------------------------------------------

// DashboardStats holds summary numbers for the admin dashboard.
type DashboardStats struct {
	ActiveRooms  int
	TotalPlayers int
}

// GetDashboardStats returns active room count (Redis) and total player count (Postgres).
func (r *Repository) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	stats := &DashboardStats{}

	// Total players from Postgres
	err := r.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_profiles`).Scan(&stats.TotalPlayers)
	if err != nil {
		return nil, fmt.Errorf("count players: %w", err)
	}

	// Active rooms from Redis
	activeRooms, err := r.redis.Client.HLen(ctx, "rooms:active").Result()
	if err != nil {
		return nil, fmt.Errorf("count active rooms: %w", err)
	}
	stats.ActiveRooms = int(activeRooms)

	return stats, nil
}

// ---------------------------------------------------------------------------
// Game Settings
// ---------------------------------------------------------------------------

// GameSetting represents a row in game_settings.
type GameSetting struct {
	Key         string
	Value       string
	ValueType   string
	Description string
	Category    string
	UpdatedAt   time.Time
}

// GetAllSettings returns all game settings ordered by category then key.
func (r *Repository) GetAllSettings(ctx context.Context) ([]GameSetting, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT key, value, value_type, description, category, updated_at
		 FROM game_settings ORDER BY category, key`)
	if err != nil {
		return nil, fmt.Errorf("query settings: %w", err)
	}
	defer rows.Close()

	var settings []GameSetting
	for rows.Next() {
		var s GameSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.ValueType, &s.Description, &s.Category, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		settings = append(settings, s)
	}
	return settings, rows.Err()
}

// UpdateSetting updates a single setting value by key.
func (r *Repository) UpdateSetting(ctx context.Context, key, value string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE game_settings SET value = $1, updated_at = CURRENT_TIMESTAMP WHERE key = $2`,
		value, key)
	if err != nil {
		return fmt.Errorf("update setting %s: %w", key, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Characters CRUD
// ---------------------------------------------------------------------------

// ConfigCharacter represents a row in config_characters.
type ConfigCharacter struct {
	CharacterID   string
	Name          string
	Role          string
	HP            int
	Damage        int
	Mobility      int
	Defense       int
	SkillPower    int
	TerrainDamage int
	Difficulty    int
	WeaponID      string
	SkillID       string
	Description   string
}

// GetCharacters returns all characters ordered by character_id.
func (r *Repository) GetCharacters(ctx context.Context) ([]ConfigCharacter, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT character_id, name, role, hp, damage, mobility, defense, skill_power,
		        terrain_damage, difficulty, weapon_id, skill_id, description
		 FROM config_characters ORDER BY character_id`)
	if err != nil {
		return nil, fmt.Errorf("query characters: %w", err)
	}
	defer rows.Close()

	var chars []ConfigCharacter
	for rows.Next() {
		var c ConfigCharacter
		if err := rows.Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage,
			&c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty,
			&c.WeaponID, &c.SkillID, &c.Description); err != nil {
			return nil, fmt.Errorf("scan character: %w", err)
		}
		chars = append(chars, c)
	}
	return chars, rows.Err()
}

// GetCharacter returns a single character by ID.
func (r *Repository) GetCharacter(ctx context.Context, id string) (*ConfigCharacter, error) {
	var c ConfigCharacter
	err := r.db.Pool.QueryRow(ctx,
		`SELECT character_id, name, role, hp, damage, mobility, defense, skill_power,
		        terrain_damage, difficulty, weapon_id, skill_id, description
		 FROM config_characters WHERE character_id = $1`, id).
		Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage,
			&c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty,
			&c.WeaponID, &c.SkillID, &c.Description)
	if err != nil {
		return nil, fmt.Errorf("get character %s: %w", id, err)
	}
	return &c, nil
}

// UpsertCharacter inserts or updates a character.
func (r *Repository) UpsertCharacter(ctx context.Context, c *ConfigCharacter) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_characters
		 (character_id, name, role, hp, damage, mobility, defense, skill_power,
		  terrain_damage, difficulty, weapon_id, skill_id, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, CURRENT_TIMESTAMP)
		 ON CONFLICT (character_id) DO UPDATE SET
		   name=EXCLUDED.name, role=EXCLUDED.role, hp=EXCLUDED.hp, damage=EXCLUDED.damage,
		   mobility=EXCLUDED.mobility, defense=EXCLUDED.defense, skill_power=EXCLUDED.skill_power,
		   terrain_damage=EXCLUDED.terrain_damage, difficulty=EXCLUDED.difficulty,
		   weapon_id=EXCLUDED.weapon_id, skill_id=EXCLUDED.skill_id, description=EXCLUDED.description,
		   updated_at=CURRENT_TIMESTAMP`,
		c.CharacterID, c.Name, c.Role, c.HP, c.Damage, c.Mobility, c.Defense,
		c.SkillPower, c.TerrainDamage, c.Difficulty, c.WeaponID, c.SkillID, c.Description)
	if err != nil {
		return fmt.Errorf("upsert character %s: %w", c.CharacterID, err)
	}
	return nil
}

// DeleteCharacter deletes a character by ID.
func (r *Repository) DeleteCharacter(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_characters WHERE character_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete character %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Weapons CRUD
// ---------------------------------------------------------------------------

// ConfigWeapon represents a row in config_weapons.
type ConfigWeapon struct {
	WeaponID        string
	Name            string
	Damage          int
	ExplosionRadius int
	TerrainDamage   int
	ProjectileWeight float64
	WindInfluence   float64
	MultiHit        int
	Description     string
}

// GetWeapons returns all weapons ordered by weapon_id.
func (r *Repository) GetWeapons(ctx context.Context) ([]ConfigWeapon, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT weapon_id, name, damage, explosion_radius, terrain_damage,
		        projectile_weight, wind_influence, multi_hit, description
		 FROM config_weapons ORDER BY weapon_id`)
	if err != nil {
		return nil, fmt.Errorf("query weapons: %w", err)
	}
	defer rows.Close()

	var weapons []ConfigWeapon
	for rows.Next() {
		var w ConfigWeapon
		if err := rows.Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius,
			&w.TerrainDamage, &w.ProjectileWeight, &w.WindInfluence, &w.MultiHit,
			&w.Description); err != nil {
			return nil, fmt.Errorf("scan weapon: %w", err)
		}
		weapons = append(weapons, w)
	}
	return weapons, rows.Err()
}

// GetWeapon returns a single weapon by ID.
func (r *Repository) GetWeapon(ctx context.Context, id string) (*ConfigWeapon, error) {
	var w ConfigWeapon
	err := r.db.Pool.QueryRow(ctx,
		`SELECT weapon_id, name, damage, explosion_radius, terrain_damage,
		        projectile_weight, wind_influence, multi_hit, description
		 FROM config_weapons WHERE weapon_id = $1`, id).
		Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius,
			&w.TerrainDamage, &w.ProjectileWeight, &w.WindInfluence, &w.MultiHit,
			&w.Description)
	if err != nil {
		return nil, fmt.Errorf("get weapon %s: %w", id, err)
	}
	return &w, nil
}

// UpsertWeapon inserts or updates a weapon.
func (r *Repository) UpsertWeapon(ctx context.Context, w *ConfigWeapon) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_weapons
		 (weapon_id, name, damage, explosion_radius, terrain_damage,
		  projectile_weight, wind_influence, multi_hit, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CURRENT_TIMESTAMP)
		 ON CONFLICT (weapon_id) DO UPDATE SET
		   name=EXCLUDED.name, damage=EXCLUDED.damage, explosion_radius=EXCLUDED.explosion_radius,
		   terrain_damage=EXCLUDED.terrain_damage, projectile_weight=EXCLUDED.projectile_weight,
		   wind_influence=EXCLUDED.wind_influence, multi_hit=EXCLUDED.multi_hit,
		   description=EXCLUDED.description, updated_at=CURRENT_TIMESTAMP`,
		w.WeaponID, w.Name, w.Damage, w.ExplosionRadius, w.TerrainDamage,
		w.ProjectileWeight, w.WindInfluence, w.MultiHit, w.Description)
	if err != nil {
		return fmt.Errorf("upsert weapon %s: %w", w.WeaponID, err)
	}
	return nil
}

// DeleteWeapon deletes a weapon by ID.
func (r *Repository) DeleteWeapon(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_weapons WHERE weapon_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete weapon %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Skills CRUD
// ---------------------------------------------------------------------------

// ConfigSkill represents a row in config_skills.
type ConfigSkill struct {
	SkillID          string
	CharacterID      string
	Name             string
	CooldownTurn     int
	EffectType       string
	ProjectileCount  int
	StatusEffectID   string
	DamageMultiplier float64
	Description      string
}

// GetSkills returns all skills ordered by skill_id.
func (r *Repository) GetSkills(ctx context.Context) ([]ConfigSkill, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT skill_id, character_id, name, cooldown_turn, effect_type,
		        projectile_count, status_effect_id, damage_multiplier, description
		 FROM config_skills ORDER BY skill_id`)
	if err != nil {
		return nil, fmt.Errorf("query skills: %w", err)
	}
	defer rows.Close()

	var skills []ConfigSkill
	for rows.Next() {
		var s ConfigSkill
		if err := rows.Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.CooldownTurn,
			&s.EffectType, &s.ProjectileCount, &s.StatusEffectID, &s.DamageMultiplier,
			&s.Description); err != nil {
			return nil, fmt.Errorf("scan skill: %w", err)
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

// GetSkill returns a single skill by ID.
func (r *Repository) GetSkill(ctx context.Context, id string) (*ConfigSkill, error) {
	var s ConfigSkill
	err := r.db.Pool.QueryRow(ctx,
		`SELECT skill_id, character_id, name, cooldown_turn, effect_type,
		        projectile_count, status_effect_id, damage_multiplier, description
		 FROM config_skills WHERE skill_id = $1`, id).
		Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.CooldownTurn,
			&s.EffectType, &s.ProjectileCount, &s.StatusEffectID, &s.DamageMultiplier,
			&s.Description)
	if err != nil {
		return nil, fmt.Errorf("get skill %s: %w", id, err)
	}
	return &s, nil
}

// UpsertSkill inserts or updates a skill.
func (r *Repository) UpsertSkill(ctx context.Context, s *ConfigSkill) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_skills
		 (skill_id, character_id, name, cooldown_turn, effect_type,
		  projectile_count, status_effect_id, damage_multiplier, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CURRENT_TIMESTAMP)
		 ON CONFLICT (skill_id) DO UPDATE SET
		   character_id=EXCLUDED.character_id, name=EXCLUDED.name,
		   cooldown_turn=EXCLUDED.cooldown_turn, effect_type=EXCLUDED.effect_type,
		   projectile_count=EXCLUDED.projectile_count, status_effect_id=EXCLUDED.status_effect_id,
		   damage_multiplier=EXCLUDED.damage_multiplier, description=EXCLUDED.description,
		   updated_at=CURRENT_TIMESTAMP`,
		s.SkillID, s.CharacterID, s.Name, s.CooldownTurn, s.EffectType,
		s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier, s.Description)
	if err != nil {
		return fmt.Errorf("upsert skill %s: %w", s.SkillID, err)
	}
	return nil
}

// GetSkillByCharacterID returns the skill for a given character.
func (r *Repository) GetSkillByCharacterID(ctx context.Context, characterID string) (*ConfigSkill, error) {
	var s ConfigSkill
	err := r.db.Pool.QueryRow(ctx,
		`SELECT skill_id, character_id, name, cooldown_turn, effect_type,
		        projectile_count, status_effect_id, damage_multiplier, description
		 FROM config_skills WHERE character_id = $1`, characterID).
		Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.CooldownTurn,
			&s.EffectType, &s.ProjectileCount, &s.StatusEffectID, &s.DamageMultiplier,
			&s.Description)
	if err != nil {
		return nil, fmt.Errorf("get skill for character %s: %w", characterID, err)
	}
	return &s, nil
}

// DeleteSkill deletes a skill by ID.
func (r *Repository) DeleteSkill(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_skills WHERE skill_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete skill %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Items CRUD
// ---------------------------------------------------------------------------

// ConfigItem represents a row in config_items.
type ConfigItem struct {
	ItemID         string
	Name           string
	Type           string
	TargetType     string
	Value          float64
	MaxUsePerMatch int
	Cooldown       int
	Description    string
}

// GetItems returns all items ordered by item_id.
func (r *Repository) GetItems(ctx context.Context) ([]ConfigItem, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT item_id, name, type, target_type, value, max_use_per_match, cooldown, description
		 FROM config_items ORDER BY item_id`)
	if err != nil {
		return nil, fmt.Errorf("query items: %w", err)
	}
	defer rows.Close()

	var items []ConfigItem
	for rows.Next() {
		var i ConfigItem
		if err := rows.Scan(&i.ItemID, &i.Name, &i.Type, &i.TargetType, &i.Value,
			&i.MaxUsePerMatch, &i.Cooldown, &i.Description); err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// GetItem returns a single item by ID.
func (r *Repository) GetItem(ctx context.Context, id string) (*ConfigItem, error) {
	var i ConfigItem
	err := r.db.Pool.QueryRow(ctx,
		`SELECT item_id, name, type, target_type, value, max_use_per_match, cooldown, description
		 FROM config_items WHERE item_id = $1`, id).
		Scan(&i.ItemID, &i.Name, &i.Type, &i.TargetType, &i.Value,
			&i.MaxUsePerMatch, &i.Cooldown, &i.Description)
	if err != nil {
		return nil, fmt.Errorf("get item %s: %w", id, err)
	}
	return &i, nil
}

// UpsertItem inserts or updates an item.
func (r *Repository) UpsertItem(ctx context.Context, i *ConfigItem) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_items
		 (item_id, name, type, target_type, value, max_use_per_match, cooldown, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8, CURRENT_TIMESTAMP)
		 ON CONFLICT (item_id) DO UPDATE SET
		   name=EXCLUDED.name, type=EXCLUDED.type, target_type=EXCLUDED.target_type,
		   value=EXCLUDED.value, max_use_per_match=EXCLUDED.max_use_per_match,
		   cooldown=EXCLUDED.cooldown, description=EXCLUDED.description,
		   updated_at=CURRENT_TIMESTAMP`,
		i.ItemID, i.Name, i.Type, i.TargetType, i.Value,
		i.MaxUsePerMatch, i.Cooldown, i.Description)
	if err != nil {
		return fmt.Errorf("upsert item %s: %w", i.ItemID, err)
	}
	return nil
}

// DeleteItem deletes an item by ID.
func (r *Repository) DeleteItem(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_items WHERE item_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete item %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Maps CRUD
// ---------------------------------------------------------------------------

// ConfigMap represents a row in config_maps.
type ConfigMap struct {
	MapID                 string
	Name                  string
	Width                 int
	Height                int
	GridWidth             int
	GridHeight            int
	CellSize              int
	DefaultWindPowerRange json.RawMessage
	TerrainLayers         json.RawMessage
	SpawnPoints           json.RawMessage
	Tiles                 json.RawMessage
	Description           string
	MinRankTier           string
}

// GetMaps returns all maps ordered by map_id.
func (r *Repository) GetMaps(ctx context.Context) ([]ConfigMap, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier
		 FROM config_maps ORDER BY map_id`)
	if err != nil {
		return nil, fmt.Errorf("query maps: %w", err)
	}
	defer rows.Close()

	var maps []ConfigMap
	for rows.Next() {
		var m ConfigMap
		if err := rows.Scan(&m.MapID, &m.Name, &m.Width, &m.Height,
			&m.GridWidth, &m.GridHeight, &m.CellSize,
			&m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints,
			&m.Tiles, &m.Description, &m.MinRankTier); err != nil {
			return nil, fmt.Errorf("scan map: %w", err)
		}
		maps = append(maps, m)
	}
	return maps, rows.Err()
}

// GetMap returns a single map by ID.
func (r *Repository) GetMap(ctx context.Context, id string) (*ConfigMap, error) {
	var m ConfigMap
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, width, height, grid_width, grid_height, cell_size,
		        default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&m.MapID, &m.Name, &m.Width, &m.Height,
			&m.GridWidth, &m.GridHeight, &m.CellSize,
			&m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints,
			&m.Tiles, &m.Description, &m.MinRankTier)
	if err != nil {
		return nil, fmt.Errorf("get map %s: %w", id, err)
	}
	return &m, nil
}

// UpsertMap inserts or updates a map.
func (r *Repository) UpsertMap(ctx context.Context, m *ConfigMap) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_maps
		 (map_id, name, width, height, grid_width, grid_height, cell_size,
		  default_wind_power_range, terrain_layers, spawn_points, tiles, description, min_rank_tier, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, CURRENT_TIMESTAMP)
		 ON CONFLICT (map_id) DO UPDATE SET
		   name=EXCLUDED.name, width=EXCLUDED.width, height=EXCLUDED.height,
		   grid_width=EXCLUDED.grid_width, grid_height=EXCLUDED.grid_height,
		   cell_size=EXCLUDED.cell_size,
		   default_wind_power_range=EXCLUDED.default_wind_power_range,
		   terrain_layers=EXCLUDED.terrain_layers, spawn_points=EXCLUDED.spawn_points,
		   tiles=EXCLUDED.tiles,
		   description=EXCLUDED.description, min_rank_tier=EXCLUDED.min_rank_tier,
		   updated_at=CURRENT_TIMESTAMP`,
		m.MapID, m.Name, m.Width, m.Height, m.GridWidth, m.GridHeight, m.CellSize,
		m.DefaultWindPowerRange, m.TerrainLayers, m.SpawnPoints, m.Tiles, m.Description, m.MinRankTier)
	if err != nil {
		return fmt.Errorf("upsert map %s: %w", m.MapID, err)
	}
	return nil
}

// DeleteMap deletes a map by ID.
func (r *Repository) DeleteMap(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_maps WHERE map_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete map %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Brick Types CRUD
// ---------------------------------------------------------------------------

// ConfigBrickType represents a row in config_brick_types.
type ConfigBrickType struct {
	BrickTypeID  int
	Name         string
	ImageID      string
	Destructible bool
	Border       json.RawMessage
	Color        string
}

// GetBrickTypes returns all brick types ordered by brick_type_id.
func (r *Repository) GetBrickTypes(ctx context.Context) ([]ConfigBrickType, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT brick_type_id, name, image_id, destructible, border, color
		 FROM config_brick_types ORDER BY brick_type_id`)
	if err != nil {
		return nil, fmt.Errorf("query brick types: %w", err)
	}
	defer rows.Close()

	var types []ConfigBrickType
	for rows.Next() {
		var bt ConfigBrickType
		if err := rows.Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible,
			&bt.Border, &bt.Color); err != nil {
			return nil, fmt.Errorf("scan brick type: %w", err)
		}
		types = append(types, bt)
	}
	return types, rows.Err()
}

// GetBrickType returns a single brick type by ID.
func (r *Repository) GetBrickType(ctx context.Context, id int) (*ConfigBrickType, error) {
	var bt ConfigBrickType
	err := r.db.Pool.QueryRow(ctx,
		`SELECT brick_type_id, name, image_id, destructible, border, color
		 FROM config_brick_types WHERE brick_type_id = $1`, id).
		Scan(&bt.BrickTypeID, &bt.Name, &bt.ImageID, &bt.Destructible,
			&bt.Border, &bt.Color)
	if err != nil {
		return nil, fmt.Errorf("get brick type %d: %w", id, err)
	}
	return &bt, nil
}

// InsertBrickType inserts a new brick type and returns the generated serial PK.
func (r *Repository) InsertBrickType(ctx context.Context, bt *ConfigBrickType) (int, error) {
	var id int
	err := r.db.Pool.QueryRow(ctx,
		`INSERT INTO config_brick_types (name, image_id, destructible, border, color)
		 VALUES ($1, $2, $3, $4, $5) RETURNING brick_type_id`,
		bt.Name, bt.ImageID, bt.Destructible, bt.Border, bt.Color).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert brick type: %w", err)
	}
	return id, nil
}

// UpdateBrickType updates an existing brick type by ID.
func (r *Repository) UpdateBrickType(ctx context.Context, bt *ConfigBrickType) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE config_brick_types SET name=$1, image_id=$2, destructible=$3,
		 border=$4, color=$5, updated_at=CURRENT_TIMESTAMP
		 WHERE brick_type_id=$6`,
		bt.Name, bt.ImageID, bt.Destructible, bt.Border, bt.Color, bt.BrickTypeID)
	if err != nil {
		return fmt.Errorf("update brick type %d: %w", bt.BrickTypeID, err)
	}
	return nil
}

// DeleteBrickType deletes a brick type by ID.
func (r *Repository) DeleteBrickType(ctx context.Context, id int) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_brick_types WHERE brick_type_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete brick type %d: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Map Tiles (Editor data)
// ---------------------------------------------------------------------------

// MapTilesData holds the tile grid and spawn points for the editor API.
type MapTilesData struct {
	MapID                 string          `json:"mapId"`
	Name                  string          `json:"name"`
	GridWidth             int             `json:"gridWidth"`
	GridHeight            int             `json:"gridHeight"`
	CellSize              int             `json:"cellSize"`
	DefaultWindPowerRange json.RawMessage `json:"defaultWindPowerRange"`
	Tiles                 json.RawMessage `json:"tiles"`
	SpawnPoints           json.RawMessage `json:"spawnPoints"`
	MinRankTier           string          `json:"minRankTier"`
	Description           string          `json:"description"`
}

// GetMapTiles returns tiles data for the map editor.
func (r *Repository) GetMapTiles(ctx context.Context, id string) (*MapTilesData, error) {
	var d MapTilesData
	err := r.db.Pool.QueryRow(ctx,
		`SELECT map_id, name, grid_width, grid_height, cell_size,
		        default_wind_power_range, tiles, spawn_points, min_rank_tier, description
		 FROM config_maps WHERE map_id = $1`, id).
		Scan(&d.MapID, &d.Name, &d.GridWidth, &d.GridHeight, &d.CellSize,
			&d.DefaultWindPowerRange, &d.Tiles, &d.SpawnPoints, &d.MinRankTier, &d.Description)
	if err != nil {
		return nil, fmt.Errorf("get map tiles %s: %w", id, err)
	}
	return &d, nil
}

// SaveMapFull saves the full map record (delegates to UpsertMap).
func (r *Repository) SaveMapFull(ctx context.Context, m *ConfigMap) error {
	return r.UpsertMap(ctx, m)
}

// ---------------------------------------------------------------------------
// Shop Offers
// ---------------------------------------------------------------------------

// ShopOffer represents a row in shop_offers.
type ShopOffer struct {
	OfferID       string
	ItemID        string
	OfferType     string
	PriceCurrency string
	PriceAmount   int
	Quantity      int
	LimitPerPlayer *int
	IsActive      bool
}

// GetShopOffers returns all shop offers ordered by offer_id.
func (r *Repository) GetShopOffers(ctx context.Context) ([]ShopOffer, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT offer_id, item_id, offer_type, price_currency, price_amount,
		        quantity, limit_per_player, is_active
		 FROM shop_offers ORDER BY offer_id`)
	if err != nil {
		return nil, fmt.Errorf("query shop offers: %w", err)
	}
	defer rows.Close()

	var offers []ShopOffer
	for rows.Next() {
		var o ShopOffer
		if err := rows.Scan(&o.OfferID, &o.ItemID, &o.OfferType, &o.PriceCurrency,
			&o.PriceAmount, &o.Quantity, &o.LimitPerPlayer, &o.IsActive); err != nil {
			return nil, fmt.Errorf("scan shop offer: %w", err)
		}
		offers = append(offers, o)
	}
	return offers, rows.Err()
}

// GetShopOffer returns a single shop offer by ID.
func (r *Repository) GetShopOffer(ctx context.Context, id string) (*ShopOffer, error) {
	var o ShopOffer
	err := r.db.Pool.QueryRow(ctx,
		`SELECT offer_id, item_id, offer_type, price_currency, price_amount,
		        quantity, limit_per_player, is_active
		 FROM shop_offers WHERE offer_id = $1`, id).
		Scan(&o.OfferID, &o.ItemID, &o.OfferType, &o.PriceCurrency,
			&o.PriceAmount, &o.Quantity, &o.LimitPerPlayer, &o.IsActive)
	if err != nil {
		return nil, fmt.Errorf("get shop offer %s: %w", id, err)
	}
	return &o, nil
}

// UpsertShopOffer inserts or updates a shop offer.
func (r *Repository) UpsertShopOffer(ctx context.Context, o *ShopOffer) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO shop_offers
		 (offer_id, item_id, offer_type, price_currency, price_amount,
		  quantity, limit_per_player, is_active)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (offer_id) DO UPDATE SET
		   item_id=EXCLUDED.item_id, offer_type=EXCLUDED.offer_type,
		   price_currency=EXCLUDED.price_currency, price_amount=EXCLUDED.price_amount,
		   quantity=EXCLUDED.quantity, limit_per_player=EXCLUDED.limit_per_player,
		   is_active=EXCLUDED.is_active`,
		o.OfferID, o.ItemID, o.OfferType, o.PriceCurrency,
		o.PriceAmount, o.Quantity, o.LimitPerPlayer, o.IsActive)
	if err != nil {
		return fmt.Errorf("upsert shop offer %s: %w", o.OfferID, err)
	}
	return nil
}

// DeleteShopOffer deletes a shop offer by ID.
func (r *Repository) DeleteShopOffer(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM shop_offers WHERE offer_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete shop offer %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Players
// ---------------------------------------------------------------------------

// PlayerInfo holds player data for the admin player list.
type PlayerInfo struct {
	PlayerID    string
	AccountID   string
	DisplayName string
	Level       int
	Coins       int
	Gems        int
	CreatedAt   time.Time
	IsBanned    bool
}

// GetPlayers returns a paginated list of players with optional search and total count.
func (r *Repository) GetPlayers(ctx context.Context, search string, page, limit int) ([]PlayerInfo, int, error) {
	// Build WHERE clause
	var whereClauses []string
	var args []interface{}
	argIdx := 1

	if search != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("pp.display_name ILIKE '%%' || $%d || '%%'", argIdx))
		args = append(args, search)
		argIdx++
	}

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Count total
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM player_profiles pp %s`, whereSQL)
	var total int
	if err := r.db.Pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count players: %w", err)
	}

	// Fetch page
	offset := (page - 1) * limit
	dataQuery := fmt.Sprintf(
		`SELECT pp.player_id, pp.account_id, pp.display_name, pp.level, pp.coin, pp.gem, pp.created_at,
		        EXISTS(
		            SELECT 1 FROM account_bans ab
		            WHERE ab.account_id = pp.account_id
		              AND ab.status = 'active'
		              AND (ab.ends_at IS NULL OR ab.ends_at > CURRENT_TIMESTAMP)
		        ) AS is_banned
		 FROM player_profiles pp
		 %s
		 ORDER BY pp.created_at DESC
		 LIMIT $%d OFFSET $%d`,
		whereSQL, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Pool.Query(ctx, dataQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query players: %w", err)
	}
	defer rows.Close()

	var players []PlayerInfo
	for rows.Next() {
		var p PlayerInfo
		if err := rows.Scan(&p.PlayerID, &p.AccountID, &p.DisplayName, &p.Level,
			&p.Coins, &p.Gems, &p.CreatedAt, &p.IsBanned); err != nil {
			return nil, 0, fmt.Errorf("scan player: %w", err)
		}
		players = append(players, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return players, total, nil
}

// BanPlayer creates an active ban for the given account.
func (r *Repository) BanPlayer(ctx context.Context, accountID, reason string) error {
	// Look up the player_id for this account
	var playerID string
	err := r.db.Pool.QueryRow(ctx,
		`SELECT player_id FROM player_profiles WHERE account_id = $1`, accountID).Scan(&playerID)
	if err != nil {
		return fmt.Errorf("find player for account %s: %w", accountID, err)
	}

	banID := generateID()
	_, err = r.db.Pool.Exec(ctx,
		`INSERT INTO account_bans (ban_id, account_id, player_id, reason_code, reason_text, source, status)
		 VALUES ($1, $2, $3, 'admin_ban', $4, 'moderator', 'active')`,
		banID, accountID, playerID, reason)
	if err != nil {
		return fmt.Errorf("ban account %s: %w", accountID, err)
	}
	return nil
}

// UnbanPlayer deactivates all active bans for the given account.
func (r *Repository) UnbanPlayer(ctx context.Context, accountID string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE account_bans SET status = 'revoked' WHERE account_id = $1 AND status = 'active'`,
		accountID)
	if err != nil {
		return fmt.Errorf("unban account %s: %w", accountID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Matchmaking JSON Settings
// ---------------------------------------------------------------------------

// GetJSONSetting returns the raw JSON value of a single game_settings row by key.
func (r *Repository) GetJSONSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

// UpsertJSONSetting updates the value of a game_settings row by key.
func (r *Repository) UpsertJSONSetting(ctx context.Context, key, value string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE game_settings SET value = $1, updated_at = CURRENT_TIMESTAMP WHERE key = $2`,
		value, key)
	if err != nil {
		return fmt.Errorf("update setting %s: %w", key, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Equipment Items
// ---------------------------------------------------------------------------

// GetAllEquipmentItems returns all rows from config_equipment_items as generic maps.
func (r *Repository) GetAllEquipmentItems(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT item_id, name, slot, category, tier, required_level, character_id,
		        gem_slots, stat_hp, stat_damage, stat_defense, stat_crit,
		        stat_move_energy, price_coin, price_gem, is_active
		 FROM config_equipment_items ORDER BY item_id`)
	if err != nil {
		return nil, fmt.Errorf("query equipment items: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var (
			itemID, name, slot, category string
			tier, characterID            *string
			requiredLevel, gemSlots      int
			statHP, statDMG, statDEF     int
			statCrit                     float64
			statMoveEnergy               int
			priceCoin, priceGem          int
			isActive                     bool
		)
		if err := rows.Scan(&itemID, &name, &slot, &category, &tier, &requiredLevel, &characterID,
			&gemSlots, &statHP, &statDMG, &statDEF, &statCrit, &statMoveEnergy,
			&priceCoin, &priceGem, &isActive); err != nil {
			return nil, fmt.Errorf("scan equipment item: %w", err)
		}
		m := map[string]interface{}{
			"ItemID":        itemID,
			"Name":          name,
			"Slot":          slot,
			"Category":      category,
			"Tier":          tier,
			"RequiredLevel": requiredLevel,
			"CharacterID":   characterID,
			"GemSlots":      gemSlots,
			"StatHP":        statHP,
			"StatDMG":       statDMG,
			"StatDEF":       statDEF,
			"StatCrit":      statCrit,
			"StatMoveEnergy": statMoveEnergy,
			"PriceCoin":     priceCoin,
			"PriceGem":      priceGem,
			"IsActive":      isActive,
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// UpsertEquipmentItem inserts or updates a config_equipment_items row.
func (r *Repository) UpsertEquipmentItem(ctx context.Context,
	itemID, name, slot, category string,
	tier, characterID *string,
	requiredLevel, gemSlots, statHP, statDMG, statDEF, statMoveEnergy, priceCoin, priceGem int,
	statCrit float64,
	isActive bool,
) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_equipment_items
		 (item_id, name, slot, category, tier, required_level, character_id,
		  gem_slots, stat_hp, stat_damage, stat_defense, stat_crit,
		  stat_move_energy, price_coin, price_gem, is_active, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16, CURRENT_TIMESTAMP)
		 ON CONFLICT (item_id) DO UPDATE SET
		   name=EXCLUDED.name, slot=EXCLUDED.slot, category=EXCLUDED.category,
		   tier=EXCLUDED.tier, required_level=EXCLUDED.required_level,
		   character_id=EXCLUDED.character_id, gem_slots=EXCLUDED.gem_slots,
		   stat_hp=EXCLUDED.stat_hp, stat_damage=EXCLUDED.stat_damage,
		   stat_defense=EXCLUDED.stat_defense, stat_crit=EXCLUDED.stat_crit,
		   stat_move_energy=EXCLUDED.stat_move_energy,
		   price_coin=EXCLUDED.price_coin, price_gem=EXCLUDED.price_gem,
		   is_active=EXCLUDED.is_active, updated_at=CURRENT_TIMESTAMP`,
		itemID, name, slot, category, tier, requiredLevel, characterID,
		gemSlots, statHP, statDMG, statDEF, statCrit, statMoveEnergy,
		priceCoin, priceGem, isActive)
	if err != nil {
		return fmt.Errorf("upsert equipment item %s: %w", itemID, err)
	}
	return nil
}

// DeleteEquipmentItem deletes a config_equipment_items row by item_id.
func (r *Repository) DeleteEquipmentItem(ctx context.Context, itemID string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_equipment_items WHERE item_id = $1`, itemID)
	if err != nil {
		return fmt.Errorf("delete equipment item %s: %w", itemID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Upgrade Rates
// ---------------------------------------------------------------------------

// GetAllUpgradeRates returns all rows from config_upgrade_rates ordered by from_level.
func (r *Repository) GetAllUpgradeRates(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
		 FROM config_upgrade_rates ORDER BY from_level, to_level`)
	if err != nil {
		return nil, fmt.Errorf("query upgrade rates: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var fromLevel, toLevel, cost, failReset int
		var maxPct float64
		if err := rows.Scan(&fromLevel, &toLevel, &cost, &maxPct, &failReset); err != nil {
			return nil, fmt.Errorf("scan upgrade rate: %w", err)
		}
		results = append(results, map[string]interface{}{
			"FromLevel": fromLevel,
			"ToLevel":   toLevel,
			"Cost":      cost,
			"MaxPct":    maxPct,
			"FailReset": failReset,
		})
	}
	return results, rows.Err()
}

// UpsertUpgradeRate inserts or updates a config_upgrade_rates row.
func (r *Repository) UpsertUpgradeRate(ctx context.Context, from, to, cost int, maxPct float64, failReset int) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (from_level, to_level) DO UPDATE SET
		   upgrade_cost=EXCLUDED.upgrade_cost, max_percent=EXCLUDED.max_percent,
		   fail_reset_to=EXCLUDED.fail_reset_to`,
		from, to, cost, maxPct, failReset)
	if err != nil {
		return fmt.Errorf("upsert upgrade rate %d->%d: %w", from, to, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Stone Configs
// ---------------------------------------------------------------------------

// GetAllStoneConfigs returns all rows from config_stones ordered by stone_level.
func (r *Repository) GetAllStoneConfigs(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT stone_level, power, price_coin, price_gem, source
		 FROM config_stones ORDER BY stone_level`)
	if err != nil {
		return nil, fmt.Errorf("query stone configs: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var level, power, priceCoin, priceGem int
		var source string
		if err := rows.Scan(&level, &power, &priceCoin, &priceGem, &source); err != nil {
			return nil, fmt.Errorf("scan stone config: %w", err)
		}
		results = append(results, map[string]interface{}{
			"StoneLevel": level,
			"Power":      power,
			"PriceCoin":  priceCoin,
			"PriceGem":   priceGem,
			"Source":     source,
		})
	}
	return results, rows.Err()
}

// UpsertStoneConfig inserts or updates a config_stones row.
func (r *Repository) UpsertStoneConfig(ctx context.Context, level, power, coin, gem int, source string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_stones (stone_level, power, price_coin, price_gem, source)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (stone_level) DO UPDATE SET
		   power=EXCLUDED.power, price_coin=EXCLUDED.price_coin,
		   price_gem=EXCLUDED.price_gem, source=EXCLUDED.source`,
		level, power, coin, gem, source)
	if err != nil {
		return fmt.Errorf("upsert stone config level %d: %w", level, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Gem Configs
// ---------------------------------------------------------------------------

// GetAllGemConfigs returns all rows from config_gems ordered by gem_type, gem_level.
func (r *Repository) GetAllGemConfigs(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT gem_type, gem_level, stat_value
		 FROM config_gems ORDER BY gem_type, gem_level`)
	if err != nil {
		return nil, fmt.Errorf("query gem configs: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var gemType string
		var gemLevel int
		var statValue float64
		if err := rows.Scan(&gemType, &gemLevel, &statValue); err != nil {
			return nil, fmt.Errorf("scan gem config: %w", err)
		}
		results = append(results, map[string]interface{}{
			"GemType":   gemType,
			"GemLevel":  gemLevel,
			"StatValue": statValue,
		})
	}
	return results, rows.Err()
}

// UpsertGemConfig inserts or updates a config_gems row.
func (r *Repository) UpsertGemConfig(ctx context.Context, gemType string, gemLevel int, statValue float64) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_gems (gem_type, gem_level, stat_value)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (gem_type, gem_level) DO UPDATE SET stat_value=EXCLUDED.stat_value`,
		gemType, gemLevel, statValue)
	if err != nil {
		return fmt.Errorf("upsert gem config %s/%d: %w", gemType, gemLevel, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Set Bonuses
// ---------------------------------------------------------------------------

// GetAllSetBonuses returns all rows from config_set_bonuses ordered by tier, pieces_required.
func (r *Repository) GetAllSetBonuses(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		 FROM config_set_bonuses ORDER BY tier, pieces_required`)
	if err != nil {
		return nil, fmt.Errorf("query set bonuses: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var tier string
		var pieces int
		var hp, dmg, def, crit float64
		if err := rows.Scan(&tier, &pieces, &hp, &dmg, &def, &crit); err != nil {
			return nil, fmt.Errorf("scan set bonus: %w", err)
		}
		results = append(results, map[string]interface{}{
			"Tier":   tier,
			"Pieces": pieces,
			"HP":     hp,
			"DMG":    dmg,
			"DEF":    def,
			"Crit":   crit,
		})
	}
	return results, rows.Err()
}

// UpsertSetBonus inserts or updates a config_set_bonuses row.
func (r *Repository) UpsertSetBonus(ctx context.Context, tier string, pieces int, hp, dmg, def, crit float64) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (tier, pieces_required) DO UPDATE SET
		   bonus_hp_pct=EXCLUDED.bonus_hp_pct, bonus_dmg_pct=EXCLUDED.bonus_dmg_pct,
		   bonus_def_pct=EXCLUDED.bonus_def_pct, bonus_crit_pct=EXCLUDED.bonus_crit_pct`,
		tier, pieces, hp, dmg, def, crit)
	if err != nil {
		return fmt.Errorf("upsert set bonus %s/%d: %w", tier, pieces, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Materials
// ---------------------------------------------------------------------------

// GetAllMaterialConfigs returns all rows from config_materials ordered by tier, material_id.
func (r *Repository) GetAllMaterialConfigs(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT material_id, name, description, source, price_gem, tier, is_active
		 FROM config_materials ORDER BY tier, material_id`)
	if err != nil {
		return nil, fmt.Errorf("query materials: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var materialID, name, description, source, tier string
		var priceGem int
		var isActive bool
		if err := rows.Scan(&materialID, &name, &description, &source, &priceGem, &tier, &isActive); err != nil {
			return nil, fmt.Errorf("scan material: %w", err)
		}
		results = append(results, map[string]interface{}{
			"MaterialID":  materialID,
			"Name":        name,
			"Description": description,
			"Source":      source,
			"PriceGem":    priceGem,
			"Tier":        tier,
			"IsActive":    isActive,
		})
	}
	return results, rows.Err()
}

// UpsertMaterial inserts or updates a config_materials row.
func (r *Repository) UpsertMaterial(ctx context.Context, materialID, name, description, source string, priceGem int, tier string, isActive bool) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_materials (material_id, name, description, source, price_gem, tier, is_active)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (material_id) DO UPDATE SET
		   name=EXCLUDED.name, description=EXCLUDED.description,
		   source=EXCLUDED.source, price_gem=EXCLUDED.price_gem,
		   tier=EXCLUDED.tier, is_active=EXCLUDED.is_active`,
		materialID, name, description, source, priceGem, tier, isActive)
	if err != nil {
		return fmt.Errorf("upsert material %s: %w", materialID, err)
	}
	return nil
}

// DeleteMaterial deletes a config_materials row by material_id.
func (r *Repository) DeleteMaterial(ctx context.Context, materialID string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_materials WHERE material_id = $1`, materialID)
	if err != nil {
		return fmt.Errorf("delete material %s: %w", materialID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Crafting Recipes
// ---------------------------------------------------------------------------

// GetAllCraftingRecipes returns all rows from config_crafting_recipes ordered by recipe_id.
func (r *Repository) GetAllCraftingRecipes(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT recipe_id, result_item_id, materials::text, is_active
		 FROM config_crafting_recipes ORDER BY recipe_id`)
	if err != nil {
		return nil, fmt.Errorf("query crafting recipes: %w", err)
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var recipeID, resultItemID, materialsJSON string
		var isActive bool
		if err := rows.Scan(&recipeID, &resultItemID, &materialsJSON, &isActive); err != nil {
			return nil, fmt.Errorf("scan crafting recipe: %w", err)
		}
		results = append(results, map[string]interface{}{
			"RecipeID":     recipeID,
			"ResultItemID": resultItemID,
			"Materials":    materialsJSON,
			"IsActive":     isActive,
		})
	}
	return results, rows.Err()
}

// UpsertCraftingRecipe inserts or updates a config_crafting_recipes row.
func (r *Repository) UpsertCraftingRecipe(ctx context.Context, recipeID, resultItemID, materialsJSON string, isActive bool) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_crafting_recipes (recipe_id, result_item_id, materials, is_active)
		 VALUES ($1,$2,$3::jsonb,$4)
		 ON CONFLICT (recipe_id) DO UPDATE SET
		   result_item_id=EXCLUDED.result_item_id, materials=EXCLUDED.materials,
		   is_active=EXCLUDED.is_active`,
		recipeID, resultItemID, materialsJSON, isActive)
	if err != nil {
		return fmt.Errorf("upsert crafting recipe %s: %w", recipeID, err)
	}
	return nil
}

// DeleteCraftingRecipe deletes a config_crafting_recipes row by recipe_id.
func (r *Repository) DeleteCraftingRecipe(ctx context.Context, recipeID string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_crafting_recipes WHERE recipe_id = $1`, recipeID)
	if err != nil {
		return fmt.Errorf("delete crafting recipe %s: %w", recipeID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Dev Tools
// ---------------------------------------------------------------------------

// ClearRooms removes the rooms:active hash from Redis, returning the number of keys deleted.
func (r *Repository) ClearRooms(ctx context.Context) (int64, error) {
	result, err := r.redis.Client.Del(ctx, "rooms:active").Result()
	if err != nil {
		return 0, fmt.Errorf("clear rooms: %w", err)
	}
	return result, nil
}

// ResetAllData deletes all player-related data from Postgres and clears Redis keys.
func (r *Repository) ResetAllData(ctx context.Context) error {
	tables := []string{
		"match_event_logs", "match_recovery_logs", "match_snapshots", "match_histories",
		"season_reward_claims", "player_ranks", "inventory_reservations", "inventory_items",
		"player_characters", "economy_transactions", "payment_transactions", "shop_purchases",
		"mission_progress", "gift_code_redemptions", "player_reports", "account_bans",
		"player_profiles", "auth_identities", "accounts",
	}

	for _, table := range tables {
		if _, err := r.db.Pool.Exec(ctx, fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	// Clear Redis keys
	r.redis.Client.Del(ctx, "rooms:active", "leaderboard:current")

	return nil
}
