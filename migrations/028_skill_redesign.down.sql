-- Back to the first skill design (only rows still on the redesign).
UPDATE config_skills s
SET name = v.name, effect_type = v.effect_type, projectile_count = v.projectile_count,
    status_effect_id = v.status_effect_id, damage_multiplier = v.damage_multiplier, description = '',
    updated_at = CURRENT_TIMESTAMP
FROM (VALUES
    ('triple_shot', 'missile_rain', 'Triple Shot', 'multi_projectile', 3, '', 1.25::float8),
    ('heavy_bomb', 'bomb_chain', 'Heavy Bomb', 'single_large_bomb', 1, '', 1.1::float8),
    ('shock_field', 'laser', 'Shock Field', 'debuff', 1, 'net', 1.3::float8),
    ('healing_bloom', 'bloom', 'Healing Bloom', 'heal', 1, 'heal', 0.0::float8)
) AS v(skill_id, new_effect_type, name, effect_type, projectile_count, status_effect_id, damage_multiplier)
WHERE s.skill_id = v.skill_id
  AND s.effect_type = v.new_effect_type;
