-- Equipment back to docs/equipment-system-design.md:
-- 1. required levels: normal tiers 10/20/30/40, crafted silver/gold/titan/diamond
--    15/35/55/75 (an earlier 016 remapped them to 1/4/7/10 and 2/5/8/10);
-- 2. character_levels extended to level 80 so every requirement is reachable;
-- 3. normal weapons are per character (<character>_weapon_lvN); the generic
--    normal_weapon_lvN leave the shop. Other normal slots stay shared.
-- Reruns on every deploy: each step is guarded so a rerun is a no-op and
-- admin edits are kept.

-- 1. Restore the remapped required levels. A slot is restored only while all
--    four of its rows (normal lv10..lv40, or crafted silver..diamond) still
--    hold exactly the remapped defaults, so an edited slot is left alone and a
--    restored slot never matches again.
WITH remap(grp, item_id, remapped, design) AS (
    SELECT 'normal_' || s.slot, 'normal_' || s.slot || '_lv' || t.design, t.remapped, t.design
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN (VALUES (10, 1), (20, 4), (30, 7), (40, 10)) AS t(design, remapped)
    UNION ALL
    SELECT 'crafted_' || s.slot, 'crafted_' || s.slot || '_' || t.tier, t.remapped, t.design
    FROM unnest(ARRAY['weapon', 'armor', 'helmet', 'pants', 'boots', 'gloves']) AS s(slot)
    CROSS JOIN (VALUES ('silver', 15, 2), ('gold', 35, 5), ('titan', 55, 8), ('diamond', 75, 10)) AS t(tier, design, remapped)
), untouched AS (
    SELECT r.grp
    FROM remap r
    JOIN config_equipment_items c ON c.item_id = r.item_id AND c.required_level = r.remapped
    GROUP BY r.grp
    HAVING COUNT(*) = 4
)
UPDATE config_equipment_items c
SET required_level = r.design, updated_at = CURRENT_TIMESTAMP
FROM remap r
JOIN untouched u ON u.grp = r.grp
WHERE c.item_id = r.item_id;

-- 2. Character level table to 80. L2-L10 keep their thresholds; from L11 the
--    step to the next level grows by 25: expRequired(L) = 5200 + 1200n +
--    25n(n+1)/2 with n = L - 10 (L20 18575, L40 52825, L75 136825, L80 151325).
--    Only the untouched 10-level default (exact text of migration 006) is replaced.
UPDATE game_settings
SET value = (
        SELECT '{"levels":[' || string_agg(format('{"level":%s,"expRequired":%s}', l.level, l.exp), ',' ORDER BY l.level) || ']}'
        FROM (
            SELECT level, exp FROM (VALUES (2, 200), (3, 400), (4, 700), (5, 1100), (6, 1600),
                                           (7, 2200), (8, 3000), (9, 4000), (10, 5200)) AS v(level, exp)
            UNION ALL
            SELECT g.level, 5200 + 1200 * (g.level - 10) + 25 * (g.level - 10) * (g.level - 9) / 2
            FROM generate_series(11, 80) AS g(level)
        ) l),
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'character_levels'
  AND value = '{"levels":[{"level":2,"expRequired":200},{"level":3,"expRequired":400},{"level":4,"expRequired":700},{"level":5,"expRequired":1100},{"level":6,"expRequired":1600},{"level":7,"expRequired":2200},{"level":8,"expRequired":3000},{"level":9,"expRequired":4000},{"level":10,"expRequired":5200}]}';

-- 3a. The generic weapons leave the shop: deactivated only while unchanged
--     from the default, and only before the per-character weapons exist (so
--     this runs once: a weapon an admin re-activates later stays active).
--     Owned pieces keep working: equip and battle stats do not check is_active.
UPDATE config_equipment_items c
SET is_active = FALSE, updated_at = CURRENT_TIMESTAMP
FROM (VALUES
    ('normal_weapon_lv10', 'Iron Sword',    10, 0, 8,  0, 0, 0, 500),
    ('normal_weapon_lv20', 'Steel Sword',   20, 0, 15, 0, 1, 0, 1500),
    ('normal_weapon_lv30', 'Mithril Sword', 30, 0, 25, 0, 2, 0, 4000),
    ('normal_weapon_lv40', 'Dragon Sword',  40, 0, 38, 0, 3, 0, 10000)
) AS seeded(item_id, name, required_level, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin)
WHERE c.item_id = seeded.item_id
  AND (c.name, c.slot, c.category, c.tier, c.required_level, c.character_id, c.gem_slots,
       c.stat_hp, c.stat_damage, c.stat_defense, c.stat_crit, c.stat_move_energy, c.price_coin, c.price_gem, c.is_active)
      IS NOT DISTINCT FROM
      (seeded.name, 'weapon', 'normal', NULL::varchar, seeded.required_level, NULL::varchar, 1::smallint,
       seeded.stat_hp, seeded.stat_damage, seeded.stat_defense, seeded.stat_crit::numeric, seeded.stat_move_energy, seeded.price_coin, 0, TRUE)
  AND NOT EXISTS (
      SELECT 1 FROM config_equipment_items w
      WHERE w.item_id IN (SELECT ch || '_weapon_lv' || lv
                          FROM unnest(ARRAY['rookie', 'tanko', 'spark', 'flora']) AS ch
                          CROSS JOIN unnest(ARRAY[10, 20, 30, 40]) AS lv));

-- 3b. Per-character weapons: the generic weapon tier's stats and price, the
--     character's own name. Same data as cmd/admin SeedEquipmentConfig.
INSERT INTO config_equipment_items
    (item_id, name, slot, category, tier, required_level, character_id, gem_slots,
     stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem)
SELECT c.character_id || '_weapon_lv' || t.lv, c.weapon_name, 'weapon', 'normal', NULL, t.lv, c.character_id, 1,
       0, t.dmg, 0, t.crit, 0, t.price, 0
FROM (VALUES ('rookie', 'Rookie Sword'), ('tanko', 'Tanko Cannon'),
             ('spark', 'Spark Staff'), ('flora', 'Flora Slingshot')) AS c(character_id, weapon_name)
CROSS JOIN (VALUES (10, 8, 0, 500), (20, 15, 1, 1500), (30, 25, 2, 4000), (40, 38, 3, 10000)) AS t(lv, dmg, crit, price)
ON CONFLICT (item_id) DO NOTHING;
