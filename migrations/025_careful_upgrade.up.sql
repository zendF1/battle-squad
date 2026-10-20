-- Careful upgrade (POST /player/equipment/upgrade with careful: true): the
-- player pays careful_gem_cost gem and the stone percent (already capped at
-- max_percent) is multiplied by (1 + careful_bonus_percent / 100), capped at
-- 100. A step with either value 0 does not offer careful.
-- Defaults are written only when careful_bonus_percent is first added, so a
-- rerun keeps admin edits (including a step set back to 0).
-- Weapons need equipment.weapon_upgrade_cost_multiplier × upgrade_cost power.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'config_upgrade_rates' AND column_name = 'careful_gem_cost'
    ) THEN
        ALTER TABLE config_upgrade_rates ADD COLUMN careful_gem_cost INT NOT NULL DEFAULT 0;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'config_upgrade_rates' AND column_name = 'careful_bonus_percent'
    ) THEN
        ALTER TABLE config_upgrade_rates ADD COLUMN careful_bonus_percent NUMERIC(5,2) NOT NULL DEFAULT 0;
        UPDATE config_upgrade_rates r SET careful_gem_cost = p.gem, careful_bonus_percent = p.bonus
        FROM (VALUES
            (0, 2, 30), (1, 2, 30), (2, 3, 30), (3, 5, 30), (4, 5, 30), (5, 10, 30), (6, 10, 30), (7, 10, 30),
            (8, 15, 30), (9, 20, 30), (10, 30, 40), (11, 60, 40), (12, 80, 40), (13, 120, 40), (14, 180, 50), (15, 250, 50)
        ) AS p(from_level, gem, bonus)
        WHERE r.from_level = p.from_level AND r.to_level = p.from_level + 1;
    END IF;
END $$;

INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('equipment.weapon_upgrade_cost_multiplier', '3', 'number', 'Vũ khí cần upgrade_cost × hệ số này sức mạnh đá (>= 1)', 'equipment')
ON CONFLICT (key) DO NOTHING;
