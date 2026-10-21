-- Missile rain tuning after the playtest: twice the missiles (12, +4 per gun rank), 25% damage
-- each. Idempotent: the row changes only while it still has 028's count and description, so
-- reruns and later admin edits are kept. Fresh databases get the same row from
-- configs/skills.yaml (admin seed).
UPDATE config_skills
SET projectile_count = 12,
    description = 'Bắn pháo hiệu; tên lửa rơi xuống quanh chỗ nó đáp (12 quả, +4 mỗi cấp súng), mỗi quả 25% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'triple_shot'
  AND projectile_count = 6
  AND description = 'Bắn pháo hiệu; tên lửa rơi xuống quanh chỗ nó đáp (6 quả, +2 mỗi cấp súng), mỗi quả 35% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau.';
