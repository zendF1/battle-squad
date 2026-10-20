-- Gun skills redesign: the four character skills keep their ids (clients and admin key on
-- them) and get new effect types, Vietnamese names and descriptions. The numbers live in
-- internal/game/match/skill_tuning.go; projectile_count is informational (rank 0) and
-- damage_multiplier scales the whole skill (1.0).
-- Idempotent: a row changes only while it still has its old effect type, so reruns and later
-- admin edits are kept. Fresh databases get the same rows from configs/skills.yaml (admin seed).
UPDATE config_skills s
SET name = v.name, effect_type = v.effect_type, projectile_count = v.projectile_count,
    status_effect_id = v.status_effect_id, damage_multiplier = 1.0, description = v.description,
    updated_at = CURRENT_TIMESTAMP
FROM (VALUES
    ('triple_shot', 'multi_projectile', 'Mưa tên lửa', 'missile_rain', 6, 'burn',
     'Bắn pháo hiệu; tên lửa rơi xuống quanh chỗ nó đáp (6 quả, +2 mỗi cấp súng), mỗi quả 35% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau.'),
    ('heavy_bomb', 'single_large_bomb', 'Bom liên hoàn', 'bomb_chain', 3, 'root',
     'Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.'),
    ('shock_field', 'debuff', 'Tia laze', 'laser', 1, 'stun',
     'Tia thẳng xuyên địa hình tới mép bản đồ, trúng mọi địch trên đường: 150% sát thương (+10% mỗi cấp súng), mỗi địch sau giảm 20%; địch trúng bị choáng, mất lượt sau.'),
    ('healing_bloom', 'heal', 'Hoa hồi xuân', 'bloom', 1, '',
     'Hồi máu cả đội theo máu tối đa: Flora 40%, đồng đội 30% (+5% mỗi cấp súng); máu dư thành khiên (tối đa 50% máu); giải mọi hiệu ứng xấu.')
) AS v(skill_id, old_effect_type, name, effect_type, projectile_count, status_effect_id, description)
WHERE s.skill_id = v.skill_id
  AND s.effect_type = v.old_effect_type;
