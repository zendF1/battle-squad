UPDATE missions
SET reward_items = (SELECT COALESCE(jsonb_agg(e), '[]'::jsonb) FROM jsonb_array_elements(reward_items) e
                    WHERE e ->> 'kind' IS DISTINCT FROM 'title')
WHERE jsonb_typeof(reward_items) = 'array' AND reward_items @> '[{"kind":"title"}]';
ALTER TABLE player_profiles DROP CONSTRAINT IF EXISTS player_profiles_equipped_title_id_fkey;
ALTER TABLE player_profiles DROP COLUMN IF EXISTS equipped_title_id;
DROP TABLE IF EXISTS player_titles;
DROP TABLE IF EXISTS config_titles;
