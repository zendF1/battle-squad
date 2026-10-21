-- Playtest tuning: Rookie's and Tanko's weapons carve half the terrain (normal shots and every
-- skill shell, whose craters are multiples of the weapon's): terrain_damage basic_rocket
-- 50 → 25 (crater 40 → 20 px), cannon_ball 130 → 65 (104 → 52 px). The characters' display
-- copy (config_characters.terrain_damage, not read by the match) follows. Damage and blast
-- radii are unchanged.
-- Reruns on every deploy: a row only changes while it still holds the old value, so a rerun
-- is a no-op and admin edits are kept (023 only touches cannon_ball at 85, Tanko at 80).
UPDATE config_weapons w
SET terrain_damage = v.new_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 50, 25),
             ('cannon_ball', 130, 65)) AS v(weapon_id, old_terrain, new_terrain)
WHERE w.weapon_id = v.weapon_id
  AND w.terrain_damage = v.old_terrain;

UPDATE config_characters c
SET terrain_damage = v.new_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 50, 25),
             ('tanko', 130, 65)) AS v(character_id, old_terrain, new_terrain)
WHERE c.character_id = v.character_id
  AND c.terrain_damage = v.old_terrain;
