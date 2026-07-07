ALTER TABLE player_characters
    ADD COLUMN IF NOT EXISTS level                INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS exp                  INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS stat_points          INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_hp             INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_damage         INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_mobility       INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_defense        INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_skill_power    INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_terrain_damage INT NOT NULL DEFAULT 0;

INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('character_progression', '{"pointsPerLevel":10,"resetCostCurrency":"coin","resetCostAmount":500,"statMultipliers":{"hp":50,"damage":5,"mobility":3,"defense":5,"skill_power":5,"terrain_damage":3}}', 'json', 'Character stat progression config', 'character'),
    ('character_levels', '{"levels":[{"level":2,"expRequired":200},{"level":3,"expRequired":400},{"level":4,"expRequired":700},{"level":5,"expRequired":1100},{"level":6,"expRequired":1600},{"level":7,"expRequired":2200},{"level":8,"expRequired":3000},{"level":9,"expRequired":4000},{"level":10,"expRequired":5200}]}', 'json', 'Character level thresholds', 'character')
ON CONFLICT (key) DO NOTHING;
