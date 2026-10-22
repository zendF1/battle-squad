package gamedata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	gd "battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"

	"github.com/go-chi/chi/v5"
)

func migrationsDir() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "..", "migrations")
}

// registeredMigrations are the files cmd/migrate runs, in its order (040 must be one of them).
func registeredMigrations(t *testing.T) []string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	src, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "..", "cmd", "migrate", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, line := range strings.Split(string(src), "\n") {
		if i := strings.Index(line, `filepath.Join("migrations", "`); i >= 0 {
			rest := line[i+len(`filepath.Join("migrations", "`):]
			names = append(names, rest[:strings.Index(rest, `"`)])
		}
	}
	if len(names) == 0 || names[len(names)-1] != "040_gamedata_texts.up.sql" {
		t.Fatalf("cmd/migrate does not end with 040: %v", names)
	}
	return names
}

func runMigration(t *testing.T, db *database.PostgresDB, name string) {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join(migrationsDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func exec(t *testing.T, db *database.PostgresDB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// gameDataDB is a schema with every migration and the config rows the dev DB has (as the
// admin seed inserts them, without the 040 texts), then 040 rerun as cmd/migrate does.
func gameDataDB(t *testing.T) (*Handler, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, 0, registeredMigrations(t)...)
	exec(t, db, `INSERT INTO config_weapons (weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit) VALUES
		('basic_rocket', 'Basic Rocket', 480, 40, 20, 1.0, 1.0, 1),
		('cannon_ball', 'Cannon Ball', 420, 60, 30, 1.5, 0.5, 1),
		('electric_orb', 'Electric Orb', 620, 30, 20, 0.8, 1.3, 1),
		('seed_bomb', 'Seed Bomb', 380, 40, 24, 0.9, 1.0, 1)`)
	exec(t, db, `INSERT INTO config_characters (character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, difficulty, weapon_id, skill_id) VALUES
		('rookie', 'Rookie', 'balanced', 1500, 480, 100, 50, 50, 20, 1, 'basic_rocket', 'triple_shot'),
		('tanko', 'Tanko', 'demolisher', 1650, 420, 80, 60, 65, 30, 2, 'cannon_ball', 'heavy_bomb'),
		('spark', 'Spark', 'striker', 1250, 620, 120, 30, 80, 20, 3, 'electric_orb', 'shock_field'),
		('flora', 'Flora', 'sustain', 1900, 380, 100, 90, 40, 24, 2, 'seed_bomb', 'healing_bloom')`)
	exec(t, db, `INSERT INTO config_skills (skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier, description) VALUES
		('triple_shot', 'rookie', 'Mưa tên lửa', 2, 'missile_rain', 12, 'burn', 1, 'old'),
		('heavy_bomb', 'tanko', 'Bom liên hoàn', 3, 'bomb_chain', 2, 'root', 1, 'old'),
		('shock_field', 'spark', 'Tia laze', 3, 'laser', 1, 'stun', 1, 'old'),
		('healing_bloom', 'flora', 'Hoa hồi xuân', 3, 'bloom', 1, '', 1, 'old')`)
	exec(t, db, `INSERT INTO config_items (item_id, name, type, target_type, value, max_use_per_match, cooldown) VALUES
		('wind_stopper', 'Wind Stopper', 'wind_cancel', 'self', 2, 1, 0),
		('teleport', 'Teleport', 'teleport', 'position', 0, 1, 0),
		('power_shot', 'Power Shot', 'attack_boost', 'self', 1.5, 1, 0),
		('drill_bomb', 'Drill Bomb', 'terrain_pierce', 'shoot', 50, 1, 0),
		('spider_net', 'Spider Net', 'move_debuff', 'shoot', 50, 1, 0),
		('freeze_bomb', 'Freeze Bomb', 'freeze', 'shoot', 1, 1, 0),
		('air_strike', 'Air Strike', 'air_attack', 'position', 30, 1, 0),
		('medkit', 'Medkit', 'heal', 'self', 450, 1, 0)`)
	// the physics rows the admin seed and 032_faster_shells leave
	exec(t, db, `INSERT INTO game_settings (key, value) VALUES
		('physics.gravity', '445'), ('physics.projectile_speed_multiplier', '9.6'), ('physics.wind_scale', '67'),
		('physics.time_step', '0.02'), ('physics.max_flight_seconds', '4.5'), ('physics.player_hit_radius', '24.0'),
		('combat.defense_k', '600') ON CONFLICT (key) DO NOTHING`)
	runMigration(t, db, "040_gamedata_texts.up.sql")
	return NewHandler(NewService(NewRepository(db))), db
}

func serve(h *Handler, etag string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Get("/gamedata", h.Get)
	req := httptest.NewRequest("GET", "/gamedata", nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func fetch(t *testing.T, h *Handler) (GameData, string) {
	t.Helper()
	rec := serve(h, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /gamedata: %d %s", rec.Code, rec.Body)
	}
	var env struct {
		Data GameData `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if want := `"` + env.Data.Version + `"`; env.Data.Version == "" || rec.Header().Get("ETag") != want {
		t.Fatalf("version %q, ETag %q", env.Data.Version, rec.Header().Get("ETag"))
	}
	return env.Data, rec.Header().Get("ETag")
}

// GET /gamedata is built from the config rows, game_settings and the 040 texts, with the
// match's rules; If-None-Match with its ETag answers 304 without a body.
func TestGameDataFromDB(t *testing.T) {
	h, _ := gameDataDB(t)
	g, etag := fetch(t, h)

	// the game_settings physics rows
	if g.Physics.Gravity != 445 || g.Physics.SpeedPerPower != 9.6 || g.Physics.WindScale != 67 || g.Physics.TimeStep != 0.02 ||
		g.Physics.MaxFlightSec != 4.5 || g.Physics.PlayerHitRadius != 24 || g.Physics.ShotOriginLift != 20 ||
		g.Physics.CollisionStepPx != 4 || g.Physics.OutOfBounds != (OutOfBounds{Left: 100, Right: 100, Below: 50}) {
		t.Fatalf("physics %+v", g.Physics)
	}
	if g.Aim != (Aim{PowerMin: 0, PowerMax: 100, AngleMin: 0, AngleMax: 180, NoAimAngle: 90, NoAimPower: 50}) {
		t.Fatalf("aim %+v", g.Aim)
	}
	if g.AimAid.GuideArcLength["bronze"] != 700 || g.AimAid.GuideArcLength["master"] != 450 || g.AimAid.DefaultTier != "bronze" ||
		g.AimAid.HitTolerance != (HitTolerance{Max: 20, MinPart: 0.2, FarPx: 1200, HighPx: 700}) {
		t.Fatalf("aim aid %+v", g.AimAid)
	}
	if g.Combat != (Combat{DefenseK: 600, PowerShotFactor: 1.5, LavaDamage: 75}) {
		t.Fatalf("combat %+v", g.Combat)
	}
	if g.Terrain != (Terrain{PlayerHeight: 40, MaxClimbPerStep: 3, CraterPxPerTerrainDamage: 0.6, BowlDepth: 0.72, BowlCeiling: 1.5,
		MaxTerrainDamageBonusPercent: 50, FallDamageThresholdPx: 150, FallDamagePerPx: 3.75}) {
		t.Fatalf("terrain %+v", g.Terrain)
	}
	if g.Movement != (Movement{PxPerEnergy: 2, EnergyPerPx: 0.5, DefaultMoveEnergy: 100, NetMoveEnergyCap: 50}) ||
		g.Timings != (Timings{TurnSec: 20, ActionIntervalSec: 0.15, BotFollowUpDelaySec: 1.5, SkipTurnDelaySec: 2}) ||
		g.SkillEnergy != (SkillEnergy{Max: 100, PerTurn: 20, OnHit: 10}) {
		t.Fatalf("movement %+v timings %+v energy %+v", g.Movement, g.Timings, g.SkillEnergy)
	}
	if r := g.SkillRanks; r.Max != 4 || r.Default != 0 || r.ByTierWord["iron"] != 1 || r.ByTierWord["lv40"] != 4 || r.ByTierWord["silver"] != 2 {
		t.Fatalf("skill ranks %+v", r)
	}

	weapons := map[string]Weapon{}
	for _, w := range g.Weapons {
		weapons[w.WeaponID] = w
	}
	if w := weapons["cannon_ball"]; len(g.Weapons) != 4 || w.CraterRadius != 18 || w.ExplosionRadius != 60 || w.ProjectileWeight != 1.5 || w.WindInfluence != 0.5 {
		t.Fatalf("weapons %+v", g.Weapons)
	}

	var ids []string
	for _, c := range g.Characters {
		ids = append(ids, c.CharacterID)
	}
	if !reflect.DeepEqual(ids, []string{"rookie", "tanko", "spark", "flora"}) || g.DefaultCharacterID != "rookie" {
		t.Fatalf("characters %v, default %q", ids, g.DefaultCharacterID)
	}
	if c := g.Characters[1]; c.Name != "Tanko" || c.Role != "demolisher" || c.RoleName != "Phá địa hình" || c.GunKind != "cannon" ||
		c.WeaponID != "cannon_ball" || c.SkillID != "heavy_bomb" || c.Mobility != 80 || !strings.HasPrefix(c.Playstyle, "Chuyên phá địa hình") {
		t.Fatalf("tanko %+v", c)
	}

	skills := map[string]Skill{}
	for _, s := range g.Skills {
		skills[s.SkillID] = s
	}
	rain := skills["triple_shot"]
	if rain.Name != "Mưa tên lửa" || rain.AimKind != "arc" || rain.StatusEffectID != "burn" || len(rain.Ranks) != 5 ||
		rain.Ranks[0].Values["count"] != 12 || rain.Ranks[4].Values["count"] != 28 || rain.Ranks[2].Values["damagePct"] != 25 ||
		rain.Params["delay"] != 0.5 || !strings.Contains(rain.Description, "{count} tên lửa") {
		t.Fatalf("rain %+v", rain)
	}
	if got := fill(rain.Description, rain.Ranks[0].Values); got != "Bắn pháo hiệu, 12 tên lửa rơi quanh chỗ đáp (+4 mỗi cấp súng), mỗi quả 25% sát thương; địch trúng bị bỏng: mất 10% máu tối đa đầu lượt sau." {
		t.Fatalf("rain rank 0: %s", got)
	}
	chain := skills["heavy_bomb"]
	var each []float64
	for _, r := range chain.Ranks {
		each = append(each, r.Values["eachPct"])
	}
	if !reflect.DeepEqual(each, []float64{60, 49, 42, 38, 34}) || chain.Ranks[4].Values["capPct"] != 180 || chain.Ranks[1].Values["count"] != 3 {
		t.Fatalf("bomb chain %+v", chain.Ranks)
	}
	if laser := skills["shock_field"]; laser.AimKind != "line" || laser.Params["halfWidth"] != 10 || laser.Params["sweepSpeed"] != 12000 ||
		laser.Ranks[3].Values["firstPct"] != 180 || laser.Ranks[0].Values["decayPct"] != 20 {
		t.Fatalf("laser %+v", laser)
	}
	bloom := skills["healing_bloom"]
	if got := fill(bloom.Description, bloom.Ranks[2].Values); bloom.AimKind != "none" ||
		got != "Hồi máu cả đội: Flora 50%, đồng đội 40% máu tối đa (+5% mỗi cấp súng); máu dư thành khiên (tối đa 50% máu); giải mọi hiệu ứng xấu." {
		t.Fatalf("bloom %s %s", bloom.AimKind, got)
	}

	var items []string
	byItem := map[string]Item{}
	for _, it := range g.Items {
		items = append(items, it.ItemID)
		byItem[it.ItemID] = it
	}
	if !reflect.DeepEqual(items, []string{"medkit", "teleport", "power_shot", "drill_bomb", "spider_net", "freeze_bomb", "air_strike", "wind_stopper"}) {
		t.Fatalf("items %v", items)
	}
	if m := byItem["medkit"]; m.Name != "Hộp cứu thương" || !m.Instant || m.AimKind != "none" || m.Value != 450 {
		t.Fatalf("medkit %+v", m)
	}
	if a := byItem["air_strike"]; a.Name != "Không kích" || a.Instant || a.AimKind != "targetX" {
		t.Fatalf("air_strike %+v", a)
	}
	if tp := byItem["teleport"]; tp.Instant || tp.AimKind != "arc" || !byItem["wind_stopper"].Instant || byItem["drill_bomb"].Value != 50 {
		t.Fatalf("teleport %+v", tp)
	}
	if g.ItemRules != (ItemRules{MaxLoadout: 3, AirStrike: AirStrike{Angle: 270, Power: 80}}) {
		t.Fatalf("item rules %+v", g.ItemRules)
	}

	effects := map[string]StatusEffect{}
	for _, e := range g.StatusEffects {
		effects[e.EffectID] = e
	}
	if len(g.StatusEffects) != 11 || g.StatusEffects[0].EffectID != "burn" {
		t.Fatalf("status effects %+v", g.StatusEffects)
	}
	if s := effects["stun"]; s != (StatusEffect{EffectID: "stun", Name: "Choáng", Banner: "Choáng — mất lượt", Bad: true, SkipsTurn: true, BlocksMove: true}) {
		t.Fatalf("stun %+v", s)
	}
	if r := effects["root"]; !r.Bad || r.SkipsTurn || !r.BlocksMove || r.Banner != "Bị cầm chân" {
		t.Fatalf("root %+v", r)
	}
	if p := effects["power_shot"]; p.Bad || p.Marker != "x1.5" || effects["net"].BlocksMove || !effects["net"].Bad || effects["burn"].SkipsTurn {
		t.Fatalf("power_shot %+v", p)
	}

	if len(g.Rank.Tiers) != 6 || g.Rank.DefaultRating != 1000 || g.Rank.RankedMode != "ranked_2v2" ||
		g.Rank.Tiers[1] != (RankTier{Tier: "silver", Name: "Bạc", MinRating: 1000, Divisions: 3, DivisionSize: 66}) ||
		g.Rank.Tiers[5] != (RankTier{Tier: "master", Name: "Cao thủ", MinRating: 2200, Divisions: 1}) {
		t.Fatalf("rank %+v", g.Rank)
	}
	lv := g.Levels.Character.Levels
	if g.Levels.Character.MaxLevel != 80 || len(lv) != 80 || lv[0] != (LevelEntry{1, 0}) || lv[1] != (LevelEntry{2, 200}) ||
		lv[9] != (LevelEntry{10, 5200}) || g.Levels.Player.ExpPerLevel != 100 {
		t.Fatalf("levels max %d, %v… player %+v", g.Levels.Character.MaxLevel, lv[:3], g.Levels.Player)
	}
	e := g.Equipment
	if e.UpgradeStepPerLevel != 0.02 || e.MaxUpgradeLevel != 16 || e.MaxStoneLevel != 12 || e.MergeMinCount != 2 || e.MergeMaxCount != 4 ||
		e.MergeRatePerItem != 25 || e.SellRate != 0.25 || e.WeaponUpgradeCostMultiplier != 3 ||
		!reflect.DeepEqual(e.Milestones, []Milestone{{6, 0.1}, {10, 0.2}, {14, 0.4}, {16, 1}}) || len(e.Tiers) != 8 ||
		e.Tiers[0] != (EquipmentTier{Tier: "iron", Name: "Sắt", Kind: "normal", IDSuffix: "lv10", SkillRank: 1}) ||
		e.Tiers[7] != (EquipmentTier{Tier: "diamond", Name: "Kim cương", Kind: "crafted", SkillRank: 4}) {
		t.Fatalf("equipment %+v", e)
	}
	if g.Economy.CoinsPerGem != 100 {
		t.Fatalf("economy %+v", g.Economy)
	}
	if mm := g.Matchmaking; mm.Queue != "ranked_2v2" || mm.Slots != 4 || mm.TeamSize != 2 || mm.BotFillAfterSec != 20 || mm.MatchStartDelaySec != 3 ||
		len(mm.Tips) != 12 || !strings.HasPrefix(mm.Tips[0], "Gió mạnh") {
		t.Fatalf("matchmaking %+v", mm)
	}
	if len(g.Modes.Lobby) != 3 || g.Modes.Lobby[1] != (LobbyMode{ModeID: "ranked", Name: "Xếp hạng 2v2", Description: "Ghép 2v2, tính điểm hạng", Queue: "ranked_2v2"}) ||
		g.Modes.Lobby[2].Bot != "ai" || len(g.Modes.Match) != 3 || g.Modes.Match[0] != (MatchMode{ModeID: "pvp_1v1", Name: "Đấu 1v1"}) {
		t.Fatalf("modes %+v", g.Modes)
	}
	if g.MapThemes.Default != "grassland" || len(g.MapThemes.Themes) != 11 {
		t.Fatalf("map themes %+v", g.MapThemes)
	}

	if rec := serve(h, etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
		t.Fatalf("If-None-Match: %d %q", rec.Code, rec.Body)
	}
	if rec := serve(h, `"stale"`); rec.Code != http.StatusOK {
		t.Fatalf("stale If-None-Match: %d", rec.Code)
	}
}

// fill replaces every {key} of a description template with the rank's value, as the client does.
func fill(tpl string, values map[string]float64) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v, _ := json.Marshal(values[k])
		tpl = strings.ReplaceAll(tpl, "{"+k+"}", string(v))
	}
	return tpl
}

// An edit of a config row or a game_settings row changes the payload and its version (the old
// ETag then gets 200); rerunning 040 keeps an admin's text edit and the version.
func TestGameDataVersionFollowsConfig(t *testing.T) {
	h, db := gameDataDB(t)
	g0, etag0 := fetch(t, h)
	if again, _ := fetch(t, h); again.Version != g0.Version {
		t.Fatal("version not stable")
	}

	exec(t, db, `UPDATE config_weapons SET terrain_damage = 40 WHERE weapon_id = 'cannon_ball'`)
	g1, _ := fetch(t, h)
	if g1.Version == g0.Version {
		t.Fatal("weapon edit kept the version")
	}
	for _, w := range g1.Weapons {
		if w.WeaponID == "cannon_ball" && w.CraterRadius != 24 {
			t.Fatalf("crater %v", w.CraterRadius)
		}
	}
	if rec := serve(h, etag0); rec.Code != http.StatusOK {
		t.Fatalf("old ETag: %d", rec.Code)
	}

	exec(t, db, `UPDATE game_settings SET value = '500' WHERE key = 'physics.gravity'`)
	g2, _ := fetch(t, h)
	if g2.Version == g1.Version || g2.Physics.Gravity != 500 {
		t.Fatalf("gravity %v, version %s", g2.Physics.Gravity, g2.Version)
	}

	exec(t, db, `UPDATE config_items SET display_name = 'Bộ cứu thương' WHERE item_id = 'medkit'`)
	exec(t, db, `UPDATE config_rank_tiers SET name = 'Đồng đỏ' WHERE tier = 'bronze'`)
	g3, _ := fetch(t, h)
	if g3.Version == g2.Version || g3.Items[0].Name != "Bộ cứu thương" || g3.Rank.Tiers[0].Name != "Đồng đỏ" {
		t.Fatalf("text edit: %q %q", g3.Items[0].Name, g3.Rank.Tiers[0].Name)
	}
	runMigration(t, db, "040_gamedata_texts.up.sql")
	if g4, _ := fetch(t, h); g4.Version != g3.Version {
		t.Fatal("040 rerun changed the payload")
	}
}

// 040's down removes the texts and its up brings back the same payload; the YAML the admin
// seeds a fresh DB from carries the same texts as 040.
func TestGameDataMigrationDownUpAndSeed(t *testing.T) {
	h, db := gameDataDB(t)
	before, _ := fetch(t, h)
	runMigration(t, db, "040_gamedata_texts.down.sql")
	if _, err := h.service.Get(context.Background()); err == nil {
		t.Fatal("served without the 040 tables")
	}
	runMigration(t, db, "040_gamedata_texts.up.sql")
	if after, _ := fetch(t, h); !reflect.DeepEqual(after, before) {
		t.Fatal("down / up changed the payload")
	}

	_, self, _, _ := runtime.Caller(0)
	prev := gd.Data
	t.Cleanup(func() { gd.Data = prev })
	if err := gd.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
	for _, c := range before.Characters {
		y := gd.Data.Characters[c.CharacterID]
		if y.RoleName != c.RoleName || y.Playstyle != c.Playstyle || y.GunKind != c.GunKind {
			t.Fatalf("configs/characters.yaml %s differs from 040", c.CharacterID)
		}
	}
	for i, it := range before.Items {
		if y := gd.Data.Items[it.ItemID]; y.DisplayName != it.Name || y.SortOrder != i+1 {
			t.Fatalf("configs/items.yaml %s differs from 040", it.ItemID)
		}
	}
	for _, s := range before.Skills {
		if gd.Data.Skills[s.SkillID].DescriptionTemplate != s.Description {
			t.Fatalf("configs/skills.yaml %s differs from 040", s.SkillID)
		}
	}
}

// TestWriteSample writes the payload of the DB at GAMEDATA_SAMPLE_DSN (the dev DB) to
// GAMEDATA_SAMPLE_OUT: the contract's sample. Skipped unless both are set.
func TestWriteSample(t *testing.T) {
	dsn, out := os.Getenv("GAMEDATA_SAMPLE_DSN"), os.Getenv("GAMEDATA_SAMPLE_OUT")
	if dsn == "" || out == "" {
		t.Skip("GAMEDATA_SAMPLE_DSN / GAMEDATA_SAMPLE_OUT not set")
	}
	db, err := database.NewPostgresDB(context.Background(), dsn, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rec := serve(NewHandler(NewService(NewRepository(db))), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	var pretty map[string]any
	_ = json.Unmarshal(env.Data, &pretty)
	body, _ := json.MarshalIndent(json.RawMessage(rec.Body.Bytes()), "", "  ")
	if err := os.WriteFile(out, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("ETag %s, version %v", rec.Header().Get("ETag"), pretty["version"])
}
