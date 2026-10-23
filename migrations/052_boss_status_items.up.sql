-- Boss status chance, harder status hits, 6 battle items in boss matches (gamedata.DefaultBosses).
-- The skills that put a status on the party (the crab's fire bombs: burn; the witch's ice spikes
-- and blizzard: freeze, frost beam: stun) gain statusChance 0.5 (each party member a cast hits
-- gets the status half of the time, rolled once per cast) and hit 1.3 x as hard (damage and the
-- rains' caps); the witch's HP 13000 -> 14000 (TestBossBalanceSim: 4 bots at the recommended
-- level still win about half). config_game_modes gains max_items (the battle items a player
-- brings into the mode's matches, NULL = rules.items maxLoadout), 6 for the boss lobby modes.
-- Idempotent: the column and its seed are added together, once; every other value changes only
-- while it is still boss v3's (050 / 051), so reruns keep admin edits.

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_game_modes' AND column_name = 'max_items') THEN
        ALTER TABLE config_game_modes ADD COLUMN max_items INT CHECK (max_items IS NULL OR max_items BETWEEN 0 AND 20);
        UPDATE config_game_modes SET max_items = 6 WHERE kind = 'lobby' AND queue LIKE 'boss:%';
    END IF;
END
$$;

-- Cua Dung Nham
UPDATE config_bosses SET skills = '[{"skillId":"fire_bombs","name":"Bom dung nham","description":"Mười quả bom lửa rơi khắp chiến trường; ai trúng có 50 % bị bỏng, mỗi chỗ nổ để lại vũng dung nham một lượt.","kind":"rain","weight":3,"cooldown":1,"target":"map","count":10,"damage":0.455,"blast":80,"crater":27,"cap":0.91,"status":"burn","statusTurns":1,"statusChance":0.5,"lavaRadius":40,"lavaTurns":1,"lavaDamage":120,"projectileKind":"magma_bomb"},{"skillId":"claw_slam","name":"Cú kẹp đập","description":"Càng đập xuống đất, sóng chấn động chạy ra hai mép map, trúng mọi người đứng trên mặt đất.","kind":"shockwave","weight":2,"cooldown":1,"damage":0.6,"blast":120,"crater":14,"step":150,"interval":0.08,"minFactor":0.4}]'
    WHERE boss_id = 'lava_crab' AND skills = '[{"skillId":"fire_bombs","name":"Bom dung nham","description":"Mười quả bom lửa rơi khắp chiến trường, gây bỏng và để lại vũng dung nham một lượt.","kind":"rain","weight":3,"cooldown":1,"target":"map","count":10,"damage":0.35,"blast":80,"crater":27,"cap":0.7,"status":"burn","statusTurns":1,"lavaRadius":40,"lavaTurns":1,"lavaDamage":120,"projectileKind":"magma_bomb"},{"skillId":"claw_slam","name":"Cú kẹp đập","description":"Càng đập xuống đất, sóng chấn động chạy ra hai mép map, trúng mọi người đứng trên mặt đất.","kind":"shockwave","weight":2,"cooldown":1,"damage":0.6,"blast":120,"crater":14,"step":150,"interval":0.08,"minFactor":0.4}]'::jsonb;

-- Phù Thuỷ Băng
UPDATE config_bosses SET skills = '[{"skillId":"ice_spikes","name":"Gai băng","description":"Mười bốn gai băng rơi rộng quanh một người chơi; ai trúng có 50 % bị đóng băng một lượt.","kind":"rain","weight":3,"cooldown":2,"target":"random","count":14,"spread":360,"damage":0.325,"blast":65,"crater":15,"cap":0.91,"status":"freeze","statusTurns":1,"statusChance":0.5,"projectileKind":"ice_spike"},{"skillId":"frost_beam","name":"Tia băng","description":"Hai mươi tia băng toả tròn đều từ phù thuỷ ra tới mép map, xuyên địa hình; ai trúng có 50 % bị choáng một lượt.","kind":"beam","weight":2,"cooldown":3,"count":20,"damage":1.04,"halfWidth":14,"pattern":"radial","status":"stun","statusTurns":1,"statusChance":0.5},{"skillId":"blizzard","name":"Bão tuyết","description":"Bốn mươi mảnh băng từ quanh phù thuỷ bay ra trút xuống khắp chiến trường; ai trúng có 50 % bị đóng băng một lượt.","kind":"rain","weight":2,"cooldown":3,"target":"map","count":40,"damage":0.195,"blast":50,"crater":12,"cap":0.78,"status":"freeze","statusTurns":1,"statusChance":0.5,"projectileKind":"ice_shard","launchFrom":"boss"}]'
    WHERE boss_id = 'ice_witch' AND skills = '[{"skillId":"ice_spikes","name":"Gai băng","description":"Mười bốn gai băng rơi rộng quanh một người chơi; ai trúng bị đóng băng một lượt.","kind":"rain","weight":3,"cooldown":2,"target":"random","count":14,"spread":360,"damage":0.25,"blast":65,"crater":15,"cap":0.7,"status":"freeze","statusTurns":1,"projectileKind":"ice_spike"},{"skillId":"frost_beam","name":"Tia băng","description":"Hai mươi tia băng toả tròn đều từ phù thuỷ ra tới mép map, xuyên địa hình; ai trúng bị choáng một lượt.","kind":"beam","weight":2,"cooldown":3,"count":20,"damage":0.8,"halfWidth":14,"pattern":"radial","status":"stun","statusTurns":1},{"skillId":"blizzard","name":"Bão tuyết","description":"Bốn mươi mảnh băng từ quanh phù thuỷ bay ra trút xuống khắp chiến trường; ai trúng bị đóng băng một lượt.","kind":"rain","weight":2,"cooldown":3,"target":"map","count":40,"damage":0.15,"blast":50,"crater":12,"cap":0.6,"status":"freeze","statusTurns":1,"projectileKind":"ice_shard","launchFrom":"boss"}]'::jsonb;
UPDATE config_bosses SET hp = 14000 WHERE boss_id = 'ice_witch' AND hp = 13000;
