-- Removes what 041_gamedata_rules added. The match then falls back to its defaults (the same
-- numbers); the corrected fall.* / move.* rows go back to their old (unread) seed values.
DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['game_settings', 'config_characters', 'config_weapons', 'config_skills', 'config_items',
        'config_maps', 'config_brick_types', 'config_status_effects', 'config_rank_tiers', 'config_equipment_tiers',
        'config_game_modes', 'config_tips'] LOOP
        IF to_regclass(t) IS NOT NULL THEN
            EXECUTE format('DROP TRIGGER IF EXISTS gamedata_revision_bump ON %I', t);
        END IF;
    END LOOP;
END
$$;
DROP FUNCTION IF EXISTS bump_gamedata_revision() CASCADE; -- and the triggers of the tables below
DROP TABLE IF EXISTS config_item_type_words;
DROP TABLE IF EXISTS config_equipment_slots;
DROP TABLE IF EXISTS config_gun_kinds;
ALTER TABLE config_rank_tiers DROP COLUMN IF EXISTS division_size;
ALTER TABLE config_rank_tiers DROP COLUMN IF EXISTS divisions;
ALTER TABLE config_rank_tiers DROP COLUMN IF EXISTS min_rating;
ALTER TABLE config_status_effects DROP COLUMN IF EXISTS blocks_move;
ALTER TABLE config_status_effects DROP COLUMN IF EXISTS skips_turn;
ALTER TABLE config_status_effects DROP COLUMN IF EXISTS bad;
ALTER TABLE config_skills DROP COLUMN IF EXISTS params;
UPDATE game_settings SET value = '30', description = 'Khoảng rơi tối thiểu (pixels) trước khi nhận fall damage'
    WHERE key = 'fall.damage_threshold' AND value = '150' AND description = 'Rơi tới chừng này px không mất máu';
UPDATE game_settings SET value = '0.5', description = 'Damage mỗi pixel rơi vượt ngưỡng'
    WHERE key = 'fall.damage_per_pixel' AND value = '3.75' AND description = 'Máu mất mỗi px rơi vượt ngưỡng (bỏ qua phòng thủ)';
UPDATE game_settings SET value = '10', description = 'Số pixel di chuyển mỗi tick khi giữ nút move'
    WHERE key = 'move.step_pixels' AND value = '2' AND description = 'Đi chừng này px tốn move.energy_cost_per_2px năng lượng';
UPDATE game_settings SET value = '0.5', description = 'Năng lượng tiêu hao mỗi 2 pixel di chuyển'
    WHERE key = 'move.energy_cost_per_2px' AND value = '1' AND description = 'Năng lượng tốn cho mỗi move.step_pixels px đi được';
DELETE FROM game_settings WHERE key LIKE 'rules.%';
DROP TABLE IF EXISTS gamedata_versions;
DROP TABLE IF EXISTS gamedata_revision;
