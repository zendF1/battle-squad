-- Elements (ngũ hành), the new character Tide, Tanko's earth rework (gamedata/elements.go,
-- configs/*.yaml, gamedata.DefaultBosses).
-- - config_elements: the five elements, their Vietnamese names and the one each beats
--   (metal → wood → earth → water → fire → metal); rules.elements: the damage percentages (PvP
--   +10 / −10, boss mode +20 on a boss the character beats, +10 from a boss that beats it).
-- - config_characters.element (rookie fire, tanko earth, spark metal, flora wood) and
--   config_bosses.element (the robot metal, the crab fire, the witch water; their minions carry
--   it), each column added and seeded once.
-- - config_weapons.bounces (0; Tide's water orb 1) and rules.physics bounceRestitution 0.55 /
--   bounceFriction 0.85 (added while absent).
-- - Tide (water, controller): her character, weapon (water_orb), skill (snow_nova, effect nova,
--   its params), gun kind water_gun and type word, her shop unlock offers (as the other three
--   characters'), two tips; her guns are 055_tide_guns.
-- - Tanko's skill becomes "Địa chấn liên hoàn" (boulders, the same numbers), with his playstyle
--   and tip, each only while it is still the old text.
-- Idempotent: tables and columns IF NOT EXISTS, the seeds of a new column with the column, rows
-- ON CONFLICT DO NOTHING, texts only while old: reruns keep admin edits.

-- ── Elements ─────────────────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS config_elements (
    element_id TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    beats TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_elements (element_id, name, beats, sort_order) VALUES
    ('metal', 'Kim', 'wood', 1), ('wood', 'Mộc', 'earth', 2), ('water', 'Thủy', 'fire', 3),
    ('fire', 'Hỏa', 'metal', 4), ('earth', 'Thổ', 'water', 5)
ON CONFLICT (element_id) DO NOTHING;
DROP TRIGGER IF EXISTS gamedata_revision_bump ON config_elements;
CREATE TRIGGER gamedata_revision_bump AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON config_elements
    FOR EACH STATEMENT EXECUTE FUNCTION bump_gamedata_revision();

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_characters' AND column_name = 'element') THEN
        ALTER TABLE config_characters ADD COLUMN element TEXT NOT NULL DEFAULT '';
        UPDATE config_characters c SET element = v.element, updated_at = CURRENT_TIMESTAMP
        FROM (VALUES ('rookie', 'fire'), ('tanko', 'earth'), ('spark', 'metal'), ('flora', 'wood'), ('tide', 'water'))
            AS v(character_id, element)
        WHERE c.character_id = v.character_id;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_bosses' AND column_name = 'element') THEN
        ALTER TABLE config_bosses ADD COLUMN element TEXT NOT NULL DEFAULT '';
        UPDATE config_bosses b SET element = v.element, updated_at = CURRENT_TIMESTAMP
        FROM (VALUES ('steel_robot', 'metal'), ('lava_crab', 'fire'), ('ice_witch', 'water')) AS v(boss_id, element)
        WHERE b.boss_id = v.boss_id;
    END IF;
END
$$;

INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('rules.elements', '{"pvpStrongPct": 10, "pvpWeakPct": 10, "bossStrongPct": 20, "bossTakenPct": 10}', 'json', 'Ngũ hành', 'rules')
ON CONFLICT (key) DO NOTHING;

-- ── Bouncing shells ──────────────────────────────────────────────────────────────────────
ALTER TABLE config_weapons ADD COLUMN IF NOT EXISTS bounces INT NOT NULL DEFAULT 0;
UPDATE game_settings SET value = (value::jsonb || '{"bounceRestitution": 0.55, "bounceFriction": 0.85}'::jsonb)::text,
    updated_at = CURRENT_TIMESTAMP
WHERE key = 'rules.physics' AND jsonb_typeof(value::jsonb) = 'object' AND NOT (value::jsonb ? 'bounceRestitution');

-- ── Tide ─────────────────────────────────────────────────────────────────────────────────
INSERT INTO config_weapons (weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit, bounces)
VALUES ('water_orb', 'Water Orb', 500, 38, 16, 0.9, 1.0, 1, 1)
ON CONFLICT (weapon_id) DO NOTHING;

INSERT INTO config_skills (skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier,
    description, description_template, params)
VALUES ('snow_nova', 'tide', 'Nổ bông tuyết', 4, 'nova', 0, 'freeze', 1.0,
    'Bông tuyết nổ ngay chỗ Tide đứng, bán kính 500px: địch ở tâm nhận 130% sát thương (+10% mỗi cấp súng), giảm dần còn 40% ở mép; mọi địch trúng, kể cả boss đang bay, bị đóng băng mất lượt sau. Đồng đội không bị ảnh hưởng.',
    'Bông tuyết nổ ngay chỗ Tide đứng, bán kính {radius}px: địch ở tâm nhận {centrePct}% sát thương (+{centrePerRankPct}% mỗi cấp súng), giảm dần còn {edgePct}% ở mép; mọi địch trúng, kể cả boss đang bay, bị đóng băng {freezeTurns} lượt. Đồng đội không bị ảnh hưởng.',
    '{"radius": 500, "centreBase": 1.3, "centrePerRank": 0.1, "edgeFactor": 0.4, "freezeTurns": 1, "waveSpeed": 1400, "castTime": 0.35}'::jsonb)
ON CONFLICT (skill_id) DO NOTHING;
-- a nova row without params (inserted before this migration knew them) gets its defaults
UPDATE config_skills SET params = '{"radius": 500, "centreBase": 1.3, "centrePerRank": 0.1, "edgeFactor": 0.4, "freezeTurns": 1, "waveSpeed": 1400, "castTime": 0.35}'::jsonb, updated_at = CURRENT_TIMESTAMP
WHERE effect_type = 'nova' AND params = '{}'::jsonb;

INSERT INTO config_characters (character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty,
    weapon_id, skill_id, role_name, playstyle, gun_kind, sort_order, element)
VALUES ('tide', 'Tide', 'controller', 1400, 500, 110, 45, 70, 16, 2,
    'water_orb', 'snow_nova', 'Khống chế',
    'Chuyên khống chế: Nổ bông tuyết đóng băng mọi địch quanh Tide, kể cả boss đang bay, nên hãy áp sát đối thủ hoặc boss rồi kích hoạt để chúng mất lượt sau. Quả cầu nước nảy một lần khi chạm đất rồi mới nổ, căn góc khéo là lăn được vào chỗ nấp của địch. Đổi lại, Tide gây ít sát thương hơn Spark và khoét đất kém xa Tanko.',
    'water_gun', 5, 'water')
ON CONFLICT (character_id) DO NOTHING;
-- Tide's texts and template while empty (a 040 down / up drops them, as the others')
UPDATE config_characters SET role_name = 'Khống chế' WHERE character_id = 'tide' AND role_name = '';
UPDATE config_characters SET playstyle = 'Chuyên khống chế: Nổ bông tuyết đóng băng mọi địch quanh Tide, kể cả boss đang bay, nên hãy áp sát đối thủ hoặc boss rồi kích hoạt để chúng mất lượt sau. Quả cầu nước nảy một lần khi chạm đất rồi mới nổ, căn góc khéo là lăn được vào chỗ nấp của địch. Đổi lại, Tide gây ít sát thương hơn Spark và khoét đất kém xa Tanko.'
WHERE character_id = 'tide' AND playstyle = '';
UPDATE config_characters SET gun_kind = 'water_gun' WHERE character_id = 'tide' AND gun_kind = '';
UPDATE config_characters SET sort_order = 5 WHERE character_id = 'tide' AND sort_order = 0;
UPDATE config_skills SET description_template = 'Bông tuyết nổ ngay chỗ Tide đứng, bán kính {radius}px: địch ở tâm nhận {centrePct}% sát thương (+{centrePerRankPct}% mỗi cấp súng), giảm dần còn {edgePct}% ở mép; mọi địch trúng, kể cả boss đang bay, bị đóng băng {freezeTurns} lượt. Đồng đội không bị ảnh hưởng.'
WHERE skill_id = 'snow_nova' AND description_template = '';

INSERT INTO config_gun_kinds (gun_kind, name, sort_order) VALUES ('water_gun', 'Súng nước', 6) ON CONFLICT (gun_kind) DO NOTHING;
INSERT INTO config_item_type_words (word, name, gun_kind, sort_order) VALUES ('water gun', 'Súng nước', 'water_gun', 12)
ON CONFLICT (word) DO NOTHING;

-- sold like the other characters (014_default_shop_catalog)
INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player, is_active) VALUES
    ('unlock_tide_coin', 'tide', 'character_unlock', 'coin', 5000, 1, 1, TRUE),
    ('unlock_tide_gem',  'tide', 'character_unlock', 'gem',  300,  1, 1, TRUE)
ON CONFLICT (offer_id) DO NOTHING;

INSERT INTO config_tips (tip_id, text, sort_order) VALUES
    ('tip_13', 'Ngũ hành: Kim khắc Mộc, Mộc khắc Thổ, Thổ khắc Thủy, Thủy khắc Hỏa, Hỏa khắc Kim. Bắn hệ bị khắc gây thêm sát thương.', 13),
    ('tip_14', 'Nổ bông tuyết của Tide đóng băng mọi địch quanh cô, kể cả boss đang bay: hãy áp sát rồi kích hoạt.', 14)
ON CONFLICT (tip_id) DO NOTHING;

-- ── Tanko: Địa chấn liên hoàn ────────────────────────────────────────────────────────────
UPDATE config_skills SET name = 'Địa chấn liên hoàn', updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND name = 'Bom liên hoàn';
UPDATE config_skills SET description = 'Bắn liên tiếp những tảng đá bọc đất cùng một đường bay (2 tảng, +1 mỗi cấp súng): mặt đất nứt toác, gai đá trồi quanh miệng hố, hố mỗi lúc một sâu; mỗi tảng 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị đất đá vùi chân, cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND description = 'Bắn liên tiếp bom lớn cùng một đường bay (2 quả, +1 mỗi cấp súng), đào sâu một hố; mỗi quả 60% sát thương (34% ở cấp súng 4), mỗi địch nhận tối đa 120% (+15% mỗi cấp); địch trúng bị cầm chân lượt sau.';
UPDATE config_skills SET description_template = 'Bắn {count} tảng đá bọc đất liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng): mặt đất nứt toác, gai đá trồi quanh miệng hố, hố mỗi lúc một sâu; mỗi tảng {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị đất đá vùi chân, cầm chân lượt sau.',
    updated_at = CURRENT_TIMESTAMP
WHERE skill_id = 'heavy_bomb' AND description_template = 'Bắn {count} quả bom lớn liên tiếp cùng đường bay (+{countPerRank} mỗi cấp súng), đào sâu một hố; mỗi quả {eachPct}% sát thương, mỗi địch nhận tối đa {capPct}%; địch trúng bị cầm chân lượt sau.';
UPDATE config_characters SET playstyle = 'Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Địa chấn liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.',
    updated_at = CURRENT_TIMESTAMP
WHERE character_id = 'tanko' AND playstyle = 'Chuyên phá địa hình: đại pháo khoét hố sâu hơn hẳn mọi người, phá chỗ nấp và đào đất ngay dưới chân đối thủ. Đạn nặng ít bị gió đẩy nên vẫn bắn chuẩn khi trời gió lớn; để dành Bom liên hoàn bắn thẳng vào địch: từng quả đào sâu thêm, địch rơi xuống đáy hố, trúng tiếp và bị cầm chân. Bù lại mỗi phát gây ít sát thương và Tanko đi được ngắn nhất, khó thoát khỏi chỗ đứng xấu.';
UPDATE config_tips SET text = 'Địa chấn liên hoàn của Tanko làm nứt đất, đào hố sâu; địch trúng bị đất đá vùi chân, cầm chân lượt sau.', updated_at = CURRENT_TIMESTAMP
WHERE tip_id = 'tip_02' AND text = 'Bom liên hoàn của Tanko đào hố sâu, địch trúng bị cầm chân lượt sau.';
