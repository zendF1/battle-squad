-- Back to 028's missile rain row (only while it still has 029's).
UPDATE config_skills
SET projectile_count = 6,
    description = 'Bắn pháo hiệu; tên lửa rơi xuống quanh chỗ nó đáp (6 quả, +2 mỗi cấp súng), mỗi quả 35% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'triple_shot'
  AND projectile_count = 12
  AND description = 'Bắn pháo hiệu; tên lửa rơi xuống quanh chỗ nó đáp (12 quả, +4 mỗi cấp súng), mỗi quả 25% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau.';
