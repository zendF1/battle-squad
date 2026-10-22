-- Removes what 043_bot_loadout added: the bots fall back to the rules.bots defaults and heal
-- without forgetting.
DELETE FROM game_settings WHERE key = 'rules.bots';

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers}', (
        SELECT jsonb_object_agg(t.key, CASE WHEN jsonb_typeof(t.value) = 'object' THEN t.value - 'medkitForgetChance' ELSE t.value END)
        FROM jsonb_each(game_settings.value::jsonb -> 'tiers') AS t
    ))::text,
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND jsonb_typeof(value::jsonb -> 'tiers') = 'object'
  AND (value::jsonb -> 'tiers') <> '{}'::jsonb;
