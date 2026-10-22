package room

import (
	"context"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database/dbtest"
)

// A match of the casual queue: MatchFound says casual_2v2, the rest is laid out like ranked.
func TestCasualRosterMode(t *testing.T) {
	result := matchmaker.MatchResult{
		Queue: matchmaker.CasualQueue,
		Teams: [2]matchmaker.MatchTeam{
			{Entries: []matchmaker.QueueEntry{rankedEntry("lp", map[string]int{"a": 1200, "b": 1000}, "a", "b")}},
			{Entries: []matchmaker.QueueEntry{rankedEntry("lc", map[string]int{"c": 1150}, "c"), rankedEntry("ld", map[string]int{"d": 1010}, "d")}},
		},
		MapID: "m",
	}
	ro := buildRankedRoster(result, nil, botIDs(), nil)
	if ro.Found.Mode != matchmaker.CasualQueue || len(ro.Players) != 4 || ro.TeamRatings[1] != 1100 {
		t.Fatalf("casual roster %+v", ro.Found)
	}
	result.Queue = ""
	if ro := buildRankedRoster(result, nil, botIDs(), nil); ro.Found.Mode != matchmaker.RankedQueue {
		t.Fatalf("no queue: mode %q", ro.Found.Mode)
	}
}

// CreateBattleFromMatch of a casual_2v2 plan (a solo + a bot against two bots): the room,
// MatchFound and the match all play casual_2v2, and the match counts for milestones and
// daily missions like a ranked one.
func TestCasualBattleFromMatchDB(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	gamedata.UseYAML(t, configsDir())

	hub := NewHub(nil, db, "test")
	found := make(chan MatchFound, 1)
	var battle *Room
	hub.SetLobbyNotifier(func(_ string, event string, data interface{}, r *Room) {
		if event == "MatchFound" {
			battle = r
			found <- data.(MatchFound)
		}
	})
	result := matchmaker.MatchResult{
		Queue: matchmaker.CasualQueue,
		Teams: [2]matchmaker.MatchTeam{
			{Entries: []matchmaker.QueueEntry{rankedEntry("l1", map[string]int{"p1": 1000}, "p1")}, Bots: 1},
			{Bots: 2},
		},
		MapID: "grassland_valley", HasBot: true,
	}
	mm := matchmaker.DefaultMatchmakingConfig()
	mm.MatchStartDelay = 60 // the match never starts during the test
	if err := hub.CreateBattleFromMatch(context.Background(), result, matchmaker.DefaultBotDifficultyConfig(), matchmaker.DefaultEloConfig(), mm); err != nil {
		t.Fatal(err)
	}
	var f MatchFound
	select {
	case f = <-found:
	case <-time.After(2 * time.Second):
		t.Fatal("no MatchFound")
	}
	t.Cleanup(battle.match.Stop)
	if f.Mode != matchmaker.CasualQueue || battle.State.Mode != matchmaker.CasualQueue || battle.match.State.Mode != matchmaker.CasualQueue {
		t.Fatalf("modes: MatchFound %q room %q match %q", f.Mode, battle.State.Mode, battle.match.State.Mode)
	}
	if !battle.match.CountsForMilestones || len(f.Teams) != 2 || len(f.Teams[0].Players)+len(f.Teams[1].Players) != 4 {
		t.Fatalf("match %+v counts %v", f, battle.match.CountsForMilestones)
	}
}
