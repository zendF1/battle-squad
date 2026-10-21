-- Back to 028's bomb chain description (only while it still has 031's).
UPDATE config_skills
SET description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb'
  AND description = 'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.';
