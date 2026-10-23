-- Boss v2: mobile bosses and bomber minions (gamedata.DefaultBosses).
-- config_bosses gains flying (the boss never falls; UnitFlew moves it) and hover (the band a
-- flying boss hovers in: {minX, maxX, minY, maxY, minMove, flySpeed}, NULL = none). The minion
-- JSON gains behavior (shooter / bomber), movement (none / walk / fly), shotDamageFactor,
-- detonateRange, flyRange, walkRange, hoverHeight, flySpeed, explodeOnDeath, explodeDamageOf,
-- explodeDamagePct, explodeRadius, explodeCrater.
-- Seeded bosses: the drones fly and bomb, the crabs walk and bomb (on arena_lava_crab_mound, 047),
-- the snowmen stand and blast when they die and the witch hovers for 30 turns with 30000 HP (on
-- arena_ice_witch_pillars, 047); at most 3 minions alive each.
-- Idempotent: the columns and the witch's flying / hover are added together, once; every other
-- value changes only while it is still the phase 1 default (046), so reruns keep admin edits.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_bosses' AND column_name = 'flying') THEN
        ALTER TABLE config_bosses ADD COLUMN flying BOOLEAN NOT NULL DEFAULT false;
        ALTER TABLE config_bosses ADD COLUMN hover JSONB;
        UPDATE config_bosses SET flying = true,
            hover = '{"minX":400,"maxX":2000,"minY":200,"maxY":430,"minMove":200,"flySpeed":220}'
            WHERE boss_id = 'ice_witch';
    END IF;
END
$$;

-- Rô-bốt Thép: flying bomber drones, at most 3
UPDATE config_bosses SET minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45,"behavior":"bomber","movement":"fly","shotDamageFactor":0.35,"detonateRange":70,"flyRange":90,"hoverHeight":60,"flySpeed":220,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'
    WHERE boss_id = 'steel_robot'
      AND minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":3}'
    WHERE boss_id = 'steel_robot' AND waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":4}'::jsonb;

-- Cua Dung Nham: walking bomber crabs (80 px a turn), the mound arena, at most 3
UPDATE config_bosses SET minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45,"behavior":"bomber","movement":"walk","shotDamageFactor":0.35,"detonateRange":70,"walkRange":80,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'
    WHERE boss_id = 'lava_crab'
      AND minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":3}'
    WHERE boss_id = 'lava_crab' AND waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":4}'::jsonb;
UPDATE config_bosses SET map_id = 'arena_lava_crab_mound' WHERE boss_id = 'lava_crab' AND map_id = 'arena_lava_crab';

-- Phù Thuỷ Băng: stationary snowmen with a death blast, the pillar arena, 30 turns, 30000 HP, at most 3
UPDATE config_bosses SET minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50,"behavior":"shooter","movement":"none","shotDamageFactor":1,"explodeOnDeath":true,"explodeDamageOf":"target","explodeDamagePct":30,"explodeRadius":140,"explodeCrater":24}]'
    WHERE boss_id = 'ice_witch'
      AND minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":3}'
    WHERE boss_id = 'ice_witch' AND waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":4}'::jsonb;
UPDATE config_bosses SET map_id = 'arena_ice_witch_pillars' WHERE boss_id = 'ice_witch' AND map_id = 'arena_ice_witch';
UPDATE config_bosses SET turn_limit = 30 WHERE boss_id = 'ice_witch' AND turn_limit = 15;
UPDATE config_bosses SET hp = 30000 WHERE boss_id = 'ice_witch' AND hp = 24000;
