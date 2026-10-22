package room

import (
	"context"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database/dbtest"
)

// CreateBattleFromMatch of a boss plan (a solo + 3 bot teammates): MatchFound and the match
// play boss:steel_robot on its arena, team 1 the human and 3 varied bots, team 2 the boss and
// its first minions (cards with role / bossId / minionId); the turn order ends with the boss.
func TestBossBattleFromMatchDB(t *testing.T) {
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
		Queue:  "boss:steel_robot",
		Teams:  [2]matchmaker.MatchTeam{{Entries: []matchmaker.QueueEntry{rankedEntry("l1", map[string]int{"p1": 1000}, "p1")}, Bots: 3}, {}},
		MapID:  "arena_steel_robot",
		HasBot: true,
	}
	mm := matchmaker.DefaultMatchmakingConfig()
	mm.MatchStartDelay = 60
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
	if f.Mode != "boss:steel_robot" || f.MapID != "arena_steel_robot" || f.MapName != "Xưởng thép" || battle.match.State.Mode != "boss:steel_robot" {
		t.Fatalf("MatchFound %+v", f)
	}
	if len(f.Teams) != 2 || len(f.Teams[0].Players) != 4 || f.Teams[0].Players[0].PlayerID != "p1" || !f.Teams[0].Players[3].IsBot {
		t.Fatalf("party %+v", f.Teams)
	}
	side := f.Teams[1].Players
	if len(side) != 3 || side[0] != (MatchFoundPlayer{PlayerID: "boss_steel_robot", DisplayName: "Rô-bốt Thép", IsBot: true, Level: 10,
		Role: "boss", BossID: "steel_robot"}) || side[1].Role != "minion" || side[1].MinionID != "drone" || side[1].CharacterID != "spark" {
		t.Fatalf("boss side %+v", side)
	}
	st := battle.match.State
	if st.Boss == nil || st.Boss.BossID != "steel_robot" || st.TurnOrder[len(st.TurnOrder)-1] != "boss_steel_robot" || len(st.Players) != 7 ||
		!battle.match.CountsForMilestones {
		t.Fatalf("match %+v order %v", st.Boss, st.TurnOrder)
	}
	if p := st.Players["p1"]; p.TeamID != 1 || p.Role != "" {
		t.Fatalf("p1 %+v", p)
	}
	for _, p := range st.Players {
		if p.Role == match.RoleMinion && (p.TeamID != 2 || !p.IsBot) {
			t.Fatalf("minion %+v", p)
		}
	}
}

// Boss arenas stay out of the QuickPlay map rotation.
func TestQuickPlaySkipsBossArenas(t *testing.T) {
	gamedata.UseYAML(t, configsDir())
	for i := 0; i < 40; i++ {
		id := quickPlayMapID(QuickPlayPayload{Bot: "ai"}, func(n int) int { return i % n })
		if gamedata.Current().IsBossArena(id) {
			t.Fatalf("quick play on %s", id)
		}
	}
}
