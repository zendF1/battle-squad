# Battle Squad Admin Dashboard — Design Spec

## Overview

Server-rendered admin dashboard for managing game configuration, player data, and dev tools. Built as a new binary `cmd/admin/main.go` in the existing `battle-squad` repo. No authentication (dev phase). Runs on a dedicated port (default `:9000`).

## Tech Stack

- Go + `html/template` (server-rendered, no JS framework)
- Same Postgres + Redis connections as API/Game servers
- Minimal CSS, no framework — clean functional UI
- Chi router (consistent with existing servers)

## Architecture

```
cmd/admin/main.go          — HTTP server, port :9000
internal/admin/
  handlers/                — HTTP handlers per page
  templates/               — Go HTML templates
    layout.html            — Base layout with sidebar nav
    dashboard.html         — Home/overview
    characters.html        — CRUD
    weapons.html           — CRUD
    skills.html            — CRUD
    items.html             — CRUD
    maps.html              — CRUD
    physics.html           — Key-value editor
    shop.html              — CRUD shop offers
    players.html           — Player list + ban/unban
    devtools.html          — Reset data, clear rooms, seed data
  repository.go            — DB queries for admin operations
  seed.go                  — Seed default physics constants + migrate YAML → DB
```

## DB Schema Changes

### New table: `game_settings`

Key-value store for physics constants, match settings, feature flags.

```sql
CREATE TABLE IF NOT EXISTS game_settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL,            -- JSON-encoded value
    value_type  TEXT NOT NULL DEFAULT 'number', -- number, string, boolean
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL DEFAULT 'general',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### New table: `config_characters`

```sql
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
```

### New table: `config_weapons`

```sql
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
```

### New table: `config_skills`

```sql
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
```

### New table: `config_items`

```sql
CREATE TABLE IF NOT EXISTS config_items (
    item_id          TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    type             TEXT NOT NULL,
    target_type      TEXT NOT NULL,
    value            DOUBLE PRECISION NOT NULL DEFAULT 0,
    max_use_per_match INT NOT NULL DEFAULT 1,
    cooldown         INT NOT NULL DEFAULT 0,
    description      TEXT NOT NULL DEFAULT '',
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### New table: `config_maps`

```sql
CREATE TABLE IF NOT EXISTS config_maps (
    map_id                  TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    width                   INT NOT NULL DEFAULT 1600,
    height                  INT NOT NULL DEFAULT 900,
    default_wind_power_range JSONB NOT NULL DEFAULT '[0, 3]',
    terrain_layers          JSONB NOT NULL DEFAULT '[]',
    spawn_points            JSONB NOT NULL DEFAULT '[]',
    description             TEXT NOT NULL DEFAULT '',
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

## Default Physics Constants

Seeded into `game_settings` on first run. Each has a human-readable description explaining its in-game effect.

| Key | Default | Category | Description |
|-----|---------|----------|-------------|
| `physics.gravity` | `200` | physics | Lực hấp dẫn kéo đạn xuống. Tăng = đạn rơi nhanh, tầm bắn ngắn |
| `physics.projectile_speed_multiplier` | `6.0` | physics | Hệ số tốc độ đạn = power × giá trị này. Tăng = đạn bay xa hơn |
| `physics.wind_scale` | `30.0` | physics | Hệ số ảnh hưởng gió lên đạn. Tăng = gió đẩy đạn lệch nhiều hơn |
| `physics.player_hit_radius` | `24.0` | physics | Bán kính va chạm của player (pixels). Tăng = dễ bắn trúng hơn |
| `physics.time_step` | `0.02` | physics | Bước thời gian mô phỏng vật lý (giây). Giảm = chính xác hơn, tốn CPU hơn |
| `physics.path_record_step` | `0.05` | physics | Khoảng thời gian ghi path đạn cho animation client |
| `physics.max_flight_seconds` | `6.0` | physics | Thời gian bay tối đa của đạn trước khi biến mất |
| `match.turn_time_seconds` | `20` | match | Thời gian mỗi lượt (giây). Hết = tự động kết thúc lượt |
| `match.idle_timeout_minutes` | `2` | match | Phút không hoạt động trước khi match bị hủy |
| `move.step_pixels` | `10` | movement | Số pixel di chuyển mỗi tick khi giữ nút move |
| `move.energy_cost_per_2px` | `0.5` | movement | Năng lượng tiêu hao mỗi 2 pixel di chuyển |
| `fall.damage_threshold` | `30` | physics | Khoảng rơi tối thiểu (pixels) trước khi nhận fall damage |
| `fall.damage_per_pixel` | `0.5` | physics | Damage mỗi pixel rơi vượt ngưỡng |

## Config Loading Flow

### Current flow:
Game server startup → load YAML files → `gamedata.Data` global

### New flow:
1. Game/API server startup → query `config_*` tables + `game_settings` → populate `gamedata.Data` + physics constants
2. Fallback: if DB tables empty, load from YAML (backwards compatible)
3. Admin dashboard edits config → updates DB
4. Game server restart to apply (acceptable for dev phase)

### `gamedata.LoadGameData` changes:
- Add `LoadGameDataFromDB(db)` function — queries DB tables, populates same `GameData` struct
- Physics constants loaded into a new `PhysicsConfig` struct accessible via `gamedata.Physics`
- `LoadGameData(configDir)` becomes fallback when DB is empty

## Dashboard Pages

### 1. Dashboard Home
- Active rooms count (from Redis `rooms:active`)
- Active matches count
- Total registered players
- Quick links to dev tools

### 2. Characters (CRUD)
Table with all characters. Click row to edit. Fields:
- characterId, name, role, hp, damage, mobility, defense, skillPower, terrainDamage, difficulty, weaponId, skillId
- Each field shows its description tooltip (e.g., "HP: Máu của nhân vật. Ảnh hưởng sức chịu đựng trong trận")

### 3. Weapons (CRUD)
- weaponId, name, damage, explosionRadius, terrainDamage, projectileWeight, windInfluence, multiHit
- Description per field (e.g., "explosionRadius: Bán kính vụ nổ (pixels). Ảnh hưởng vùng phá đất và splash damage lên enemy gần")

### 4. Skills (CRUD)
- skillId, characterId, name, cooldownTurn, effectType, projectileCount, statusEffectId, damageMultiplier

### 5. Items (CRUD)
- itemId, name, type, targetType, value, maxUsePerMatch, cooldown

### 6. Maps (CRUD)
- mapId, name, width, height, windPowerRange, terrainLayers (JSON editor), spawnPoints (JSON editor)

### 7. Physics Settings
- Key-value list grouped by category
- Each row: key, current value, input field, description
- Save button per row or save-all

### 8. Shop Offers
- CRUD on `shop_offers` table (existing)
- Fields: offerId, itemType, itemId, currencyType, price, maxPurchases, isActive

### 9. Players
- Paginated player list from `player_profiles`
- Search by displayName
- View: playerId, displayName, coins, gems, created_at
- Actions: Ban (creates `account_bans` record), Unban (deletes ban)

### 10. Dev Tools
- **Clear Rooms** — DELETE from Redis `rooms:active`
- **Reset All Data** — Truncate player tables (same as existing `/dev/reset-data`)
- **Seed Config** — Re-seed default physics constants + YAML data into config tables
- **Seed Test Players** — Create N test player accounts for testing
- Each action has a confirmation step

## UI Layout

```
┌──────────────────────────────────────────────┐
│  Battle Squad Admin                          │
├────────────┬─────────────────────────────────┤
│ Dashboard  │                                 │
│ Characters │   [Page Content]                │
│ Weapons    │                                 │
│ Skills     │   Table with inline edit /      │
│ Items      │   modal forms                   │
│ Maps       │                                 │
│ Physics    │   Each field has a description  │
│ Shop       │   line below it explaining     │
│ Players    │   what it does in-game         │
│ Dev Tools  │                                 │
├────────────┴─────────────────────────────────┤
│  Footer: server status                       │
└──────────────────────────────────────────────┘
```

- Sidebar: fixed left nav, dark background
- Content: white/light background, clean tables
- Forms: simple HTML forms with labels + descriptions under each field
- No JavaScript framework — vanilla JS only for confirmations and JSON editors

## Migration Strategy

1. New migration `004_admin_config_tables.up.sql` creates all `config_*` tables + `game_settings`
2. `cmd/admin/main.go` on first run: if `config_characters` is empty, seed from existing YAML files
3. `gamedata.LoadGameDataFromDB()` queries DB, falls back to YAML if empty
4. Existing `LoadGameData(configDir)` stays for backwards compatibility
