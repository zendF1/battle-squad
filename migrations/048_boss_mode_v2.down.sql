-- Undoes 048_boss_mode_v2: the seeded values that are still the v2 defaults go back to phase 1
-- (046), then the flying / hover columns go.

UPDATE config_bosses SET minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45}]'
    WHERE boss_id = 'steel_robot'
      AND minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45,"behavior":"bomber","movement":"fly","shotDamageFactor":0.35,"detonateRange":70,"flyRange":90,"hoverHeight":60,"flySpeed":220,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":4}'
    WHERE boss_id = 'steel_robot' AND waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":3}'::jsonb;

UPDATE config_bosses SET minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45}]'
    WHERE boss_id = 'lava_crab'
      AND minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45,"behavior":"bomber","movement":"walk","shotDamageFactor":0.35,"detonateRange":70,"walkRange":80,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":4}'
    WHERE boss_id = 'lava_crab' AND waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":3}'::jsonb;
UPDATE config_bosses SET map_id = 'arena_lava_crab' WHERE boss_id = 'lava_crab' AND map_id = 'arena_lava_crab_mound';

UPDATE config_bosses SET minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50}]'
    WHERE boss_id = 'ice_witch'
      AND minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50,"behavior":"shooter","movement":"none","shotDamageFactor":1,"explodeOnDeath":true,"explodeDamageOf":"target","explodeDamagePct":30,"explodeRadius":140,"explodeCrater":24}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":4}'
    WHERE boss_id = 'ice_witch' AND waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":3}'::jsonb;
UPDATE config_bosses SET map_id = 'arena_ice_witch' WHERE boss_id = 'ice_witch' AND map_id = 'arena_ice_witch_pillars';
UPDATE config_bosses SET turn_limit = 15 WHERE boss_id = 'ice_witch' AND turn_limit = 30;
UPDATE config_bosses SET hp = 24000 WHERE boss_id = 'ice_witch' AND hp = 30000;

ALTER TABLE config_bosses DROP COLUMN IF EXISTS hover;
ALTER TABLE config_bosses DROP COLUMN IF EXISTS flying;
