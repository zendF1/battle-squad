-- Battle Squad is a shooting game: weapons are guns. Renames the weapons
-- seeded by 016 and 019 (ids, stats and prices unchanged), tier word first:
-- - per-character: rookie Bazooka, tanko Cannon, spark Shock Rifle,
--   flora Seed Launcher (Iron/Steel/Mithril/Dragon by lv10..lv40);
-- - generic normal_weapon_lvN (inactive): Iron/Steel/Mithril/Dragon Blaster;
-- - crafted_weapon_<tier>: Silver/Gold/Titan/Diamond Blaster.
-- A row is renamed only while it still has its old default name, so admin
-- renames are kept and a rerun changes nothing.

UPDATE config_equipment_items c
SET name = r.new_name, updated_at = CURRENT_TIMESTAMP
FROM (
    SELECT ch.character_id || '_weapon_lv' || t.lv AS item_id, ch.old_name, t.tier_word || ' ' || ch.gun AS new_name
    FROM (VALUES ('rookie', 'Rookie Sword', 'Bazooka'), ('tanko', 'Tanko Cannon', 'Cannon'),
                 ('spark', 'Spark Staff', 'Shock Rifle'), ('flora', 'Flora Slingshot', 'Seed Launcher')) AS ch(character_id, old_name, gun)
    CROSS JOIN (VALUES (10, 'Iron'), (20, 'Steel'), (30, 'Mithril'), (40, 'Dragon')) AS t(lv, tier_word)
    UNION ALL
    SELECT 'normal_weapon_lv' || t.lv, t.tier_word || ' Sword', t.tier_word || ' Blaster'
    FROM (VALUES (10, 'Iron'), (20, 'Steel'), (30, 'Mithril'), (40, 'Dragon')) AS t(lv, tier_word)
    UNION ALL
    SELECT 'crafted_weapon_' || lower(t.tier_word), t.tier_word || ' Sword', t.tier_word || ' Blaster'
    FROM (VALUES ('Silver'), ('Gold'), ('Titan'), ('Diamond')) AS t(tier_word)
) r
WHERE c.item_id = r.item_id
  AND c.name = r.old_name;
