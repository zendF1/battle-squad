-- Default equipment catalog: items, upgrade rates, stones, gems, set bonuses,
-- materials and crafting recipes (same data as cmd/admin SeedEquipmentConfig).
-- Stable ids; ON CONFLICT DO NOTHING so reruns keep admin edits.
-- Required levels fit the character level cap (10): normal tiers 1/4/7/10,
-- crafted silver/gold/titan/diamond 2/5/8/10.

INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to) VALUES
    (0, 1, 1, 80, 0),
    (1, 2, 2, 76, 1),
    (2, 3, 6, 72, 2),
    (3, 4, 18, 68, 3),
    (4, 5, 40, 64, 4),
    (5, 6, 80, 60, 5),
    (6, 7, 180, 56, 6),
    (7, 8, 500, 52, 6),
    (8, 9, 1200, 45, 6),
    (9, 10, 3000, 40, 6),
    (10, 11, 6000, 35, 10),
    (11, 12, 15000, 30, 10),
    (12, 13, 25000, 25, 10),
    (13, 14, 50000, 20, 10),
    (14, 15, 80000, 15, 14),
    (15, 16, 200000, 10, 14)
ON CONFLICT (from_level, to_level) DO NOTHING;

INSERT INTO config_stones (stone_level, power, price_coin, price_gem, source) VALUES
    (1, 1, 100, 0, 'coin_shop'),
    (2, 3, 250, 0, 'coin_shop'),
    (3, 9, 700, 0, 'coin_shop'),
    (4, 27, 2000, 0, 'coin_shop'),
    (5, 81, 5500, 0, 'coin_shop'),
    (6, 243, 15000, 0, 'coin_shop'),
    (7, 729, 0, 50, 'gem_shop'),
    (8, 2187, 0, 140, 'gem_shop'),
    (9, 6561, 0, 400, 'gem_shop'),
    (10, 19683, 0, 1100, 'gem_shop'),
    (11, 59049, 0, 0, 'merge_only'),
    (12, 177147, 0, 0, 'merge_only')
ON CONFLICT (stone_level) DO NOTHING;

INSERT INTO config_gems (gem_type, gem_level, stat_value)
SELECT g.gem_type, v.lvl, v.val
FROM (VALUES
    ('hp',       ARRAY[20, 40, 70, 110, 160, 220, 300, 400, 520, 660]::numeric[]),
    ('damage',   ARRAY[3, 6, 10, 15, 22, 30, 40, 52, 67, 85]::numeric[]),
    ('defense',  ARRAY[2, 4, 7, 11, 16, 22, 30, 40, 52, 66]::numeric[]),
    ('critical', ARRAY[1, 2, 3, 5, 7, 9, 12, 15, 18, 22]::numeric[])
) AS g(gem_type, vals)
CROSS JOIN LATERAL unnest(g.vals) WITH ORDINALITY AS v(val, lvl)
ON CONFLICT (gem_type, gem_level) DO NOTHING;

INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct) VALUES
    ('silver', 2, 3, 0, 0, 0),
    ('silver', 4, 0, 0, 3, 0),
    ('silver', 6, 5, 0, 5, 0),
    ('gold', 2, 5, 0, 0, 0),
    ('gold', 4, 0, 5, 0, 0),
    ('gold', 6, 8, 8, 5, 0),
    ('titan', 2, 8, 0, 0, 0),
    ('titan', 4, 0, 8, 0, 0),
    ('titan', 6, 12, 12, 8, 5),
    ('diamond', 2, 10, 0, 0, 0),
    ('diamond', 4, 0, 10, 0, 0),
    ('diamond', 6, 15, 15, 12, 10)
ON CONFLICT (tier, pieces_required) DO NOTHING;

-- Normal items: sold in the shop for coin, 1 gem slot, any character.
-- Ids keep the old lvN suffix; required_level is 1/4/7/10.
INSERT INTO config_equipment_items
    (item_id, name, slot, category, tier, required_level, character_id, gem_slots,
     stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem) VALUES
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
    ('normal_gloves_lv40', 'Dragon Gloves',  'gloves', 'normal', NULL, 10, NULL, 1, 0, 12, 0, 8, 0, 5000, 0)
ON CONFLICT (item_id) DO NOTHING;

-- Crafted items: made from recipes (not sold), 2 gem slots, tier set bonuses.
INSERT INTO config_equipment_items
    (item_id, name, slot, category, tier, required_level, character_id, gem_slots,
     stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem) VALUES
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
ON CONFLICT (item_id) DO NOTHING;

-- Rows seeded earlier by cmd/admin carry the old required levels
-- (10/20/30/40, crafted 15/35/55/75). Remap only rows still holding those
-- defaults, so a level an admin changed is kept and a rerun changes nothing.
UPDATE config_equipment_items c
SET required_level = m.new_level, updated_at = CURRENT_TIMESTAMP
FROM (
    SELECT 'normal_' || s.slot || '_lv' || t.old_level AS item_id, t.old_level, t.new_level
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN (VALUES (10, 1), (20, 4), (30, 7), (40, 10)) AS t(old_level, new_level)
    UNION ALL
    SELECT 'crafted_' || s.slot || '_' || t.tier, t.old_level, t.new_level
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN (VALUES ('silver', 15, 2), ('gold', 35, 5), ('titan', 55, 8), ('diamond', 75, 10)) AS t(tier, old_level, new_level)
) m
WHERE c.item_id = m.item_id AND c.required_level = m.old_level;

INSERT INTO config_materials (material_id, name, source, tier, price_gem) VALUES
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
ON CONFLICT (material_id) DO NOTHING;

-- One recipe per slot and tier: the tier's 5 materials, each ×baseQty
-- (weapon ×weaponQty). JSON key is materialId.
INSERT INTO config_crafting_recipes (recipe_id, result_item_id, materials)
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
ON CONFLICT (recipe_id) DO NOTHING;

-- Recipes seeded earlier by cmd/admin used the key material_id; rewrite them
-- to materialId (the reader accepts both).
UPDATE config_crafting_recipes r
SET materials = (
        SELECT jsonb_agg(jsonb_build_object(
                   'materialId', COALESCE(e->>'materialId', e->>'material_id'),
                   'quantity', (e->>'quantity')::int)
               ORDER BY e_ord)
        FROM jsonb_array_elements(r.materials) WITH ORDINALITY AS x(e, e_ord)),
    updated_at = CURRENT_TIMESTAMP
WHERE jsonb_typeof(r.materials) = 'array'
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(r.materials) e WHERE e ? 'material_id');
