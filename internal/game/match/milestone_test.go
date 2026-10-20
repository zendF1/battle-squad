package match

import (
	"context"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database/dbtest"
)

func TestMilestoneCounts(t *testing.T) {
	m := &Match{CountsForMilestones: true}
	cases := []struct {
		name string
		p    BattlePlayerState
		want bool
	}{
		{"played", BattlePlayerState{ShotTurns: 3}, true},
		{"too short", BattlePlayerState{ShotTurns: 2}, false},
		{"left", BattlePlayerState{ShotTurns: 9, Left: true}, false},
		{"bot", BattlePlayerState{ShotTurns: 9, IsBot: true}, false},
	}
	for _, c := range cases {
		if got := m.milestoneCounts(&c.p); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	m.CountsForMilestones = false // custom room / practice target
	if m.milestoneCounts(&BattlePlayerState{ShotTurns: 9}) {
		t.Error("a non-counting match counted")
	}
}

// Shots count once per resolved action; a leaver is marked and never counts, the player who
// stays counts once they resolved 3 actions.
func TestMatchEndPassesMilestoneCounts(t *testing.T) {
	m, clients := newFlowMatch(t)
	m.CountsForMilestones = true
	var got map[string]*PlayerStats
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		got = stats
		return nil, nil
	}
	cur := m.State.CurrentPlayerID
	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"}) // refused: resolving
	if m.State.Players[cur].ShotTurns != 1 {
		t.Fatalf("shot turns %d", m.State.Players[cur].ShotTurns)
	}
	m.State.Players[cur].ShotTurns = 3
	other := m.other(cur)
	m.State.Players[other].ShotTurns = 5
	m.processLeave(context.Background(), clients[other])

	if got == nil || !m.State.Players[other].Left {
		t.Fatal("match did not end on the leave")
	}
	if !got[cur].Counts || !got[cur].IsWinner || got[other].Counts {
		t.Fatalf("counts: stayer %+v leaver %+v", got[cur], got[other])
	}
}

func TestRewardsRecordMilestoneCounters(t *testing.T) {
	db := newMatchSchema(t)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2", "p3"} {
		dbtest.CreatePlayer(t, db, id, 0, 0)
	}
	value := func(playerID, name string) int {
		t.Helper()
		var v int
		err := db.Pool.QueryRow(ctx, `SELECT COALESCE((SELECT value FROM player_stat_counters WHERE player_id = $1 AND counter = $2), 0)`, playerID, name).Scan(&v)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	play := func(matchID, mode string, stats ...*PlayerStats) {
		t.Helper()
		byID := map[string]*PlayerStats{}
		for _, s := range stats {
			byID[s.PlayerID] = s
		}
		if _, err := ProcessMatchRewards(ctx, db, economy.NewRepository(), matchID, mode, "map", byID, nil,
			map[int]int{1: 1000, 2: 1000}, EloParams{KFactor: 32, RatingFloor: 0, BotModifier: 1}); err != nil {
			t.Fatal(err)
		}
	}
	// A counted ranked win for p1 (spark), a counted loss for p2, p3 left
	play("m1", "ranked_2v2",
		&PlayerStats{PlayerID: "p1", CharacterID: "spark", TeamID: 1, IsWinner: true, Counts: true},
		&PlayerStats{PlayerID: "p2", CharacterID: "tanko", TeamID: 2, Counts: true},
		&PlayerStats{PlayerID: "p3", CharacterID: "flora", TeamID: 2},
		&PlayerStats{PlayerID: "bot_x", CharacterID: "rookie", TeamID: 1, IsWinner: true, Counts: true},
	)
	// A custom-room 1v1 never counts (the room leaves CountsForMilestones false)
	play("m2", "pvp_1v1", &PlayerStats{PlayerID: "p1", CharacterID: "spark", TeamID: 1, IsWinner: true})

	want := map[[2]string]int{
		{"p1", "matches"}: 1, {"p1", "wins"}: 1, {"p1", "char_wins:spark"}: 1,
		{"p2", "matches"}: 1, {"p2", "wins"}: 0, {"p2", "char_wins:tanko"}: 0,
		{"p3", "matches"}: 0,
	}
	for k, v := range want {
		if got := value(k[0], k[1]); got != v {
			t.Errorf("%s %s = %d, want %d", k[0], k[1], got, v)
		}
	}
	// Rank peaks follow every rating write: p1 rose above 1000, p2 fell to bronze but the
	// season's highest_tier (silver at the start) is the peak
	var rating int
	if err := db.Pool.QueryRow(ctx, `SELECT rating FROM player_ranks WHERE player_id = 'p1'`).Scan(&rating); err != nil {
		t.Fatal(err)
	}
	if value("p1", "rank_peak") != 2 || value("p1", "rating_peak") != rating || value("p2", "rank_peak") != 2 {
		t.Errorf("rank peaks p1 %d/%d (rating %d) p2 %d", value("p1", "rank_peak"), value("p1", "rating_peak"), rating, value("p2", "rank_peak"))
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE player_ranks SET rating = 1180 WHERE player_id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	play("m3", "pvp_1v1", &PlayerStats{PlayerID: "p1", CharacterID: "spark", TeamID: 1, IsWinner: true})
	if value("p1", "rank_peak") != 3 {
		t.Errorf("gold peak %d", value("p1", "rank_peak"))
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE player_ranks SET rating = 900 WHERE player_id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	play("m4", "pvp_1v1", &PlayerStats{PlayerID: "p1", CharacterID: "spark", TeamID: 1})
	if value("p1", "rank_peak") != 3 || value("p1", "rating_peak") != 1205 {
		t.Errorf("peaks dropped: %d / %d", value("p1", "rank_peak"), value("p1", "rating_peak"))
	}
}
