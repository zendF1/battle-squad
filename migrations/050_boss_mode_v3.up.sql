-- Boss v3: free summons, wide boss skills, leaping snowmen, the witch's sky ice islands
-- (gamedata.DefaultBosses; the arena is 051_boss_arena_islands).
-- config_bosses gains summon ({minionId, count, cooldownRounds, firstRound}, NULL = none): the boss
-- summons for free at the start of its turn (3 drones, 4 crabs, 5 snowmen; waves.maxAlive the
-- same) and still attacks; the summon skills leave the skill lists. Skills: the robot's shell
-- rain over 70 % of the map around the party and its 5-beam horizontal sweep, the crab's map-wide
-- fire bombs and ground shockwave, the witch's wider ice spikes, 20-beam radial burst (stun) and
-- map-wide blizzard (freeze); boss damage x 1.5. Minions: drones fly 270 px, crabs walk 240 px
-- (move energy 120), bombers' shots 0.8 x, snowmen leapers with shots 1.3 x. The witch moves to
-- arena_ice_witch_islands.
-- Idempotent: the column and its seed are added together, once; every other value changes only
-- while it is still boss v2's (046 / 048 / 049), so reruns keep admin edits.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_bosses' AND column_name = 'summon') THEN
        ALTER TABLE config_bosses ADD COLUMN summon JSONB;
        UPDATE config_bosses SET summon = '{"minionId":"drone","count":3,"cooldownRounds":3,"firstRound":2}' WHERE boss_id = 'steel_robot';
        UPDATE config_bosses SET summon = '{"minionId":"crab","count":4,"cooldownRounds":3,"firstRound":2}' WHERE boss_id = 'lava_crab';
        UPDATE config_bosses SET summon = '{"minionId":"snowman","count":5,"cooldownRounds":3,"firstRound":2}' WHERE boss_id = 'ice_witch';
    END IF;
END
$$;

-- Rô-bốt Thép
UPDATE config_bosses SET skills = '[{"skillId":"shell_barrage","name":"Mưa đạn pháo","description":"Hai mươi bốn quả đạn pháo dội xuống bảy phần mười chiến trường quanh cả đội.","kind":"rain","weight":3,"cooldown":0,"target":"party","count":24,"spread":840,"damage":0.3,"blast":75,"crater":21,"cap":1,"projectileKind":"steel_shell"},{"skillId":"laser_beam","name":"Tia laze nghiền","description":"Năm tia laze xuyên địa hình quét ngang toàn map ở độ cao của từng người chơi.","kind":"beam","weight":2,"cooldown":2,"count":5,"damage":0.9,"halfWidth":16,"pattern":"sweep"}]'
    WHERE boss_id = 'steel_robot' AND skills = '[{"skillId":"shell_barrage","name":"Mưa đạn pháo","description":"Mười quả đạn pháo dội xuống quanh một người chơi.","kind":"rain","weight":3,"cooldown":0,"target":"random","count":10,"spread":180,"damage":0.35,"blast":55,"crater":14,"cap":1.2},{"skillId":"laser_beam","name":"Tia laze nghiền","description":"Tia laze xuyên địa hình bắn thẳng vào người yếu nhất, trúng mọi người trên đường.","kind":"beam","weight":2,"cooldown":2,"target":"lowest_hp","damage":1.3,"decay":0.85,"halfWidth":16},{"skillId":"drone_wave","name":"Gọi drone","description":"Thả hai drone. Còn drone thì Rô-bốt Thép được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"drone"}]'::jsonb;
UPDATE config_bosses SET minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45,"behavior":"bomber","movement":"fly","shotDamageFactor":0.8,"detonateRange":70,"flyRange":270,"hoverHeight":60,"flySpeed":220,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20,"projectileKind":"drone_bolt"}]'
    WHERE boss_id = 'steel_robot' AND minions = '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45,"behavior":"bomber","movement":"fly","shotDamageFactor":0.35,"detonateRange":70,"flyRange":90,"hoverHeight":60,"flySpeed":220,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":3}'
    WHERE boss_id = 'steel_robot' AND waves = '{"initial":[{"minionId":"drone","count":2}],"maxAlive":3}'::jsonb;
UPDATE config_bosses SET description = 'Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo phủ kín chiến trường, quét năm tia laze ngang map và thả drone che chắn.'
    WHERE boss_id = 'steel_robot' AND description = 'Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo dồn dập, bắn tia laze xuyên đất và gọi drone che chắn.';
UPDATE config_bosses SET damage = 630 WHERE boss_id = 'steel_robot' AND damage = 420;
UPDATE config_bosses SET hp = 10000 WHERE boss_id = 'steel_robot' AND hp = 12000;

-- Cua Dung Nham
UPDATE config_bosses SET skills = '[{"skillId":"fire_bombs","name":"Bom dung nham","description":"Mười sáu quả bom lửa rơi khắp chiến trường, gây bỏng và để lại vũng dung nham hai lượt.","kind":"rain","weight":3,"cooldown":1,"target":"map","count":10,"damage":0.35,"blast":80,"crater":27,"cap":0.7,"status":"burn","statusTurns":1,"lavaRadius":40,"lavaTurns":1,"lavaDamage":120,"projectileKind":"magma_bomb"},{"skillId":"claw_slam","name":"Cú kẹp đập","description":"Càng đập xuống đất, sóng chấn động chạy ra hai mép map, trúng mọi người đứng trên mặt đất.","kind":"shockwave","weight":2,"cooldown":1,"damage":0.6,"blast":120,"crater":14,"step":150,"interval":0.08,"minFactor":0.4}]'
    WHERE boss_id = 'lava_crab' AND skills = '[{"skillId":"fire_bombs","name":"Bom dung nham","description":"Bốn quả bom lửa rơi xuống, gây bỏng và để lại vũng dung nham hai lượt.","kind":"rain","weight":3,"cooldown":1,"target":"random","count":4,"spread":260,"damage":0.55,"blast":60,"crater":18,"cap":1,"status":"burn","statusTurns":1,"lavaRadius":50,"lavaTurns":2,"lavaDamage":150},{"skillId":"claw_slam","name":"Càng nghiền","description":"Một cú kẹp nặng giáng xuống người đứng gần nhất.","kind":"rain","weight":2,"cooldown":1,"target":"nearest","count":1,"damage":1.6,"blast":80,"crater":36},{"skillId":"crab_wave","name":"Gọi cua con","description":"Hai cua con bò ra. Còn cua con thì Cua Dung Nham được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"crab"}]'::jsonb;
UPDATE config_bosses SET minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":120,"bodySize":0.75,"hitChance":45,"behavior":"bomber","movement":"walk","shotDamageFactor":0.8,"detonateRange":70,"walkRange":240,"explodeDamageOf":"target","explodeDamagePct":35,"explodeRadius":80,"explodeCrater":20,"projectileKind":"crab_spit"}]'
    WHERE boss_id = 'lava_crab' AND minions = '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45,"behavior":"bomber","movement":"walk","shotDamageFactor":0.35,"detonateRange":70,"walkRange":80,"explodeDamageOf":"target","explodeDamagePct":50,"explodeRadius":80,"explodeCrater":20}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":4}'
    WHERE boss_id = 'lava_crab' AND waves = '{"initial":[{"minionId":"crab","count":2}],"maxAlive":3}'::jsonb;
UPDATE config_bosses SET description = 'Con cua khổng lồ sống trong miệng núi lửa. Ném bom dung nham khắp map để lại vũng lửa, đập càng tạo sóng chấn động chạy dọc mặt đất.'
    WHERE boss_id = 'lava_crab' AND description = 'Con cua khổng lồ sống trong miệng núi lửa. Ném bom dung nham để lại vũng lửa, kẹp càng nghiền nát người đến gần.';
UPDATE config_bosses SET damage = 720 WHERE boss_id = 'lava_crab' AND damage = 480;
UPDATE config_bosses SET hp = 15000 WHERE boss_id = 'lava_crab' AND hp = 18000;

-- Phù Thuỷ Băng
UPDATE config_bosses SET skills = '[{"skillId":"ice_spikes","name":"Gai băng","description":"Mười bốn gai băng rơi rộng quanh một người chơi; ai trúng bị đóng băng một lượt.","kind":"rain","weight":3,"cooldown":2,"target":"random","count":14,"spread":360,"damage":0.25,"blast":65,"crater":15,"cap":0.7,"status":"freeze","statusTurns":1,"projectileKind":"ice_spike"},{"skillId":"frost_beam","name":"Tia băng","description":"Hai mươi tia băng toả tròn đều từ phù thuỷ ra tới mép map, xuyên địa hình; ai trúng bị choáng một lượt.","kind":"beam","weight":2,"cooldown":3,"count":20,"damage":0.8,"halfWidth":14,"pattern":"radial","status":"stun","statusTurns":1},{"skillId":"blizzard","name":"Bão tuyết","description":"Bốn mươi mảnh băng từ quanh phù thuỷ bay ra trút xuống khắp chiến trường; ai trúng bị đóng băng một lượt.","kind":"rain","weight":2,"cooldown":3,"target":"map","count":40,"damage":0.15,"blast":50,"crater":12,"cap":0.6,"status":"freeze","statusTurns":1,"projectileKind":"ice_shard","launchFrom":"boss"}]'
    WHERE boss_id = 'ice_witch' AND skills = '[{"skillId":"ice_spikes","name":"Gai băng","description":"Tám gai băng rơi quanh một người chơi; ai trúng bị đóng băng một lượt.","kind":"rain","weight":3,"cooldown":2,"target":"random","count":8,"spread":220,"damage":0.3,"blast":45,"crater":10,"cap":0.9,"status":"freeze","statusTurns":1},{"skillId":"frost_beam","name":"Tia băng giá","description":"Tia băng xuyên địa hình bắn vào người yếu nhất, giữ chân mọi người trúng một lượt.","kind":"beam","weight":2,"cooldown":1,"target":"lowest_hp","damage":1.1,"decay":0.9,"halfWidth":14,"status":"root","statusTurns":1},{"skillId":"blizzard","name":"Bão tuyết","description":"Mười sáu mảnh băng rải khắp chiến trường, ai trúng bị lưới băng làm chậm.","kind":"rain","weight":2,"cooldown":1,"target":"random","count":16,"spread":420,"damage":0.18,"blast":40,"crater":8,"cap":0.8,"status":"net","statusTurns":1},{"skillId":"snowman_wave","name":"Gọi người tuyết","description":"Hai người tuyết hiện ra. Còn người tuyết thì Phù Thuỷ Băng được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"snowman"}]'::jsonb;
UPDATE config_bosses SET minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50,"behavior":"leaper","movement":"none","shotDamageFactor":1.3,"detonateRange":70,"explodeOnDeath":true,"explodeDamageOf":"target","explodeDamagePct":15,"explodeRadius":140,"explodeCrater":24,"leapDetonateNextTurn":true,"projectileKind":"snowball"}]'
    WHERE boss_id = 'ice_witch' AND minions = '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50,"behavior":"shooter","movement":"none","shotDamageFactor":1,"explodeOnDeath":true,"explodeDamageOf":"target","explodeDamagePct":30,"explodeRadius":140,"explodeCrater":24}]'::jsonb;
UPDATE config_bosses SET waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":5}'
    WHERE boss_id = 'ice_witch' AND waves = '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":3}'::jsonb;
UPDATE config_bosses SET description = 'Phù thuỷ của đỉnh tuyết vĩnh cửu, bay giữa các đảo băng. Hai mươi tia băng toả tròn làm choáng, bão tuyết phủ cả map đóng băng người trúng, người tuyết nhảy tới nổ tung.'
    WHERE boss_id = 'ice_witch' AND description = 'Phù thuỷ của đỉnh tuyết vĩnh cửu. Gai băng đóng băng người trúng, tia băng giá giữ chân, bão tuyết phủ cả chiến trường.';
UPDATE config_bosses SET damage = 780 WHERE boss_id = 'ice_witch' AND damage = 520;
UPDATE config_bosses SET hp = 13000 WHERE boss_id = 'ice_witch' AND hp = 30000;
UPDATE config_bosses SET map_id = 'arena_ice_witch_islands'
    WHERE boss_id = 'ice_witch' AND map_id = 'arena_ice_witch_pillars';
