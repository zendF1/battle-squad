DELETE FROM mission_progress WHERE mission_id IN (SELECT mission_id FROM missions WHERE type IN ('daily', 'mission_chest'));
DELETE FROM missions WHERE type IN ('daily', 'mission_chest');
DROP TABLE IF EXISTS player_mission_activity;
ALTER TABLE mission_progress DROP COLUMN IF EXISTS period;
ALTER TABLE missions DROP COLUMN IF EXISTS goto_screen;
ALTER TABLE missions DROP COLUMN IF EXISTS activity;
ALTER TABLE missions DROP COLUMN IF EXISTS title;
