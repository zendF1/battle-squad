-- Config tables

CREATE TABLE IF NOT EXISTS config_equipment_items (
    item_id        VARCHAR(100) PRIMARY KEY,
    name           VARCHAR(200) NOT NULL,
    slot           VARCHAR(20)  NOT NULL,
    category       VARCHAR(20)  NOT NULL,
    tier           VARCHAR(20),
    required_level INT          NOT NULL DEFAULT 1,
    character_id   VARCHAR(50),
    gem_slots      SMALLINT     NOT NULL DEFAULT 1,
    stat_hp        INT          NOT NULL DEFAULT 0,
    stat_damage    INT          NOT NULL DEFAULT 0,
    stat_defense   INT          NOT NULL DEFAULT 0,
    stat_crit      NUMERIC(5,2) NOT NULL DEFAULT 0,
    stat_move_energy INT        NOT NULL DEFAULT 0,
    price_coin     INT          NOT NULL DEFAULT 0,
    price_gem      INT          NOT NULL DEFAULT 0,
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_upgrade_rates (
    from_level     SMALLINT     NOT NULL,
    to_level       SMALLINT     NOT NULL,
    upgrade_cost   INT          NOT NULL,
    max_percent    NUMERIC(5,2) NOT NULL,
    fail_reset_to  SMALLINT     NOT NULL,
    PRIMARY KEY (from_level, to_level)
);

CREATE TABLE IF NOT EXISTS config_set_bonuses (
    tier             VARCHAR(20)  NOT NULL,
    pieces_required  SMALLINT     NOT NULL,
    bonus_hp_pct     NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_dmg_pct    NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_def_pct    NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_crit_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tier, pieces_required)
);

CREATE TABLE IF NOT EXISTS config_gems (
    gem_type   VARCHAR(20)   NOT NULL,
    gem_level  SMALLINT      NOT NULL,
    stat_value NUMERIC(10,2) NOT NULL,
    PRIMARY KEY (gem_type, gem_level)
);

CREATE TABLE IF NOT EXISTS config_stones (
    stone_level SMALLINT    PRIMARY KEY,
    power       INT         NOT NULL,
    price_coin  INT         NOT NULL DEFAULT 0,
    price_gem   INT         NOT NULL DEFAULT 0,
    source      VARCHAR(20) NOT NULL
);

CREATE TABLE IF NOT EXISTS config_materials (
    material_id VARCHAR(100) PRIMARY KEY,
    name        VARCHAR(200) NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    source      VARCHAR(20)  NOT NULL,
    price_gem   INT          NOT NULL DEFAULT 0,
    tier        VARCHAR(20),
    is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_crafting_recipes (
    recipe_id      VARCHAR(100) PRIMARY KEY,
    result_item_id VARCHAR(100) NOT NULL REFERENCES config_equipment_items(item_id),
    materials      JSONB        NOT NULL,
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Player data tables

CREATE TABLE IF NOT EXISTS player_gems (
    gem_id    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    gem_type  VARCHAR(20) NOT NULL,
    gem_level SMALLINT    NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT player_gems_level_check CHECK (gem_level BETWEEN 1 AND 10)
);

CREATE INDEX IF NOT EXISTS idx_player_gems_player_id ON player_gems(player_id);

CREATE TABLE IF NOT EXISTS player_equipment (
    equipment_id  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id     VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    item_id       VARCHAR(100) NOT NULL,
    slot          VARCHAR(20)  NOT NULL,
    category      VARCHAR(20)  NOT NULL,
    tier          VARCHAR(20),
    upgrade_level SMALLINT     NOT NULL DEFAULT 0,
    gem_slot_1    UUID REFERENCES player_gems(gem_id),
    gem_slot_2    UUID REFERENCES player_gems(gem_id),
    is_equipped   BOOLEAN      NOT NULL DEFAULT FALSE,
    equipped_on   VARCHAR(50),
    is_locked     BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT player_equipment_upgrade_level_check CHECK (upgrade_level BETWEEN 0 AND 16)
);

CREATE INDEX IF NOT EXISTS idx_player_equipment_player_id ON player_equipment(player_id);
CREATE INDEX IF NOT EXISTS idx_player_equipment_equipped ON player_equipment(player_id, equipped_on) WHERE is_equipped = TRUE;

CREATE TABLE IF NOT EXISTS equipment_upgrade_log (
    log_id       UUID     PRIMARY KEY DEFAULT gen_random_uuid(),
    equipment_id UUID     NOT NULL REFERENCES player_equipment(equipment_id) ON DELETE CASCADE,
    from_level   SMALLINT NOT NULL,
    to_level     SMALLINT NOT NULL,
    stones_used  JSONB    NOT NULL DEFAULT '[]',
    total_power  INT      NOT NULL DEFAULT 0,
    success      BOOLEAN  NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS player_stones (
    player_id   VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    stone_level SMALLINT    NOT NULL,
    quantity    INT         NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, stone_level),
    CONSTRAINT player_stones_level_check CHECK (stone_level BETWEEN 1 AND 12)
);

CREATE TABLE IF NOT EXISTS player_materials (
    player_id   VARCHAR(64)  NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    material_id VARCHAR(100) NOT NULL,
    quantity    INT          NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, material_id)
);
