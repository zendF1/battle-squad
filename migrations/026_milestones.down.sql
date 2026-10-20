DELETE FROM mission_progress WHERE mission_id IN (SELECT mission_id FROM missions WHERE type = 'milestone');
DELETE FROM missions WHERE type = 'milestone';
DROP TABLE IF EXISTS player_stat_counters;
DROP INDEX IF EXISTS idx_missions_type_active;
ALTER TABLE missions DROP COLUMN IF EXISTS description;
ALTER TABLE missions DROP COLUMN IF EXISTS sort_order;
ALTER TABLE missions DROP COLUMN IF EXISTS track;
