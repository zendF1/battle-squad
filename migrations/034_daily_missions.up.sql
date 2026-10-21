-- Daily missions ("Nhiệm vụ ngày"): GET /missions/daily, POST /missions/claim,
-- POST /missions/chests/claim (internal/api/mission).
-- * A daily mission is a missions row with type 'daily' (title, activity points, goto screen).
--   Its progress is a mission_progress row whose period is the day it counts for
--   ('2026-10-21', UTC+7); a row of an older day reads as 0 / unclaimed and restarts on the
--   next write (lazy reset, no worker).
-- * An activity chest is a missions row with type 'mission_chest', track 'daily' or 'weekly',
--   required_value = the activity threshold, reward_coin / reward_gem / reward_items like a
--   milestone. Its claim is a mission_progress row with is_claimed and the period it was
--   claimed for (a day, or an ISO week '2026-W43' in UTC+7).
-- * Activity points: player_mission_activity, one row per player and track with its period.
-- Idempotent: columns and table IF NOT EXISTS, seeds ON CONFLICT DO NOTHING (admin edits kept).
-- Self-contained on top of 001 (track / sort_order come from 026, re-added here IF NOT EXISTS)
-- so tests that build a partial schema can apply it.

ALTER TABLE missions ADD COLUMN IF NOT EXISTS track VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE missions ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
ALTER TABLE missions ADD COLUMN IF NOT EXISTS title TEXT NOT NULL DEFAULT '';
ALTER TABLE missions ADD COLUMN IF NOT EXISTS activity INT NOT NULL DEFAULT 0;
ALTER TABLE missions ADD COLUMN IF NOT EXISTS goto_screen VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE mission_progress ADD COLUMN IF NOT EXISTS period VARCHAR(16) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS player_mission_activity (
    player_id  VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    track      VARCHAR(10) NOT NULL, -- daily, weekly
    period     VARCHAR(16) NOT NULL, -- 2026-10-21 / 2026-W43 (UTC+7)
    points     INT         NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, track)
);

INSERT INTO missions (mission_id, type, track, target, required_value, sort_order, title, activity, goto_screen)
SELECT v.id, 'daily', 'daily', v.target, v.required, v.sort_order, v.title, v.activity, v.goto_screen
FROM (VALUES
    ('daily_login',       'login',         1,    1,  'Đăng nhập trò chơi',          10, 'none'),
    ('daily_play_3',      'play_match',    3,    2,  'Chơi 3 trận đấu',             20, 'play'),
    ('daily_win_1',       'win_match',     1,    3,  'Thắng 1 trận đấu',            20, 'play'),
    ('daily_damage_3000', 'damage_dealt',  3000, 4,  'Gây 3000 sát thương',         20, 'play'),
    ('daily_kill_3',      'enemy_killed',  3,    5,  'Hạ gục 3 kẻ địch',            20, 'play'),
    ('daily_hit_10',      'shots_hit',     10,   6,  'Bắn trúng 10 phát',           10, 'play'),
    ('daily_skill_2',     'skill_used',    2,    7,  'Dùng kỹ năng 2 lần',          10, 'play'),
    ('daily_item_3',      'item_used',     3,    8,  'Dùng 3 vật phẩm trong trận',  10, 'play'),
    ('daily_upgrade_1',   'upgrade',       1,    9,  'Cường hóa trang bị 1 lần',    10, 'equipment'),
    ('daily_shop_1',      'shop_purchase', 1,    10, 'Mua 1 món ở cửa hàng',        10, 'shop')
) AS v(id, target, required, sort_order, title, activity, goto_screen)
ON CONFLICT (mission_id) DO NOTHING;

INSERT INTO missions (mission_id, type, track, target, required_value, sort_order, reward_coin, reward_gem, reward_items)
SELECT v.id, 'mission_chest', v.track, 'activity', v.threshold, v.threshold, v.coin, v.gem, v.items::jsonb
FROM (VALUES
    ('chest_d20',  'daily',  20,  300,  0,  '[]'),
    ('chest_d40',  'daily',  40,  500,  0,  '[]'),
    ('chest_d60',  'daily',  60,  0,    10, '[]'),
    ('chest_d80',  'daily',  80,  500,  0,  '[{"kind":"item","id":"medkit","qty":2}]'),
    ('chest_d100', 'daily',  100, 1000, 20, '[]'),
    ('chest_w150', 'weekly', 150, 2000, 0,  '[]'),
    ('chest_w300', 'weekly', 300, 0,    30, '[]'),
    ('chest_w450', 'weekly', 450, 0,    0,  '[{"kind":"stone","level":4,"qty":3}]'),
    ('chest_w600', 'weekly', 600, 3000, 80, '[]')
) AS v(id, track, threshold, coin, gem, items)
ON CONFLICT (mission_id) DO NOTHING;
