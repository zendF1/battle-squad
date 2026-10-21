-- Back to 031's bomb chain row (only while it still has 033's).
UPDATE config_skills
SET projectile_count = 3,
    description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb'
  AND projectile_count = 2
  AND description = 'Bắn liên tiếp bom lớn cùng một đường bay (2 quả, +1 mỗi cấp súng), đào sâu một hố; mỗi quả 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.';
