-- Removes what 044_mode_select added: the categories, the casual rows and the mode columns;
-- the renamed modes get their old default names back.
DROP TABLE IF EXISTS config_mode_categories;
DELETE FROM config_game_modes WHERE mode_id IN ('casual', 'casual_2v2');
UPDATE config_game_modes SET name = 'Xếp hạng 2v2' WHERE mode_id IN ('ranked', 'ranked_2v2') AND name = 'Đấu hạng 2vs2';
UPDATE config_game_modes SET name = 'Đấu với máy' WHERE mode_id = 'bot' AND name = 'Đấu máy';
UPDATE config_game_modes SET sort_order = 1 WHERE mode_id = 'practice' AND sort_order = 4;
ALTER TABLE config_game_modes DROP COLUMN IF EXISTS category;
ALTER TABLE config_game_modes DROP COLUMN IF EXISTS icon;
ALTER TABLE config_game_modes DROP COLUMN IF EXISTS party_size;
ALTER TABLE config_game_modes DROP COLUMN IF EXISTS ranked;
ALTER TABLE config_game_modes DROP COLUMN IF EXISTS enabled;
