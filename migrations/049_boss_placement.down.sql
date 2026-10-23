-- Undoes 049_boss_placement: the values that are still 049's go back to 047's / 048's.

UPDATE config_maps
    SET spawn_points = '[{"x":200,"y":796},{"x":2200,"y":796},{"x":456,"y":796},{"x":1944,"y":796},{"x":1192,"y":636},{"x":1080,"y":636},{"x":1320,"y":636},{"x":872,"y":796},{"x":1528,"y":796}]'
    WHERE map_id = 'arena_lava_crab_mound'
      AND spawn_points = '[{"x":200,"y":796},{"x":2200,"y":796},{"x":456,"y":796},{"x":1944,"y":796},{"x":1192,"y":636},{"x":936,"y":764},{"x":1464,"y":764},{"x":872,"y":796},{"x":1528,"y":796}]'::jsonb;

UPDATE config_bosses SET hover = jsonb_set(hover, '{minY}', '200')
    WHERE boss_id = 'ice_witch' AND hover IS NOT NULL AND hover -> 'minY' = '320'::jsonb;
