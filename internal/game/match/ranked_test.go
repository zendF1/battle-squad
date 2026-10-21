package match

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// A player who left is never a winner (nor in a draw) even when their team wins;
// the teammate who stayed wins.
func TestLeaverNeverWins(t *testing.T) {
	setFlowGameData()
	players := []*BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 200, Y: 300}, IsAlive: true},
		{PlayerID: "p3", TeamID: 1, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 300, Y: 300}, IsAlive: true},
		{PlayerID: "p2", TeamID: 2, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 1400, Y: 300}, IsAlive: true},
	}
	clients := map[string]*ws.Client{}
	for _, p := range players {
		clients[p.PlayerID] = &ws.Client{PlayerID: p.PlayerID, Send: make(chan ws.Message, 256)}
	}
	m := NewMatch("m1", "r1", RankedMode, "none", players, map[string]*ws.Client{"p1": clients["p1"], "p2": clients["p2"], "p3": clients["p3"]}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	var got map[string]*PlayerStats
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		got = stats
		return nil, nil
	}

	m.processLeave(context.Background(), clients["p3"])
	if got != nil {
		t.Fatal("match ended while both teams had a player alive")
	}
	m.State.Players["p2"].IsAlive = false
	m.checkWinCondition(context.Background())
	if got == nil {
		t.Fatal("match did not end")
	}
	if !got["p1"].IsWinner || got["p3"].IsWinner || got["p3"].IsDraw || !got["p3"].Left || got["p2"].IsWinner || got["p1"].Left {
		t.Fatalf("stayer %+v leaver %+v loser %+v", got["p1"], got["p3"], got["p2"])
	}
}

// A matchmade match waits StartDelay before MatchStarted; a Leave sent meanwhile
// is handled after the start.
func TestStartDelay(t *testing.T) {
	setFlowGameData()
	players := []*BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 200, Y: 300}, IsAlive: true},
		{PlayerID: "p2", TeamID: 2, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 1400, Y: 300}, IsAlive: true},
	}
	c1 := &ws.Client{PlayerID: "p1", Send: make(chan ws.Message, 256)}
	c2 := &ws.Client{PlayerID: "p2", Send: make(chan ws.Message, 256)}
	m := NewMatch("m1", "r1", RankedMode, "none", players, map[string]*ws.Client{"p1": c1, "p2": c2}, nil, nil, nil, nil)
	m.StartDelay = 400 * time.Millisecond
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	start := time.Now()
	done := runMatch(t, m)
	m.ProcessEvent(context.Background(), c2, ws.Message{Event: "Leave"})

	var events []string
	deadline := time.After(3 * time.Second)
	for len(events) == 0 || events[len(events)-1] != "MatchEnded" {
		select {
		case msg := <-c1.Send:
			if msg.Event == "MatchStarted" && time.Since(start) < m.StartDelay {
				t.Fatalf("MatchStarted after %v", time.Since(start))
			}
			events = append(events, msg.Event)
		case <-deadline:
			t.Fatalf("events %v", events)
		}
	}
	waitRun(t, done)
	if events[0] != "MatchStarted" {
		t.Fatalf("events %v", events)
	}
}

// rankedRewards plays one settled match through ProcessMatchRewards.
func rankedRewards(t *testing.T, db *database.PostgresDB, matchID, mode string, ratings map[int]int, params EloParams, stats ...*PlayerStats) map[string]RewardResult {
	t.Helper()
	byID := map[string]*PlayerStats{}
	for _, s := range stats {
		byID[s.PlayerID] = s
	}
	res, err := ProcessMatchRewards(context.Background(), db, economy.NewRepository(), matchID, mode, "map", byID, nil, ratings, params)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRankedRewards(t *testing.T) {
	db := newMatchSchema(t)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2", "p3", "p4", "p5"} {
		dbtest.CreatePlayer(t, db, id, 0, 0)
	}
	rating := func(id string) int {
		t.Helper()
		var r int
		if err := db.Pool.QueryRow(ctx, `SELECT rating FROM player_ranks WHERE player_id = $1`, id).Scan(&r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	rows := func() int {
		var n int
		if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM player_ranks`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	elo := EloParams{KFactor: 32, DefaultRating: 1000, BotModifier: 1}

	// Unranked modes never touch rating: no rank fields, no player_ranks row
	for _, mode := range []string{"pvp_1v1", "pvp_2v2"} {
		res := rankedRewards(t, db, "u_"+mode, mode, map[int]int{1: 1000, 2: 1000}, elo,
			&PlayerStats{PlayerID: "p1", TeamID: 1, IsWinner: true}, &PlayerStats{PlayerID: "p2", TeamID: 2})
		raw, _ := json.Marshal(res["p1"])
		if res["p1"].RatingChange != 0 || res["p1"].RankChange != nil || strings.Contains(string(raw), "Rating\"") || strings.Contains(string(raw), "Tier") {
			t.Fatalf("%s rewards %s", mode, raw)
		}
	}
	if rows() != 0 {
		t.Fatal("an unranked match wrote player_ranks")
	}

	// Ranked: p1 + p2 win, but p2 left: p2 loses the full rating and gets no coin/EXP
	coinBefore, _ := dbtest.Balance(t, db, "p2")
	res := rankedRewards(t, db, "r1", RankedMode, map[int]int{1: 1000, 2: 1000}, elo,
		&PlayerStats{PlayerID: "p1", TeamID: 1, IsWinner: true, Damage: 3000},
		&PlayerStats{PlayerID: "p2", TeamID: 1, Left: true, Damage: 3000, Kills: 2},
		&PlayerStats{PlayerID: "p3", TeamID: 2},
		&PlayerStats{PlayerID: "p4", TeamID: 2},
	)
	if r := res["p1"]; r.RatingChange != 16 || *r.RankChange != (RankChange{1000, "silver", 3, 1016, "silver", 3}) || r.CoinGained != 50 || r.ExpGained != 90 {
		t.Fatalf("winner %+v %+v", r, r.RankChange)
	}
	if r := res["p2"]; r.RatingChange != -16 || r.RankChange.NewRating != 984 || r.RankChange.NewTier != "bronze" || r.CoinGained != 0 || r.ExpGained != 0 {
		t.Fatalf("leaver %+v %+v", r, r.RankChange)
	}
	if r := res["p3"]; r.RatingChange != -16 || r.CoinGained != 30 {
		t.Fatalf("loser %+v", r)
	}
	if coin, _ := dbtest.Balance(t, db, "p2"); coin != coinBefore {
		t.Fatalf("leaver got %d coin", coin-coinBefore)
	}
	var result string
	if err := db.Pool.QueryRow(ctx, `SELECT result FROM match_histories WHERE match_id = 'r1' AND player_id = 'p2'`).Scan(&result); err != nil || result != "loss" {
		t.Fatalf("leaver history %q %v", result, err)
	}
	raw, _ := json.Marshal(res["p1"])
	for _, k := range []string{`"ratingChange":16`, `"oldRating":1000`, `"oldTier":"silver"`, `"oldDivision":3`, `"newRating":1016`, `"newTier":"silver"`, `"newDivision":3`} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("MatchEnded rewards %s lacks %s", raw, k)
		}
	}
	// The season was created on the first ranked write
	var name string
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM rank_seasons WHERE status = 'active'`).Scan(&name); err != nil || name != "Mùa 1" {
		t.Fatalf("season %q %v", name, err)
	}

	// A player without a row starts at the elo config's default rating
	res = rankedRewards(t, db, "r2", RankedMode, map[int]int{1: 1200, 2: 1200}, EloParams{KFactor: 32, DefaultRating: 1200, BotModifier: 1},
		&PlayerStats{PlayerID: "p5", TeamID: 1, IsDraw: true}, &PlayerStats{PlayerID: "p4", TeamID: 2, IsDraw: true})
	if r := res["p5"].RankChange; r.OldRating != 1200 || r.OldTier != "gold" || r.OldDivision != 3 || r.NewRating != 1200 {
		t.Fatalf("default rating %+v", r)
	}

	// The floor cuts a loss, and ratingChange reports what was applied
	res = rankedRewards(t, db, "r3", RankedMode, map[int]int{1: 1000, 2: 1000}, EloParams{KFactor: 32, RatingFloor: 975, DefaultRating: 1000, BotModifier: 1},
		&PlayerStats{PlayerID: "p3", TeamID: 1}, &PlayerStats{PlayerID: "p1", TeamID: 2, IsWinner: true})
	if res["p3"].RatingChange != -9 || rating("p3") != 975 {
		t.Fatalf("floor: change %d rating %d", res["p3"].RatingChange, rating("p3"))
	}

	// A match with bots: the change is scaled by the bot modifier (p1 1032 after r3)
	res = rankedRewards(t, db, "r4", RankedMode, map[int]int{1: 1016, 2: 1016}, EloParams{KFactor: 32, DefaultRating: 1000, BotModifier: 0.5, HasBot: true},
		&PlayerStats{PlayerID: "p1", TeamID: 1, IsWinner: true}, &PlayerStats{PlayerID: "bot_x", TeamID: 1, IsWinner: true})
	if res["p1"].RatingChange != 8 || rating("p1") != 1040 {
		t.Fatalf("bot match: change %d rating %d", res["p1"].RatingChange, rating("p1"))
	}
	if _, ok := res["bot_x"]; ok {
		t.Fatal("a bot got rewards")
	}
}

func TestMatchMVP(t *testing.T) {
	ps := func(list ...*BattlePlayerState) map[string]*BattlePlayerState {
		out := map[string]*BattlePlayerState{}
		for _, p := range list {
			out[p.PlayerID] = p
		}
		return out
	}
	cases := []struct {
		name    string
		players map[string]*BattlePlayerState
		winner  int
		want    string
	}{
		{"most damage", ps(&BattlePlayerState{PlayerID: "a", DamageDealt: 100, KillCount: 3}, &BattlePlayerState{PlayerID: "b", DamageDealt: 200}), 1, "b"},
		{"kills break ties", ps(&BattlePlayerState{PlayerID: "a", DamageDealt: 200, KillCount: 1}, &BattlePlayerState{PlayerID: "b", DamageDealt: 200, KillCount: 2}), 0, "b"},
		{"winning team breaks ties", ps(&BattlePlayerState{PlayerID: "a", TeamID: 1, DamageDealt: 200}, &BattlePlayerState{PlayerID: "b", TeamID: 2, DamageDealt: 200}), 2, "b"},
		{"player id last", ps(&BattlePlayerState{PlayerID: "b", TeamID: 1}, &BattlePlayerState{PlayerID: "a", TeamID: 1}), 1, "a"},
		{"bots count", ps(&BattlePlayerState{PlayerID: "bot_1", IsBot: true, DamageDealt: 9}, &BattlePlayerState{PlayerID: "p", DamageDealt: 1}), 1, "bot_1"},
		{"none", ps(), 0, ""},
	}
	for _, c := range cases {
		for i := 0; i < 5; i++ { // map order varies
			if got := matchMVP(c.players, c.winner); got != c.want {
				t.Fatalf("%s: %s, want %s", c.name, got, c.want)
			}
		}
	}
}

// MatchEnded carries every player's stats (bots too) and the MVP.
func TestMatchEndedStatsAndMVP(t *testing.T) {
	m, clients := newFlowMatch(t)
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.State.Players["p1"].DamageDealt, m.State.Players["p1"].ShotsFired, m.State.Players["p1"].ShotsHit = 700, 3, 2
	m.State.Players["p2"].DamageDealt, m.State.Players["p2"].KillCount = 300, 1
	for len(clients["p1"].Send) > 0 {
		<-clients["p1"].Send
	}
	m.processLeave(context.Background(), clients["p2"])

	var ended struct {
		WinningTeam int                 `json:"winningTeam"`
		Stats       map[string]EndStats `json:"stats"`
		MVP         *string             `json:"mvpPlayerId"`
	}
	for msg := range clients["p1"].Send {
		if msg.Event == "MatchEnded" {
			if err := json.Unmarshal(msg.Data, &ended); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if ended.WinningTeam != 1 || ended.MVP == nil || *ended.MVP != "p1" ||
		ended.Stats["p1"] != (EndStats{DamageDealt: 700, ShotsHit: 2, ShotsFired: 3}) || ended.Stats["p2"] != (EndStats{DamageDealt: 300, Kills: 1}) {
		t.Fatalf("%+v mvp %v", ended, ended.MVP)
	}
}
