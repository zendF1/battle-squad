package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	apigamedata "battle-squad/internal/api/gamedata"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/gamedatasync"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/migrate"
	"battle-squad/internal/shared/ranked"
)

// registered are the migration files cmd/migrate runs (migrations.FS), in its order.
func registered(t *testing.T) []string {
	t.Helper()
	return dbtest.Migrations(t)
}

func runSQL(t *testing.T, db *database.PostgresDB, file string) {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	sql, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "migrations", file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

func execSQL(t *testing.T, db *database.PostgresDB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func configsDir() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "configs")
}

// seededDB is a schema with every migration and the admin seed (a fresh install).
func seededDB(t *testing.T) *database.PostgresDB {
	t.Helper()
	db := dbtest.NewSchema(t, 0, registered(t)...)
	if _, err := migrate.SeedAll(context.Background(), db.Pool, os.DirFS(configsDir())); err != nil {
		t.Fatal(err)
	}
	return db
}

func setting(t *testing.T, db *database.PostgresDB, key string) string {
	t.Helper()
	var v string
	if err := db.Pool.QueryRow(context.Background(), `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&v); err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return v
}

// The migration seeds exactly the Go defaults, both on a DB that had the config rows before it
// (the dev DB: params, flags and bands filled, the dead fall / move rows corrected) and on a
// fresh one (migration, then the admin seed); a rerun of every migration keeps admin edits;
// down / up gives the defaults back.
func TestGameDataRulesMigration(t *testing.T) {
	ctx := context.Background()
	names := registered(t)
	at := slices.Index(names, "041_gamedata_rules.up.sql")
	if at < 0 {
		t.Fatalf("cmd/migrate does not run 041: %v", names)
	}

	// the dev DB order: config rows (and the old seed of the dead rows) before 041
	db := dbtest.NewSchema(t, 0, names[:at]...)
	snap, err := gamedata.LoadYAML(configsDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snap.Data.Skills {
		execSQL(t, db, `INSERT INTO config_skills (skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id,
			damage_multiplier, description) VALUES ($1, $2, $3, 0, $4, 1, $5, 1, '')`, s.SkillID, s.CharacterID, s.Name, s.EffectType, s.StatusEffectID)
	}
	execSQL(t, db, `INSERT INTO game_settings (key, value, value_type, description, category) VALUES
		('fall.damage_threshold', '30', 'number', 'Khoảng rơi tối thiểu (pixels) trước khi nhận fall damage', 'physics'),
		('fall.damage_per_pixel', '0.5', 'number', 'Damage mỗi pixel rơi vượt ngưỡng', 'physics'),
		('move.step_pixels', '10', 'number', 'Số pixel di chuyển mỗi tick khi giữ nút move', 'movement'),
		('move.energy_cost_per_2px', '0.5', 'number', 'Năng lượng tiêu hao mỗi 2 pixel di chuyển', 'movement')`)
	for _, n := range names[at:] { // 041 and the migrations after it (043 adds rules.bots)
		runSQL(t, db, n)
	}
	if err := migrate.SeedConfigFromYAML(ctx, db.Pool, os.DirFS(configsDir())); err != nil { // the other config rows (the skills exist)
		t.Fatal(err)
	}
	checkDefaults(t, db)

	// admin edits survive a rerun of every migration
	execSQL(t, db, `UPDATE game_settings SET value = '{"lavaDamage": 90, "critMultiplier": 2, "burnMaxHpPercent": 10, "shieldCapPercent": 50}' WHERE key = 'rules.combat'`)
	execSQL(t, db, `UPDATE game_settings SET value = '120' WHERE key = 'fall.damage_threshold'`)
	execSQL(t, db, `UPDATE config_skills SET params = params || '{"baseCount": 14}'::jsonb WHERE skill_id = 'triple_shot'`)
	execSQL(t, db, `UPDATE config_status_effects SET bad = FALSE WHERE effect_id = 'net'`)
	execSQL(t, db, `UPDATE config_rank_tiers SET min_rating = 950 WHERE tier = 'silver'`)
	execSQL(t, db, `UPDATE config_gun_kinds SET name = 'Pháo' WHERE gun_kind = 'cannon'`)
	for _, n := range names {
		runSQL(t, db, n)
	}
	s, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if s.Rules.Combat.LavaDamage != 90 || s.Rules.Combat.CritMultiplier != 2 || s.Physics.FallDamageThreshold != 120 ||
		gamedata.RainTuningOf(s.Data.Skills["triple_shot"]).BaseCount != 14 || s.StatusRule("net").Bad || s.RankBands()[1].MinRating != 950 ||
		s.GunKinds[1].Name != "Pháo" {
		t.Fatalf("a rerun lost an admin edit: %+v %+v", s.Rules.Combat, s.RankBands()[1])
	}

	// down / up: the defaults again (the edited rows go with their columns)
	runSQL(t, db, "041_gamedata_rules.down.sql")
	if _, err := gamedata.LoadFromDB(ctx, db); err == nil {
		t.Fatal("game data read without the 041 tables")
	}
	var cols int
	_ = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND column_name IN ('params', 'bad', 'skips_turn', 'blocks_move', 'min_rating')`).Scan(&cols)
	if cols != 0 || setting(t, db, "fall.damage_threshold") != "120" {
		t.Fatalf("down left %d columns", cols)
	}
	for _, n := range names[at:] {
		runSQL(t, db, n)
	}
	execSQL(t, db, `UPDATE game_settings SET value = '150' WHERE key = 'fall.damage_threshold'`)
	checkDefaults(t, db)

	// a fresh install: every migration, then the admin seed
	checkDefaults(t, seededDB(t))
}

// checkDefaults: the DB's rules, tuning, status rules, bands and scalar rows are the Go defaults.
func checkDefaults(t *testing.T, db *database.PostgresDB) {
	t.Helper()
	ctx := context.Background()
	settings := map[string]string{}
	rows, err := db.Pool.Query(ctx, `SELECT key, value FROM game_settings`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		settings[k] = v
	}
	rows.Close()
	for _, g := range gamedata.RuleGroups {
		if _, ok := settings[g.Key]; !ok {
			t.Fatalf("no %s row", g.Key)
		}
	}
	r, errs := gamedata.RulesFromSettings(settings)
	if len(errs) != 0 || !reflect.DeepEqual(r, gamedata.DefaultRules()) {
		t.Fatalf("rules rows %v: %+v", errs, r)
	}
	if p := gamedata.PhysicsFromSettings(settings); p.FallDamageThreshold != 150 || p.FallDamagePerPixel != 3.75 || p.PxPerEnergy() != 2 ||
		p.EnergyPerPx() != 0.5 || p.TurnTimeSeconds != 20 {
		t.Fatalf("scalar rows %+v", p)
	}
	var raw []byte
	var effect string
	prows, err := db.Pool.Query(ctx, `SELECT effect_type, params FROM config_skills`)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for prows.Next() {
		_ = prows.Scan(&effect, &raw)
		var params map[string]float64
		_ = json.Unmarshal(raw, &params)
		if !reflect.DeepEqual(params, gamedata.DefaultSkillParams(effect)) {
			t.Fatalf("%s params %v", effect, params)
		}
		n++
	}
	prows.Close()
	if n != 5 { // 054: Tide's snow_nova
		t.Fatalf("%d skills", n)
	}
	s, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range gamedata.DefaultStatusEffects {
		want := gamedata.StatusRule{Bad: d.Bad, SkipsTurn: d.SkipsTurn, BlocksMove: d.BlocksMove || d.SkipsTurn}
		if s.StatusRule(d.EffectID) != want {
			t.Fatalf("%s: %+v", d.EffectID, s.StatusRule(d.EffectID))
		}
	}
	if !reflect.DeepEqual(s.RankBands(), ranked.DefaultBands) || len(s.GunKinds) != 6 || len(s.Slots) != 6 || len(s.TypeWords) != 12 {
		t.Fatalf("bands %+v, %d gun kinds, %d slots, %d type words", s.RankBands(), len(s.GunKinds), len(s.Slots), len(s.TypeWords))
	}
}

// Every write to a config table bumps the stored revision (the trigger).
func TestConfigWritesBumpTheRevision(t *testing.T) {
	db := seededDB(t)
	ctx := context.Background()
	r0, _ := gamedatasync.Revision(ctx, db)
	execSQL(t, db, `UPDATE config_weapons SET damage = damage + 1 WHERE weapon_id = 'cannon_ball'`)
	r1, _ := gamedatasync.Revision(ctx, db)
	execSQL(t, db, `UPDATE game_settings SET value = '21' WHERE key = 'match.turn_time_seconds'`)
	execSQL(t, db, `INSERT INTO config_tips (tip_id, text) VALUES ('t99', 'x')`)
	r2, _ := gamedatasync.Revision(ctx, db)
	if !(r1 > r0 && r2 >= r1+2) {
		t.Fatalf("revisions %d %d %d", r0, r1, r2)
	}
	if rev, err := gamedatasync.Bump(ctx, db); err != nil || rev != r2+1 {
		t.Fatalf("bump %d %v", rev, err)
	}
}

// adminServer is the dashboard on a seeded schema (with Redis when TEST_REDIS_ADDR or
// localhost:6379 answers, notifying on a test channel).
func adminServer(t *testing.T) (*Server, *database.PostgresDB, http.Handler) {
	t.Helper()
	db := seededDB(t)
	s := NewServer(db, testRedis(t), configsDir())
	s.notifyChannel = "gamedata:changed:test:" + t.Name()
	snap, err := gamedata.LoadFromDB(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	snap.Version = "test"
	gamedata.UseForTest(t, snap)
	return s, db, s.Routes()
}

func testRedis(t *testing.T) *database.RedisClient {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	r, err := database.NewRedisClient(addr, "", 0, 4, 0)
	if err != nil {
		return nil
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func post(h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

// Every game data page renders, with the version header.
func TestGameDataPagesRender(t *testing.T) {
	_, _, h := adminServer(t)
	pages := []string{"/", "/gamedata", "/rules", "/physics", "/skills", "/skills/tuning?id=triple_shot", "/skills/tuning?id=healing_bloom",
		"/characters/detail?id=tanko", "/items/edit?id=spider_net", "/matchmaking"}
	for _, tt := range textTables {
		pages = append(pages, "/texts/"+tt.Path)
	}
	for _, p := range pages {
		rec := get(h, p)
		body := rec.Body.String()
		if rec.Code != 200 || strings.Contains(body, "template exec error") || !strings.Contains(body, "Tải lại cấu hình") {
			t.Fatalf("%s: %d %.300s", p, rec.Code, body)
		}
	}
	if body := get(h, "/texts/rank-tiers").Body.String(); !strings.Contains(body, "Bạch kim") || !strings.Contains(body, `value="1800"`) {
		t.Fatal("rank tiers page")
	}
	if body := get(h, "/skills/tuning?id=heavy_bomb").Body.String(); !strings.Contains(body, "Bắn 6 tảng đá bọc đất") {
		t.Fatalf("rank 4 description missing")
	}
}

// Admin saves are validated; a save bumps the revision and notifies the servers; the reloaded
// game data carries the edit (and a new version).
func TestAdminSavesBumpTheVersion(t *testing.T) {
	s, db, h := adminServer(t)
	ctx := context.Background()
	var notes <-chan string
	if s.redis != nil {
		sub := s.redis.Client.Subscribe(ctx, s.notifyChannel)
		t.Cleanup(func() { sub.Close() })
		if _, err := sub.Receive(ctx); err != nil {
			t.Fatal(err)
		}
		ch := make(chan string, 16)
		go func() {
			for m := range sub.Channel() {
				ch <- m.Payload
			}
		}()
		notes = ch
	}
	before, _ := gamedatasync.Revision(ctx, db)
	v0 := versionOf(t, db)

	saved := func(rec *httptest.ResponseRecorder) bool {
		loc := rec.Header().Get("Location")
		return rec.Code == http.StatusSeeOther && strings.Contains(loc, "flash=") && !strings.Contains(loc, "error=")
	}

	// a rules group: an out-of-range value is refused, nothing changes
	g, _ := gamedata.RuleGroup("rules.combat")
	form := url.Values{"group": {g.Key}}
	for k, v := range g.FormValues(gamedata.DefaultRules()) {
		form.Set("f_"+k, v)
	}
	form.Set("f_critMultiplier", "0.5")
	if rec := post(h, "/rules/save", form); saved(rec) {
		t.Fatal("crit 0.5 accepted")
	}
	if after, _ := gamedatasync.Revision(ctx, db); after != before {
		t.Fatal("a refused save bumped the revision")
	}
	form.Set("f_critMultiplier", "1.75")
	if rec := post(h, "/rules/save", form); !saved(rec) {
		t.Fatalf("rules save: %s", rec.Header().Get("Location"))
	}
	if !strings.Contains(setting(t, db, "rules.combat"), `"critMultiplier": 1.75`) {
		t.Fatal(setting(t, db, "rules.combat"))
	}
	after, _ := gamedatasync.Revision(ctx, db)
	if after <= before {
		t.Fatalf("revision %d → %d", before, after)
	}
	if notes != nil {
		select {
		case <-notes:
		case <-time.After(3 * time.Second):
			t.Fatal("no notification")
		}
	}
	v1 := versionOf(t, db)
	if v1 == v0 {
		t.Fatal("the save kept the game data version")
	}

	// the default character must exist
	pg, _ := gamedata.RuleGroup("rules.player")
	pform := url.Values{"group": {pg.Key}, "f_expPerLevel": {"100"}, "f_defaultCharacterId": {"nobody"}}
	if rec := post(h, "/rules/save", pform); saved(rec) {
		t.Fatal("unknown default character accepted")
	}

	// the scalar rows: every one required and in range
	sform := url.Values{"scalars": {"1"}}
	for _, sc := range gamedata.ScalarSettings {
		sform.Set("s_"+sc.Key, setting(t, db, sc.Key))
	}
	sform.Set("s_match.turn_time_seconds", "0")
	if rec := post(h, "/rules/save", sform); saved(rec) {
		t.Fatal("turn time 0 accepted")
	}
	sform.Set("s_match.turn_time_seconds", "25")
	if rec := post(h, "/rules/save", sform); !saved(rec) || setting(t, db, "match.turn_time_seconds") != "25" {
		t.Fatalf("scalars: %s", rec.Header().Get("Location"))
	}

	// skill tuning
	tform := url.Values{"skill_id": {"triple_shot"}, "description_template": {"{count} tên lửa, {damagePct}%"}}
	for k, v := range gamedata.DefaultSkillParams(gamedata.EffectMissileRain) {
		tform.Set("param_"+k, gamedata.FormatParam(v))
	}
	tform.Set("param_baseCount", "12.5")
	if rec := post(h, "/skills/tuning/save", tform); saved(rec) {
		t.Fatal("fractional missile count accepted")
	}
	tform.Set("param_baseCount", "14")
	tform.Set("description_template", "{count} tên lửa {nope}")
	if rec := post(h, "/skills/tuning/save", tform); saved(rec) {
		t.Fatal("unknown placeholder accepted")
	}
	tform.Set("description_template", "{count} tên lửa, {damagePct}%")
	if rec := post(h, "/skills/tuning/save", tform); !saved(rec) {
		t.Fatalf("tuning: %s", rec.Header().Get("Location"))
	}

	// text tables: rank bands must stay valid; type words need a known gun kind
	tier := url.Values{"key": {"silver"}, "c_name": {"Bạc"}, "c_sort_order": {"2"}, "c_min_rating": {"0"}, "c_divisions": {"3"}, "c_division_size": {"66"}}
	if rec := post(h, "/texts/rank-tiers/save", tier); saved(rec) {
		t.Fatal("silver from rating 0 accepted")
	}
	tier.Set("c_min_rating", "950")
	if rec := post(h, "/texts/rank-tiers/save", tier); !saved(rec) {
		t.Fatalf("rank tier: %s", rec.Header().Get("Location"))
	}
	word := url.Values{"new": {"1"}, "key": {"laser gun"}, "c_name": {"Súng laze"}, "c_gun_kind": {"ray"}, "c_sort_order": {"12"}}
	if rec := post(h, "/texts/type-words/save", word); saved(rec) {
		t.Fatal("unknown gun kind accepted")
	}
	word.Set("c_gun_kind", "shock_rifle")
	if rec := post(h, "/texts/type-words/save", word); !saved(rec) {
		t.Fatalf("type word: %s", rec.Header().Get("Location"))
	}
	if rec := post(h, "/texts/tips/save", url.Values{"new": {"1"}, "key": {"Bad Key"}, "c_text": {"x"}, "c_sort_order": {"1"}}); saved(rec) {
		t.Fatal("bad key accepted")
	}
	if rec := post(h, "/texts/rank-tiers/delete", url.Values{"key": {"gold"}}); saved(rec) {
		t.Fatal("rank tier deleted")
	}

	// the items and characters pages save their texts too
	item := url.Values{"item_id": {"medkit"}, "name": {"Medkit"}, "type": {"heal"}, "target_type": {"self"}, "value": {"500"},
		"max_use_per_match": {"1"}, "cooldown": {"0"}, "display_name": {"Bộ cứu thương"}, "sort_order": {"1"}}
	if rec := post(h, "/items/save", item); !strings.Contains(rec.Header().Get("Location"), "flash=") {
		t.Fatalf("item: %s", rec.Header().Get("Location"))
	}
	item.Set("value", "-1")
	if rec := post(h, "/items/save", item); !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatal("negative item value accepted")
	}

	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Rules.Combat.CritMultiplier != 1.75 || snap.Physics.TurnTimeSeconds != 25 || gamedata.RainTuningOf(snap.Data.Skills["triple_shot"]).BaseCount != 14 ||
		snap.Data.Skills["triple_shot"].DescriptionTemplate != "{count} tên lửa, {damagePct}%" || snap.RankBands()[1].MinRating != 950 ||
		snap.Data.Items["medkit"].Value != 500 || snap.Data.Items["medkit"].DisplayName != "Bộ cứu thương" || len(snap.TypeWords) != 13 {
		t.Fatalf("saved game data %+v", snap.Rules.Combat)
	}

	// "Tải lại cấu hình" bumps the revision (and notifies) without another change
	r0, _ := gamedatasync.Revision(ctx, db)
	if rec := post(h, "/gamedata/reload", url.Values{}); !strings.Contains(rec.Header().Get("Location"), "flash=") {
		t.Fatalf("reload: %s", rec.Header().Get("Location"))
	}
	if r1, _ := gamedatasync.Revision(ctx, db); r1 != r0+1 {
		t.Fatalf("reload revision %d → %d", r0, r1)
	}
}

func versionOf(t *testing.T, db *database.PostgresDB) string {
	t.Helper()
	snap, err := gamedata.LoadFromDB(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return apigamedata.VersionOf(snap)
}
