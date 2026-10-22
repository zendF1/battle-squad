-- Boss mode: config_bosses (one row per boss: stats, skills, minions, waves, rewards, daily bonus
-- runs; gamedata.BossConfig, edited on the admin Boss page), a lobby mode per boss in the Boss
-- category (queue boss:<bossId>, party of 4) and a match mode of the same id as the queue (match
-- history shows the boss name), and player_boss_runs (the boss runs a player finished each UTC+7
-- day, per boss; the first daily_bonus_runs use the big reward table).
-- Idempotent: IF NOT EXISTS, seed rows ON CONFLICT DO NOTHING (admin edits survive reruns). The
-- arenas (config_bosses.map_id) are inserted by 045_boss_arenas.

CREATE TABLE IF NOT EXISTS config_bosses (
    boss_id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    recommended_level INT NOT NULL DEFAULT 1,
    enabled BOOLEAN NOT NULL DEFAULT true,
    sort_order INT NOT NULL DEFAULT 0,
    map_id TEXT NOT NULL DEFAULT '',
    hp INT NOT NULL DEFAULT 10000,
    defense INT NOT NULL DEFAULT 50,
    damage INT NOT NULL DEFAULT 400,
    body_size DOUBLE PRECISION NOT NULL DEFAULT 3,
    turn_limit INT NOT NULL DEFAULT 15,
    shielded_damage_factor DOUBLE PRECISION NOT NULL DEFAULT 0.1,
    out_of_map TEXT NOT NULL DEFAULT 'clamp',
    skills JSONB NOT NULL DEFAULT '[]',
    minions JSONB NOT NULL DEFAULT '[]',
    waves JSONB NOT NULL DEFAULT '{"initial":[],"maxAlive":0}',
    rewards JSONB NOT NULL DEFAULT '{}',
    daily_bonus_runs INT NOT NULL DEFAULT 3,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO config_bosses (boss_id, name, description, recommended_level, enabled, sort_order, map_id, hp, defense, damage,
    body_size, turn_limit, shielded_damage_factor, out_of_map, skills, minions, waves, rewards, daily_bonus_runs) VALUES
    ('steel_robot', 'Rô-bốt Thép',
     'Người khổng lồ cơ khí canh giữ xưởng sắt bỏ hoang. Nã pháo dồn dập, bắn tia laze xuyên đất và gọi drone che chắn.',
     10, true, 1, 'arena_steel_robot', 12000, 60, 420, 3, 15, 0.1, 'clamp',
     '[{"skillId":"shell_barrage","name":"Mưa đạn pháo","description":"Mười quả đạn pháo dội xuống quanh một người chơi.","kind":"rain","weight":3,"cooldown":0,"target":"random","count":10,"spread":180,"damage":0.35,"blast":55,"crater":14,"cap":1.2},{"skillId":"laser_beam","name":"Tia laze nghiền","description":"Tia laze xuyên địa hình bắn thẳng vào người yếu nhất, trúng mọi người trên đường.","kind":"beam","weight":2,"cooldown":2,"target":"lowest_hp","damage":1.3,"decay":0.85,"halfWidth":16},{"skillId":"drone_wave","name":"Gọi drone","description":"Thả hai drone. Còn drone thì Rô-bốt Thép được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"drone"}]',
     '[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,"hitChance":45}]',
     '{"initial":[{"minionId":"drone","count":2}],"maxAlive":4}',
     '{"minionDamageFactor":0.3,"bonus":{"baseGold":100,"goldPer1000Damage":60,"maxGold":1500,"winGold":400,"baseExp":40,"expPer1000Damage":25,"maxExp":600,"winExp":120,"damagePerDropRoll":2000,"maxDropRolls":4,"winDropRolls":1,"emptyWeight":2,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":2},{"itemId":"power_shot","weight":2,"min":1,"max":1},{"itemId":"teleport","weight":2,"min":1,"max":1},{"itemId":"drill_bomb","weight":2,"min":1,"max":1},{"itemId":"freeze_bomb","weight":1,"min":1,"max":1}]},"normal":{"baseGold":30,"goldPer1000Damage":15,"maxGold":400,"winGold":100,"baseExp":20,"expPer1000Damage":8,"maxExp":200,"winExp":40,"damagePerDropRoll":6000,"maxDropRolls":1,"winDropRolls":0,"emptyWeight":6,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":1},{"itemId":"power_shot","weight":1,"min":1,"max":1},{"itemId":"teleport","weight":1,"min":1,"max":1}]}}',
     3),
    ('lava_crab', 'Cua Dung Nham',
     'Con cua khổng lồ sống trong miệng núi lửa. Ném bom dung nham để lại vũng lửa, kẹp càng nghiền nát người đến gần.',
     25, true, 2, 'arena_lava_crab', 18000, 90, 480, 3.2, 15, 0.1, 'clamp',
     '[{"skillId":"fire_bombs","name":"Bom dung nham","description":"Bốn quả bom lửa rơi xuống, gây bỏng và để lại vũng dung nham hai lượt.","kind":"rain","weight":3,"cooldown":1,"target":"random","count":4,"spread":260,"damage":0.55,"blast":60,"crater":18,"cap":1,"status":"burn","statusTurns":1,"lavaRadius":50,"lavaTurns":2,"lavaDamage":150},{"skillId":"claw_slam","name":"Càng nghiền","description":"Một cú kẹp nặng giáng xuống người đứng gần nhất.","kind":"rain","weight":2,"cooldown":1,"target":"nearest","count":1,"damage":1.6,"blast":80,"crater":36},{"skillId":"crab_wave","name":"Gọi cua con","description":"Hai cua con bò ra. Còn cua con thì Cua Dung Nham được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"crab"}]',
     '[{"minionId":"crab","name":"Cua con","characterId":"tanko","hp":800,"defense":40,"damage":180,"moveEnergy":50,"bodySize":0.75,"hitChance":45}]',
     '{"initial":[{"minionId":"crab","count":2}],"maxAlive":4}',
     '{"minionDamageFactor":0.3,"bonus":{"baseGold":100,"goldPer1000Damage":80,"maxGold":2000,"winGold":520,"baseExp":40,"expPer1000Damage":32,"maxExp":800,"winExp":160,"damagePerDropRoll":2000,"maxDropRolls":4,"winDropRolls":1,"emptyWeight":2,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":2},{"itemId":"power_shot","weight":2,"min":1,"max":1},{"itemId":"teleport","weight":2,"min":1,"max":1},{"itemId":"drill_bomb","weight":2,"min":1,"max":1},{"itemId":"freeze_bomb","weight":1,"min":1,"max":1}]},"normal":{"baseGold":30,"goldPer1000Damage":20,"maxGold":520,"winGold":130,"baseExp":20,"expPer1000Damage":10,"maxExp":260,"winExp":52,"damagePerDropRoll":6000,"maxDropRolls":1,"winDropRolls":0,"emptyWeight":6,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":1},{"itemId":"power_shot","weight":1,"min":1,"max":1},{"itemId":"teleport","weight":1,"min":1,"max":1}]}}',
     3),
    ('ice_witch', 'Phù Thuỷ Băng',
     'Phù thuỷ của đỉnh tuyết vĩnh cửu. Gai băng đóng băng người trúng, tia băng giá giữ chân, bão tuyết phủ cả chiến trường.',
     40, true, 3, 'arena_ice_witch', 24000, 80, 520, 2.8, 15, 0.1, 'clamp',
     '[{"skillId":"ice_spikes","name":"Gai băng","description":"Tám gai băng rơi quanh một người chơi; ai trúng bị đóng băng một lượt.","kind":"rain","weight":3,"cooldown":2,"target":"random","count":8,"spread":220,"damage":0.3,"blast":45,"crater":10,"cap":0.9,"status":"freeze","statusTurns":1},{"skillId":"frost_beam","name":"Tia băng giá","description":"Tia băng xuyên địa hình bắn vào người yếu nhất, giữ chân mọi người trúng một lượt.","kind":"beam","weight":2,"cooldown":1,"target":"lowest_hp","damage":1.1,"decay":0.9,"halfWidth":14,"status":"root","statusTurns":1},{"skillId":"blizzard","name":"Bão tuyết","description":"Mười sáu mảnh băng rải khắp chiến trường, ai trúng bị lưới băng làm chậm.","kind":"rain","weight":2,"cooldown":1,"target":"random","count":16,"spread":420,"damage":0.18,"blast":40,"crater":8,"cap":0.8,"status":"net","statusTurns":1},{"skillId":"snowman_wave","name":"Gọi người tuyết","description":"Hai người tuyết hiện ra. Còn người tuyết thì Phù Thuỷ Băng được che chắn.","kind":"summon","weight":2,"cooldown":3,"count":2,"minionId":"snowman"}]',
     '[{"minionId":"snowman","name":"Người tuyết","characterId":"flora","hp":900,"defense":30,"damage":170,"moveEnergy":40,"bodySize":0.85,"hitChance":50}]',
     '{"initial":[{"minionId":"snowman","count":3}],"maxAlive":4}',
     '{"minionDamageFactor":0.3,"bonus":{"baseGold":100,"goldPer1000Damage":96,"maxGold":2400,"winGold":640,"baseExp":40,"expPer1000Damage":40,"maxExp":960,"winExp":192,"damagePerDropRoll":2000,"maxDropRolls":4,"winDropRolls":1,"emptyWeight":2,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":2},{"itemId":"power_shot","weight":2,"min":1,"max":1},{"itemId":"teleport","weight":2,"min":1,"max":1},{"itemId":"drill_bomb","weight":2,"min":1,"max":1},{"itemId":"freeze_bomb","weight":1,"min":1,"max":1}]},"normal":{"baseGold":30,"goldPer1000Damage":24,"maxGold":640,"winGold":160,"baseExp":20,"expPer1000Damage":13,"maxExp":320,"winExp":64,"damagePerDropRoll":6000,"maxDropRolls":1,"winDropRolls":0,"emptyWeight":6,"drops":[{"itemId":"medkit","weight":3,"min":1,"max":1},{"itemId":"power_shot","weight":1,"min":1,"max":1},{"itemId":"teleport","weight":1,"min":1,"max":1}]}}',
     3)
ON CONFLICT (boss_id) DO NOTHING;

DROP TRIGGER IF EXISTS gamedata_revision_bump ON config_bosses;
CREATE TRIGGER gamedata_revision_bump AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON config_bosses
    FOR EACH STATEMENT EXECUTE FUNCTION bump_gamedata_revision();

-- One lobby mode per boss (the Boss tab) and the match mode its matches play as
INSERT INTO config_game_modes (mode_id, kind, name, description, queue, bot, sort_order, category, icon, party_size, ranked, enabled) VALUES
    ('boss_steel_robot', 'lobby', 'Rô-bốt Thép', 'Đánh boss, tối đa 4 người', 'boss:steel_robot', '', 11, 'boss', 'boss', 4, false, true),
    ('boss_lava_crab', 'lobby', 'Cua Dung Nham', 'Đánh boss, tối đa 4 người', 'boss:lava_crab', '', 12, 'boss', 'boss', 4, false, true),
    ('boss_ice_witch', 'lobby', 'Phù Thuỷ Băng', 'Đánh boss, tối đa 4 người', 'boss:ice_witch', '', 13, 'boss', 'boss', 4, false, true),
    ('boss:steel_robot', 'match', 'Rô-bốt Thép', '', '', '', 21, '', '', 1, false, true),
    ('boss:lava_crab', 'match', 'Cua Dung Nham', '', '', '', 22, '', '', 1, false, true),
    ('boss:ice_witch', 'match', 'Phù Thuỷ Băng', '', '', '', 23, '', '', 1, false, true)
ON CONFLICT (mode_id) DO NOTHING;

-- The boss runs a player finished per UTC+7 day (mission.PeriodsAt), per boss
CREATE TABLE IF NOT EXISTS player_boss_runs (
    player_id VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    boss_id TEXT NOT NULL,
    day TEXT NOT NULL,
    runs INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (player_id, boss_id, day)
);
