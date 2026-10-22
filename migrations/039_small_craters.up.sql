-- Craters scaled to the characters (playtest: a normal shot dug a ~200 px wide bowl next to a
-- 60 px tall character). Together with craterPxPerTerrainDamage 0.8 → 0.6 px (match/damage.go):
-- Rookie's normal shot now carves the hole one missile of its missile rain carved before (12 px
-- radius), Tanko 1.5 × Rookie, Spark as Rookie, Flora 1.2 × Rookie:
--   basic_rocket 25 → 20 (crater 20 → 12 px), electric_orb 25 → 20 (20 → 12 px),
--   seed_bomb 30 → 24 (24 → 14.4 px), cannon_ball 65 → 30 (52 → 18 px).
-- Skill shells follow their weapon (bomb chain bombs carve the weapon's bowl); the missile
-- rain's missiles keep their 12 px (rainCrater 0.6 → 1.0). The blast (damage) radii are unchanged.
-- The characters' display copy (config_characters.terrain_damage, not read by the match) follows.
-- Reruns on every deploy: a row only changes while it still holds the old value, so a rerun is
-- a no-op and admin edits are kept.
UPDATE config_weapons w
SET terrain_damage = v.new_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('basic_rocket', 25, 20),
             ('electric_orb', 25, 20),
             ('seed_bomb', 30, 24),
             ('cannon_ball', 65, 30)) AS v(weapon_id, old_terrain, new_terrain)
WHERE w.weapon_id = v.weapon_id
  AND w.terrain_damage = v.old_terrain;

UPDATE config_characters c
SET terrain_damage = v.new_terrain, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('rookie', 25, 20),
             ('spark', 25, 20),
             ('flora', 30, 24),
             ('tanko', 65, 30)) AS v(character_id, old_terrain, new_terrain)
WHERE c.character_id = v.character_id
  AND c.terrain_damage = v.old_terrain;
