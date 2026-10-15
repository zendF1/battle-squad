-- Removes the default equipment catalog rows. Items and materials a player
-- still owns stay, so their stats and inventory keep resolving. The
-- required_level remap is not reverted. Tables are kept.
DELETE FROM config_crafting_recipes
WHERE recipe_id IN (
    SELECT 'recipe_' || s.slot || '_' || t.tier
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN unnest(ARRAY['silver', 'gold', 'titan', 'diamond']) AS t(tier)
);

DELETE FROM config_equipment_items c
WHERE c.item_id IN (
    SELECT 'normal_' || s.slot || '_lv' || l.lvl
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN unnest(ARRAY[10, 20, 30, 40]) AS l(lvl)
    UNION ALL
    SELECT 'crafted_' || s.slot || '_' || t.tier
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN unnest(ARRAY['silver', 'gold', 'titan', 'diamond']) AS t(tier)
)
AND NOT EXISTS (SELECT 1 FROM player_equipment pe WHERE pe.item_id = c.item_id)
AND NOT EXISTS (SELECT 1 FROM config_crafting_recipes r WHERE r.result_item_id = c.item_id);

DELETE FROM config_materials m
WHERE m.material_id IN (
    'silver_ore', 'woven_thread', 'beast_hide', 'crystal_powder', 'silver_blueprint',
    'gold_ore', 'enchanted_silk', 'small_dragon_scale', 'magic_essence', 'gold_blueprint',
    'titan_ore', 'titan_fiber', 'energy_core', 'titan_solvent', 'titan_blueprint',
    'raw_diamond', 'light_weave', 'boss_soul_shard', 'phoenix_tear', 'diamond_blueprint'
)
AND NOT EXISTS (SELECT 1 FROM player_materials pm WHERE pm.material_id = m.material_id AND pm.quantity > 0);

DELETE FROM config_set_bonuses
WHERE tier IN ('silver', 'gold', 'titan', 'diamond') AND pieces_required IN (2, 4, 6);

DELETE FROM config_gems
WHERE gem_type IN ('hp', 'damage', 'defense', 'critical') AND gem_level BETWEEN 1 AND 10
AND NOT EXISTS (SELECT 1 FROM player_gems g WHERE g.gem_type = config_gems.gem_type AND g.gem_level = config_gems.gem_level);

DELETE FROM config_stones
WHERE stone_level BETWEEN 1 AND 12
AND NOT EXISTS (SELECT 1 FROM player_stones s WHERE s.stone_level = config_stones.stone_level AND s.quantity > 0);

DELETE FROM config_upgrade_rates WHERE from_level BETWEEN 0 AND 15 AND to_level = from_level + 1;
