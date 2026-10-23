-- Undoes 053_snowman_death_blast: the witch's snowman loses deathBlastDamageFactor while it is
-- still 053's 0.5 (its death blast deals its full explodeDamagePct again).

UPDATE config_bosses
SET minions = (
        SELECT jsonb_agg(CASE WHEN e->>'minionId' = 'snowman' AND e->'deathBlastDamageFactor' = '0.5'::jsonb
                              THEN e - 'deathBlastDamageFactor' ELSE e END ORDER BY i)
        FROM jsonb_array_elements(minions) WITH ORDINALITY AS t(e, i)),
    updated_at = CURRENT_TIMESTAMP
WHERE boss_id = 'ice_witch' AND jsonb_typeof(minions) = 'array'
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(minions) e
              WHERE e->>'minionId' = 'snowman' AND e->'deathBlastDamageFactor' = '0.5'::jsonb);
