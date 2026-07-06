# Admin Dashboard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a server-rendered admin dashboard for managing game config, players, shop, and dev tools.

**Architecture:** New Go binary `cmd/admin/main.go` using Chi router + `html/template`. Config tables in Postgres replace YAML files. Game/API servers load config from DB with YAML fallback.

**Tech Stack:** Go, Chi, html/template, pgx/v5, PostgreSQL, Redis

---

## File Structure

```
migrations/
  004_admin_config_tables.up.sql          — New config tables + game_settings
  004_admin_config_tables.down.sql        — Drop config tables

cmd/admin/main.go                         — Admin server entry point

internal/admin/
  server.go                               — Router setup, template loading
  repository.go                           — All DB queries for admin
  handlers_dashboard.go                   — Dashboard home page
  handlers_config.go                      — Characters, Weapons, Skills, Items, Maps CRUD
  handlers_physics.go                     — Physics settings key-value editor
  handlers_shop.go                        — Shop offers CRUD
  handlers_players.go                     — Player list, ban/unban
  handlers_devtools.go                    — Clear rooms, reset data, seed config
  seed.go                                 — Seed defaults from YAML + physics constants
  templates/
    layout.html                           — Base layout with sidebar
    dashboard.html                        — Home overview
    config_list.html                      — Generic CRUD table (reused for chars/weapons/skills/items/maps)
    config_edit.html                      — Generic edit form
    physics.html                          — Physics key-value editor
    shop.html                             — Shop offers list
    shop_edit.html                        — Shop offer edit form
    players.html                          — Player list with search
    devtools.html                         — Dev tools page

internal/game/gamedata/
  loader.go                               — Modify: add LoadGameDataFromDB()
  loader.go                               — Modify: add PhysicsConfig struct + loading
```

---

### Task 1: Database Migration — Config Tables

**Files:**
- Create: `migrations/004_admin_config_tables.up.sql`
- Create: `migrations/004_admin_config_tables.down.sql`
- Modify: `cmd/migrate/main.go`

- [ ] **Step 1: Create up migration**

```sql
-- migrations/004_admin_config_tables.up.sql

CREATE TABLE IF NOT EXISTS game_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,
    value_type  TEXT NOT NULL DEFAULT 'number',
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'general',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_characters (
    character_id   TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    role           TEXT NOT NULL,
    hp             INT NOT NULL,
    damage         INT NOT NULL,
    mobility       INT NOT NULL,
    defense        INT NOT NULL,
    skill_power    INT NOT NULL,
    terrain_damage INT NOT NULL,
    difficulty     INT NOT NULL DEFAULT 1,
    weapon_id      TEXT NOT NULL,
    skill_id       TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_weapons (
    weapon_id         TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    damage            INT NOT NULL,
    explosion_radius  INT NOT NULL,
    terrain_damage    INT NOT NULL,
    projectile_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    wind_influence    DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    multi_hit         INT NOT NULL DEFAULT 1,
    description       TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_skills (
    skill_id          TEXT PRIMARY KEY,
    character_id      TEXT NOT NULL,
    name              TEXT NOT NULL,
    cooldown_turn     INT NOT NULL,
    effect_type       TEXT NOT NULL,
    projectile_count  INT NOT NULL DEFAULT 1,
    status_effect_id  TEXT NOT NULL DEFAULT '',
    damage_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    description       TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_items (
    item_id           TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    type              TEXT NOT NULL,
    target_type       TEXT NOT NULL,
    value             DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_use_per_match INT NOT NULL DEFAULT 1,
    cooldown          INT NOT NULL DEFAULT 0,
    description       TEXT NOT NULL DEFAULT '',
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_maps (
    map_id                   TEXT PRIMARY KEY,
    name                     TEXT NOT NULL,
    width                    INT NOT NULL DEFAULT 1600,
    height                   INT NOT NULL DEFAULT 900,
    default_wind_power_range JSONB NOT NULL DEFAULT '[0, 3]',
    terrain_layers           JSONB NOT NULL DEFAULT '[]',
    spawn_points             JSONB NOT NULL DEFAULT '[]',
    description              TEXT NOT NULL DEFAULT '',
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

- [ ] **Step 2: Create down migration**

```sql
-- migrations/004_admin_config_tables.down.sql
DROP TABLE IF EXISTS config_maps;
DROP TABLE IF EXISTS config_items;
DROP TABLE IF EXISTS config_skills;
DROP TABLE IF EXISTS config_weapons;
DROP TABLE IF EXISTS config_characters;
DROP TABLE IF EXISTS game_settings;
```

- [ ] **Step 3: Register migration in migrate tool**

Add to `cmd/migrate/main.go` migrations slice:
```go
filepath.Join("migrations", "004_admin_config_tables.up.sql"),
```

- [ ] **Step 4: Run migration**

```bash
go run cmd/migrate/main.go
```

- [ ] **Step 5: Commit**

```bash
git add migrations/004_admin_config_tables.up.sql migrations/004_admin_config_tables.down.sql cmd/migrate/main.go
git commit -m "feat(admin): add config tables migration"
```

---

### Task 2: Seed Tool — Populate Config Tables from YAML + Physics Defaults

**Files:**
- Create: `internal/admin/seed.go`

- [ ] **Step 1: Create seed.go**

This file reads existing YAML configs and inserts them into DB tables. Also seeds `game_settings` with default physics constants.

```go
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

func SeedAll(ctx context.Context, db *database.PostgresDB, configDir string) error {
	if err := SeedSettings(ctx, db); err != nil {
		return fmt.Errorf("seed settings: %w", err)
	}
	if err := SeedConfigFromYAML(ctx, db, configDir); err != nil {
		return fmt.Errorf("seed config from YAML: %w", err)
	}
	return nil
}

func SeedSettings(ctx context.Context, db *database.PostgresDB) error {
	for _, s := range defaultSettings {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO game_settings (key, value, value_type, description, category)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (key) DO NOTHING`,
			s.Key, s.Value, s.ValueType, s.Description, s.Category,
		)
		if err != nil {
			return fmt.Errorf("seed setting %s: %w", s.Key, err)
		}
	}
	observability.Log.Info().Msg("seeded game_settings defaults")
	return nil
}

func SeedConfigFromYAML(ctx context.Context, db *database.PostgresDB, configDir string) error {
	if err := gamedata.LoadGameData(configDir); err != nil {
		return fmt.Errorf("load YAML: %w", err)
	}

	for _, c := range gamedata.Data.Characters {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_characters (character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			 ON CONFLICT (character_id) DO NOTHING`,
			c.CharacterID, c.Name, c.Role, c.HP, c.Damage, c.Mobility, c.Defense, c.SkillPower, c.TerrainDamage, c.Difficulty, c.WeaponID, c.SkillID,
		)
		if err != nil {
			return fmt.Errorf("seed character %s: %w", c.CharacterID, err)
		}
	}

	for _, w := range gamedata.Data.Weapons {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_weapons (weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			 ON CONFLICT (weapon_id) DO NOTHING`,
			w.WeaponID, w.Name, w.Damage, w.ExplosionRadius, w.TerrainDamage, w.ProjectileWeight, w.WindInfluence, w.MultiHit,
		)
		if err != nil {
			return fmt.Errorf("seed weapon %s: %w", w.WeaponID, err)
		}
	}

	for _, s := range gamedata.Data.Skills {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_skills (skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			 ON CONFLICT (skill_id) DO NOTHING`,
			s.SkillID, s.CharacterID, s.Name, s.CooldownTurn, s.EffectType, s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier,
		)
		if err != nil {
			return fmt.Errorf("seed skill %s: %w", s.SkillID, err)
		}
	}

	for _, i := range gamedata.Data.Items {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_items (item_id, name, type, target_type, value, max_use_per_match, cooldown)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)
			 ON CONFLICT (item_id) DO NOTHING`,
			i.ItemID, i.Name, i.Type, i.TargetType, i.Value, i.MaxUsePerMatch, i.Cooldown,
		)
		if err != nil {
			return fmt.Errorf("seed item %s: %w", i.ItemID, err)
		}
	}

	for _, m := range gamedata.Data.Maps {
		windRange, _ := json.Marshal(m.DefaultWindPowerRange)
		layers, _ := json.Marshal(m.TerrainLayers)
		spawns, _ := json.Marshal(m.SpawnPoints)
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_maps (map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points)
			 VALUES ($1,$2,$3,$4,$5,$6,$7)
			 ON CONFLICT (map_id) DO NOTHING`,
			m.MapID, m.Name, m.Width, m.Height, windRange, layers, spawns,
		)
		if err != nil {
			return fmt.Errorf("seed map %s: %w", m.MapID, err)
		}
	}

	observability.Log.Info().Msg("seeded config tables from YAML")
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/admin/seed.go
git commit -m "feat(admin): add seed tool for config tables"
```

---

### Task 3: Admin Repository — All DB Queries

**Files:**
- Create: `internal/admin/repository.go`

- [ ] **Step 1: Create repository.go**

Contains all CRUD queries for config tables, game_settings, players, shop, and stats.

```go
package admin

import (
	"context"
	"encoding/json"
	"time"

	"battle-squad/internal/shared/database"
)

type Repository struct {
	db    *database.PostgresDB
	redis *database.RedisClient
}

func NewRepository(db *database.PostgresDB, redis *database.RedisClient) *Repository {
	return &Repository{db: db, redis: redis}
}

// --- Dashboard Stats ---

type DashboardStats struct {
	ActiveRooms   int
	TotalPlayers  int
}

func (r *Repository) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	stats := &DashboardStats{}
	r.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM player_profiles").Scan(&stats.TotalPlayers)
	if r.redis != nil {
		count, _ := r.redis.Client.HLen(ctx, "rooms:active").Result()
		stats.ActiveRooms = int(count)
	}
	return stats, nil
}

// --- Game Settings ---

type GameSetting struct {
	Key         string
	Value       string
	ValueType   string
	Description string
	Category    string
	UpdatedAt   time.Time
}

func (r *Repository) GetAllSettings(ctx context.Context) ([]GameSetting, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT key, value, value_type, description, category, updated_at FROM game_settings ORDER BY category, key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var settings []GameSetting
	for rows.Next() {
		var s GameSetting
		if err := rows.Scan(&s.Key, &s.Value, &s.ValueType, &s.Description, &s.Category, &s.UpdatedAt); err != nil {
			return nil, err
		}
		settings = append(settings, s)
	}
	return settings, nil
}

func (r *Repository) UpdateSetting(ctx context.Context, key, value string) error {
	_, err := r.db.Pool.Exec(ctx, "UPDATE game_settings SET value = $1, updated_at = CURRENT_TIMESTAMP WHERE key = $2", value, key)
	return err
}

// --- Generic Config CRUD ---

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

func (r *Repository) GetCharacters(ctx context.Context) ([]ConfigCharacter, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id, description FROM config_characters ORDER BY character_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ConfigCharacter
	for rows.Next() {
		var c ConfigCharacter
		if err := rows.Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage, &c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty, &c.WeaponID, &c.SkillID, &c.Description); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, nil
}

func (r *Repository) GetCharacter(ctx context.Context, id string) (*ConfigCharacter, error) {
	var c ConfigCharacter
	err := r.db.Pool.QueryRow(ctx, "SELECT character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id, description FROM config_characters WHERE character_id = $1", id).
		Scan(&c.CharacterID, &c.Name, &c.Role, &c.HP, &c.Damage, &c.Mobility, &c.Defense, &c.SkillPower, &c.TerrainDamage, &c.Difficulty, &c.WeaponID, &c.SkillID, &c.Description)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) UpsertCharacter(ctx context.Context, c *ConfigCharacter) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_characters (character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, CURRENT_TIMESTAMP)
		 ON CONFLICT (character_id) DO UPDATE SET name=$2, role=$3, hp=$4, damage=$5, mobility=$6, defense=$7, skill_power=$8, terrain_damage=$9, difficulty=$10, weapon_id=$11, skill_id=$12, description=$13, updated_at=CURRENT_TIMESTAMP`,
		c.CharacterID, c.Name, c.Role, c.HP, c.Damage, c.Mobility, c.Defense, c.SkillPower, c.TerrainDamage, c.Difficulty, c.WeaponID, c.SkillID, c.Description,
	)
	return err
}

func (r *Repository) DeleteCharacter(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM config_characters WHERE character_id = $1", id)
	return err
}

// --- Weapons ---

type ConfigWeapon struct {
	WeaponID         string
	Name             string
	Damage           int
	ExplosionRadius  int
	TerrainDamage    int
	ProjectileWeight float64
	WindInfluence    float64
	MultiHit         int
	Description      string
}

func (r *Repository) GetWeapons(ctx context.Context) ([]ConfigWeapon, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit, description FROM config_weapons ORDER BY weapon_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ConfigWeapon
	for rows.Next() {
		var w ConfigWeapon
		if err := rows.Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius, &w.TerrainDamage, &w.ProjectileWeight, &w.WindInfluence, &w.MultiHit, &w.Description); err != nil {
			return nil, err
		}
		items = append(items, w)
	}
	return items, nil
}

func (r *Repository) GetWeapon(ctx context.Context, id string) (*ConfigWeapon, error) {
	var w ConfigWeapon
	err := r.db.Pool.QueryRow(ctx, "SELECT weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit, description FROM config_weapons WHERE weapon_id = $1", id).
		Scan(&w.WeaponID, &w.Name, &w.Damage, &w.ExplosionRadius, &w.TerrainDamage, &w.ProjectileWeight, &w.WindInfluence, &w.MultiHit, &w.Description)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *Repository) UpsertWeapon(ctx context.Context, w *ConfigWeapon) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_weapons (weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CURRENT_TIMESTAMP)
		 ON CONFLICT (weapon_id) DO UPDATE SET name=$2, damage=$3, explosion_radius=$4, terrain_damage=$5, projectile_weight=$6, wind_influence=$7, multi_hit=$8, description=$9, updated_at=CURRENT_TIMESTAMP`,
		w.WeaponID, w.Name, w.Damage, w.ExplosionRadius, w.TerrainDamage, w.ProjectileWeight, w.WindInfluence, w.MultiHit, w.Description,
	)
	return err
}

func (r *Repository) DeleteWeapon(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM config_weapons WHERE weapon_id = $1", id)
	return err
}

// --- Skills ---

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

func (r *Repository) GetSkills(ctx context.Context) ([]ConfigSkill, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier, description FROM config_skills ORDER BY skill_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ConfigSkill
	for rows.Next() {
		var s ConfigSkill
		if err := rows.Scan(&s.SkillID, &s.CharacterID, &s.Name, &s.CooldownTurn, &s.EffectType, &s.ProjectileCount, &s.StatusEffectID, &s.DamageMultiplier, &s.Description); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, nil
}

func (r *Repository) UpsertSkill(ctx context.Context, s *ConfigSkill) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_skills (skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CURRENT_TIMESTAMP)
		 ON CONFLICT (skill_id) DO UPDATE SET character_id=$2, name=$3, cooldown_turn=$4, effect_type=$5, projectile_count=$6, status_effect_id=$7, damage_multiplier=$8, description=$9, updated_at=CURRENT_TIMESTAMP`,
		s.SkillID, s.CharacterID, s.Name, s.CooldownTurn, s.EffectType, s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier, s.Description,
	)
	return err
}

func (r *Repository) DeleteSkill(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM config_skills WHERE skill_id = $1", id)
	return err
}

// --- Items ---

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

func (r *Repository) GetItems(ctx context.Context) ([]ConfigItem, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT item_id, name, type, target_type, value, max_use_per_match, cooldown, description FROM config_items ORDER BY item_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ConfigItem
	for rows.Next() {
		var i ConfigItem
		if err := rows.Scan(&i.ItemID, &i.Name, &i.Type, &i.TargetType, &i.Value, &i.MaxUsePerMatch, &i.Cooldown, &i.Description); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, nil
}

func (r *Repository) UpsertItem(ctx context.Context, i *ConfigItem) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_items (item_id, name, type, target_type, value, max_use_per_match, cooldown, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8, CURRENT_TIMESTAMP)
		 ON CONFLICT (item_id) DO UPDATE SET name=$2, type=$3, target_type=$4, value=$5, max_use_per_match=$6, cooldown=$7, description=$8, updated_at=CURRENT_TIMESTAMP`,
		i.ItemID, i.Name, i.Type, i.TargetType, i.Value, i.MaxUsePerMatch, i.Cooldown, i.Description,
	)
	return err
}

func (r *Repository) DeleteItem(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM config_items WHERE item_id = $1", id)
	return err
}

// --- Maps ---

type ConfigMap struct {
	MapID                string
	Name                 string
	Width                int
	Height               int
	DefaultWindPowerRange json.RawMessage
	TerrainLayers        json.RawMessage
	SpawnPoints          json.RawMessage
	Description          string
}

func (r *Repository) GetMaps(ctx context.Context) ([]ConfigMap, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points, description FROM config_maps ORDER BY map_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ConfigMap
	for rows.Next() {
		var m ConfigMap
		if err := rows.Scan(&m.MapID, &m.Name, &m.Width, &m.Height, &m.DefaultWindPowerRange, &m.TerrainLayers, &m.SpawnPoints, &m.Description); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, nil
}

func (r *Repository) UpsertMap(ctx context.Context, m *ConfigMap) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_maps (map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points, description, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8, CURRENT_TIMESTAMP)
		 ON CONFLICT (map_id) DO UPDATE SET name=$2, width=$3, height=$4, default_wind_power_range=$5, terrain_layers=$6, spawn_points=$7, description=$8, updated_at=CURRENT_TIMESTAMP`,
		m.MapID, m.Name, m.Width, m.Height, m.DefaultWindPowerRange, m.TerrainLayers, m.SpawnPoints, m.Description,
	)
	return err
}

func (r *Repository) DeleteMap(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM config_maps WHERE map_id = $1", id)
	return err
}

// --- Shop Offers ---

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

func (r *Repository) GetShopOffers(ctx context.Context) ([]ShopOffer, error) {
	rows, err := r.db.Pool.Query(ctx, "SELECT offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active FROM shop_offers ORDER BY offer_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ShopOffer
	for rows.Next() {
		var o ShopOffer
		if err := rows.Scan(&o.OfferID, &o.ItemID, &o.OfferType, &o.PriceCurrency, &o.PriceAmount, &o.Quantity, &o.LimitPerPlayer, &o.IsActive); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, nil
}

func (r *Repository) UpsertShopOffer(ctx context.Context, o *ShopOffer) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		 ON CONFLICT (offer_id) DO UPDATE SET item_id=$2, offer_type=$3, price_currency=$4, price_amount=$5, quantity=$6, limit_per_player=$7, is_active=$8`,
		o.OfferID, o.ItemID, o.OfferType, o.PriceCurrency, o.PriceAmount, o.Quantity, o.LimitPerPlayer, o.IsActive,
	)
	return err
}

func (r *Repository) DeleteShopOffer(ctx context.Context, id string) error {
	_, err := r.db.Pool.Exec(ctx, "DELETE FROM shop_offers WHERE offer_id = $1", id)
	return err
}

// --- Players ---

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

func (r *Repository) GetPlayers(ctx context.Context, search string, page, limit int) ([]PlayerInfo, int, error) {
	offset := (page - 1) * limit
	var total int

	countQuery := "SELECT COUNT(*) FROM player_profiles"
	listQuery := `SELECT pp.player_id, pp.account_id, pp.display_name, pp.level, pp.coins, pp.gems, pp.created_at,
		EXISTS(SELECT 1 FROM account_bans WHERE account_id = pp.account_id AND is_active = TRUE AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)) as is_banned
		FROM player_profiles pp ORDER BY pp.created_at DESC LIMIT $1 OFFSET $2`

	if search != "" {
		countQuery = "SELECT COUNT(*) FROM player_profiles WHERE display_name ILIKE '%' || $1 || '%'"
		listQuery = `SELECT pp.player_id, pp.account_id, pp.display_name, pp.level, pp.coins, pp.gems, pp.created_at,
			EXISTS(SELECT 1 FROM account_bans WHERE account_id = pp.account_id AND is_active = TRUE AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)) as is_banned
			FROM player_profiles pp WHERE pp.display_name ILIKE '%' || $1 || '%' ORDER BY pp.created_at DESC LIMIT $2 OFFSET $3`
	}

	if search != "" {
		r.db.Pool.QueryRow(ctx, countQuery, search).Scan(&total)
	} else {
		r.db.Pool.QueryRow(ctx, countQuery).Scan(&total)
	}

	var rows interface{ Next() bool; Scan(...interface{}) error; Close() }
	var err error
	if search != "" {
		rows2, err2 := r.db.Pool.Query(ctx, listQuery, search, limit, offset)
		if err2 != nil {
			return nil, 0, err2
		}
		rows = rows2
		defer rows2.Close()
	} else {
		rows2, err2 := r.db.Pool.Query(ctx, listQuery, limit, offset)
		if err2 != nil {
			return nil, 0, err2
		}
		rows = rows2
		defer rows2.Close()
		_ = err
	}

	var players []PlayerInfo
	for rows.Next() {
		var p PlayerInfo
		if err := rows.Scan(&p.PlayerID, &p.AccountID, &p.DisplayName, &p.Level, &p.Coins, &p.Gems, &p.CreatedAt, &p.IsBanned); err != nil {
			return nil, 0, err
		}
		players = append(players, p)
	}
	return players, total, nil
}

func (r *Repository) BanPlayer(ctx context.Context, accountID, reason string) error {
	banID := "ban-" + accountID[:8]
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO account_bans (ban_id, account_id, reason, banned_by, is_active)
		 VALUES ($1, $2, $3, 'admin', TRUE)`,
		banID, accountID, reason,
	)
	return err
}

func (r *Repository) UnbanPlayer(ctx context.Context, accountID string) error {
	_, err := r.db.Pool.Exec(ctx,
		"UPDATE account_bans SET is_active = FALSE WHERE account_id = $1 AND is_active = TRUE",
		accountID,
	)
	return err
}

// --- Dev Tools ---

func (r *Repository) ClearRooms(ctx context.Context) (int64, error) {
	deleted, err := r.redis.Client.Del(ctx, "rooms:active").Result()
	return deleted, err
}

func (r *Repository) ResetAllData(ctx context.Context) error {
	tables := []string{
		"match_event_logs", "match_recovery_logs", "match_snapshots", "match_histories",
		"season_reward_claims", "player_ranks", "inventory_reservations", "inventory_items",
		"player_characters", "economy_transactions", "payment_transactions", "shop_purchases",
		"mission_progress", "gift_code_redemptions", "player_reports", "account_bans",
		"player_profiles", "auth_identities", "accounts",
	}
	for _, table := range tables {
		r.db.Pool.Exec(ctx, "DELETE FROM "+table)
	}
	r.redis.Client.Del(ctx, "rooms:active")
	r.redis.Client.Del(ctx, "leaderboard:current")
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/admin/repository.go
git commit -m "feat(admin): add repository with all CRUD queries"
```

---

### Task 4: Admin Server + Templates + All Handlers

**Files:**
- Create: `internal/admin/server.go`
- Create: `internal/admin/handlers_dashboard.go`
- Create: `internal/admin/handlers_config.go`
- Create: `internal/admin/handlers_physics.go`
- Create: `internal/admin/handlers_shop.go`
- Create: `internal/admin/handlers_players.go`
- Create: `internal/admin/handlers_devtools.go`
- Create: `internal/admin/templates/layout.html`
- Create: `internal/admin/templates/dashboard.html`
- Create: `internal/admin/templates/config_list.html`
- Create: `internal/admin/templates/config_edit.html`
- Create: `internal/admin/templates/physics.html`
- Create: `internal/admin/templates/shop.html`
- Create: `internal/admin/templates/shop_edit.html`
- Create: `internal/admin/templates/players.html`
- Create: `internal/admin/templates/devtools.html`
- Create: `cmd/admin/main.go`

This is the largest task. All handlers follow the same simple pattern: query repo → render template. Templates use `{{template "layout" .}}` for consistent layout.

- [ ] **Step 1: Create server.go** — Router setup, template loading, static handler
- [ ] **Step 2: Create layout.html** — Base HTML with sidebar nav, CSS, content block
- [ ] **Step 3: Create dashboard handler + template** — Stats overview page
- [ ] **Step 4: Create config handlers + templates** — CRUD for characters, weapons, skills, items, maps (generic list/edit pattern)
- [ ] **Step 5: Create physics handler + template** — Key-value editor with descriptions
- [ ] **Step 6: Create shop handler + template** — Shop offers CRUD
- [ ] **Step 7: Create players handler + template** — Player list with search, ban/unban
- [ ] **Step 8: Create devtools handler + template** — Clear rooms, reset data, seed config buttons
- [ ] **Step 9: Create cmd/admin/main.go** — Entry point: config, DB, Redis, seed on start, serve
- [ ] **Step 10: Build and verify**

```bash
go build ./cmd/admin/...
```

- [ ] **Step 11: Commit**

```bash
git add cmd/admin/ internal/admin/
git commit -m "feat(admin): add admin dashboard server with all pages"
```

---

### Task 5: Integrate Config Loading from DB into Game Server

**Files:**
- Modify: `internal/game/gamedata/loader.go`
- Modify: `internal/game/match/shooting.go`
- Modify: `cmd/game/main.go`

- [ ] **Step 1: Add LoadGameDataFromDB to gamedata/loader.go**

Add a `PhysicsConfig` struct and `LoadGameDataFromDB(db)` function that queries `config_*` tables and `game_settings`. Falls back to YAML if DB tables are empty.

- [ ] **Step 2: Add PhysicsConfig loading**

Replace hardcoded constants in `shooting.go` with `gamedata.Physics.Gravity`, `gamedata.Physics.ProjectileSpeedMultiplier`, etc.

- [ ] **Step 3: Update cmd/game/main.go to load from DB first**

```go
// Try DB first, fall back to YAML
if err := gamedata.LoadGameDataFromDB(db); err != nil {
    log.Warn().Err(err).Msg("failed to load from DB, falling back to YAML")
    if err := gamedata.LoadGameData("configs"); err != nil {
        log.Fatal().Err(err).Msg("failed to load game data")
    }
}
```

- [ ] **Step 4: Build and verify**

```bash
go build ./...
```

- [ ] **Step 5: Commit**

```bash
git add internal/game/gamedata/loader.go internal/game/match/shooting.go cmd/game/main.go
git commit -m "feat: load game config from DB with YAML fallback"
```

---

### Task 6: Run and Verify End-to-End

- [ ] **Step 1: Run migration**

```bash
go run cmd/migrate/main.go
```

- [ ] **Step 2: Start admin server**

```bash
ADMIN_PORT=9000 go run cmd/admin/main.go
```

- [ ] **Step 3: Verify dashboard loads at http://localhost:9000**
- [ ] **Step 4: Verify config tables seeded from YAML**
- [ ] **Step 5: Edit a physics setting and verify it saves**
- [ ] **Step 6: Restart game server and verify it loads config from DB**
- [ ] **Step 7: Final commit**

```bash
git add .
git commit -m "feat(admin): admin dashboard complete with config management"
```
