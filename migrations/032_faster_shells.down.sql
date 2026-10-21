-- Back to the slower shells (only rows still at 032's values).
UPDATE game_settings g
SET value = v.old_value, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('physics.projectile_speed_multiplier', '6.0', '9.6'),
             ('physics.gravity', '200', '445'),
             ('physics.wind_scale', '30.0', '67'),
             ('physics.max_flight_seconds', '6.0', '4.5')) AS v(key, old_value, new_value)
WHERE g.key = v.key
  -- the same number however it is written ('6', '6.0'); CASE: never casts a non-number
  AND CASE WHEN g.value ~ '^[0-9]+(\.[0-9]*)?$' THEN g.value::numeric END = v.new_value::numeric;
