-- Titles ("Danh hiệu"): cosmetic plates a player earns from milestone rewards
-- ({"kind":"title","id":...} in missions.reward_items), lists with GET /player/titles,
-- wears one of with POST /player/titles/equip (player_profiles.equipped_title_id) and shows
-- in the profile and in every match players payload. Rerunnable: seeds keep admin edits,
-- a title item is appended to its milestone only when missing, the backfill skips owned rows.

CREATE TABLE IF NOT EXISTS config_titles (
    title_id    TEXT PRIMARY KEY,
    name        TEXT        NOT NULL,
    rarity      TEXT        NOT NULL CHECK (rarity IN ('blue', 'orange', 'purple', 'red')),
    sort_order  INT         NOT NULL DEFAULT 0,
    description TEXT        NOT NULL DEFAULT '',
    is_active   BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS player_titles (
    player_id   VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    title_id    TEXT        NOT NULL REFERENCES config_titles(title_id) ON DELETE CASCADE,
    obtained_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, title_id)
);

ALTER TABLE player_profiles ADD COLUMN IF NOT EXISTS equipped_title_id TEXT;
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'player_profiles_equipped_title_id_fkey' AND conrelid = 'player_profiles'::regclass
    ) THEN
        ALTER TABLE player_profiles ADD CONSTRAINT player_profiles_equipped_title_id_fkey
            FOREIGN KEY (equipped_title_id) REFERENCES config_titles(title_id) ON DELETE SET NULL;
    END IF;
END $$;

-- Seeded titles (internal/api/title/defaults.go; admin SeedTitles writes the same)
INSERT INTO config_titles (title_id, name, rarity, sort_order, description) VALUES
    ('veteran',       'Chiến binh kỳ cựu',     'blue',   10, '1 nhân vật đạt cấp 40'),
    ('smith',         'Thợ rèn lành nghề',     'blue',   20, '1 trang bị đạt +10'),
    ('sharpshooter',  'Xạ thủ bách chiến',     'orange', 30, 'Thắng 100 trận'),
    ('old_hand',      'Lão làng',              'orange', 40, 'Chơi 1000 trận'),
    ('elite_armor',   'Chiến giáp tinh anh',   'purple', 50, '1 bộ trang bị hoàn chỉnh +14'),
    ('battle_master', 'Bậc thầy chiến trường', 'purple', 60, '1 nhân vật đạt cấp 80'),
    ('legend_smith',  'Thợ rèn huyền thoại',   'red',    70, '1 trang bị đạt +16'),
    ('grandmaster',   'Cao thủ',               'red',    80, 'Lần đầu đạt hạng Cao thủ')
ON CONFLICT (title_id) DO NOTHING;

-- Each title is the reward of one milestone: append {"kind":"title","id":...} to that
-- milestone's reward_items unless it is already there (other items are kept).
UPDATE missions m
SET reward_items = (CASE WHEN jsonb_typeof(m.reward_items) = 'array' THEN m.reward_items ELSE '[]'::jsonb END)
                   || jsonb_build_array(jsonb_build_object('kind', 'title', 'id', v.title_id))
FROM (VALUES
    ('ms_char_lv_40', 'veteran'),
    ('ms_upgrade_10', 'smith'),
    ('ms_wins_0100', 'sharpshooter'),
    ('ms_matches_1000', 'old_hand'),
    ('ms_set_14', 'elite_armor'),
    ('ms_char_lv_80', 'battle_master'),
    ('ms_upgrade_16', 'legend_smith'),
    ('ms_rank_master', 'grandmaster')
) AS v(mission_id, title_id)
WHERE m.mission_id = v.mission_id
  AND NOT (CASE WHEN jsonb_typeof(m.reward_items) = 'array' THEN m.reward_items ELSE '[]'::jsonb END)
          @> jsonb_build_array(jsonb_build_object('kind', 'title', 'id', v.title_id));

-- Backfill: a player who already claimed a milestone holding a title item owns that title,
-- obtained when the claim was recorded.
INSERT INTO player_titles (player_id, title_id, obtained_at)
SELECT p.player_id, it ->> 'id', MIN(COALESCE(p.updated_at, now()))
FROM mission_progress p
JOIN missions m ON m.mission_id = p.mission_id AND m.type = 'milestone'
CROSS JOIN LATERAL jsonb_array_elements(
    CASE WHEN jsonb_typeof(m.reward_items) = 'array' THEN m.reward_items ELSE '[]'::jsonb END) AS it
JOIN config_titles t ON t.title_id = it ->> 'id'
WHERE p.is_claimed AND it ->> 'kind' = 'title'
GROUP BY p.player_id, it ->> 'id'
ON CONFLICT (player_id, title_id) DO NOTHING;
