-- Ranked 2v2 phase 1. Matchmaking: an entry waiting botFillAfter seconds gets its
-- free slots filled with bots; matchStartDelay seconds pass between MatchFound
-- and MatchStarted (the client's reveal). Only adds a key the row lacks, so
-- admin edits survive reruns.
UPDATE game_settings
SET value = (value::jsonb || '{"botFillAfter":20}'::jsonb)::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'matchmaking'
  AND jsonb_typeof(value::jsonb) = 'object'
  AND NOT (value::jsonb ? 'botFillAfter');

UPDATE game_settings
SET value = (value::jsonb || '{"matchStartDelay":3}'::jsonb)::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'matchmaking'
  AND jsonb_typeof(value::jsonb) = 'object'
  AND NOT (value::jsonb ? 'matchStartDelay');

-- The season the servers auto-created is "Mùa 1"
UPDATE rank_seasons SET name = 'Mùa 1' WHERE season_id = 'season_1' AND name IN ('Default Season', 'Pre-Season Alpha');
