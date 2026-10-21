package rank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
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
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	return names
}

func newRankHandler(t *testing.T) (*Handler, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	return NewHandler(NewService(NewRepository(db), economy.NewRepository(), db)), db
}

func exec(t *testing.T, db *database.PostgresDB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

// rankMe calls GET /rank/me as the player and returns the data object.
func rankMe(t *testing.T, h *Handler, playerID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/rank/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, playerID))
	rec := httptest.NewRecorder()
	h.GetRankMe(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

// With no season the first call creates "Mùa 1"; a player without a row gets
// 1000 silver 3 and rankPos 0; rows get their leaderboard position.
func TestRankMe(t *testing.T) {
	h, db := newRankHandler(t)
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		dbtest.CreatePlayer(t, db, id, 0, 0)
	}

	me := rankMe(t, h, "p4")
	season, _ := me["season"].(map[string]any)
	if me["rating"] != 1000.0 || me["tier"] != "silver" || me["division"] != 3.0 || me["rankPos"] != 0.0 || me["highestTier"] != "silver" {
		t.Fatalf("default %v", me)
	}
	if season["seasonId"] != "season_1" || season["name"] != "Mùa 1" || me["seasonId"] != "season_1" {
		t.Fatalf("season %v", season)
	}
	ends, err := time.Parse(time.RFC3339, season["endsAt"].(string))
	if err != nil || ends.Before(time.Now().Add(80*24*time.Hour)) {
		t.Fatalf("endsAt %v %v", season["endsAt"], err)
	}
	for _, k := range []string{"playerId", "displayName", "wins", "losses", "draws", "winStreak", "updatedAt"} {
		if _, ok := me[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}

	exec(t, db, `INSERT INTO player_ranks (player_id, season_id, rating, tier, division, wins, highest_tier) VALUES
		('p1', 'season_1', 1100, 'silver', 2, 1, 'silver'),
		('p2', 'season_1', 1250, 'gold', 3, 0, 'gold'),
		('p3', 'season_1', 1100, 'silver', 2, 4, 'silver')`)
	for id, want := range map[string]float64{"p2": 1, "p3": 2, "p1": 3, "p4": 0} {
		if got := rankMe(t, h, id)["rankPos"]; got != want {
			t.Errorf("%s rankPos %v, want %v", id, got, want)
		}
	}

	// The season endpoint returns the same real row
	cur, err := h.service.GetCurrentSeason(context.Background())
	if err != nil || cur.SeasonID != "season_1" || cur.Name != "Mùa 1" {
		t.Fatalf("current season %+v %v", cur, err)
	}
}

// The elo config's default rating is the rating of a player without a row.
func TestRankMeDefaultFromEloConfig(t *testing.T) {
	h, db := newRankHandler(t)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `UPDATE game_settings SET value = '{"kFactor":32,"ratingFloor":0,"defaultRating":1250}' WHERE key = 'elo'`)
	me := rankMe(t, h, "p1")
	if me["rating"] != 1250.0 || me["tier"] != "gold" || me["division"] != 3.0 {
		t.Fatalf("default %v", me)
	}
}

// Migration 035 adds botFillAfter / matchStartDelay to the matchmaking row once
// (an admin edit survives reruns), renames the auto-created season, and its down
// file undoes both.
func TestRanked2v2Migration(t *testing.T) {
	_, db := newRankHandler(t)
	ctx := context.Background()
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	cfg := func() map[string]any {
		var raw string
		if err := db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'matchmaking'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		out := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if c := cfg(); c["botFillAfter"] != 20.0 || c["matchStartDelay"] != 3.0 || c["maxWaitTime"] != 60.0 {
		t.Fatalf("after up %v", c)
	}
	exec(t, db, `UPDATE game_settings SET value = (value::jsonb || '{"botFillAfter":30}')::text WHERE key = 'matchmaking'`)
	exec(t, db, `INSERT INTO rank_seasons (season_id, name, starts_at, ends_at, status) VALUES ('season_1', 'Default Season', now(), now(), 'active')`)
	exec(t, db, read("035_ranked_2v2.up.sql"))
	if c := cfg(); c["botFillAfter"] != 30.0 || c["matchStartDelay"] != 3.0 {
		t.Fatalf("rerun %v", c)
	}
	var name string
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM rank_seasons WHERE season_id = 'season_1'`).Scan(&name); err != nil || name != "Mùa 1" {
		t.Fatalf("season name %q %v", name, err)
	}

	exec(t, db, read("035_ranked_2v2.down.sql"))
	if c := cfg(); c["botFillAfter"] != nil || c["matchStartDelay"] != nil || c["maxWaitTime"] != 60.0 {
		t.Fatalf("after down %v", c)
	}
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM rank_seasons WHERE season_id = 'season_1'`).Scan(&name); err != nil || name != "Default Season" {
		t.Fatalf("season name after down %q %v", name, err)
	}
	exec(t, db, read("035_ranked_2v2.up.sql"))
	if c := cfg(); c["botFillAfter"] != 20.0 || c["matchStartDelay"] != 3.0 {
		t.Fatalf("down / up %v", c)
	}
}
