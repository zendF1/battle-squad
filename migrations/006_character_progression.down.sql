ALTER TABLE player_characters
    DROP COLUMN IF EXISTS level,
    DROP COLUMN IF EXISTS exp,
    DROP COLUMN IF EXISTS stat_points,
    DROP COLUMN IF EXISTS bonus_hp,
    DROP COLUMN IF EXISTS bonus_damage,
    DROP COLUMN IF EXISTS bonus_mobility,
    DROP COLUMN IF EXISTS bonus_defense,
    DROP COLUMN IF EXISTS bonus_skill_power,
    DROP COLUMN IF EXISTS bonus_terrain_damage;

DELETE FROM game_settings WHERE key IN ('character_progression', 'character_levels');
