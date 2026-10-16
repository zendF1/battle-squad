-- Puts back the sword names of 016/019 on weapons that still have the gun
-- name 020 gave them. Run before 019's and 016's down (they match the old names).

UPDATE config_equipment_items c
SET name = r.old_name, updated_at = CURRENT_TIMESTAMP
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
  AND c.name = r.new_name;
