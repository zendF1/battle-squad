-- Milestone rewards ("Mốc thưởng"): one-time, account-wide rewards claimed by hand
-- (GET /milestones, POST /milestones/claim, /milestones/claim-all). Each milestone is a
-- missions row with type 'milestone'; its claim is a mission_progress row with is_claimed.
-- Progress comes from player_characters / player_equipment at read time and from
-- player_stat_counters, which the match end (matches, wins, char_wins:<character>,
-- rank_peak, rating_peak) and successful upgrades (upgrade_peak, weapon_upgrade_peak) keep.
-- The counters are backfilled once, when the table is first created, so reruns never count
-- a match twice.

ALTER TABLE missions ADD COLUMN IF NOT EXISTS track VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE missions ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
ALTER TABLE missions ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_missions_type_active ON missions (type, is_active);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'player_stat_counters'
    ) THEN
        CREATE TABLE player_stat_counters (
            player_id  VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
            counter    VARCHAR(64) NOT NULL,
            value      INT         NOT NULL DEFAULT 0,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
            PRIMARY KEY (player_id, counter)
        );

        -- Upgrade peaks: current levels plus successful steps of pieces still owned
        -- (a sold piece's log is gone with it).
        INSERT INTO player_stat_counters (player_id, counter, value)
        SELECT player_id, c.counter, MAX(lvl)
        FROM (
            SELECT e.player_id, e.slot, e.upgrade_level AS lvl FROM player_equipment e
            UNION ALL
            SELECT e.player_id, e.slot, l.to_level FROM equipment_upgrade_log l
            JOIN player_equipment e ON e.equipment_id = l.equipment_id
            WHERE l.success
        ) peaks
        CROSS JOIN (VALUES ('upgrade_peak'), ('weapon_upgrade_peak')) AS c(counter)
        WHERE c.counter = 'upgrade_peak' OR peaks.slot = 'weapon'
        GROUP BY player_id, c.counter
        HAVING MAX(lvl) > 0;

        -- Rank peak (tier order bronze 1 .. master 6) and rating peak from every season row
        INSERT INTO player_stat_counters (player_id, counter, value)
        SELECT player_id, 'rank_peak', MAX(GREATEST(
            CASE highest_tier WHEN 'bronze' THEN 1 WHEN 'silver' THEN 2 WHEN 'gold' THEN 3
                WHEN 'platinum' THEN 4 WHEN 'diamond' THEN 5 WHEN 'master' THEN 6 ELSE 0 END,
            CASE tier WHEN 'bronze' THEN 1 WHEN 'silver' THEN 2 WHEN 'gold' THEN 3
                WHEN 'platinum' THEN 4 WHEN 'diamond' THEN 5 WHEN 'master' THEN 6 ELSE 0 END))
        FROM player_ranks GROUP BY player_id;
        INSERT INTO player_stat_counters (player_id, counter, value)
        SELECT player_id, 'rating_peak', MAX(rating) FROM player_ranks GROUP BY player_id;

        -- Matches / wins / wins per character, with the live counting rules as far as the
        -- stored data allows: a matchmade match (ranked_2v2) or a quick play against the AI
        -- bot (pvp_1v1 whose MatchStarted has a bot other than the idle "Target Bot"), where
        -- the player resolved >= 3 shots (ShotResolved events). Leaving early is not stored,
        -- and matches without event logs are not counted.
        INSERT INTO player_stat_counters (player_id, counter, value)
        WITH started AS (
            SELECT DISTINCT ON (match_id) match_id, data
            FROM match_event_logs WHERE event_type = 'MatchStarted'
            ORDER BY match_id, seq
        ), shots AS (
            SELECT match_id, player_id, COUNT(*) AS n
            FROM match_event_logs WHERE event_type = 'ShotResolved'
            GROUP BY match_id, player_id
        ), counted AS (
            SELECT h.player_id, h.result, s.data -> 'players' -> h.player_id ->> 'characterId' AS character_id
            FROM match_histories h
            JOIN started s ON s.match_id = h.match_id
            JOIN shots sh ON sh.match_id = h.match_id AND sh.player_id = h.player_id
            WHERE sh.n >= 3
              AND (h.mode = 'ranked_2v2' OR (h.mode = 'pvp_1v1' AND EXISTS (
                    SELECT 1 FROM jsonb_each(s.data -> 'players') p
                    WHERE (p.value ->> 'isBot')::boolean AND p.value ->> 'displayName' <> 'Target Bot')))
        )
        SELECT player_id, 'matches', COUNT(*) FROM counted GROUP BY player_id
        UNION ALL
        SELECT player_id, 'wins', COUNT(*) FROM counted WHERE result = 'win' GROUP BY player_id
        UNION ALL
        SELECT player_id, 'char_wins:' || character_id, COUNT(*) FROM counted
        WHERE result = 'win' AND COALESCE(character_id, '') <> ''
        GROUP BY player_id, character_id;
    END IF;
END $$;

-- Seeded milestones (internal/api/milestone/defaults.go; admin SeedMilestones writes the same)
INSERT INTO missions (mission_id, type, track, target, required_value, sort_order, reward_coin, reward_gem, reward_items, description)
SELECT v.id, 'milestone', v.track, v.target, v.required, v.sort_order, v.coin, v.gem, v.items::jsonb, v.description
FROM (VALUES
    ('ms_char_lv_05', 'character', 'char_level', 5, 5, 1000, 0, '[]', ''),
    ('ms_char_lv_10', 'character', 'char_level', 10, 10, 2000, 0, '[{"kind":"stone","level":4,"qty":3}]', ''),
    ('ms_char_lv_15', 'character', 'char_level', 15, 15, 0, 20, '[]', ''),
    ('ms_char_lv_20', 'character', 'char_level', 20, 20, 0, 30, '[{"kind":"stone","level":6,"qty":2}]', ''),
    ('ms_char_lv_25', 'character', 'char_level', 25, 25, 5000, 0, '[]', ''),
    ('ms_char_lv_30', 'character', 'char_level', 30, 30, 0, 40, '[{"kind":"stone","level":7,"qty":1}]', ''),
    ('ms_char_lv_35', 'character', 'char_level', 35, 35, 8000, 0, '[]', ''),
    ('ms_char_lv_40', 'character', 'char_level', 40, 40, 0, 50, '[{"kind":"stone","level":7,"qty":2}]', ''),
    ('ms_char_lv_45', 'character', 'char_level', 45, 45, 10000, 0, '[]', ''),
    ('ms_char_lv_50', 'character', 'char_level', 50, 50, 0, 60, '[{"kind":"stone","level":8,"qty":1}]', ''),
    ('ms_char_lv_55', 'character', 'char_level', 55, 55, 12000, 0, '[]', ''),
    ('ms_char_lv_60', 'character', 'char_level', 60, 60, 0, 100, '[{"kind":"stone","level":9,"qty":1}]', ''),
    ('ms_char_lv_65', 'character', 'char_level', 65, 65, 15000, 0, '[]', ''),
    ('ms_char_lv_70', 'character', 'char_level', 70, 70, 0, 80, '[{"kind":"stone","level":8,"qty":2}]', ''),
    ('ms_char_lv_75', 'character', 'char_level', 75, 75, 20000, 0, '[]', ''),
    ('ms_char_lv_80', 'character', 'char_level', 80, 80, 0, 300, '[{"kind":"stone","level":10,"qty":1}]', ''),
    ('ms_upgrade_03', 'upgrade', 'upgrade_peak', 3, 103, 0, 0, '[{"kind":"stone","level":3,"qty":5}]', ''),
    ('ms_upgrade_06', 'upgrade', 'upgrade_peak', 6, 106, 0, 20, '[{"kind":"stone","level":6,"qty":3}]', ''),
    ('ms_upgrade_10', 'upgrade', 'upgrade_peak', 10, 110, 0, 50, '[{"kind":"stone","level":8,"qty":2}]', ''),
    ('ms_upgrade_12', 'upgrade', 'upgrade_peak', 12, 112, 0, 0, '[{"kind":"stone","level":9,"qty":1}]', ''),
    ('ms_upgrade_14', 'upgrade', 'upgrade_peak', 14, 114, 0, 150, '[{"kind":"stone","level":10,"qty":1}]', ''),
    ('ms_upgrade_16', 'upgrade', 'upgrade_peak', 16, 116, 0, 500, '[{"kind":"stone","level":11,"qty":1}]', ''),
    ('ms_weapon_14', 'upgrade', 'weapon_upgrade_peak', 14, 214, 0, 100, '[]', 'Mở khoá hào quang tím sau lưng'),
    ('ms_weapon_16', 'upgrade', 'weapon_upgrade_peak', 16, 216, 0, 300, '[]', 'Mở khoá hào quang đỏ sau lưng'),
    ('ms_set_03', 'upgrade', 'set_level:3', 6, 303, 0, 0, '[{"kind":"stone","level":5,"qty":3}]', ''),
    ('ms_set_06', 'upgrade', 'set_level:6', 6, 306, 0, 50, '[{"kind":"stone","level":7,"qty":2}]', ''),
    ('ms_set_10', 'upgrade', 'set_level:10', 6, 310, 0, 150, '[{"kind":"stone","level":9,"qty":2}]', ''),
    ('ms_set_14', 'upgrade', 'set_level:14', 6, 314, 0, 300, '[{"kind":"stone","level":10,"qty":2}]', ''),
    ('ms_set_16', 'upgrade', 'set_level:16', 6, 316, 0, 1000, '[{"kind":"stone","level":11,"qty":2}]', ''),
    ('ms_matches_0010', 'combat', 'matches', 10, 10, 1000, 0, '[]', ''),
    ('ms_matches_0050', 'combat', 'matches', 50, 50, 3000, 10, '[]', ''),
    ('ms_matches_0100', 'combat', 'matches', 100, 100, 0, 20, '[{"kind":"stone","level":7,"qty":1}]', ''),
    ('ms_matches_0300', 'combat', 'matches', 300, 300, 0, 50, '[{"kind":"stone","level":8,"qty":2}]', ''),
    ('ms_matches_1000', 'combat', 'matches', 1000, 1000, 0, 200, '[{"kind":"stone","level":10,"qty":1}]', ''),
    ('ms_wins_0005', 'combat', 'wins', 5, 10005, 1500, 0, '[]', ''),
    ('ms_wins_0025', 'combat', 'wins', 25, 10025, 5000, 20, '[]', ''),
    ('ms_wins_0100', 'combat', 'wins', 100, 10100, 0, 50, '[{"kind":"stone","level":8,"qty":1}]', ''),
    ('ms_wins_0500', 'combat', 'wins', 500, 10500, 0, 150, '[{"kind":"stone","level":9,"qty":1}]', ''),
    ('ms_char_wins_0010', 'combat', 'char_wins', 10, 20010, 2000, 0, '[]', ''),
    ('ms_char_wins_0050', 'combat', 'char_wins', 50, 20050, 0, 30, '[{"kind":"stone","level":7,"qty":1}]', ''),
    ('ms_char_wins_0200', 'combat', 'char_wins', 200, 20200, 0, 100, '[{"kind":"stone","level":9,"qty":1}]', ''),
    ('ms_rank_silver', 'rank', 'rank_peak', 2, 2, 2000, 0, '[]', ''),
    ('ms_rank_gold', 'rank', 'rank_peak', 3, 3, 0, 30, '[{"kind":"stone","level":7,"qty":1}]', ''),
    ('ms_rank_platinum', 'rank', 'rank_peak', 4, 4, 0, 50, '[{"kind":"stone","level":7,"qty":2}]', ''),
    ('ms_rank_diamond', 'rank', 'rank_peak', 5, 5, 0, 100, '[{"kind":"stone","level":8,"qty":1}]', ''),
    ('ms_rank_master', 'rank', 'rank_peak', 6, 6, 0, 200, '[{"kind":"stone","level":9,"qty":1}]', '')
) AS v(id, track, target, required, sort_order, coin, gem, items, description)
ON CONFLICT (mission_id) DO NOTHING;
