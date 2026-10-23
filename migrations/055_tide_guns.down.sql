-- Undoes 055_tide_guns: Tide's guns as 055 seeded them that no player owns.
DELETE FROM config_equipment_items c
USING (VALUES ('tide_weapon_lv10', 'Iron Water Gun'), ('tide_weapon_lv20', 'Steel Water Gun'),
              ('tide_weapon_lv30', 'Mithril Water Gun'), ('tide_weapon_lv40', 'Dragon Water Gun')) AS s(item_id, name)
WHERE c.item_id = s.item_id AND c.name = s.name
  AND NOT EXISTS (SELECT 1 FROM player_equipment p WHERE p.item_id = c.item_id);
