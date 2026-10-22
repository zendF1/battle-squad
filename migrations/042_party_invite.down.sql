UPDATE game_settings
SET value = (value::jsonb - 'inviteTimeoutSec')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'matchmaking'
  AND jsonb_typeof(value::jsonb) = 'object'
  AND value::jsonb ? 'inviteTimeoutSec';
