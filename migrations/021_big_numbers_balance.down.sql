-- Back to the small-number balance, for rows still holding 021's values.

DELETE FROM game_settings WHERE key = 'combat.defense_k' AND value = '600';

UPDATE config_items
SET value = 30, updated_at = CURRENT_TIMESTAMP
WHERE item_id = 'medkit' AND value = 450;

UPDATE config_weapons w
SET damage = v.old_damage, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 60, 480),
             ('cannon_ball',  75, 600),
             ('electric_orb', 55, 440),
             ('seed_bomb',    45, 360)) AS v(weapon_id, old_damage, new_damage)
WHERE w.weapon_id = v.weapon_id
  AND w.damage = v.new_damage;

UPDATE config_characters c
SET hp = v.old_hp, damage = v.old_damage, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 100, 60, 1500, 480),
             ('tanko',  130, 75, 1950, 600),
             ('spark',   90, 55, 1350, 440),
             ('flora',   95, 45, 1425, 360)) AS v(character_id, old_hp, old_damage, new_hp, new_damage)
WHERE c.character_id = v.character_id
  AND c.hp = v.new_hp AND c.damage = v.new_damage;

UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{statMultipliers}',
                      '{"hp":50,"damage":5,"mobility":3,"defense":5,"skill_power":5,"terrain_damage":3}'::jsonb)::text,
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'character_progression'
  AND value::jsonb -> 'statMultipliers' =
      '{"hp":2,"damage":5,"mobility":1,"defense":1,"skill_power":1,"terrain_damage":1}'::jsonb;
