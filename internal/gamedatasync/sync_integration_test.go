package gamedatasync_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	apigamedata "battle-squad/internal/api/gamedata"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/gamedatasync"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/migrate"
	"battle-squad/internal/shared/ranked"

	"github.com/go-chi/chi/v5"
)

func migrations(t *testing.T) []string {
	return dbtest.Migrations(t)
}

func seeded(t *testing.T) *database.PostgresDB {
	t.Helper()
	db := dbtest.NewSchema(t, 0, migrations(t)...)
	_, self, _, _ := runtime.Caller(0)
	if _, err := migrate.SeedAll(context.Background(), db.Pool, os.DirFS(filepath.Join(filepath.Dir(self), "..", "..", "configs"))); err != nil {
		t.Fatal(err)
	}
	return db
}

func redisOrSkip(t *testing.T) *database.RedisClient {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	r, err := database.NewRedisClient(addr, "", 0, 4, 0)
	if err != nil {
		t.Skip("no redis at " + addr)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// isolate restores the published game data and rank bands when the test ends.
func isolate(t *testing.T) {
	gamedata.UseForTest(t, gamedata.Current())
	t.Cleanup(func() { _ = ranked.SetBands(ranked.DefaultBands) })
}

func newReloader(t *testing.T, db *database.PostgresDB, r *database.RedisClient, poll time.Duration) *gamedatasync.Reloader {
	t.Helper()
	rl := gamedatasync.New(db, r, "test", t.Name(), "")
	rl.Channel = "gamedata:changed:test:" + t.Name()
	rl.LoadedKey = "gamedata:loaded:test:" + t.Name()
	rl.Poll = poll
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if r != nil {
		t.Cleanup(func() { r.Client.Del(context.Background(), rl.LoadedKey) })
	}
	if err := rl.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return rl
}

func waitFor(t *testing.T, what string, ok func() bool, kick func()) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		if kick != nil {
			kick()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func serveGameData(h http.Handler, path, etag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A notification reloads the game data: new matches get it (with its version, the same as GET
// /gamedata's) while a match started before keeps the old one, whose payload stays served
// under GET /gamedata?version=.
func TestReloadOnNotification(t *testing.T) {
	isolate(t)
	db, rds := seeded(t), redisOrSkip(t)
	rl := newReloader(t, db, rds, 0) // no polling: only the notification
	ctx := context.Background()

	v1 := gamedata.Current().Version
	if len(v1) != 16 || gamedata.Current().Source != "db" {
		t.Fatalf("first load %q %s", v1, gamedata.Current().Source)
	}

	api := chi.NewRouter()
	api.Get("/gamedata", apigamedata.NewHandler(apigamedata.NewService(apigamedata.NewRepository(db)).WithSnapshots(gamedata.Current)).Get)
	rec := serveGameData(api, "/gamedata", "")
	if rec.Header().Get("ETag") != `"`+v1+`"` {
		t.Fatalf("GET /gamedata ETag %s, snapshot %s", rec.Header().Get("ETag"), v1)
	}

	// a match started now carries the version GET /gamedata reports
	players := []*match.BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "rookie", HP: 1500, MaxHP: 1500, Position: match.Vector2{X: 300}, IsAlive: true},
		{PlayerID: "p2", TeamID: 2, CharacterID: "tanko", HP: 1650, MaxHP: 1650, Position: match.Vector2{X: 1300}, IsAlive: true},
	}
	old := match.NewMatch("m1", "r1", "pvp_1v1", "grassland_valley", players, map[string]*ws.Client{}, nil, nil, nil, nil)
	t.Cleanup(old.Stop)
	if old.GameData().Version != v1 {
		t.Fatalf("match version %s, GET /gamedata %s", old.GameData().Version, v1)
	}

	// an admin edit, then the notification
	if _, err := db.Pool.Exec(ctx, `UPDATE game_settings SET value = '{"lavaDamage": 99, "critMultiplier": 1.5, "burnMaxHpPercent": 10, "shieldCapPercent": 50}'
		WHERE key = 'rules.combat'`); err != nil {
		t.Fatal(err)
	}
	rev, _ := gamedatasync.Revision(ctx, db)
	waitFor(t, "the reload", func() bool { return gamedata.Current().Rules.Combat.LavaDamage == 99 },
		func() { _ = gamedatasync.NotifyOn(ctx, rds, rl.Channel, rev) })
	v2 := gamedata.Current().Version
	if v2 == v1 || gamedata.Current().Revision != rev {
		t.Fatalf("versions %s %s, revision %d (want %d)", v1, v2, gamedata.Current().Revision, rev)
	}
	if old.GameData().Version != v1 || old.GameData().Rules.Combat.LavaDamage != 75 {
		t.Fatal("the running match lost its game data")
	}

	// GET /gamedata serves the new version; ?version= the old match's exact payload
	rec = serveGameData(api, "/gamedata", `"`+v1+`"`)
	var env struct {
		Data apigamedata.GameData `json:"data"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &env) != nil || env.Data.Version != v2 || env.Data.Combat.LavaDamage != 99 {
		t.Fatalf("current: %d %s", rec.Code, env.Data.Version)
	}
	rec = serveGameData(api, "/gamedata?version="+v1, "")
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &env) != nil || env.Data.Version != v1 || env.Data.Combat.LavaDamage != 75 ||
		rec.Header().Get("ETag") != `"`+v1+`"` {
		t.Fatalf("archived: %d %s", rec.Code, env.Data.Version)
	}
	if rec := serveGameData(api, "/gamedata?version="+v1, `"`+v1+`"`); rec.Code != http.StatusNotModified {
		t.Fatalf("archived 304: %d", rec.Code)
	}
	if rec := serveGameData(api, "/gamedata?version=0000000000000000", ""); rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "GAMEDATA_VERSION_NOT_FOUND") {
		t.Fatalf("unknown version: %d %s", rec.Code, rec.Body)
	}

	// a new match plays with the new game data
	n := match.NewMatch("m2", "r1", "pvp_1v1", "grassland_valley", players, map[string]*ws.Client{}, nil, nil, nil, nil)
	t.Cleanup(n.Stop)
	if n.GameData().Version != v2 || n.GameData().Rules.Combat.LavaDamage != 99 {
		t.Fatal("new match")
	}

	// the status every process reports
	st, err := rds.Client.HGetAll(ctx, rl.LoadedKey).Result()
	if err != nil || !strings.Contains(st["test/"+t.Name()], v2) {
		t.Fatalf("loaded status %v %v", st, err)
	}

	// a notification without a change keeps the snapshot (the same pointer)
	before := gamedata.Current()
	if _, err := rl.Reload(ctx); err != nil || gamedata.Current() != before {
		t.Fatal("a reload without changes replaced the snapshot")
	}

	// a broken config keeps the current game data (and reports the error)
	if _, err := db.Pool.Exec(ctx, `UPDATE config_characters SET weapon_id = 'gone' WHERE character_id = 'rookie'`); err != nil {
		t.Fatal(err)
	}
	if _, err := rl.Reload(ctx); err == nil || gamedata.Current() != before {
		t.Fatal("a broken config was published")
	}
	st, _ = rds.Client.HGetAll(ctx, rl.LoadedKey).Result()
	if !strings.Contains(st["test/"+t.Name()], "non-existent weapon") {
		t.Fatalf("error not reported: %v", st)
	}
}

// Without a notification the revision poll reloads; the rank bands follow the snapshot.
func TestReloadByPolling(t *testing.T) {
	isolate(t)
	db := seeded(t)
	newReloader(t, db, nil, 100*time.Millisecond)
	ctx := context.Background()
	v1 := gamedata.Current().Version
	if _, err := db.Pool.Exec(ctx, `UPDATE config_rank_tiers SET min_rating = 900 WHERE tier = 'silver'`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the poll", func() bool { return gamedata.Current().Version != v1 }, nil)
	if tier, _ := ranked.TierAndDivision(950); tier != "silver" {
		t.Fatalf("rating 950 is %s with the reloaded bands", tier)
	}
}
