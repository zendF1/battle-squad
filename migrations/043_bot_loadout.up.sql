-- Bots get a character, level, gun and items (rules.bots), and use them: a medkit at low HP,
-- a teleport when boxed in, the random item when it helps. Idempotent: the rules row is
-- inserted only when missing, medkitForgetChance only added to tiers that lack it (only
-- bronze bots sometimes forget to heal), so admin edits survive reruns.
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('rules.bots', '{"levelSpread": 3, "weaponUpgradeMin": 0, "weaponUpgradePerLevel": 0.2, "defaultItems": ["medkit","teleport"], "randomItems": ["power_shot","drill_bomb","spider_net","freeze_bomb","air_strike","wind_stopper"], "randomItemCount": 1, "medkitHpPercent": 35, "teleportLavaMarginPx": 40, "teleportEnemyMarginPx": 120}', 'json', 'Bot: cấp, súng, vật phẩm, ngưỡng hồi máu, dịch chuyển', 'rules')
ON CONFLICT (key) DO NOTHING;

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,bronze,medkitForgetChance}', '0.15')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND jsonb_typeof(value::jsonb #> '{tiers,bronze}') = 'object'
  AND NOT (value::jsonb #> '{tiers,bronze}' ? 'medkitForgetChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers}', (
        SELECT jsonb_object_agg(t.key, CASE WHEN jsonb_typeof(t.value) = 'object' AND NOT (t.value ? 'medkitForgetChance')
                 THEN t.value || '{"medkitForgetChance": 0}'::jsonb ELSE t.value END)
        FROM jsonb_each(game_settings.value::jsonb -> 'tiers') AS t
    ))::text,
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND jsonb_typeof(value::jsonb -> 'tiers') = 'object'
  AND EXISTS (SELECT 1 FROM jsonb_each(game_settings.value::jsonb -> 'tiers') AS t
              WHERE jsonb_typeof(t.value) = 'object' AND NOT (t.value ? 'medkitForgetChance'));
