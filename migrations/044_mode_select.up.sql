-- The mode select screen and Đấu thường 2vs2 (the casual_2v2 queue): config_game_modes gains
-- category (a config_mode_categories row), icon (a key the client maps to a UI icon),
-- party_size (the max lobby members), ranked (changes rating) and enabled (hidden from GET
-- /gamedata modes.lobby when false); the casual lobby / match rows.
-- Idempotent: the columns and their seed values are added together, once, so reruns keep
-- admin edits; names change only while they are the old default; seed rows ON CONFLICT DO
-- NOTHING.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_game_modes' AND column_name = 'category') THEN
        ALTER TABLE config_game_modes ADD COLUMN category TEXT NOT NULL DEFAULT '';
        ALTER TABLE config_game_modes ADD COLUMN icon TEXT NOT NULL DEFAULT '';
        ALTER TABLE config_game_modes ADD COLUMN party_size INT NOT NULL DEFAULT 1;
        ALTER TABLE config_game_modes ADD COLUMN ranked BOOLEAN NOT NULL DEFAULT false;
        ALTER TABLE config_game_modes ADD COLUMN enabled BOOLEAN NOT NULL DEFAULT true;

        UPDATE config_game_modes SET category = 'pvp' WHERE kind = 'lobby';
        UPDATE config_game_modes SET icon = 'trophy', party_size = 2, ranked = true WHERE mode_id = 'ranked';
        UPDATE config_game_modes SET icon = 'robot' WHERE mode_id = 'bot';
        UPDATE config_game_modes SET icon = 'target' WHERE mode_id = 'practice';
        -- PvP order: casual, ranked, bot, practice (practice was first)
        UPDATE config_game_modes SET sort_order = 4 WHERE mode_id = 'practice' AND sort_order = 1;
    END IF;
END
$$;

UPDATE config_game_modes SET name = 'Đấu hạng 2vs2' WHERE mode_id = 'ranked' AND name = 'Xếp hạng 2v2';
UPDATE config_game_modes SET name = 'Đấu hạng 2vs2' WHERE mode_id = 'ranked_2v2' AND name = 'Xếp hạng 2v2';
UPDATE config_game_modes SET name = 'Đấu máy' WHERE mode_id = 'bot' AND name = 'Đấu với máy';

INSERT INTO config_game_modes (mode_id, kind, name, description, queue, bot, sort_order, category, icon, party_size, ranked, enabled) VALUES
    ('casual', 'lobby', 'Đấu thường 2vs2', 'Ghép 2v2, không tính điểm hạng', 'casual_2v2', '', 1, 'pvp', 'swords', 2, false, true),
    ('casual_2v2', 'match', 'Đấu thường 2vs2', '', '', '', 7, '', '', 1, false, true)
ON CONFLICT (mode_id) DO NOTHING;

-- The sections of the mode select screen, in order
CREATE TABLE IF NOT EXISTS config_mode_categories (
    category_id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_mode_categories (category_id, name, sort_order) VALUES
    ('pvp', 'PvP', 1),
    ('boss', 'Boss', 2)
ON CONFLICT (category_id) DO NOTHING;

DROP TRIGGER IF EXISTS gamedata_revision_bump ON config_mode_categories;
CREATE TRIGGER gamedata_revision_bump AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON config_mode_categories
    FOR EACH STATEMENT EXECUTE FUNCTION bump_gamedata_revision();
