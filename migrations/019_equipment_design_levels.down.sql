-- Reverts 019 where its rows are untouched: removes the per-character weapons
-- nobody owns that are exactly as seeded, re-activates the unchanged generic
-- weapons and puts back the 10-level character table while it still equals
-- the 80-level default. Required levels stay at the design values (the
-- 1/4/7/10 and 2/5/8/10 remap was the bug).

DELETE FROM config_equipment_items w
USING (
    SELECT c.character_id || '_weapon_lv' || t.lv AS item_id, c.weapon_name AS name, t.lv, c.character_id, t.dmg, t.crit, t.price
    FROM (VALUES ('rookie', 'Rookie Sword'), ('tanko', 'Tanko Cannon'),
                 ('spark', 'Spark Staff'), ('flora', 'Flora Slingshot')) AS c(character_id, weapon_name)
    CROSS JOIN (VALUES (10, 8, 0, 500), (20, 15, 1, 1500), (30, 25, 2, 4000), (40, 38, 3, 10000)) AS t(lv, dmg, crit, price)
) AS seeded
WHERE w.item_id = seeded.item_id
  AND (w.name, w.slot, w.category, w.tier, w.required_level, w.character_id, w.gem_slots,
       w.stat_hp, w.stat_damage, w.stat_defense, w.stat_crit, w.stat_move_energy, w.price_coin, w.price_gem, w.is_active)
      IS NOT DISTINCT FROM
      (seeded.name, 'weapon', 'normal', NULL::varchar, seeded.lv, seeded.character_id::varchar, 1::smallint,
       0, seeded.dmg, 0, seeded.crit::numeric, 0, seeded.price, 0, TRUE)
  AND NOT EXISTS (SELECT 1 FROM player_equipment pe WHERE pe.item_id = w.item_id)
  AND NOT EXISTS (SELECT 1 FROM config_crafting_recipes cr WHERE cr.result_item_id = w.item_id);

UPDATE config_equipment_items c
SET is_active = TRUE, updated_at = CURRENT_TIMESTAMP
FROM (VALUES
    ('normal_weapon_lv10', 'Iron Sword',    10, 8,  0, 500),
    ('normal_weapon_lv20', 'Steel Sword',   20, 15, 1, 1500),
    ('normal_weapon_lv30', 'Mithril Sword', 30, 25, 2, 4000),
    ('normal_weapon_lv40', 'Dragon Sword',  40, 38, 3, 10000)
) AS seeded(item_id, name, required_level, stat_damage, stat_crit, price_coin)
WHERE c.item_id = seeded.item_id
  AND (c.name, c.slot, c.category, c.tier, c.required_level, c.character_id, c.gem_slots,
       c.stat_hp, c.stat_damage, c.stat_defense, c.stat_crit, c.stat_move_energy, c.price_coin, c.price_gem, c.is_active)
      IS NOT DISTINCT FROM
      (seeded.name, 'weapon', 'normal', NULL::varchar, seeded.required_level, NULL::varchar, 1::smallint,
       0, seeded.stat_damage, 0, seeded.stat_crit::numeric, 0, seeded.price_coin, 0, FALSE);

UPDATE game_settings
SET value = '{"levels":[{"level":2,"expRequired":200},{"level":3,"expRequired":400},{"level":4,"expRequired":700},{"level":5,"expRequired":1100},{"level":6,"expRequired":1600},{"level":7,"expRequired":2200},{"level":8,"expRequired":3000},{"level":9,"expRequired":4000},{"level":10,"expRequired":5200}]}',
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'character_levels'
  AND value = (
        SELECT '{"levels":[' || string_agg(format('{"level":%s,"expRequired":%s}', l.level, l.exp), ',' ORDER BY l.level) || ']}'
        FROM (
            SELECT level, exp FROM (VALUES (2, 200), (3, 400), (4, 700), (5, 1100), (6, 1600),
                                           (7, 2200), (8, 3000), (9, 4000), (10, 5200)) AS v(level, exp)
            UNION ALL
            SELECT g.level, 5200 + 1200 * (g.level - 10) + 25 * (g.level - 10) * (g.level - 9) / 2
            FROM generate_series(11, 80) AS g(level)
        ) l);
