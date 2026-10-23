-- The snowman's death blast (explodeOnDeath, damage cause death_blast: it is shot dead, burns or is
-- caught in another blast) deals half of its detonation (actionMode self_destruct, unchanged):
-- config_bosses.minions gains deathBlastDamageFactor (x explodeDamagePct for the death blast only;
-- absent / 0 = 1, gamedata.DefaultBosses), 0.5 for Phù Thuỷ Băng's snowman.
-- Idempotent: the factor is set only while the snowman has none, so reruns keep admin edits (the
-- admin Bosses page always stores the field, 0 included).

UPDATE config_bosses
SET minions = (
        SELECT jsonb_agg(CASE WHEN e->>'minionId' = 'snowman' AND NOT (e ? 'deathBlastDamageFactor')
                              THEN e || '{"deathBlastDamageFactor": 0.5}'::jsonb ELSE e END ORDER BY i)
        FROM jsonb_array_elements(minions) WITH ORDINALITY AS t(e, i)),
    updated_at = CURRENT_TIMESTAMP
WHERE boss_id = 'ice_witch' AND jsonb_typeof(minions) = 'array'
  AND EXISTS (SELECT 1 FROM jsonb_array_elements(minions) e
              WHERE e->>'minionId' = 'snowman' AND NOT (e ? 'deathBlastDamageFactor'));
