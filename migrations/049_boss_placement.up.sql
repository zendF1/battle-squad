-- Boss v2 placement fixes (047 / 048 are frozen as applied).
-- arena_lava_crab_mound: the first two minion spawns (x 1080 / 1320 on the mound's top) were
-- inside the crab's drawn body (x 1192, bodySize 3.2, ~158 px half-width); they move down the
-- mound's slopes to x 936 / 1464 (mapgen.ArenasV2), > 40 px clear of it. A fresh crab still
-- needs >= 3 walks (80 px) to reach the nearest party spawn.
-- ice_witch: the hover band's top goes from y 200 to 320 so her body (up to ~160 px above her
-- feet) stays below the client's boss HP bar (gamedata.DefaultBosses).
-- Idempotent: each value changes only while it is still 047's / 048's, so reruns keep admin edits.

UPDATE config_maps
    SET spawn_points = '[{"x":200,"y":796},{"x":2200,"y":796},{"x":456,"y":796},{"x":1944,"y":796},{"x":1192,"y":636},{"x":936,"y":764},{"x":1464,"y":764},{"x":872,"y":796},{"x":1528,"y":796}]'
    WHERE map_id = 'arena_lava_crab_mound'
      AND spawn_points = '[{"x":200,"y":796},{"x":2200,"y":796},{"x":456,"y":796},{"x":1944,"y":796},{"x":1192,"y":636},{"x":1080,"y":636},{"x":1320,"y":636},{"x":872,"y":796},{"x":1528,"y":796}]'::jsonb;

UPDATE config_bosses SET hover = jsonb_set(hover, '{minY}', '320')
    WHERE boss_id = 'ice_witch' AND hover IS NOT NULL AND hover -> 'minY' = '200'::jsonb;
