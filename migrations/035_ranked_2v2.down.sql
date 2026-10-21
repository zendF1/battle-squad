UPDATE game_settings
SET value = (value::jsonb - 'botFillAfter' - 'matchStartDelay')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'matchmaking'
  AND jsonb_typeof(value::jsonb) = 'object'
  AND (value::jsonb ? 'botFillAfter' OR value::jsonb ? 'matchStartDelay');

UPDATE rank_seasons SET name = 'Default Season' WHERE season_id = 'season_1' AND name = 'Mùa 1';
