package mission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

// testNow is Wednesday 2026-10-21 17:00 in Vietnam (week 2026-W43).
var testNow = utc("2026-10-21T10:00:00Z")

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

// newService builds every migration; maxConns 1 makes a pool query made while a claim tx
// holds the only connection hang the test.
func newService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB, *time.Time) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, allMigrations(t)...)
	svc := NewService(NewRepository(db), economy.NewRepository(), db)
	now := testNow
	svc.now = func() time.Time { return now }
	return svc, db, &now
}

func exec(t *testing.T, db *database.PostgresDB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func scalar(t *testing.T, db *database.PostgresDB, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr model.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err %v, want %s", err, code)
	}
}

func mission(t *testing.T, st DailyState, id string) MissionView {
	t.Helper()
	for _, m := range st.Missions {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("mission %s not listed", id)
	return MissionView{}
}

func chest(t *testing.T, st DailyState, id string) ChestView {
	t.Helper()
	for _, c := range append(append([]ChestView{}, st.Chests...), st.Weekly...) {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("chest %s not listed", id)
	return ChestView{}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// recordMatch counts a match in its own tx, as the match end does.
func recordMatch(t *testing.T, db *database.PostgresDB, playerID string, s MatchStats, now time.Time) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := RecordMatchTx(ctx, tx, playerID, s, now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDailyLoginAndMatchProgress(t *testing.T) {
	svc, db, _ := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	ctx := context.Background()

	st, err := svc.Daily(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Day != "2026-10-21" || st.Week != "2026-W43" || len(st.Missions) != 10 || len(st.Chests) != 5 || len(st.Weekly) != 4 {
		t.Fatalf("state %+v", st)
	}
	if st.Missions[0].ID != "daily_login" || st.Missions[9].ID != "daily_shop_1" {
		t.Fatalf("seed order %v .. %v", st.Missions[0].ID, st.Missions[9].ID)
	}
	if m := mission(t, st, "daily_login"); m.Progress != 1 || m.State != StateClaimable || m.Title != "Đăng nhập trò chơi" || m.Goto != "none" {
		t.Fatalf("login %+v", m)
	}
	for _, c := range append(st.Chests, st.Weekly...) {
		if c.State != StateLocked {
			t.Fatalf("chest %+v", c)
		}
	}
	// A second GET writes nothing new
	if _, err := svc.Daily(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM mission_progress WHERE player_id = 'p1'`); n != 1 {
		t.Fatalf("%d progress rows", n)
	}

	recordMatch(t, db, "p1", MatchStats{Won: true, Damage: 1800, Kills: 1, ShotsHit: 4, SkillUses: 1, ItemsUsed: 2}, testNow)
	recordMatch(t, db, "p1", MatchStats{Damage: 1700, Kills: 1, ShotsHit: 5, SkillUses: 1, ItemsUsed: 2}, testNow)
	st, err = svc.Daily(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]any{
		"daily_play_3":      {2, StateProgress},
		"daily_win_1":       {1, StateClaimable},
		"daily_damage_3000": {3000, StateClaimable}, // 3500 capped
		"daily_kill_3":      {2, StateProgress},
		"daily_hit_10":      {9, StateProgress},
		"daily_skill_2":     {2, StateClaimable},
		"daily_item_3":      {3, StateClaimable}, // 4 capped
		"daily_upgrade_1":   {0, StateProgress},
	}
	for id, w := range want {
		if m := mission(t, st, id); m.Progress != w[0] || m.State != w[1] {
			t.Errorf("%s: %d %s, want %v %v", id, m.Progress, m.State, w[0], w[1])
		}
	}
	if v := scalar(t, db, `SELECT current_value FROM mission_progress WHERE player_id = 'p1' AND mission_id = 'daily_damage_3000'`); v != 3500 {
		t.Errorf("stored damage %d", v)
	}

	// Upgrades and shop purchases count through their own txs
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecordUpgradeTx(ctx, tx, "p1", testNow); err != nil {
		t.Fatal(err)
	}
	if err := RecordShopPurchaseTx(ctx, tx, "p1", testNow); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.Daily(ctx, "p1")
	if mission(t, st, "daily_upgrade_1").State != StateClaimable || mission(t, st, "daily_shop_1").State != StateClaimable {
		t.Fatalf("upgrade / shop %+v %+v", mission(t, st, "daily_upgrade_1"), mission(t, st, "daily_shop_1"))
	}
}

func TestClaimMissionAndChests(t *testing.T) {
	svc, db, now := newService(t, 1)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	ctx := context.Background()
	recordMatch(t, db, "p1", MatchStats{Won: true}, testNow)

	// Claiming without a GET first still counts today's login
	res, err := svc.ClaimMission(ctx, "p1", "daily_login")
	if err != nil {
		t.Fatal(err)
	}
	if res.Activity != 10 || res.State.Activity != 10 || res.State.WeeklyActivity != 10 ||
		mission(t, res.State, "daily_login").State != StateClaimed {
		t.Fatalf("claim login %+v", res)
	}
	_, err = svc.ClaimMission(ctx, "p1", "daily_login")
	wantCode(t, err, "MISSION_ALREADY_CLAIMED")
	_, err = svc.ClaimMission(ctx, "p1", "daily_play_3")
	wantCode(t, err, "MISSION_NOT_DONE")
	_, err = svc.ClaimMission(ctx, "p1", "nope")
	wantCode(t, err, "MISSION_NOT_FOUND")
	_, err = svc.ClaimMission(ctx, "p1", "chest_d20")
	wantCode(t, err, "MISSION_NOT_FOUND")
	_, err = svc.ClaimMission(ctx, "p1", "")
	wantCode(t, err, "BAD_REQUEST")
	_, err = svc.ClaimMission(ctx, "ghost", "daily_login")
	wantCode(t, err, "NOT_FOUND")
	// A milestone is not a daily mission
	exec(t, db, `INSERT INTO mission_progress (player_id, mission_id, current_value) VALUES ('p1', 'ms_wins_0005', 99)`)
	_, err = svc.ClaimMission(ctx, "p1", "ms_wins_0005")
	wantCode(t, err, "MISSION_NOT_FOUND")

	// Chest at 20: locked at 10, claimable at 30
	_, err = svc.ClaimChest(ctx, "p1", "chest_d20")
	wantCode(t, err, "CHEST_LOCKED")
	if res, err = svc.ClaimMission(ctx, "p1", "daily_win_1"); err != nil || res.State.Activity != 30 {
		t.Fatalf("claim win %+v %v", res, err)
	}
	if c := chest(t, res.State, "chest_d20"); c.State != StateClaimable {
		t.Fatalf("chest_d20 %+v", c)
	}
	cres, err := svc.ClaimChest(ctx, "p1", "chest_d20")
	if err != nil {
		t.Fatal(err)
	}
	if cres.Rewards.Coin != 300 || cres.Rewards.Items == nil || chest(t, cres.State, "chest_d20").State != StateClaimed ||
		chest(t, cres.State, "chest_d40").State != StateLocked {
		t.Fatalf("chest claim %+v", cres)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 300 {
		t.Fatalf("coin %d", coin)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM economy_transactions WHERE player_id = 'p1' AND source = 'mission' AND ref_id = 'chest_d20' AND amount = 300`); n != 1 {
		t.Fatalf("%d economy rows", n)
	}
	_, err = svc.ClaimChest(ctx, "p1", "chest_d20")
	wantCode(t, err, "CHEST_ALREADY_CLAIMED")
	_, err = svc.ClaimChest(ctx, "p1", "chest_d40")
	wantCode(t, err, "CHEST_LOCKED")
	_, err = svc.ClaimChest(ctx, "p1", "daily_login")
	wantCode(t, err, "CHEST_NOT_FOUND")
	_, err = svc.ClaimChest(ctx, "p1", "nope")
	wantCode(t, err, "CHEST_NOT_FOUND")
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 300 {
		t.Fatalf("a refusal paid: coin %d", coin)
	}

	// Item and stone rewards; weekly activity
	exec(t, db, `UPDATE player_mission_activity SET points = 80 WHERE player_id = 'p1' AND track = 'daily'`)
	exec(t, db, `UPDATE player_mission_activity SET points = 450 WHERE player_id = 'p1' AND track = 'weekly'`)
	if cres, err = svc.ClaimChest(ctx, "p1", "chest_d80"); err != nil || cres.Rewards.Coin != 500 || len(cres.Rewards.Items) != 1 {
		t.Fatalf("chest_d80 %+v %v", cres, err)
	}
	if n := scalar(t, db, `SELECT quantity FROM inventory_items WHERE player_id = 'p1' AND item_id = 'medkit' AND source = 'mission_reward'`); n != 2 {
		t.Fatalf("medkits %d", n)
	}
	if cres, err = svc.ClaimChest(ctx, "p1", "chest_w450"); err != nil || chest(t, cres.State, "chest_w450").State != StateClaimed ||
		chest(t, cres.State, "chest_w600").State != StateLocked {
		t.Fatalf("chest_w450 %+v %v", cres, err)
	}
	if n := scalar(t, db, `SELECT quantity FROM player_stones WHERE player_id = 'p1' AND stone_level = 4`); n != 3 {
		t.Fatalf("stones %d", n)
	}

	// Next day: missions, daily activity and daily chests start over; the week goes on
	*now = testNow.Add(24 * time.Hour)
	st, err := svc.Daily(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Day != "2026-10-22" || st.Activity != 0 || st.WeeklyActivity != 450 {
		t.Fatalf("next day %+v", st)
	}
	if m := mission(t, st, "daily_win_1"); m.Progress != 0 || m.State != StateProgress {
		t.Fatalf("yesterday's win %+v", m)
	}
	if m := mission(t, st, "daily_login"); m.Progress != 1 || m.State != StateClaimable {
		t.Fatalf("today's login %+v", m)
	}
	if chest(t, st, "chest_d20").State != StateLocked || chest(t, st, "chest_w450").State != StateClaimed {
		t.Fatalf("chests next day %+v %+v", chest(t, st, "chest_d20"), chest(t, st, "chest_w450"))
	}
	recordMatch(t, db, "p1", MatchStats{Won: true}, *now) // yesterday's claimed row restarts unclaimed
	if _, err := svc.ClaimMission(ctx, "p1", "daily_login"); err != nil {
		t.Fatal(err)
	}
	if res, err = svc.ClaimMission(ctx, "p1", "daily_win_1"); err != nil || res.State.Activity != 30 || res.State.WeeklyActivity != 480 {
		t.Fatalf("next day claims %+v %v", res, err)
	}
	if _, err := svc.ClaimChest(ctx, "p1", "chest_d20"); err != nil {
		t.Fatalf("chest_d20 again the next day: %v", err)
	}

	// Next week: weekly activity and weekly chests start over
	*now = testNow.Add(7 * 24 * time.Hour)
	st, _ = svc.Daily(ctx, "p1")
	if st.Week != "2026-W44" || st.WeeklyActivity != 0 || chest(t, st, "chest_w450").State != StateLocked {
		t.Fatalf("next week %+v", st)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 1100 {
		t.Fatalf("coin %d, want 300 + 500 + 300", coin)
	}
}

// An older period's rows read as 0 / unclaimed, whatever they hold.
func TestOlderPeriodReadsAsZero(t *testing.T) {
	svc, db, _ := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed, period) VALUES
		('p1', 'daily_play_3', 3, TRUE, '2026-10-20'), ('p1', 'daily_win_1', 1, FALSE, '2026-10-20'),
		('p1', 'chest_d20', 20, TRUE, '2026-10-20'), ('p1', 'chest_w150', 150, TRUE, '2026-W42')`)
	exec(t, db, `INSERT INTO player_mission_activity (player_id, track, period, points) VALUES
		('p1', 'daily', '2026-10-20', 100), ('p1', 'weekly', '2026-W42', 600)`)
	st, err := svc.Daily(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Activity != 0 || st.WeeklyActivity != 0 {
		t.Fatalf("activity %d / %d", st.Activity, st.WeeklyActivity)
	}
	for _, id := range []string{"daily_play_3", "daily_win_1"} {
		if m := mission(t, st, id); m.Progress != 0 || m.State != StateProgress {
			t.Fatalf("%s %+v", id, m)
		}
	}
	if chest(t, st, "chest_d20").State != StateLocked || chest(t, st, "chest_w150").State != StateLocked {
		t.Fatal("an old claim still reads claimed")
	}
	_, err = svc.ClaimMission(context.Background(), "p1", "daily_play_3")
	wantCode(t, err, "MISSION_NOT_DONE")
}

func TestConcurrentChestClaimGrantsOnce(t *testing.T) {
	svc, db, _ := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_mission_activity (player_id, track, period, points) VALUES ('p1', 'daily', '2026-10-21', 100)`)
	exec(t, db, `INSERT INTO mission_progress (player_id, mission_id, current_value, period) VALUES ('p1', 'daily_win_1', 1, '2026-10-21')`)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	merrs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.ClaimChest(context.Background(), "p1", "chest_d100")
			_, merrs[i] = svc.ClaimMission(context.Background(), "p1", "daily_win_1")
		}(i)
	}
	wg.Wait()
	ok, mok := 0, 0
	for i := range errs {
		if errs[i] == nil {
			ok++
		} else {
			wantCode(t, errs[i], "CHEST_ALREADY_CLAIMED")
		}
		if merrs[i] == nil {
			mok++
		} else {
			wantCode(t, merrs[i], "MISSION_ALREADY_CLAIMED")
		}
	}
	coin, gem := dbtest.Balance(t, db, "p1")
	if ok != 1 || mok != 1 || coin != 1000 || gem != 20 {
		t.Fatalf("%d chest / %d mission claims, coin %d gem %d", ok, mok, coin, gem)
	}
	if a := scalar(t, db, `SELECT points FROM player_mission_activity WHERE player_id = 'p1' AND track = 'daily'`); a != 120 {
		t.Fatalf("activity %d", a)
	}
}

func call(t *testing.T, fn http.HandlerFunc, playerID string, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	if playerID != "" {
		req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, playerID))
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, env
}

func TestHandlers(t *testing.T) {
	svc, db, _ := newService(t, 0)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)

	code, env := call(t, h.GetDailyMissions, "p1", "")
	data, _ := env["data"].(map[string]any)
	if code != 200 || data["day"] != "2026-10-21" || data["dayResetAt"] != "2026-10-21T17:00:00Z" ||
		data["week"] != "2026-W43" || data["weekResetAt"] != "2026-10-25T17:00:00Z" || data["activity"] != 0.0 {
		t.Fatalf("daily %d %v", code, env)
	}
	if ms, _ := data["missions"].([]any); len(ms) != 10 {
		t.Fatalf("missions %v", data["missions"])
	}

	code, env = call(t, h.ClaimMission, "p1", `{"missionId":"daily_login"}`)
	data, _ = env["data"].(map[string]any)
	if st, _ := data["state"].(map[string]any); code != 200 || data["activity"] != 10.0 || st["activity"] != 10.0 {
		t.Fatalf("claim %d %v", code, env)
	}
	exec(t, db, `UPDATE player_mission_activity SET points = 20 WHERE player_id = 'p1' AND track = 'daily'`)
	code, env = call(t, h.ClaimChest, "p1", `{"chestId":"chest_d20"}`)
	data, _ = env["data"].(map[string]any)
	if r, _ := data["rewards"].(map[string]any); code != 200 || r["coin"] != 300.0 || data["state"] == nil {
		t.Fatalf("chest %d %v", code, env)
	}

	cases := []struct {
		fn     http.HandlerFunc
		player string
		body   string
		status int
		code   string
	}{
		{h.ClaimMission, "p1", `{"missionId":"daily_login"}`, 409, "MISSION_ALREADY_CLAIMED"},
		{h.ClaimMission, "p1", `{"missionId":"daily_play_3"}`, 400, "MISSION_NOT_DONE"},
		{h.ClaimMission, "p1", `{"missionId":"nope"}`, 404, "MISSION_NOT_FOUND"},
		{h.ClaimMission, "p1", `not json`, 400, "BAD_REQUEST"},
		{h.ClaimMission, "", `{"missionId":"daily_login"}`, 401, "AUTH_UNAUTHORIZED"},
		{h.ClaimChest, "p1", `{"chestId":"chest_d20"}`, 409, "CHEST_ALREADY_CLAIMED"},
		{h.ClaimChest, "p1", `{"chestId":"chest_d40"}`, 400, "CHEST_LOCKED"},
		{h.ClaimChest, "p1", `{"chestId":"nope"}`, 404, "CHEST_NOT_FOUND"},
		{h.ClaimChest, "p1", `{}`, 400, "BAD_REQUEST"},
		{h.ClaimChest, "", `{"chestId":"chest_d20"}`, 401, "AUTH_UNAUTHORIZED"},
		{h.GetDailyMissions, "", ``, 401, "AUTH_UNAUTHORIZED"},
	}
	for _, c := range cases {
		code, env := call(t, c.fn, c.player, c.body)
		e, _ := env["error"].(map[string]any)
		if code != c.status || e["code"] != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.body, code, env, c.status, c.code)
		}
	}
}

// 034 reruns without changing anything; admin edits to the seeds are kept.
func TestMigrationRerun(t *testing.T) {
	_, db, _ := newService(t, 0)
	exec(t, db, `UPDATE missions SET activity = 15 WHERE mission_id = 'daily_hit_10'`)
	exec(t, db, readMigration(t, "034_daily_missions.up.sql"))
	if n := scalar(t, db, `SELECT COUNT(*) FROM missions WHERE type IN ('daily', 'mission_chest')`); n != 19 {
		t.Fatalf("%d rows", n)
	}
	if a := scalar(t, db, `SELECT activity FROM missions WHERE mission_id = 'daily_hit_10'`); a != 15 {
		t.Fatalf("admin edit lost: %d", a)
	}
	if total := scalar(t, db, `SELECT SUM(activity) FROM missions WHERE type = 'daily'`); total != 145 {
		t.Fatalf("total activity %d", total)
	}
	exec(t, db, readMigration(t, "034_daily_missions.down.sql"))
	exec(t, db, readMigration(t, "034_daily_missions.up.sql"))
	if total := scalar(t, db, `SELECT SUM(activity) FROM missions WHERE type = 'daily'`); total != 140 {
		t.Fatalf("total activity after down / up %d", total)
	}
}
