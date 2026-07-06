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
