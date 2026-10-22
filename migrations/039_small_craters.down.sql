-- Back to 029's terrain damage (only rows still at 039's values). The match's
-- craterPxPerTerrainDamage (0.6 px since 039) is code: with it these values carve 15 / 39 px,
-- the old 20 / 52 px need the 0.8 of before.
UPDATE config_weapons w
SET terrain_damage = v.old_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 25, 20),
             ('electric_orb', 25, 20),
             ('seed_bomb', 30, 24),
             ('cannon_ball', 65, 30)) AS v(weapon_id, old_terrain, new_terrain)
WHERE w.weapon_id = v.weapon_id
  AND w.terrain_damage = v.new_terrain;

UPDATE config_characters c
SET terrain_damage = v.old_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 25, 20),
             ('spark', 25, 20),
             ('flora', 30, 24),
             ('tanko', 65, 30)) AS v(character_id, old_terrain, new_terrain)
WHERE c.character_id = v.character_id
  AND c.terrain_damage = v.new_terrain;
