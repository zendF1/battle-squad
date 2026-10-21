-- Bomb chain after playtest 6: one bomb less (2, +1 per gun rank), each bomb keeps the damage
-- it had (the numbers live in internal/game/match/skill_bombchain.go). Only the display count and
-- the description change here. Idempotent: the row changes only while it still has 031's count
-- and description, so reruns and later admin edits are kept. Fresh databases get the same row
-- from configs/skills.yaml (admin seed).
UPDATE config_skills
SET projectile_count = 2,
    description = 'Bắn liên tiếp bom lớn cùng một đường bay (2 quả, +1 mỗi cấp súng), đào sâu một hố; mỗi quả 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb'
  AND projectile_count = 3
  AND description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.';
