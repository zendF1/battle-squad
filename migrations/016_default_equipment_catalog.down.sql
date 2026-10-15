-- Removes the default equipment catalog rows that are exactly as migration
-- 016 seeded them (same keys and values): rows an admin edited stay, and
-- items or materials a player owns stay so their stats and inventory keep
-- resolving. The required_level remap and the recipe key rename are not
-- reverted. config_upgrade_rates, config_stones, config_gems and
-- config_set_bonuses are left as they are (no-op): upgrades, dismantle
-- refunds, gem and set stats of existing gear depend on them. Tables are kept.

DELETE FROM config_crafting_recipes r
USING (
    SELECT 'recipe_' || s.slot || '_' || t.tier,
           'crafted_' || s.slot || '_' || t.tier,
           (SELECT jsonb_agg(jsonb_build_object(
                       'materialId', m.material_id,
                       'quantity', CASE WHEN s.slot = 'weapon' THEN t.weapon_qty ELSE t.base_qty END)
                   ORDER BY m.ord)
            FROM unnest(t.materials) WITH ORDINALITY AS m(material_id, ord))
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN (VALUES
        ('silver',  10, 15, ARRAY['silver_ore', 'woven_thread', 'beast_hide', 'crystal_powder', 'silver_blueprint']),
        ('gold',    20, 30, ARRAY['gold_ore', 'enchanted_silk', 'small_dragon_scale', 'magic_essence', 'gold_blueprint']),
        ('titan',   30, 45, ARRAY['titan_ore', 'titan_fiber', 'energy_core', 'titan_solvent', 'titan_blueprint']),
        ('diamond', 40, 60, ARRAY['raw_diamond', 'light_weave', 'boss_soul_shard', 'phoenix_tear', 'diamond_blueprint'])
    ) AS t(tier, base_qty, weapon_qty, materials)
) AS seeded(recipe_id, result_item_id, materials)
WHERE r.recipe_id = seeded.recipe_id
  AND r.result_item_id = seeded.result_item_id
  AND r.materials = seeded.materials
  AND r.is_active;

DELETE FROM config_equipment_items c
USING (VALUES
    ('normal_weapon_lv10', 'Iron Sword',     'weapon', 'normal', NULL, 1,  NULL, 1, 0, 8,  0, 0, 0, 500,   0),
    ('normal_weapon_lv20', 'Steel Sword',    'weapon', 'normal', NULL, 4,  NULL, 1, 0, 15, 0, 1, 0, 1500,  0),
    ('normal_weapon_lv30', 'Mithril Sword',  'weapon', 'normal', NULL, 7,  NULL, 1, 0, 25, 0, 2, 0, 4000,  0),
    ('normal_weapon_lv40', 'Dragon Sword',   'weapon', 'normal', NULL, 10, NULL, 1, 0, 38, 0, 3, 0, 10000, 0),
    ('normal_armor_lv10',  'Iron Vest',      'armor',  'normal', NULL, 1,  NULL, 1, 30,  0, 3,  0, 0, 400,  0),
    ('normal_armor_lv20',  'Steel Vest',     'armor',  'normal', NULL, 4,  NULL, 1, 60,  0, 6,  0, 0, 1200, 0),
    ('normal_armor_lv30',  'Mithril Vest',   'armor',  'normal', NULL, 7,  NULL, 1, 100, 0, 10, 0, 0, 3500, 0),
    ('normal_armor_lv40',  'Dragon Vest',    'armor',  'normal', NULL, 10, NULL, 1, 150, 0, 15, 0, 0, 8000, 0),
    ('normal_helmet_lv10', 'Iron Cap',       'helmet', 'normal', NULL, 1,  NULL, 1, 20,  0, 2,  0, 0, 300,  0),
    ('normal_helmet_lv20', 'Steel Cap',      'helmet', 'normal', NULL, 4,  NULL, 1, 40,  0, 4,  0, 0, 900,  0),
    ('normal_helmet_lv30', 'Mithril Cap',    'helmet', 'normal', NULL, 7,  NULL, 1, 70,  0, 7,  0, 0, 2500, 0),
    ('normal_helmet_lv40', 'Dragon Cap',     'helmet', 'normal', NULL, 10, NULL, 1, 110, 0, 11, 0, 0, 6000, 0),
    ('normal_pants_lv10',  'Iron Pants',     'pants',  'normal', NULL, 1,  NULL, 1, 20,  0, 2,  0, 0, 350,  0),
    ('normal_pants_lv20',  'Steel Pants',    'pants',  'normal', NULL, 4,  NULL, 1, 45,  0, 5,  0, 0, 1000, 0),
    ('normal_pants_lv30',  'Mithril Pants',  'pants',  'normal', NULL, 7,  NULL, 1, 75,  0, 8,  0, 0, 3000, 0),
    ('normal_pants_lv40',  'Dragon Pants',   'pants',  'normal', NULL, 10, NULL, 1, 120, 0, 12, 0, 0, 7000, 0),
    ('normal_boots_lv10',  'Iron Boots',     'boots',  'normal', NULL, 1,  NULL, 1, 10, 0, 1, 0, 5,  300,  0),
    ('normal_boots_lv20',  'Steel Boots',    'boots',  'normal', NULL, 4,  NULL, 1, 20, 0, 2, 0, 10, 800,  0),
    ('normal_boots_lv30',  'Mithril Boots',  'boots',  'normal', NULL, 7,  NULL, 1, 35, 0, 4, 0, 15, 2200, 0),
    ('normal_boots_lv40',  'Dragon Boots',   'boots',  'normal', NULL, 10, NULL, 1, 55, 0, 6, 0, 20, 5500, 0),
    ('normal_gloves_lv10', 'Iron Gloves',    'gloves', 'normal', NULL, 1,  NULL, 1, 0, 2,  0, 2, 0, 250,  0),
    ('normal_gloves_lv20', 'Steel Gloves',   'gloves', 'normal', NULL, 4,  NULL, 1, 0, 4,  0, 4, 0, 750,  0),
    ('normal_gloves_lv30', 'Mithril Gloves', 'gloves', 'normal', NULL, 7,  NULL, 1, 0, 7,  0, 6, 0, 2000, 0),
    ('normal_gloves_lv40', 'Dragon Gloves',  'gloves', 'normal', NULL, 10, NULL, 1, 0, 12, 0, 8, 0, 5000, 0),
    ('crafted_weapon_silver',  'Silver Sword',   'weapon', 'crafted', 'silver',  2,  NULL, 2, 0, 10, 0, 0, 0, 0, 0),
    ('crafted_weapon_gold',    'Gold Sword',     'weapon', 'crafted', 'gold',    5,  NULL, 2, 0, 33, 0, 3, 0, 0, 0),
    ('crafted_weapon_titan',   'Titan Sword',    'weapon', 'crafted', 'titan',   8,  NULL, 2, 0, 57, 0, 5, 0, 0, 0),
    ('crafted_weapon_diamond', 'Diamond Sword',  'weapon', 'crafted', 'diamond', 10, NULL, 2, 0, 76, 0, 6, 0, 0, 0),
    ('crafted_armor_silver',   'Silver Vest',    'armor',  'crafted', 'silver',  2,  NULL, 2, 39,  0, 4,  0, 0, 0, 0),
    ('crafted_armor_gold',     'Gold Vest',      'armor',  'crafted', 'gold',    5,  NULL, 2, 130, 0, 13, 0, 0, 0, 0),
    ('crafted_armor_titan',    'Titan Vest',     'armor',  'crafted', 'titan',   8,  NULL, 2, 225, 0, 23, 0, 0, 0, 0),
    ('crafted_armor_diamond',  'Diamond Vest',   'armor',  'crafted', 'diamond', 10, NULL, 2, 300, 0, 30, 0, 0, 0, 0),
    ('crafted_helmet_silver',  'Silver Cap',     'helmet', 'crafted', 'silver',  2,  NULL, 2, 26,  0, 3,  0, 0, 0, 0),
    ('crafted_helmet_gold',    'Gold Cap',       'helmet', 'crafted', 'gold',    5,  NULL, 2, 91,  0, 9,  0, 0, 0, 0),
    ('crafted_helmet_titan',   'Titan Cap',      'helmet', 'crafted', 'titan',   8,  NULL, 2, 165, 0, 17, 0, 0, 0, 0),
    ('crafted_helmet_diamond', 'Diamond Cap',    'helmet', 'crafted', 'diamond', 10, NULL, 2, 220, 0, 22, 0, 0, 0, 0),
    ('crafted_pants_silver',   'Silver Pants',   'pants',  'crafted', 'silver',  2,  NULL, 2, 26,  0, 3,  0, 0, 0, 0),
    ('crafted_pants_gold',     'Gold Pants',     'pants',  'crafted', 'gold',    5,  NULL, 2, 98,  0, 10, 0, 0, 0, 0),
    ('crafted_pants_titan',    'Titan Pants',    'pants',  'crafted', 'titan',   8,  NULL, 2, 180, 0, 18, 0, 0, 0, 0),
    ('crafted_pants_diamond',  'Diamond Pants',  'pants',  'crafted', 'diamond', 10, NULL, 2, 240, 0, 24, 0, 0, 0, 0),
    ('crafted_boots_silver',   'Silver Boots',   'boots',  'crafted', 'silver',  2,  NULL, 2, 13,  0, 1,  0, 7,  0, 0),
    ('crafted_boots_gold',     'Gold Boots',     'boots',  'crafted', 'gold',    5,  NULL, 2, 46,  0, 5,  0, 20, 0, 0),
    ('crafted_boots_titan',    'Titan Boots',    'boots',  'crafted', 'titan',   8,  NULL, 2, 83,  0, 9,  0, 30, 0, 0),
    ('crafted_boots_diamond',  'Diamond Boots',  'boots',  'crafted', 'diamond', 10, NULL, 2, 110, 0, 12, 0, 40, 0, 0),
    ('crafted_gloves_silver',  'Silver Gloves',  'gloves', 'crafted', 'silver',  2,  NULL, 2, 0, 3,  0, 3,  0, 0, 0),
    ('crafted_gloves_gold',    'Gold Gloves',    'gloves', 'crafted', 'gold',    5,  NULL, 2, 0, 9,  0, 8,  0, 0, 0),
    ('crafted_gloves_titan',   'Titan Gloves',   'gloves', 'crafted', 'titan',   8,  NULL, 2, 0, 18, 0, 12, 0, 0, 0),
    ('crafted_gloves_diamond', 'Diamond Gloves', 'gloves', 'crafted', 'diamond', 10, NULL, 2, 0, 24, 0, 16, 0, 0, 0)
) AS seeded(item_id, name, slot, category, tier, required_level, character_id, gem_slots,
            stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem)
WHERE c.item_id = seeded.item_id
  AND (c.name, c.slot, c.category, c.tier, c.required_level, c.character_id, c.gem_slots,
       c.stat_hp, c.stat_damage, c.stat_defense, c.stat_crit, c.stat_move_energy, c.price_coin, c.price_gem, c.is_active)
      IS NOT DISTINCT FROM
      (seeded.name, seeded.slot, seeded.category, seeded.tier::varchar, seeded.required_level, seeded.character_id::varchar, seeded.gem_slots::smallint,
       seeded.stat_hp, seeded.stat_damage, seeded.stat_defense, seeded.stat_crit::numeric, seeded.stat_move_energy, seeded.price_coin, seeded.price_gem, TRUE)
  AND NOT EXISTS (SELECT 1 FROM player_equipment pe WHERE pe.item_id = c.item_id)
  AND NOT EXISTS (SELECT 1 FROM config_crafting_recipes cr WHERE cr.result_item_id = c.item_id);

DELETE FROM config_materials m
USING (VALUES
    ('silver_ore',         'Silver Ore',         'drop',     'silver',  5),
    ('woven_thread',       'Woven Thread',       'drop',     'silver',  5),
    ('beast_hide',         'Beast Hide',         'drop',     'silver',  5),
    ('crystal_powder',     'Crystal Powder',     'gem_shop', 'silver',  10),
    ('silver_blueprint',   'Silver Blueprint',   'gem_shop', 'silver',  20),
    ('gold_ore',           'Gold Ore',           'drop',     'gold',    15),
    ('enchanted_silk',     'Enchanted Silk',     'drop',     'gold',    15),
    ('small_dragon_scale', 'Small Dragon Scale', 'drop',     'gold',    15),
    ('magic_essence',      'Magic Essence',      'gem_shop', 'gold',    30),
    ('gold_blueprint',     'Gold Blueprint',     'gem_shop', 'gold',    50),
    ('titan_ore',          'Titan Ore',          'drop',     'titan',   40),
    ('titan_fiber',        'Titan Fiber',        'drop',     'titan',   40),
    ('energy_core',        'Energy Core',        'drop',     'titan',   40),
    ('titan_solvent',      'Titan Solvent',      'gem_shop', 'titan',   80),
    ('titan_blueprint',    'Titan Blueprint',    'gem_shop', 'titan',   120),
    ('raw_diamond',        'Raw Diamond',        'drop',     'diamond', 100),
    ('light_weave',        'Light Weave',        'drop',     'diamond', 100),
    ('boss_soul_shard',    'Boss Soul Shard',    'drop',     'diamond', 100),
    ('phoenix_tear',       'Phoenix Tear',       'gem_shop', 'diamond', 200),
    ('diamond_blueprint',  'Diamond Blueprint',  'gem_shop', 'diamond', 300)
) AS seeded(material_id, name, source, tier, price_gem)
WHERE m.material_id = seeded.material_id
  AND (m.name, m.source, m.tier, m.price_gem, m.description, m.is_active)
      IS NOT DISTINCT FROM (seeded.name, seeded.source, seeded.tier, seeded.price_gem, '', TRUE)
  AND NOT EXISTS (SELECT 1 FROM player_materials pm WHERE pm.material_id = m.material_id AND pm.quantity > 0);
