package matchmaker

import (
	"reflect"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// group is a boss queue entry of the players ids (a party of len(ids)), queued waited s ago.
func group(waited int64, ids ...string) QueueEntry {
	ratings := map[string]int{}
	for _, id := range ids {
		ratings[id] = 1000
	}
	return QueueEntry{EntryID: "e_" + ids[0], LobbyID: "l_" + ids[0], PlayerIDs: ids, Rating: 1000, PlayerRatings: ratings,
		TeamSize: len(ids), QueuedAt: testNow - waited, Queue: "boss:steel_robot"}
}

// Boss queues: first come, up to 4 humans on team 1, no rating; bot teammates fill the rest
// after botFillAfter; team 2 is left to the room (the boss's side).
func TestPlanBossMatches(t *testing.T) {
	cfg := DefaultMatchmakingConfig() // botFillAfter 20
	cases := []struct {
		name    string
		entries []QueueEntry
		want    []string
	}{
		{"party of 4 starts at once", []QueueEntry{group(0, "a", "b", "c", "d")}, []string{"a+b+c+d vs "}},
		{"solo waits", []QueueEntry{group(5, "a")}, nil},
		{"solo after botFillAfter: 3 bots", []QueueEntry{group(20, "a")}, []string{"a+bot+bot+bot vs "}},
		{"party of 3 + solo: full", []QueueEntry{group(1, "a", "b", "c"), group(0, "d")}, []string{"a+b+c+d vs "}},
		{"4 solos, first come", []QueueEntry{group(4, "a"), group(3, "b"), group(2, "c"), group(1, "d"), group(0, "e")},
			[]string{"a+b+c+d vs "}},
		{"ratings do not matter", []QueueEntry{
			{EntryID: "e_x", PlayerIDs: []string{"x"}, Rating: 100, PlayerRatings: map[string]int{"x": 100}, QueuedAt: testNow},
			{EntryID: "e_y", PlayerIDs: []string{"y"}, Rating: 2500, PlayerRatings: map[string]int{"y": 2500}, QueuedAt: testNow},
			group(0, "z", "w")}, []string{"w+x+y+z vs "}},
		{"party of 3 skips a party of 2 that does not fit", []QueueEntry{group(30, "a", "b", "c"), group(10, "d", "e"), group(5, "f")},
			[]string{"a+b+c+f vs "}},
		{"2 + 2 after the wait", []QueueEntry{group(25, "a", "b"), group(2, "c", "d")}, []string{"a+b+c+d vs "}},
		{"two parties of 3 after the wait: one each with a bot", []QueueEntry{group(25, "a", "b", "c"), group(21, "d", "e", "f")},
			[]string{"a+b+c+bot vs ", "d+e+f+bot vs "}},
		{"too big an entry is ignored", []QueueEntry{group(30, "a", "b", "c", "d", "e")}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := planBossMatches(c.entries, testNow, cfg)
			var ls []string
			for _, r := range got {
				ls = append(ls, layout(r))
				if r.Teams[1].Humans() != 0 || r.Teams[1].Bots != 0 || r.HasBot != (r.Teams[0].Bots > 0) {
					t.Fatalf("result %+v", r)
				}
			}
			if !reflect.DeepEqual(ls, c.want) {
				t.Fatalf("got %v, want %v", ls, c.want)
			}
		})
	}
}

// Boss queues are known while their boss is enabled; party sizes: 4 for a boss queue (or its
// lobby mode's party size), 2 for 2v2.
func TestBossQueues(t *testing.T) {
	bosses := gamedata.DefaultBosses()
	bosses[2].Enabled = false
	gd := gamedata.Current().WithBosses(bosses).WithModes([]gamedata.GameModeConfig{
		{ModeID: "boss_lava_crab", Kind: "lobby", Queue: "boss:lava_crab", PartySize: 3, Enabled: true},
	})
	gamedata.UseForTest(t, gd)
	if !KnownQueue("boss:steel_robot") || !KnownQueue("boss:lava_crab") || KnownQueue("boss:ice_witch") || KnownQueue("boss:nope") ||
		!KnownQueue(RankedQueue) || KnownQueue("boss:") {
		t.Fatal("known queues")
	}
	if got := AllQueues(); !reflect.DeepEqual(got, []string{RankedQueue, CasualQueue, "boss:steel_robot", "boss:lava_crab"}) {
		t.Fatalf("queues %v", got)
	}
	if PartySize("boss:steel_robot") != 4 || PartySize("boss:lava_crab") != 3 || PartySize(RankedQueue) != 2 || MaxPartySize("boss:x") != 4 {
		t.Fatal("party sizes")
	}
	if !IsBossQueue("boss:ice_witch") || IsBossQueue(CasualQueue) {
		t.Fatal("IsBossQueue")
	}
}
