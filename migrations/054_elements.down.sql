-- Undoes 054_elements: Tanko's old texts (while still 054's), Tide's rows nobody owns or bought
-- (her character, weapon and skill only when no player has her: she then stays playable), her gun
-- kind, type word, offers and tips while unchanged, the rules rows, the bounce keys of
-- rules.physics, the element and bounces columns and config_elements.

UPDATE config_skills SET name = 'Bom liên hoàn', updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND name = 'Địa chấn liên hoàn';
UPDATE config_skills SET description = 'Bắn liên tiếp bom lớn cùng một đường bay (2 quả, +1 mỗi cấp súng), đào sâu một hố; mỗi quả 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND description = 'Bắn liên tiếp những tảng đá bọc đất cùng một đường bay (2 tảng, +1 mỗi cấp súng): mặt đất nứt toác, gai đá trồi quanh miệng hố, hố mỗi lúc một sâu; mỗi tảng 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị đất đá vùi chân, cầm chân lượt sau.';
UPDATE config_skills SET description_template = 'Bắn {count} quả bom lớn liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng), đào sâu một hố; mỗi quả {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND description_template = 'Bắn {count} tảng đá bọc đất liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng): mặt đất nứt toác, gai đá trồi quanh miệng hố, hố mỗi lúc một sâu; mỗi tảng {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị đất đá vùi chân, cầm chân lượt sau.';
UPDATE config_characters SET playstyle = 'Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Bom liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.',
    updated_at = CURRENT_TIMESTAMP
WHERE character_id = 'tanko' AND playstyle = 'Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Địa chấn liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.';
UPDATE config_tips SET text = 'Bom liên hoàn của Tanko đào hố sâu, địch trúng bị cầm chân lượt sau.', updated_at = CURRENT_TIMESTAMP
WHERE tip_id = 'tip_02' AND text = 'Địa chấn liên hoàn của Tanko làm nứt đất, đào hố sâu; địch trúng bị đất đá vùi chân, cầm chân lượt sau.';

DELETE FROM config_tips WHERE (tip_id, text) IN (('tip_13', 'Ngũ hành: Kim khắc Mộc, Mộc khắc Thổ, Thổ khắc Thủy, Thủy khắc Hỏa, Hỏa khắc Kim. Bắn hệ bị khắc gây thêm sát thương.'), ('tip_14', 'Nổ bông tuyết của Tide đóng băng mọi địch quanh cô, kể cả boss đang bay: hãy áp sát rồi kích hoạt.'));
DELETE FROM shop_offers o WHERE o.offer_id IN ('unlock_tide_coin', 'unlock_tide_gem')
    AND NOT EXISTS (SELECT 1 FROM shop_purchases p WHERE p.offer_id = o.offer_id);

DELETE FROM config_characters WHERE character_id = 'tide'
    AND NOT EXISTS (SELECT 1 FROM player_characters WHERE character_id = 'tide');
DELETE FROM config_skills WHERE skill_id = 'snow_nova' AND NOT EXISTS (SELECT 1 FROM config_characters WHERE skill_id = 'snow_nova');
DELETE FROM config_weapons WHERE weapon_id = 'water_orb' AND NOT EXISTS (SELECT 1 FROM config_characters WHERE weapon_id = 'water_orb');
DELETE FROM config_item_type_words WHERE word = 'water gun' AND name = 'Súng nước' AND gun_kind = 'water_gun'
    AND NOT EXISTS (SELECT 1 FROM config_characters WHERE gun_kind = 'water_gun');
DELETE FROM config_gun_kinds g WHERE g.gun_kind = 'water_gun' AND g.name = 'Súng nước'
    AND NOT EXISTS (SELECT 1 FROM config_characters c WHERE c.gun_kind = 'water_gun')
    AND NOT EXISTS (SELECT 1 FROM config_item_type_words w WHERE w.gun_kind = 'water_gun');

DELETE FROM game_settings WHERE key = 'rules.elements';
UPDATE game_settings SET value = (value::jsonb - 'bounceRestitution' - 'bounceFriction')::text, updated_at = CURRENT_TIMESTAMP
WHERE key = 'rules.physics' AND jsonb_typeof(value::jsonb) = 'object' AND (value::jsonb ? 'bounceRestitution' OR value::jsonb ? 'bounceFriction');

ALTER TABLE config_weapons DROP COLUMN IF EXISTS bounces;
ALTER TABLE config_bosses DROP COLUMN IF EXISTS element;
ALTER TABLE config_characters DROP COLUMN IF EXISTS element;
DROP TABLE IF EXISTS config_elements;
