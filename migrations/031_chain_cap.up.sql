-- Bomb chain tuning after the second playtest: one target takes at most 120% (+15% per gun
-- rank) of the whole chain, the 180% (+15%) total is still split over the bombs. Only the
-- description changes here (the numbers live in internal/game/match/skill_bombchain.go).
-- Idempotent: the row changes only while it still has 028's description, so reruns and later
-- admin edits are kept. Fresh databases get the same row from configs/skills.yaml (admin seed).
UPDATE config_skills
SET description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb'
  AND description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.';
