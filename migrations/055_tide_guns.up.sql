-- Tide's guns (054_elements adds Tide): tide_weapon_lv10 .. lv40, Iron .. Dragon Water Gun, the
-- generic weapon tiers' stats and prices like the other characters' guns (019 / 020; cmd/admin
-- SeedEquipmentConfig). ON CONFLICT DO NOTHING keeps admin edits.
INSERT INTO config_equipment_items
    (item_id, name, slot, category, tier, required_level, character_id, gem_slots,
     stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem)
SELECT 'tide_weapon_lv' || t.lv, t.tier_word || ' Water Gun', 'weapon', 'normal', NULL, t.lv, 'tide', 1,
       0, t.dmg, 0, t.crit, 0, t.price, 0
FROM (VALUES (10, 'Iron', 8, 0, 500), (20, 'Steel', 15, 1, 1500), (30, 'Mithril', 25, 2, 4000), (40, 'Dragon', 38, 3, 10000))
    AS t(lv, tier_word, dmg, crit, price)
ON CONFLICT (item_id) DO NOTHING;
