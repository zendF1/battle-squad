package matchmaker

import (
	"sort"
	"strings"
	"testing"
)

const testNow = int64(10_000)

// solo / party build queue entries that queued waited seconds before testNow.
func solo(id string, rating int, waited int64) QueueEntry {
	return QueueEntry{
		EntryID: "e_" + id, LobbyID: "l_" + id, PlayerIDs: []string{id}, Rating: rating,
		PlayerRatings: map[string]int{id: rating}, TeamSize: 1, QueuedAt: testNow - waited,
	}
}

func party(a string, ra int, b string, rb int, waited int64) QueueEntry {
	return QueueEntry{
		EntryID: "e_" + a + b, LobbyID: "l_" + a + b, PlayerIDs: []string{a, b}, Rating: max(ra, rb),
		PlayerRatings: map[string]int{a: ra, b: rb}, TeamSize: 2, QueuedAt: testNow - waited,
	}
}

// layout writes a match as "a+b vs c+bot" (players sorted within a team).
func layout(r MatchResult) string {
	var sides []string
	for _, t := range r.Teams {
		var names []string
		for _, e := range t.Entries {
			names = append(names, e.PlayerIDs...)
		}
		sort.Strings(names)
		for i := 0; i < t.Bots; i++ {
			names = append(names, "bot")
		}
		sides = append(sides, strings.Join(names, "+"))
	}
	return sides[0] + " vs " + sides[1]
}

func layouts(rs []MatchResult) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = layout(r)
	}
	return out
}

func TestPlanPartyVsParty(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	got := planMatches([]QueueEntry{party("a", 1000, "b", 1040, 3), party("c", 1020, "d", 1060, 1)}, testNow, cfg)
	if len(got) != 1 || layout(got[0]) != "a+b vs c+d" || got[0].HasBot {
		t.Fatalf("got %v", layouts(got))
	}
}

func TestPlanPartyVsTwoSolos(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	got := planMatches([]QueueEntry{solo("c", 1010, 2), party("a", 1000, "b", 1050, 5), solo("d", 990, 1)}, testNow, cfg)
	if len(got) != 1 || layout(got[0]) != "a+b vs c+d" || got[0].HasBot {
		t.Fatalf("got %v", layouts(got))
	}
}

// Four solos split into the pairing with the closest team averages: 1000+1090 vs
// 1030+1060 (2090 vs 2090), not the two best together.
func TestPlanFourSolosBalanced(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	got := planMatches([]QueueEntry{
		solo("a", 1000, 4), solo("b", 1030, 3), solo("c", 1060, 2), solo("d", 1090, 1),
	}, testNow, cfg)
	if len(got) != 1 {
		t.Fatalf("got %v", layouts(got))
	}
	if l := layout(got[0]); l != "a+d vs b+c" && l != "b+c vs a+d" {
		t.Fatalf("unbalanced split %s", l)
	}
	if got[0].HasBot || got[0].Teams[0].Bots+got[0].Teams[1].Bots != 0 {
		t.Fatal("bots in a full match")
	}
}

// Entries further apart than the allowed range (100 at first) never share a
// match, and nothing is bot-filled before botFillAfter.
func TestPlanRatingRangeAndWait(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	entries := []QueueEntry{solo("a", 1000, 5), solo("b", 1000, 5), solo("c", 1000, 5), solo("d", 1250, 5)}
	if got := planMatches(entries, testNow, cfg); len(got) != 0 {
		t.Fatalf("matched out of range / before bot fill: %v", layouts(got))
	}
	// The range widens with the wait (100 + 50 per 10 s, max 300): at 50 s d is in range
	for i := range entries {
		entries[i].QueuedAt = testNow - 15
	}
	if got := planMatches(entries, testNow, cfg); len(got) != 0 {
		t.Fatalf("15 s: %v", layouts(got))
	}
	cfg.BotFillAfter = 100
	for i := range entries {
		entries[i].QueuedAt = testNow - 50
	}
	if got := planMatches(entries, testNow, cfg); len(got) != 1 || len(got[0].Entries()) != 4 {
		t.Fatalf("50 s: %v", layouts(got))
	}
}

func TestPlanBotFill(t *testing.T) {
	cfg := DefaultMatchmakingConfig() // botFillAfter 20
	cases := []struct {
		name    string
		entries []QueueEntry
		want    []string
	}{
		{"one human", []QueueEntry{solo("a", 1000, 20)}, []string{"a+bot vs bot+bot"}},
		{"one human waiting less", []QueueEntry{solo("a", 1000, 19)}, nil},
		{"party alone", []QueueEntry{party("a", 1000, "b", 1100, 25)}, []string{"a+b vs bot+bot"}},
		// Two solos become opponents, each with a bot
		{"two solos", []QueueEntry{solo("a", 1000, 21), solo("b", 1050, 2)}, []string{"a+bot vs b+bot"}},
		// Three solos: two teammates against the third and a bot, the closest averages
		// (bot = 1000): b+c (2000) vs a+bot (1000 + 1000)
		{"three solos", []QueueEntry{solo("a", 1000, 30), solo("b", 950, 2), solo("c", 1050, 1)}, []string{"b+c vs a+bot"}},
		{"solo and party", []QueueEntry{solo("a", 1000, 22), party("b", 1000, "c", 1040, 1)}, []string{"a+bot vs b+c"}},
		{"party and solo", []QueueEntry{party("a", 1000, "b", 1040, 22), solo("c", 1000, 1)}, []string{"a+b vs c+bot"}},
		// A human out of range is not used
		{"out of range", []QueueEntry{solo("a", 1000, 20), solo("b", 1500, 1)}, []string{"a+bot vs bot+bot"}},
	}
	for _, c := range cases {
		got := layouts(planMatches(c.entries, testNow, cfg))
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The oldest entry is served first, and an entry is used once per tick.
func TestPlanOldestFirstAndOnce(t *testing.T) {
	cfg := DefaultMatchmakingConfig()
	got := planMatches([]QueueEntry{
		solo("new", 1000, 1), solo("old", 1000, 40), solo("mid", 1000, 21),
	}, testNow, cfg)
	if len(got) != 1 {
		t.Fatalf("got %v", layouts(got))
	}
	seen := map[string]int{}
	for _, r := range got {
		for _, e := range r.Entries() {
			seen[e.EntryID]++
		}
	}
	if seen["e_old"] != 1 || seen["e_mid"] != 1 || seen["e_new"] != 1 {
		t.Fatalf("entries used %v (%v)", seen, layouts(got))
	}
}

func TestMatchResultRatings(t *testing.T) {
	r := planMatches([]QueueEntry{party("a", 1000, "b", 1101, 25)}, testNow, DefaultMatchmakingConfig())[0]
	if !r.HasBot || r.Teams[1].Bots != 2 || r.BotRating() != 1051 || r.MaxHumanRating() != 1101 {
		t.Fatalf("hasBot %v bots %d botRating %d max %d", r.HasBot, r.Teams[1].Bots, r.BotRating(), r.MaxHumanRating())
	}
}
