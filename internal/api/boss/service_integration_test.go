package boss

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/observability"
)

func allMigrations(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = filepath.Base(f)
	}
	return out
}

// GET /boss/runs: every enabled boss with its daily bonus runs, the runs today (the UTC+7
// day) and the bonus runs left; yesterday's runs do not count; no token is UNAUTHORIZED.
func TestBossRunsDB(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_DSN") == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	ctx := context.Background()
	for _, r := range []struct {
		boss, day string
		runs      int
	}{{"steel_robot", "2026-10-22", 2}, {"lava_crab", "2026-10-22", 5}, {"ice_witch", "2026-10-21", 3}} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO player_boss_runs (player_id, boss_id, day, runs) VALUES ('p1', $1, $2, $3)`, r.boss, r.day, r.runs); err != nil {
			t.Fatal(err)
		}
	}
	bosses := gamedata.DefaultBosses()
	bosses[1].DailyBonusRuns = 4
	snap := gamedata.Current().WithBosses(append(bosses, gamedata.BossConfig{BossID: "off", Enabled: false, DailyBonusRuns: 3}))
	svc := NewService(NewRepository(db), func() *gamedata.Snapshot { return snap })
	svc.now = func() time.Time { return time.Date(2026, 10, 22, 16, 59, 0, 0, time.UTC) } // 23:59 UTC+7
	h := NewHandler(svc)

	req := httptest.NewRequest("GET", "/boss/runs", nil).WithContext(context.WithValue(ctx, observability.PlayerIDKey, "p1"))
	rec := httptest.NewRecorder()
	h.GetRuns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var env struct {
		Data RunsState `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	want := []BossRuns{
		{BossID: "steel_robot", DailyBonusRuns: 3, RunsToday: 2, BonusRunsLeft: 1},
		{BossID: "lava_crab", DailyBonusRuns: 4, RunsToday: 5, BonusRunsLeft: 0},
		{BossID: "ice_witch", DailyBonusRuns: 3, RunsToday: 0, BonusRunsLeft: 3},
	}
	if env.Data.Day != "2026-10-22" || !env.Data.ResetAt.Equal(time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC)) || !reflect.DeepEqual(env.Data.Bosses, want) {
		t.Fatalf("runs %+v", env.Data)
	}

	rec = httptest.NewRecorder()
	h.GetRuns(rec, httptest.NewRequest("GET", "/boss/runs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no player: %d", rec.Code)
	}
}
