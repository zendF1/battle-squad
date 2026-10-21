-- Playtest tuning: shells fly faster and punchier. Shell speed power × 6 → 9.6 px/s (×1.6),
-- gravity 200 → 445 px/s² (×2.23), wind scale 30 → 67 (×2.23, so the wind's drift keeps its
-- share of a shot), at most 6 → 4.5 s of flight. The same power flies ~15% further
-- (range ∝ v² / g) and a shot to the same spot takes ~0.67 × the time.
-- Reruns on every deploy: a row only changes while it still holds the old value, so a rerun
-- is a no-op and admin edits are kept. The admin seed (SeedSettings) inserts the new values on
-- a fresh database.
UPDATE game_settings g
SET value = v.new_value, updated_at = CURRENT_TIMESTAMP
FROM (VALUES ('physics.projectile_speed_multiplier', '6', '9.6'),
             ('physics.gravity', '200', '445'),
             ('physics.wind_scale', '30', '67'),
             ('physics.max_flight_seconds', '6', '4.5')) AS v(key, old_value, new_value)
WHERE g.key = v.key
  -- the same number however it is written ('6', '6.0'); CASE: never casts a non-number
  AND CASE WHEN g.value ~ '^[0-9]+(\.[0-9]*)?$' THEN g.value::numeric END = v.old_value::numeric;
