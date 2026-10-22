-- GET /gamedata: the Vietnamese display texts (and the display order) that only the Godot
-- client had (battle-squad-game @935c5af), so the server sends them and admins edit them.
-- Character role names, play styles and gun kinds; item names and order; skill description
-- templates ({placeholder} = a number of the skill's rank in GET /gamedata skills[].ranks);
-- status effect, rank tier, equipment tier, mode names and the matchmaking tips.
-- Idempotent: columns and tables IF NOT EXISTS; a text is filled only while it is still empty
-- (an order while 0) and seed rows ON CONFLICT DO NOTHING, so reruns keep admin edits.

ALTER TABLE config_characters ADD COLUMN IF NOT EXISTS role_name TEXT NOT NULL DEFAULT '';
ALTER TABLE config_characters ADD COLUMN IF NOT EXISTS playstyle TEXT NOT NULL DEFAULT '';
ALTER TABLE config_characters ADD COLUMN IF NOT EXISTS gun_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE config_characters ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
ALTER TABLE config_items ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE config_items ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0;
ALTER TABLE config_skills ADD COLUMN IF NOT EXISTS description_template TEXT NOT NULL DEFAULT '';

UPDATE config_characters SET role_name = 'Cân bằng' WHERE character_id = 'rookie' AND role_name = '';
UPDATE config_characters SET playstyle = 'Không nổi bật hẳn mặt nào nhưng cũng chẳng có điểm yếu rõ ràng: máu, sát thương và tầm di chuyển đều vừa phải, đạn chịu gió bình thường. Hợp để tập căn góc và lực; khi đầy năng lượng, Mưa tên lửa trút xuống cả một vùng quanh pháo hiệu, khó né và đốt máu đối thủ ở lượt sau. Đổi lại, Rookie đánh không đau bằng Spark và không lì bằng Flora.' WHERE character_id = 'rookie' AND playstyle = '';
UPDATE config_characters SET gun_kind = 'bazooka' WHERE character_id = 'rookie' AND gun_kind = '';
UPDATE config_characters SET sort_order = 1 WHERE character_id = 'rookie' AND sort_order = 0;
UPDATE config_characters SET role_name = 'Phá địa hình' WHERE character_id = 'tanko' AND role_name = '';
UPDATE config_characters SET playstyle = 'Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Bom liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.' WHERE character_id = 'tanko' AND playstyle = '';
UPDATE config_characters SET gun_kind = 'cannon' WHERE character_id = 'tanko' AND gun_kind = '';
UPDATE config_characters SET sort_order = 2 WHERE character_id = 'tanko' AND sort_order = 0;
UPDATE config_characters SET role_name = 'Sát thương' WHERE character_id = 'spark' AND role_name = '';
UPDATE config_characters SET playstyle = 'Đánh đau nhất: chỉ vài phát trúng là hạ được đối thủ, còn Tia laze xuyên địa hình, trúng mọi địch trên đường và làm họ mất lượt sau. Spark chạy xa nhất nên dễ đổi chỗ tìm góc bắn hoặc né khỏi tầm ngắm. Điểm yếu là máu và phòng thủ mỏng nhất, đạn nhẹ bị gió đẩy mạnh nên phải tính gió thật kỹ.' WHERE character_id = 'spark' AND playstyle = '';
UPDATE config_characters SET gun_kind = 'shock_rifle' WHERE character_id = 'spark' AND gun_kind = '';
UPDATE config_characters SET sort_order = 3 WHERE character_id = 'spark' AND sort_order = 0;
UPDATE config_characters SET role_name = 'Bền bỉ' WHERE character_id = 'flora' AND role_name = '';
UPDATE config_characters SET playstyle = 'Lì đòn nhất: máu và phòng thủ cao nhất, lại có Hoa hồi xuân hồi máu cả đội, máu dư thành khiên, nên càng đánh lâu càng có lợi. Hãy đứng vững, bắn chắc tay và kéo trận dài để đối thủ cạn máu trước. Bù lại sát thương thấp và đạn gần như không khoét đất, nên khó kết thúc trận nhanh.' WHERE character_id = 'flora' AND playstyle = '';
UPDATE config_characters SET gun_kind = 'seed_launcher' WHERE character_id = 'flora' AND gun_kind = '';
UPDATE config_characters SET sort_order = 4 WHERE character_id = 'flora' AND sort_order = 0;

UPDATE config_items SET display_name = 'Hộp cứu thương' WHERE item_id = 'medkit' AND display_name = '';
UPDATE config_items SET sort_order = 1 WHERE item_id = 'medkit' AND sort_order = 0;
UPDATE config_items SET display_name = 'Dịch chuyển' WHERE item_id = 'teleport' AND display_name = '';
UPDATE config_items SET sort_order = 2 WHERE item_id = 'teleport' AND sort_order = 0;
UPDATE config_items SET display_name = 'Tăng lực' WHERE item_id = 'power_shot' AND display_name = '';
UPDATE config_items SET sort_order = 3 WHERE item_id = 'power_shot' AND sort_order = 0;
UPDATE config_items SET display_name = 'Bom khoan' WHERE item_id = 'drill_bomb' AND display_name = '';
UPDATE config_items SET sort_order = 4 WHERE item_id = 'drill_bomb' AND sort_order = 0;
UPDATE config_items SET display_name = 'Lưới nhện' WHERE item_id = 'spider_net' AND display_name = '';
UPDATE config_items SET sort_order = 5 WHERE item_id = 'spider_net' AND sort_order = 0;
UPDATE config_items SET display_name = 'Bom băng' WHERE item_id = 'freeze_bomb' AND display_name = '';
UPDATE config_items SET sort_order = 6 WHERE item_id = 'freeze_bomb' AND sort_order = 0;
UPDATE config_items SET display_name = 'Không kích' WHERE item_id = 'air_strike' AND display_name = '';
UPDATE config_items SET sort_order = 7 WHERE item_id = 'air_strike' AND sort_order = 0;
UPDATE config_items SET display_name = 'Dừng gió' WHERE item_id = 'wind_stopper' AND display_name = '';
UPDATE config_items SET sort_order = 8 WHERE item_id = 'wind_stopper' AND sort_order = 0;

UPDATE config_skills SET description_template = 'Bắn pháo hiệu, {count} tên lửa rơi quanh chỗ đáp (+{countPerRank} mỗi cấp súng), mỗi quả {damagePct}% sát thương; địch trúng bị bỏng: mất {burnPct}% máu tối đa đầu lượt sau.' WHERE skill_id = 'triple_shot' AND description_template = '';
UPDATE config_skills SET description_template = 'Bắn {count} quả bom lớn liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng), đào sâu một hố; mỗi quả {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị cầm chân lượt sau.' WHERE skill_id = 'heavy_bomb' AND description_template = '';
UPDATE config_skills SET description_template = 'Tia thẳng xuyên địa hình tới mép bản đồ, trúng mọi địch trên đường: {firstPct}% sát thương, mỗi địch sau giảm {decayPct}%; địch trúng bị choáng, mất lượt sau.' WHERE skill_id = 'shock_field' AND description_template = '';
UPDATE config_skills SET description_template = 'Hồi máu cả đội: Flora {selfPct}%, đồng đội {allyPct}% máu tối đa (+{perRankPct}% mỗi cấp súng); máu dư thành khiên (tối đa {shieldCapPct}% máu); giải mọi hiệu ứng xấu.' WHERE skill_id = 'healing_bloom' AND description_template = '';

CREATE TABLE IF NOT EXISTS config_status_effects (
    effect_id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    marker TEXT NOT NULL DEFAULT '',  -- above the player's head while it is active (shot modifiers)
    banner TEXT NOT NULL DEFAULT '',  -- the notice while it skips the turn or blocks moving
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_status_effects (effect_id, name, marker, banner, sort_order) VALUES
    ('burn', 'Bỏng', '', '', 1),
    ('root', 'Bị cầm chân', '', 'Bị cầm chân', 2),
    ('stun', 'Choáng', '', 'Choáng — mất lượt', 3),
    ('freeze', 'Bị đóng băng', '', 'Đóng băng — mất lượt', 4),
    ('net', 'Bị lưới', '', '', 5),
    ('power_shot', 'Tăng lực', 'x1.5', '', 6),
    ('drill_bomb', 'Bom khoan', 'Khoan', '', 7),
    ('spider_net', 'Lưới nhện', 'Đạn lưới', '', 8),
    ('freeze_bomb', 'Bom băng', 'Đạn băng', '', 9),
    ('air_strike', 'Không kích', 'Không kích', '', 10),
    ('wind_stop', 'Dừng gió', '', '', 11)
ON CONFLICT (effect_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS config_rank_tiers (
    tier TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_rank_tiers (tier, name, sort_order) VALUES
    ('bronze', 'Đồng', 1),
    ('silver', 'Bạc', 2),
    ('gold', 'Vàng', 3),
    ('platinum', 'Bạch kim', 4),
    ('diamond', 'Kim cương', 5),
    ('master', 'Cao thủ', 6)
ON CONFLICT (tier) DO NOTHING;

CREATE TABLE IF NOT EXISTS config_equipment_tiers (
    tier TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'normal',  -- normal (shop, id suffix lvN) or crafted
    id_suffix TEXT NOT NULL DEFAULT '',  -- the lvN ending the ids of a normal tier's items
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_equipment_tiers (tier, name, kind, id_suffix, sort_order) VALUES
    ('iron', 'Sắt', 'normal', 'lv10', 1),
    ('steel', 'Thép', 'normal', 'lv20', 2),
    ('mithril', 'Mithril', 'normal', 'lv30', 3),
    ('dragon', 'Rồng', 'normal', 'lv40', 4),
    ('silver', 'Bạc', 'crafted', '', 5),
    ('gold', 'Vàng', 'crafted', '', 6),
    ('titan', 'Titan', 'crafted', '', 7),
    ('diamond', 'Kim cương', 'crafted', '', 8)
ON CONFLICT (tier) DO NOTHING;

CREATE TABLE IF NOT EXISTS config_game_modes (
    mode_id TEXT PRIMARY KEY,
    kind TEXT NOT NULL DEFAULT 'match',  -- lobby: a choice on Home; match: a match's mode (history, results)
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    queue TEXT NOT NULL DEFAULT '',  -- lobby: the matchmaking queue it joins
    bot TEXT NOT NULL DEFAULT '',  -- lobby: the QuickPlay bot it plays against
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_game_modes (mode_id, kind, name, description, queue, bot, sort_order) VALUES
    ('practice', 'lobby', 'Luyện tập', 'Tập bắn với bia đứng yên', '', '', 1),
    ('ranked', 'lobby', 'Xếp hạng 2v2', 'Ghép 2v2, tính điểm hạng', 'ranked_2v2', '', 2),
    ('bot', 'lobby', 'Đấu với máy', 'Đấu 1v1 với máy biết bắn', '', 'ai', 3),
    ('pvp_1v1', 'match', 'Đấu 1v1', '', '', '', 4),
    ('pvp_2v2', 'match', 'Đấu 2v2', '', '', '', 5),
    ('ranked_2v2', 'match', 'Xếp hạng 2v2', '', '', '', 6)
ON CONFLICT (mode_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS config_tips (
    tip_id TEXT PRIMARY KEY,
    text TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_tips (tip_id, text, sort_order) VALUES
    ('tip_01', 'Gió mạnh đẩy đạn rất xa: nhìn mũi tên gió trước khi bắn.', 1),
    ('tip_02', 'Bom liên hoàn của Tanko đào hố sâu, địch trúng bị cầm chân lượt sau.', 2),
    ('tip_03', 'Tia laze của Spark xuyên địa hình và làm choáng mọi địch trên đường bắn.', 3),
    ('tip_04', 'Hoa hồi xuân của Flora hồi máu cả đội và giải mọi hiệu ứng xấu.', 4),
    ('tip_05', 'Mưa tên lửa của Rookie làm địch bỏng, mất máu đầu lượt sau.', 5),
    ('tip_06', 'Bắn vào chân địch: hố sâu làm chúng rơi xuống và mất máu.', 6),
    ('tip_07', 'Bom băng đóng băng địch một lượt, cả đội tha hồ dồn sát thương.', 7),
    ('tip_08', 'Dịch chuyển giúp thoát khỏi hố sâu hoặc chiếm chỗ cao.', 8),
    ('tip_09', 'Bị địch bắn trúng cũng nạp thêm năng lượng kỹ năng.', 9),
    ('tip_10', 'Thắng trận xếp hạng để lên điểm, thua sẽ bị trừ điểm.', 10),
    ('tip_11', 'Rời trận giữa chừng tính là thua, kể cả khi đồng đội thắng.', 11),
    ('tip_12', 'Khiên chịu sát thương trước máu: máu hồi dư của Flora thành khiên.', 12)
ON CONFLICT (tip_id) DO NOTHING;
