-- "Big numbers" balance: HP ×15, weapon damage ×8, defense unchanged,
-- damage taken = raw × K / (K + defense) with K = 600 (was 100), and one stat
-- point (allocated or from equipment) is worth hp 2, damage 5, defense 1,
-- mobility 1, skill_power 1%, terrain_damage 1%.
-- Reruns on every deploy: each step only touches rows still holding the old
-- default, so a rerun is a no-op and admin edits are kept.

-- 1. statMultipliers: only while the stored object is exactly the old default
--    (migration 006's 50/5/3/5/5/3); the other progression keys are kept.
UPDATE game_settings
SET value = jsonb_set(value::jsonb, '{statMultipliers}',
                      '{"hp":2,"damage":5,"mobility":1,"defense":1,"skill_power":1,"terrain_damage":1}'::jsonb)::text,
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'character_progression'
  AND value::jsonb -> 'statMultipliers' =
      '{"hp":50,"damage":5,"mobility":3,"defense":5,"skill_power":5,"terrain_damage":3}'::jsonb;

-- 2. Base HP ×15 and damage ×8, per character row still holding both old values.
UPDATE config_characters c
SET hp = v.new_hp, damage = v.new_damage, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 100, 60, 1500, 480),
             ('tanko',  130, 75, 1950, 600),
             ('spark',   90, 55, 1350, 440),
             ('flora',   95, 45, 1425, 360)) AS v(character_id, old_hp, old_damage, new_hp, new_damage)
WHERE c.character_id = v.character_id
  AND c.hp = v.old_hp AND c.damage = v.old_damage;

-- 3. Weapon damage ×8.
UPDATE config_weapons w
SET damage = v.new_damage, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 60, 480),
             ('cannon_ball',  75, 600),
             ('electric_orb', 55, 440),
             ('seed_bomb',    45, 360)) AS v(weapon_id, old_damage, new_damage)
WHERE w.weapon_id = v.weapon_id
  AND w.damage = v.old_damage;

-- 4. Medkit heal ×15 (the game server now reads it from config_items).
UPDATE config_items
SET value = 450, updated_at = CURRENT_TIMESTAMP
WHERE item_id = 'medkit' AND value = 30;

-- 5. Defense constant K, editable on the admin settings page.
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('combat.defense_k', '600', 'number', 'Hằng số phòng thủ K: sát thương nhận = sát thương × K / (K + phòng thủ)', 'combat')
ON CONFLICT (key) DO NOTHING;
