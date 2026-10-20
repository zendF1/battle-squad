-- Removes the careful columns, and the weapon multiplier while it still holds
-- 025's value; the code falls back to 3 without it.

ALTER TABLE config_upgrade_rates DROP COLUMN IF EXISTS careful_bonus_percent;
ALTER TABLE config_upgrade_rates DROP COLUMN IF EXISTS careful_gem_cost;
DELETE FROM game_settings WHERE key = 'equipment.weapon_upgrade_cost_multiplier' AND value = '3';
