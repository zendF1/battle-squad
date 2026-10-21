package mission

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestBuildState(t *testing.T) {
	p := PeriodsAt(utc("2026-10-21T10:00:00Z")) // 2026-10-21, 2026-W43
	missions := []DailyMission{
		{ID: "b_old", Target: "win_match", Required: 1, Activity: 20, SortOrder: 2, Value: 5, Claimed: true, Period: "2026-10-20"},
		{ID: "a_capped", Target: "damage_dealt", Required: 3000, Activity: 20, SortOrder: 2, Value: 4500, Period: p.Day},
		{ID: "login", Target: "login", Required: 1, Activity: 10, SortOrder: 1, Goto: "none", Value: 1, Claimed: true, Period: p.Day},
		{ID: "partial", Target: "play_match", Required: 3, Activity: 20, SortOrder: 3, Goto: "play", Value: 2, Period: p.Day},
		{ID: "none", Target: "shots_hit", Required: 10, Activity: 10, SortOrder: 4},
	}
	chests := []Chest{
		{ID: "chest_w300", Track: TrackWeekly, Threshold: 300},
		{ID: "chest_d40", Track: TrackDaily, Threshold: 40},
		{ID: "chest_d20", Track: TrackDaily, Threshold: 20, Claimed: true, Period: p.Day,
			Rewards: Rewards{Coin: 300, Items: []RewardItem{{Kind: "item", ID: "medkit", Qty: 2}}}},
		{ID: "chest_d60", Track: TrackDaily, Threshold: 60, Claimed: true, Period: "2026-10-20"}, // yesterday's claim
		{ID: "chest_w150", Track: TrackWeekly, Threshold: 150, Claimed: true, Period: "2026-W42"},
		{ID: "chest_m1", Track: "monthly", Threshold: 1},
	}
	activity := []ActivityRow{{Track: TrackDaily, Period: p.Day, Points: 40}, {Track: TrackWeekly, Period: p.Week, Points: 150}}

	st := BuildState(p, missions, chests, activity)
	if st.Day != "2026-10-21" || st.Week != "2026-W43" || st.Activity != 40 || st.WeeklyActivity != 150 ||
		!st.DayResetAt.Equal(utc("2026-10-21T17:00:00Z")) || !st.WeekResetAt.Equal(utc("2026-10-25T17:00:00Z")) {
		t.Fatalf("header %+v", st)
	}
	type ms struct {
		id, state string
		progress  int
	}
	var gotM []ms
	for _, m := range st.Missions {
		gotM = append(gotM, ms{m.ID, m.State, m.Progress})
	}
	wantM := []ms{
		{"login", StateClaimed, 1},
		{"a_capped", StateClaimable, 3000}, // capped at required; sort order 2 then id
		{"b_old", StateProgress, 0},        // yesterday's row reads as 0 / unclaimed
		{"partial", StateProgress, 2},
		{"none", StateProgress, 0},
	}
	if !reflect.DeepEqual(gotM, wantM) {
		t.Fatalf("missions %v, want %v", gotM, wantM)
	}
	if m := st.Missions[3]; m.Goto != "play" || m.Activity != 20 || m.Required != 3 || m.Target != "play_match" {
		t.Fatalf("mission fields %+v", m)
	}

	type cs struct{ id, state string }
	var gotC, gotW []cs
	for _, c := range st.Chests {
		gotC = append(gotC, cs{c.ID, c.State})
	}
	for _, c := range st.Weekly {
		gotW = append(gotW, cs{c.ID, c.State})
	}
	wantC := []cs{{"chest_d20", StateClaimed}, {"chest_d40", StateClaimable}, {"chest_d60", StateLocked}}
	wantW := []cs{{"chest_w150", StateClaimable}, {"chest_w300", StateLocked}} // last week's claim is gone
	if !reflect.DeepEqual(gotC, wantC) || !reflect.DeepEqual(gotW, wantW) {
		t.Fatalf("chests %v weekly %v", gotC, gotW)
	}

	// JSON: field names of the contract, items never null
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"day", "dayResetAt", "week", "weekResetAt", "activity", "weeklyActivity", "missions", "chests", "weekly"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("missing %s in %s", k, raw)
		}
	}
	if doc["dayResetAt"] != "2026-10-21T17:00:00Z" {
		t.Errorf("dayResetAt %v", doc["dayResetAt"])
	}
	mission := doc["missions"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "title", "target", "required", "progress", "activity", "goto", "state"} {
		if _, ok := mission[k]; !ok {
			t.Errorf("mission misses %s", k)
		}
	}
	chest := doc["chests"].([]any)[1].(map[string]any)
	rewards := chest["rewards"].(map[string]any)
	if chest["track"] != "daily" || chest["threshold"] != 40.0 || rewards["items"] == nil || rewards["coin"] != 0.0 {
		t.Errorf("chest %v", chest)
	}
}

func TestBuildStateEmpty(t *testing.T) {
	p := PeriodsAt(utc("2026-10-21T10:00:00Z"))
	st := BuildState(p, nil, nil, []ActivityRow{{Track: TrackDaily, Period: "2026-10-20", Points: 90}, {Track: TrackWeekly, Period: "2026-W42", Points: 600}})
	if st.Activity != 0 || st.WeeklyActivity != 0 || st.Missions == nil || st.Chests == nil || st.Weekly == nil {
		t.Fatalf("empty state %+v", st)
	}
}

func TestMatchDeltas(t *testing.T) {
	got := matchDeltas(MatchStats{Won: true, Damage: 1200, Kills: 0, ShotsHit: 4, SkillUses: 1, ItemsUsed: 2})
	want := map[string]int{TargetPlayMatch: 1, TargetWinMatch: 1, TargetDamageDealt: 1200, TargetShotsHit: 4, TargetSkillUsed: 1, TargetItemUsed: 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deltas %v", got)
	}
	if got := matchDeltas(MatchStats{}); !reflect.DeepEqual(got, map[string]int{TargetPlayMatch: 1}) {
		t.Fatalf("a lost match %v", got)
	}
}
