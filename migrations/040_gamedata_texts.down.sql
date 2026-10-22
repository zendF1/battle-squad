-- Removes what 040_gamedata_texts added (the texts go with their columns and tables).
DROP TABLE IF EXISTS config_tips;
DROP TABLE IF EXISTS config_game_modes;
DROP TABLE IF EXISTS config_equipment_tiers;
DROP TABLE IF EXISTS config_rank_tiers;
DROP TABLE IF EXISTS config_status_effects;
ALTER TABLE config_skills DROP COLUMN IF EXISTS description_template;
ALTER TABLE config_items DROP COLUMN IF EXISTS sort_order;
ALTER TABLE config_items DROP COLUMN IF EXISTS display_name;
ALTER TABLE config_characters DROP COLUMN IF EXISTS sort_order;
ALTER TABLE config_characters DROP COLUMN IF EXISTS gun_kind;
ALTER TABLE config_characters DROP COLUMN IF EXISTS playstyle;
ALTER TABLE config_characters DROP COLUMN IF EXISTS role_name;
