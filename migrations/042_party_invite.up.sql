-- Party invites (Xếp hạng 2v2): a lobby invite waits inviteTimeoutSec seconds for its
-- answer. Only adds the key the row lacks, so admin edits survive reruns.
UPDATE game_settings
SET value = (value::jsonb || '{"inviteTimeoutSec":30}'::jsonb)::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'matchmaking'
  AND jsonb_typeof(value::jsonb) = 'object'
  AND NOT (value::jsonb ? 'inviteTimeoutSec');
