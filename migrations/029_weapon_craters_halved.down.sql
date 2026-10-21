-- Back to the full craters (only rows still at 029's values).
UPDATE config_weapons w
SET terrain_damage = v.old_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 50, 25),
             ('cannon_ball', 130, 65)) AS v(weapon_id, old_terrain, new_terrain)
WHERE w.weapon_id = v.weapon_id
  AND w.terrain_damage = v.new_terrain;

UPDATE config_characters c
SET terrain_damage = v.old_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 50, 25),
             ('tanko', 130, 65)) AS v(character_id, old_terrain, new_terrain)
WHERE c.character_id = v.character_id
  AND c.terrain_damage = v.new_terrain;
