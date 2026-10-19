-- Character identities: Rookie balanced, Tanko digs (demolisher), Spark hits
-- hardest (striker), Flora lasts longest (sustain).
-- - config_characters.mobility is now the base move energy per turn (100/80/120/100,
--   every character had 100 before); role ids renamed.
-- - weapon terrain_damage now sets the crater: radius = terrain_damage × 0.8 px.
-- - heavy_bomb ×1.1 damage (blast ×2, crater ×2.5 in code), shock_field ×1.3.
-- Reruns on every deploy: each column only changes while it still holds the old
-- default, so a rerun is a no-op and admin edits are kept.

UPDATE config_characters c
SET role           = CASE WHEN c.role = v.old_role THEN v.new_role ELSE c.role END,
    hp             = CASE WHEN c.hp = v.old_hp THEN v.new_hp ELSE c.hp END,
    damage         = CASE WHEN c.damage = v.old_damage THEN v.new_damage ELSE c.damage END,
    mobility       = CASE WHEN c.mobility = v.old_mobility THEN v.new_mobility ELSE c.mobility END,
    defense        = CASE WHEN c.defense = v.old_defense THEN v.new_defense ELSE c.defense END,
    terrain_damage = CASE WHEN c.terrain_damage = v.old_terrain THEN v.new_terrain ELSE c.terrain_damage END,
    updated_at     = CURRENT_TIMESTAMP
FROM (VALUES
    -- id,      old role / hp / dmg / mob / def / terrain,   new role / hp / dmg / mob / def / terrain
    ('rookie', 'balanced',     1500, 480, 70, 50, 50,  'balanced',   1500, 480, 100, 50, 50),
    ('tanko',  'heavy_gunner', 1950, 600, 40, 80, 80,  'demolisher', 1650, 420,  80, 60, 130),
    ('spark',  'controller',   1350, 440, 75, 40, 40,  'striker',    1250, 620, 120, 30, 25),
    ('flora',  'support',      1425, 360, 65, 50, 30,  'sustain',    1900, 380, 100, 90, 30)
) AS v(character_id, old_role, old_hp, old_damage, old_mobility, old_defense, old_terrain,
       new_role, new_hp, new_damage, new_mobility, new_defense, new_terrain)
WHERE c.character_id = v.character_id
  AND (c.role = v.old_role AND v.old_role <> v.new_role
       OR c.hp = v.old_hp AND v.old_hp <> v.new_hp
       OR c.damage = v.old_damage AND v.old_damage <> v.new_damage
       OR c.mobility = v.old_mobility AND v.old_mobility <> v.new_mobility
       OR c.defense = v.old_defense AND v.old_defense <> v.new_defense
       OR c.terrain_damage = v.old_terrain AND v.old_terrain <> v.new_terrain);

UPDATE config_weapons w
SET damage           = CASE WHEN w.damage = v.old_damage THEN v.new_damage ELSE w.damage END,
    explosion_radius = CASE WHEN w.explosion_radius = v.old_radius THEN v.new_radius ELSE w.explosion_radius END,
    terrain_damage   = CASE WHEN w.terrain_damage = v.old_terrain THEN v.new_terrain ELSE w.terrain_damage END,
    wind_influence   = CASE WHEN w.wind_influence = v.old_wind THEN v.new_wind ELSE w.wind_influence END,
    updated_at       = CURRENT_TIMESTAMP
FROM (VALUES
    -- id,            old dmg / radius / terrain / wind,  new dmg / radius / terrain / wind
    ('cannon_ball',  600, 50, 85, 0.5::float8,  420, 60, 130, 0.5::float8),
    ('electric_orb', 440, 35, 30, 1.2::float8,  620, 30,  25, 1.3::float8),
    ('seed_bomb',    360, 40, 30, 1.0::float8,  380, 40,  30, 1.0::float8)
) AS v(weapon_id, old_damage, old_radius, old_terrain, old_wind, new_damage, new_radius, new_terrain, new_wind)
WHERE w.weapon_id = v.weapon_id
  AND (w.damage = v.old_damage AND v.old_damage <> v.new_damage
       OR w.explosion_radius = v.old_radius AND v.old_radius <> v.new_radius
       OR w.terrain_damage = v.old_terrain AND v.old_terrain <> v.new_terrain
       OR w.wind_influence = v.old_wind AND v.old_wind <> v.new_wind);

UPDATE config_skills s
SET damage_multiplier = v.new_mult, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('heavy_bomb',  1.3::float8, 1.1::float8),
             ('shock_field', 0.9::float8, 1.3::float8)) AS v(skill_id, old_mult, new_mult)
WHERE s.skill_id = v.skill_id
  AND s.damage_multiplier = v.old_mult;
