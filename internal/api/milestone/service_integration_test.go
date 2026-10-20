package milestone

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
	"strings"
	"sync"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/mission"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

// allMigrations lists every up migration in order, as cmd/migrate applies them.
func allMigrations(t *testing.T, upTo string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var names []string
	for _, f := range files {
		name := filepath.Base(f)
		if upTo != "" && name > upTo {
			break
		}
		names = append(names, name)
	}
	return names
}

func newService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, allMigrations(t, "")...)
	return NewService(NewRepository(db), economy.NewRepository(), db), db
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

func setCounter(t *testing.T, db *database.PostgresDB, playerID, counter string, v int) {
	t.Helper()
	exec(t, db, `INSERT INTO player_stat_counters (player_id, counter, value) VALUES ($1, $2, $3)
		ON CONFLICT (player_id, counter) DO UPDATE SET value = EXCLUDED.value`, playerID, counter, v)
}

func wear(t *testing.T, db *database.PostgresDB, playerID, character string, level int, slots ...string) {
	t.Helper()
	for _, slot := range slots {
		exec(t, db, `INSERT INTO player_equipment (player_id, item_id, slot, category, upgrade_level, is_equipped, equipped_on)
			VALUES ($1, $2, $3, 'normal', $4, TRUE, $5)`, playerID, "it_"+slot, slot, level, character)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr model.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err %v, want %s", err, code)
	}
}

func node(t *testing.T, list ListResponse, id string) Node {
	t.Helper()
	for _, tr := range list.Tracks {
		for _, n := range tr.Nodes {
			if n.ID == id {
				return n
			}
		}
	}
	t.Fatalf("node %s not listed", id)
	return Node{}
}

func stones(t *testing.T, db *database.PostgresDB, playerID string) map[int]int {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(), `SELECT stone_level, quantity FROM player_stones WHERE player_id = $1`, playerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var l, q int
		if err := rows.Scan(&l, &q); err != nil {
			t.Fatal(err)
		}
		out[l] = q
	}
	return out
}

func TestListProgressPerTrack(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_characters (player_id, character_id, level) VALUES ('p1', 'rookie', 12), ('p1', 'tanko', 23)`)
	wear(t, db, "p1", "rookie", 6, "helmet", "armor", "gloves", "pants", "boots", "weapon")
	wear(t, db, "p1", "tanko", 10, "helmet", "armor")
	setCounter(t, db, "p1", CounterUpgradePeak, 14) // a +14 piece was sold
	setCounter(t, db, "p1", CounterMatches, 37)
	setCounter(t, db, "p1", CounterWins, 5)
	setCounter(t, db, "p1", "char_wins:tanko", 4)
	setCounter(t, db, "p1", CounterRankPeak, 3)

	list, err := svc.List(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Tracks) != 4 || list.Tracks[0].Track != TrackCharacter || list.Tracks[3].Track != TrackRank {
		t.Fatalf("tracks %+v", list.Tracks)
	}
	want := map[string][2]any{
		"ms_char_lv_20":     {23, StateClaimable},
		"ms_char_lv_25":     {23, StateLocked},
		"ms_upgrade_14":     {14, StateClaimable},
		"ms_upgrade_16":     {14, StateLocked},
		"ms_weapon_14":      {6, StateLocked},
		"ms_set_06":         {6, StateClaimable},
		"ms_set_10":         {2, StateLocked},
		"ms_matches_0010":   {37, StateClaimable},
		"ms_matches_0050":   {37, StateLocked},
		"ms_wins_0005":      {5, StateClaimable},
		"ms_char_wins_0010": {4, StateLocked},
		"ms_rank_gold":      {3, StateClaimable},
		"ms_rank_platinum":  {3, StateLocked},
	}
	for id, w := range want {
		n := node(t, list, id)
		if n.Progress != w[0].(int) || n.State != w[1].(string) {
			t.Errorf("%s: progress %d state %s, want %v", id, n.Progress, n.State, w)
		}
	}
	w14 := node(t, list, "ms_weapon_14")
	if w14.Description == "" || w14.Rewards.Gem != 100 {
		t.Errorf("weapon node %+v", w14)
	}
	// char 5,10,15,20 + upgrade 3,6,10,12,14 + set 3,6 + matches 10 + wins 5 + rank silver, gold
	if list.ClaimableCount != 15 {
		t.Errorf("claimable %d", list.ClaimableCount)
	}
	sum, err := svc.Summary(context.Background(), "p1")
	if err != nil || sum.ClaimableCount != 15 {
		t.Errorf("summary %+v %v", sum, err)
	}
}

func TestClaimGrantsOnce(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 100, 5)
	exec(t, db, `INSERT INTO player_characters (player_id, character_id, level) VALUES ('p1', 'spark', 10)`)
	exec(t, db, `INSERT INTO player_stones (player_id, stone_level, quantity) VALUES ('p1', 4, 1)`)
	ctx := context.Background()

	res, err := svc.Claim(ctx, "p1", "ms_char_lv_10") // stone 4×3 + coin 2000
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Claimed) != 1 || res.Rewards.Coin != 2000 || res.Rewards.Gem != 0 || len(res.Rewards.Items) != 1 ||
		res.Coin != 2100 || res.Gem != 5 || res.ClaimableCount != 1 {
		t.Fatalf("result %+v", res)
	}
	if len(res.Stones) != 1 || res.Stones[0].StoneLevel != 4 || res.Stones[0].Quantity != 4 || stones(t, db, "p1")[4] != 4 {
		t.Fatalf("stones %+v", res.Stones)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM economy_transactions WHERE player_id = 'p1' AND source = 'milestone_reward' AND ref_id = 'ms_char_lv_10' AND amount = 2000 AND balance_after = 2100`); n != 1 {
		t.Fatalf("economy log %d", n)
	}

	_, err = svc.Claim(ctx, "p1", "ms_char_lv_10")
	wantCode(t, err, "MILESTONE_ALREADY_CLAIMED")
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 2100 || stones(t, db, "p1")[4] != 4 {
		t.Fatalf("second claim granted: coin %d", coin)
	}
	list, _ := svc.List(ctx, "p1")
	if node(t, list, "ms_char_lv_10").State != StateClaimed || list.ClaimableCount != 1 {
		t.Fatalf("after claim %+v", node(t, list, "ms_char_lv_10"))
	}
}

func TestClaimRefusalsChangeNothing(t *testing.T) {
	svc, db := newService(t, 1) // a pool query while the claim tx holds the only connection hangs
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterMatches, 9)
	ctx := context.Background()

	_, err := svc.Claim(ctx, "p1", "ms_matches_0010")
	wantCode(t, err, "MILESTONE_NOT_REACHED")
	_, err = svc.Claim(ctx, "p1", "ms_nope")
	wantCode(t, err, "NOT_FOUND")
	_, err = svc.Claim(ctx, "p1", "")
	wantCode(t, err, "BAD_REQUEST")
	_, err = svc.Claim(ctx, "ghost", "ms_matches_0010")
	wantCode(t, err, "NOT_FOUND")

	// A reward kind the server cannot grant yet (a future title) is not served nor claimable
	exec(t, db, `INSERT INTO missions (mission_id, type, track, target, required_value, reward_items)
		VALUES ('ms_title_test', 'milestone', 'combat', 'matches', 1, '[{"kind":"title","id":"veteran"}]')`)
	_, err = svc.Claim(ctx, "p1", "ms_title_test")
	wantCode(t, err, "NOT_FOUND")
	res, err := svc.ClaimAll(ctx, "p1")
	if err != nil || len(res.Claimed) != 0 || res.ClaimableCount != 0 {
		t.Fatalf("claim-all %+v %v", res, err)
	}
	list, _ := svc.List(ctx, "p1")
	for _, tr := range list.Tracks {
		for _, n := range tr.Nodes {
			if n.ID == "ms_title_test" {
				t.Fatal("title node listed")
			}
		}
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 0 || gem != 0 ||
		scalar(t, db, `SELECT COUNT(*) FROM mission_progress`) != 0 || scalar(t, db, `SELECT COUNT(*) FROM economy_transactions`) != 0 {
		t.Fatal("a refusal changed something")
	}

	// The old POST /missions/claim does not see milestones
	exec(t, db, `INSERT INTO mission_progress (player_id, mission_id, current_value) VALUES ('p1', 'ms_wins_0005', 99)`)
	if err := mission.NewService(mission.NewRepository(db), economy.NewRepository(), db).ClaimReward(ctx, "p1", "ms_wins_0005"); err == nil {
		t.Fatal("mission claim granted a milestone")
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 0 {
		t.Fatal("mission claim paid")
	}
}

func TestConcurrentDoubleClaimGrantsOnce(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterWins, 25)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, errs[i] = svc.Claim(ctx, "p1", "ms_wins_0025")
			} else {
				_, errs[i] = svc.ClaimAll(ctx, "p1")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !strings.Contains(err.Error(), "already claimed") {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	// wins 5 (coin 1500) + wins 25 (coin 5000, gem 20), each exactly once
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 6500 || gem != 20 {
		t.Fatalf("coin %d gem %d", coin, gem)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM economy_transactions WHERE source = 'milestone_reward'`); n != 3 {
		t.Fatalf("economy logs %d", n)
	}
}

func TestClaimAll(t *testing.T) {
	svc, db := newService(t, 1)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterRankPeak, 4)  // silver, gold, platinum
	setCounter(t, db, "p1", CounterMatches, 100) // 10, 50, 100
	ctx := context.Background()
	if _, err := svc.Claim(ctx, "p1", "ms_rank_silver"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.ClaimAll(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(res.Claimed)
	if strings.Join(res.Claimed, ",") != "ms_matches_0010,ms_matches_0050,ms_matches_0100,ms_rank_gold,ms_rank_platinum" {
		t.Fatalf("claimed %v", res.Claimed)
	}
	// coin 1000 + 3000; gem 10 + 20 + 30 + 50; stone 7: 1 + 1 + 2
	if res.Rewards.Coin != 4000 || res.Rewards.Gem != 110 || len(res.Rewards.Items) != 1 || res.Rewards.Items[0].Qty != 4 ||
		res.Coin != 6000 || res.Gem != 110 || res.ClaimableCount != 0 || stones(t, db, "p1")[7] != 4 {
		t.Fatalf("result %+v", res)
	}
	again, err := svc.ClaimAll(ctx, "p1")
	if err != nil || len(again.Claimed) != 0 || again.Coin != 6000 {
		t.Fatalf("second claim-all %+v %v", again, err)
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
	svc, db := newService(t, 0)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterMatches, 10)

	code, env := call(t, h.Summary, "p1", "")
	if data, _ := env["data"].(map[string]any); code != 200 || data["claimableCount"] != 1.0 {
		t.Fatalf("summary %d %v", code, env)
	}
	code, env = call(t, h.List, "p1", "")
	data, _ := env["data"].(map[string]any)
	if tracks, _ := data["tracks"].([]any); code != 200 || len(tracks) != 4 {
		t.Fatalf("list %d %v", code, env)
	}
	code, env = call(t, h.Claim, "p1", `{"id":"ms_matches_0010"}`)
	if data, _ := env["data"].(map[string]any); code != 200 || data["coin"] != 1000.0 {
		t.Fatalf("claim %d %v", code, env)
	}
	cases := []struct {
		fn     http.HandlerFunc
		player string
		body   string
		status int
		code   string
	}{
		{h.Claim, "p1", `{"id":"ms_matches_0010"}`, 409, "MILESTONE_ALREADY_CLAIMED"},
		{h.Claim, "p1", `{"id":"ms_matches_0050"}`, 400, "MILESTONE_NOT_REACHED"},
		{h.Claim, "p1", `{"id":"nope"}`, 404, "NOT_FOUND"},
		{h.Claim, "p1", `not json`, 400, "BAD_REQUEST"},
		{h.Claim, "", `{"id":"ms_matches_0010"}`, 401, "AUTH_UNAUTHORIZED"},
		{h.ClaimAll, "", ``, 401, "AUTH_UNAUTHORIZED"},
	}
	for _, c := range cases {
		code, env := call(t, c.fn, c.player, c.body)
		e, _ := env["error"].(map[string]any)
		if code != c.status || e["code"] != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.body, code, env, c.status, c.code)
		}
	}
	code, env = call(t, h.ClaimAll, "p1", "")
	if data, _ := env["data"].(map[string]any); code != 200 || len(data["claimed"].([]any)) != 0 {
		t.Fatalf("claim-all %d %v", code, env)
	}
}

// The 026 backfill counts existing data once; a rerun changes nothing.
func TestMigrationBackfill(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t, "025_careful_upgrade.up.sql")...)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	dbtest.CreatePlayer(t, db, "p2", 0, 0)
	ctx := context.Background()

	// p1: a weapon at +4 that once reached +7, an armor +9; p2 nothing upgraded
	var weapon string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO player_equipment (player_id, item_id, slot, category, upgrade_level)
		VALUES ('p1', 'gun', 'weapon', 'normal', 4) RETURNING equipment_id`).Scan(&weapon); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `INSERT INTO equipment_upgrade_log (equipment_id, from_level, to_level, success) VALUES ($1, 6, 7, TRUE), ($1, 7, 4, FALSE)`, weapon)
	exec(t, db, `INSERT INTO player_equipment (player_id, item_id, slot, category, upgrade_level) VALUES ('p1', 'arm', 'armor', 'normal', 9)`)
	exec(t, db, `INSERT INTO rank_seasons (season_id, name, starts_at, ends_at, status) VALUES ('s1', 'S1', now(), now(), 'ended'), ('s2', 'S2', now(), now(), 'active')`)
	exec(t, db, `INSERT INTO player_ranks (player_id, season_id, rating, tier, highest_tier) VALUES ('p1', 's1', 1550, 'platinum', 'platinum'), ('p1', 's2', 900, 'bronze', 'silver')`)

	// Matches: (id, mode, result, character, human shots, bot name)
	type mt struct {
		id, mode, result, char string
		shots                  int
		bot                    string
	}
	matches := []mt{
		{"m1", "pvp_1v1", "win", "spark", 3, "Máy"},         // quick play vs AI: counts
		{"m2", "pvp_1v1", "win", "spark", 5, "Máy"},         // counts
		{"m3", "pvp_1v1", "loss", "tanko", 4, "Máy"},        // counts, loss
		{"m4", "pvp_1v1", "win", "spark", 2, "Máy"},         // too few shots
		{"m5", "pvp_1v1", "win", "rookie", 6, "Target Bot"}, // idle practice
		{"m6", "pvp_1v1", "win", "rookie", 6, ""},           // custom room between humans
		{"m7", "ranked_2v2", "win", "tanko", 3, "Bot 1234"}, // matchmade with a bot: counts
	}
	for _, m := range matches {
		players := map[string]any{"p1": map[string]any{"isBot": false, "characterId": m.char, "displayName": "P1"}}
		if m.bot != "" {
			players["bot_"+m.id] = map[string]any{"isBot": true, "characterId": "rookie", "displayName": m.bot}
		} else {
			players["p2"] = map[string]any{"isBot": false, "characterId": "rookie", "displayName": "P2"}
		}
		data, _ := json.Marshal(map[string]any{"mode": m.mode, "players": players})
		exec(t, db, `INSERT INTO match_histories (match_id, player_id, mode, map_id, result) VALUES ($1, 'p1', $2, 'map', $3)`, m.id, m.mode, m.result)
		exec(t, db, `INSERT INTO match_event_logs (match_id, seq, event_type, data) VALUES ($1, 1, 'MatchStarted', $2)`, m.id, string(data))
		for i := 0; i < m.shots; i++ {
			exec(t, db, `INSERT INTO match_event_logs (match_id, seq, event_type, player_id, data) VALUES ($1, $2, 'ShotResolved', 'p1', '[]')`, m.id, i+2)
		}
	}
	exec(t, db, `INSERT INTO match_histories (match_id, player_id, mode, map_id, result) VALUES ('m8', 'p1', 'ranked_2v2', 'map', 'win')`) // no event logs

	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "026_milestones.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	counters := func() map[string]int {
		rows, err := db.Pool.Query(ctx, `SELECT player_id || '/' || counter, value FROM player_stat_counters`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]int{}
		for rows.Next() {
			var k string
			var v int
			if err := rows.Scan(&k, &v); err != nil {
				t.Fatal(err)
			}
			out[k] = v
		}
		return out
	}
	for run := 0; run < 2; run++ {
		if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		got := counters()
		want := map[string]int{
			"p1/upgrade_peak": 9, "p1/weapon_upgrade_peak": 7, "p1/rank_peak": 4, "p1/rating_peak": 1550,
			"p1/matches": 4, "p1/wins": 3, "p1/char_wins:spark": 2, "p1/char_wins:tanko": 1,
		}
		if len(got) != len(want) {
			t.Fatalf("run %d: counters %v", run, got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("run %d: %s = %d, want %d (all %v)", run, k, got[k], v, got)
			}
		}
		if n := scalar(t, db, `SELECT COUNT(*) FROM missions WHERE type = 'milestone'`); n != len(Defaults) {
			t.Fatalf("run %d: %d milestones", run, n)
		}
	}

	svc := NewService(NewRepository(db), economy.NewRepository(), db)
	list, err := svc.List(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	// upgrade 3, 6 + rank silver, gold, platinum (no character, 4 matches, 3 wins)
	if list.ClaimableCount != 5 {
		t.Fatalf("claimable after backfill %d", list.ClaimableCount)
	}
}
