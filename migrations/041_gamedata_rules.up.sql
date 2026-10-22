-- Game data phase S2: every gameplay rule and number in the DB (admins edit them in the
-- dashboard), a stored revision that every config write bumps (the servers reload on it), the
-- archive of published GET /gamedata payloads, and the last Vietnamese texts the client had.
-- Idempotent: tables and columns IF NOT EXISTS, seed rows ON CONFLICT DO NOTHING, one-time
-- fills only in the run that adds their column, so reruns keep admin edits.

-- ── The game data revision ──────────────────────────────────────────────────────────────
-- One row; a statement trigger on every config table bumps it, the servers poll it (and the
-- admin also notifies them on Redis gamedata:changed).
CREATE TABLE IF NOT EXISTS gamedata_revision (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    revision BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO gamedata_revision (id, revision) VALUES (1, 1) ON CONFLICT (id) DO NOTHING;

CREATE OR REPLACE FUNCTION bump_gamedata_revision() RETURNS trigger AS $$
BEGIN
    UPDATE gamedata_revision SET revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE id = 1;
    RETURN NULL;
END
$$ LANGUAGE plpgsql;

-- ── Published payloads ──────────────────────────────────────────────────────────────────
-- GET /gamedata?version=<v> serves the exact game data a match started with (MatchStarted
-- gamedataVersion), also after the config changed. payload is the JSON text as served.
CREATE TABLE IF NOT EXISTS gamedata_versions (
    version TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ── Rules: one game_settings JSON row per group (gamedata.RuleGroups) ──────────────────
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('rules.physics', '{"collisionStepPx": 4, "shotOriginLift": 20, "outOfBoundsLeft": 100, "outOfBoundsRight": 100, "outOfBoundsBelow": 50}', 'json', 'Đường bay của đạn: bước kiểm tra va chạm, điểm bắn, giới hạn ra ngoài bản đồ', 'rules'),
    ('rules.aim', '{"powerMax": 100, "angleMin": 0, "angleMax": 180, "noAimAngle": 90, "noAimPower": 50}', 'json', 'Ngắm bắn: lực tối đa, góc của ná, góc / lực khi không ngắm', 'rules'),
    ('rules.aim_aid', '{"guideArcLength": {"bronze": 700, "silver": 650, "gold": 600, "platinum": 550, "diamond": 500, "master": 450}, "hitToleranceMax": 20, "hitToleranceMinPart": 0.2, "hitToleranceFarPx": 1200, "hitToleranceHighPx": 700}', 'json', 'Hỗ trợ ngắm của client: độ dài đường ngắm theo hạng, sai số báo trúng', 'rules'),
    ('rules.combat', '{"lavaDamage": 75, "critMultiplier": 1.5, "burnMaxHpPercent": 10, "shieldCapPercent": 50}', 'json', 'Chiến đấu: dung nham, chí mạng, bỏng, khiên tối đa', 'rules'),
    ('rules.terrain', '{"playerHeight": 40, "maxClimbPerStep": 3, "craterPxPerTerrainDamage": 0.6, "bowlDepth": 0.72, "bowlCeiling": 1.5, "maxTerrainDamageBonusPercent": 50}', 'json', 'Địa hình: chiều cao nhân vật, leo dốc, kích thước hố', 'rules'),
    ('rules.movement', '{"defaultMoveEnergy": 100}', 'json', 'Di chuyển: năng lượng mặc định', 'rules'),
    ('rules.timings', '{"actionIntervalSec": 0.15, "botFollowUpDelaySec": 1.5, "skipTurnDelaySec": 2}', 'json', 'Thời gian: khoảng cách hành động, bot chờ, mất lượt', 'rules'),
    ('rules.skill_energy', '{"max": 100, "perTurn": 20, "onHit": 10}', 'json', 'Năng lượng kỹ năng', 'rules'),
    ('rules.skill_ranks', '{"byTierWord": {"lv10": 1, "lv20": 2, "lv30": 3, "lv40": 4, "iron": 1, "steel": 2, "mithril": 3, "dragon": 4, "silver": 2, "gold": 3, "titan": 4, "diamond": 4}, "default": 0}', 'json', 'Cấp kỹ năng theo từ cuối mã súng', 'rules'),
    ('rules.items', '{"maxLoadout": 3, "airStrikeAngle": 270, "airStrikePower": 80}', 'json', 'Vật phẩm: số mang vào trận, không kích', 'rules'),
    ('rules.player', '{"expPerLevel": 100, "defaultCharacterId": "rookie"}', 'json', 'Người chơi: EXP mỗi cấp, nhân vật mặc định', 'rules'),
    ('rules.equipment', '{"upgradeStepPerLevel": 0.02, "maxUpgradeLevel": 16, "maxStoneLevel": 12, "mergeMinCount": 2, "mergeMaxCount": 4}', 'json', 'Trang bị: cường hóa, đá, ghép', 'rules'),
    ('rules.economy', '{"coinsPerGem": 100}', 'json', 'Kinh tế: tỉ lệ gem → coin gốc', 'rules')
ON CONFLICT (key) DO NOTHING;

-- ── The scalar rows the match did not read: corrected to what it played with ────────────
-- Only rows still holding the old seed (value and description) change, so an admin edit and a
-- rerun keep theirs. A DB without them gets the correct ones.
UPDATE game_settings SET value = '150', description = 'Rơi tới chừng này px không mất máu'
    WHERE key = 'fall.damage_threshold' AND value = '30' AND description = 'Khoảng rơi tối thiểu (pixels) trước khi nhận fall damage';
UPDATE game_settings SET value = '3.75', description = 'Máu mất mỗi px rơi vượt ngưỡng (bỏ qua phòng thủ)'
    WHERE key = 'fall.damage_per_pixel' AND value = '0.5' AND description = 'Damage mỗi pixel rơi vượt ngưỡng';
UPDATE game_settings SET value = '2', description = 'Đi chừng này px tốn move.energy_cost_per_2px năng lượng'
    WHERE key = 'move.step_pixels' AND value = '10' AND description = 'Số pixel di chuyển mỗi tick khi giữ nút move';
UPDATE game_settings SET value = '1', description = 'Năng lượng tốn cho mỗi move.step_pixels px đi được'
    WHERE key = 'move.energy_cost_per_2px' AND value = '0.5' AND description = 'Năng lượng tiêu hao mỗi 2 pixel di chuyển';
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('match.turn_time_seconds', '20', 'number', 'Thời gian mỗi lượt (giây). Hết = tự động kết thúc lượt', 'match'),
    ('match.idle_timeout_minutes', '2', 'number', 'Phút không hoạt động trước khi match bị hủy', 'match'),
    ('move.step_pixels', '2', 'number', 'Đi chừng này px tốn move.energy_cost_per_2px năng lượng', 'movement'),
    ('move.energy_cost_per_2px', '1', 'number', 'Năng lượng tốn cho mỗi move.step_pixels px đi được', 'movement'),
    ('fall.damage_threshold', '150', 'number', 'Rơi tới chừng này px không mất máu', 'physics'),
    ('fall.damage_per_pixel', '3.75', 'number', 'Máu mất mỗi px rơi vượt ngưỡng (bỏ qua phòng thủ)', 'physics')
ON CONFLICT (key) DO NOTHING;

-- ── Skill tuning: config_skills.params (gamedata.SkillParamSpecs), filled once ──────────
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_skills' AND column_name = 'params') THEN
        ALTER TABLE config_skills ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb;
        UPDATE config_skills SET params = '{"baseCount": 12, "countPerRank": 4, "spreadBase": 90, "spreadPerRank": 15, "focus": 2, "delay": 0.5, "interval": 0.05, "startY": -60, "dropSpeed": 1500, "damage": 0.25, "blast": 0.7, "crater": 1, "cap": 2, "burnTurns": 1}'::jsonb
            WHERE effect_type = 'missile_rain';
        UPDATE config_skills SET params = '{"baseCount": 2, "countPerRank": 1, "launchGap": 0.1, "totalBase": 1.8, "totalPerRank": 0.15, "capBase": 1.2, "capPerRank": 0.15, "blast": 1.3, "fallSpeed": 3, "hitGap": 0.08, "rootTurns": 1}'::jsonb
            WHERE effect_type = 'bomb_chain';
        UPDATE config_skills SET params = '{"halfWidth": 10, "firstBase": 1.5, "firstPerRank": 0.1, "decay": 0.8, "chargeTime": 0.3, "beamTime": 0.6, "sweepSpeed": 12000, "stunTurns": 1}'::jsonb
            WHERE effect_type = 'laser';
        UPDATE config_skills SET params = '{"selfBase": 40, "allyBase": 30, "perRank": 5, "riseTime": 0.55, "crossTime": 0.2, "fallTime": 0.65, "skyY": -140, "fallSpeed": 400, "linger": 0.9}'::jsonb
            WHERE effect_type = 'bloom';
    END IF;
END
$$;

-- ── Status effect rules: config_status_effects columns, filled once ────────────────────
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_status_effects' AND column_name = 'bad') THEN
        ALTER TABLE config_status_effects ADD COLUMN bad BOOLEAN NOT NULL DEFAULT FALSE;
        ALTER TABLE config_status_effects ADD COLUMN skips_turn BOOLEAN NOT NULL DEFAULT FALSE;
        ALTER TABLE config_status_effects ADD COLUMN blocks_move BOOLEAN NOT NULL DEFAULT FALSE;
        UPDATE config_status_effects SET bad = TRUE WHERE effect_id IN ('burn', 'root', 'stun', 'freeze', 'net');
        UPDATE config_status_effects SET skips_turn = TRUE WHERE effect_id IN ('stun', 'freeze');
        UPDATE config_status_effects SET blocks_move = TRUE WHERE effect_id IN ('root', 'stun', 'freeze');
    END IF;
END
$$;

-- ── Rank bands: config_rank_tiers columns, filled once (ranked.DefaultBands) ───────────
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_schema = current_schema() AND table_name = 'config_rank_tiers' AND column_name = 'min_rating') THEN
        ALTER TABLE config_rank_tiers ADD COLUMN min_rating INT NOT NULL DEFAULT 0;
        ALTER TABLE config_rank_tiers ADD COLUMN divisions INT NOT NULL DEFAULT 1;
        ALTER TABLE config_rank_tiers ADD COLUMN division_size INT NOT NULL DEFAULT 0;
        UPDATE config_rank_tiers t SET min_rating = b.min_rating, divisions = b.divisions, division_size = b.division_size
        FROM (VALUES ('bronze', 0, 3, 333), ('silver', 1000, 3, 66), ('gold', 1200, 3, 100),
                     ('platinum', 1500, 3, 100), ('diamond', 1800, 3, 133), ('master', 2200, 1, 0))
             AS b(tier, min_rating, divisions, division_size)
        WHERE t.tier = b.tier;
    END IF;
END
$$;
INSERT INTO config_rank_tiers (tier, name, sort_order, min_rating, divisions, division_size) VALUES
    ('bronze', 'Đồng', 1, 0, 3, 333), ('silver', 'Bạc', 2, 1000, 3, 66), ('gold', 'Vàng', 3, 1200, 3, 100),
    ('platinum', 'Bạch kim', 4, 1500, 3, 100), ('diamond', 'Kim cương', 5, 1800, 3, 133), ('master', 'Cao thủ', 6, 2200, 1, 0)
ON CONFLICT (tier) DO NOTHING;

-- ── The last client-only texts (battle-squad-game 60a712b screens/home/equipment_model.gd) ──
-- Gun kinds (characters[].gunKind) and their names.
CREATE TABLE IF NOT EXISTS config_gun_kinds (
    gun_kind TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_gun_kinds (gun_kind, name, sort_order) VALUES
    ('bazooka', 'Bazooka', 1), ('cannon', 'Đại pháo', 2), ('shock_rifle', 'Súng điện', 3),
    ('seed_launcher', 'Súng hạt', 4), ('blaster', 'Súng', 5)
ON CONFLICT (gun_kind) DO NOTHING;

-- Equipment slots in display order and their names.
CREATE TABLE IF NOT EXISTS config_equipment_slots (
    slot TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_equipment_slots (slot, name, sort_order) VALUES
    ('helmet', 'Mũ', 1), ('armor', 'Áo', 2), ('gloves', 'Găng tay', 3), ('pants', 'Quần', 4),
    ('boots', 'Giày', 5), ('weapon', 'Vũ khí', 6)
ON CONFLICT (slot) DO NOTHING;

-- The type part of "<Tier> <Type>" equipment item names (lower case), its Vietnamese word and,
-- for a gun, its gun kind.
CREATE TABLE IF NOT EXISTS config_item_type_words (
    word TEXT PRIMARY KEY,
    name TEXT NOT NULL DEFAULT '',
    gun_kind TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO config_item_type_words (word, name, gun_kind, sort_order) VALUES
    ('sword', 'Kiếm', '', 1), ('vest', 'Áo giáp', '', 2), ('cap', 'Mũ', '', 3), ('pants', 'Quần', '', 4),
    ('boots', 'Giày', '', 5), ('gloves', 'Găng', '', 6), ('bazooka', 'Bazooka', 'bazooka', 7),
    ('cannon', 'Đại pháo', 'cannon', 8), ('shock rifle', 'Súng điện', 'shock_rifle', 9),
    ('seed launcher', 'Súng hạt', 'seed_launcher', 10), ('blaster', 'Súng', 'blaster', 11)
ON CONFLICT (word) DO NOTHING;

-- ── Every config write bumps the revision ───────────────────────────────────────────────
DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['game_settings', 'config_characters', 'config_weapons', 'config_skills', 'config_items',
        'config_maps', 'config_brick_types', 'config_status_effects', 'config_rank_tiers', 'config_equipment_tiers',
        'config_game_modes', 'config_tips', 'config_gun_kinds', 'config_equipment_slots', 'config_item_type_words'] LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS gamedata_revision_bump ON %I', t);
        EXECUTE format('CREATE TRIGGER gamedata_revision_bump AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON %I
            FOR EACH STATEMENT EXECUTE FUNCTION bump_gamedata_revision()', t);
    END LOOP;
END
$$;
