-- Bots now roll each shot against a per-tier hit chance (%) instead of adding
-- aim error. Only fills tiers that lack hitChance, so admin edits survive reruns.

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,bronze,hitChance}', '60')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,bronze}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,bronze}' ? 'hitChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,silver,hitChance}', '70')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,silver}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,silver}' ? 'hitChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,gold,hitChance}', '80')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,gold}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,gold}' ? 'hitChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,platinum,hitChance}', '90')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,platinum}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,platinum}' ? 'hitChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,diamond,hitChance}', '95')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,diamond}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,diamond}' ? 'hitChance');

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{tiers,master,hitChance}', '99')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'bot_difficulty'
  AND value::jsonb #> '{tiers,master}' IS NOT NULL
  AND NOT (value::jsonb #> '{tiers,master}' ? 'hitChance');
