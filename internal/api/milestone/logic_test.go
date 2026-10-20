package milestone

import (
	"testing"
)

func sample() PlayerProgress {
	return PlayerProgress{
		Counters: map[string]int{
			CounterMatches: 37, CounterWins: 12, "char_wins:spark": 9, "char_wins:tanko": 3,
			CounterUpgradePeak: 12, CounterWeaponPeak: 7, CounterRankPeak: 3,
		},
		CharLevels: map[string]int{"rookie": 22, "tanko": 41, "spark": 1},
		Pieces: []Piece{
			// rookie wears 6 slots, 5 of them at >= 6
			{Slot: "helmet", Level: 6, EquippedOn: "rookie"}, {Slot: "armor", Level: 10, EquippedOn: "rookie"},
			{Slot: "gloves", Level: 6, EquippedOn: "rookie"}, {Slot: "pants", Level: 8, EquippedOn: "rookie"},
			{Slot: "boots", Level: 5, EquippedOn: "rookie"}, {Slot: "weapon", Level: 9, EquippedOn: "rookie"},
			// a +14 armor in the bag counts for the piece peak only
			{Slot: "armor", Level: 14},
			{Slot: "helmet", Level: 3, EquippedOn: "tanko"},
		},
	}
}

func TestProgressPerTarget(t *testing.T) {
	p := sample()
	cases := map[string]int{
		TargetCharLevel:   41, // highest character
		TargetUpgradePeak: 14, // current +14 beats the counter 12
		TargetWeaponPeak:  9,  // current +9 weapon beats the counter 7
		"set_level:6":     5,
		"set_level:5":     6,
		"set_level:3":     6,
		"set_level:11":    0,
		TargetMatches:     37,
		TargetWins:        12,
		TargetCharWins:    9, // most wins with one character
		TargetRankPeak:    3,
		"unknown":         0,
	}
	for target, want := range cases {
		if got := Progress(target, p); got != want {
			t.Errorf("%s: got %d want %d", target, got, want)
		}
	}
	// A peak counter above every current piece (fail reset, sold piece) is kept
	p.Pieces = nil
	if got := Progress(TargetUpgradePeak, p); got != 12 {
		t.Errorf("peak after the pieces are gone: %d", got)
	}
	if got := Progress(TargetCharLevel, PlayerProgress{}); got != 0 {
		t.Errorf("no character: %d", got)
	}
}

func TestSetProgressCountsSlotsOncePerCharacter(t *testing.T) {
	pieces := []Piece{
		{Slot: "helmet", Level: 10, EquippedOn: "flora"}, {Slot: "helmet", Level: 10, EquippedOn: "flora"},
		{Slot: "armor", Level: 10, EquippedOn: "spark"},
	}
	if got := SetProgress(pieces, 10); got != 1 {
		t.Fatalf("got %d", got)
	}
}

func TestStateAndSupported(t *testing.T) {
	m := Milestone{ID: "x", Target: TargetWins, Required: 5}
	if State(m, 4) != StateLocked || State(m, 5) != StateClaimable || State(m, 99) != StateClaimable {
		t.Fatal("locked / claimable")
	}
	m.Claimed = true
	if State(m, 0) != StateClaimed {
		t.Fatal("claimed")
	}
	ok := []Milestone{
		{Target: TargetWins, Required: 1},
		{Target: "set_level:6", Required: 6, Rewards: Rewards{Items: []RewardItem{{Kind: "stone", Level: 12, Qty: 1}}}},
		{Target: TargetMatches, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "item", ID: "medkit", Qty: 2}}}},
		// A title item filled from its active config_titles row; titles carry no qty
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "stone", Level: 8, Qty: 1}, {Kind: "title", ID: "veteran", Name: "Chiến binh kỳ cựu", Rarity: "blue"}}}},
	}
	for _, m := range ok {
		if !Supported(m) {
			t.Errorf("should be supported: %+v", m)
		}
	}
	bad := []Milestone{
		{Target: "nope", Required: 1},
		{Target: TargetWins, Required: 0},
		// A title id with no active config_titles row: the repository leaves the name empty
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "title", ID: "veteran", Qty: 1}}}},
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "title", Name: "X", Rarity: "red"}}}},
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "badge", ID: "x", Qty: 1}}}},
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "stone", Level: 13, Qty: 1}}}},
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "stone", Level: 3, Qty: 0}}}},
		{Target: TargetWins, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "item", Qty: 1}}}},
	}
	for _, m := range bad {
		if Supported(m) {
			t.Errorf("should not be supported: %+v", m)
		}
	}
}

func TestBuildListOrdersTracksAndNodes(t *testing.T) {
	ms := []Milestone{
		{ID: "r2", Track: TrackRank, Target: TargetRankPeak, Required: 2, SortOrder: 2},
		{ID: "w25", Track: TrackCombat, Target: TargetWins, Required: 25, SortOrder: 25},
		{ID: "w5", Track: TrackCombat, Target: TargetWins, Required: 5, SortOrder: 5, Claimed: true},
		{ID: "m10", Track: TrackCombat, Target: TargetMatches, Required: 10, SortOrder: 1},
		{ID: "c5", Track: TrackCharacter, Target: TargetCharLevel, Required: 5, SortOrder: 5},
		{ID: "t", Track: TrackRank, Target: TargetRankPeak, Required: 1, Rewards: Rewards{Items: []RewardItem{{Kind: "title", ID: "x"}}}},
		{ID: "z", Track: "zeta", Target: TargetWins, Required: 1},
	}
	out := BuildList(ms, sample())
	var tracks []string
	for _, tr := range out.Tracks {
		tracks = append(tracks, tr.Track)
	}
	if len(tracks) != 4 || tracks[0] != TrackCharacter || tracks[1] != TrackCombat || tracks[2] != TrackRank || tracks[3] != "zeta" {
		t.Fatalf("tracks %v", tracks)
	}
	combat := out.Tracks[1].Nodes
	if combat[0].ID != "m10" || combat[1].ID != "w5" || combat[2].ID != "w25" {
		t.Fatalf("combat order %+v", combat)
	}
	if combat[1].State != StateClaimed || combat[2].State != StateLocked || combat[2].Progress != 12 || combat[0].State != StateClaimable {
		t.Fatalf("combat states %+v", combat)
	}
	// c5, m10, r2 and z are claimable; the title node is not served
	if out.ClaimableCount != 4 || len(out.Tracks[2].Nodes) != 1 {
		t.Fatalf("claimable %d rank nodes %d", out.ClaimableCount, len(out.Tracks[2].Nodes))
	}
	if out.Tracks[0].Nodes[0].Rewards.Items == nil {
		t.Fatal("items must be [] not null")
	}
}

func TestSumRewardsMergesStones(t *testing.T) {
	a := Rewards{Coin: 100, Items: []RewardItem{{Kind: "stone", Level: 7, Qty: 1}}}
	b := Rewards{Coin: 50, Gem: 20, Items: []RewardItem{{Kind: "stone", Level: 7, Qty: 2}, {Kind: "stone", Level: 9, Qty: 1}}}
	got := SumRewards(a, b)
	if got.Coin != 150 || got.Gem != 20 || len(got.Items) != 2 || got.Items[0].Qty != 3 || got.Items[1].Level != 9 {
		t.Fatalf("%+v", got)
	}
	if a.Items[0].Qty != 1 {
		t.Fatal("a changed")
	}
}

func TestDefaults(t *testing.T) {
	seen := map[string]bool{}
	perTrack := map[string]int{}
	for _, d := range Defaults {
		if seen[d.ID] {
			t.Fatalf("duplicate %s", d.ID)
		}
		seen[d.ID] = true
		perTrack[d.Track]++
		items := append([]RewardItem{}, d.Items...)
		titles := 0
		for i := range items {
			if items[i].Kind == RewardKindTitle {
				titles++
				if items[i].ID != TitleRewards[d.ID] {
					t.Errorf("%s grants title %s, want %s", d.ID, items[i].ID, TitleRewards[d.ID])
				}
				items[i].Name, items[i].Rarity = "name", "blue" // as Repository.Milestones fills them
			}
		}
		if _, ok := TitleRewards[d.ID]; (titles == 1) != ok || titles > 1 {
			t.Errorf("%s: %d title items", d.ID, titles)
		}
		m := Milestone{ID: d.ID, Track: d.Track, Target: d.Target, Required: d.Required, Rewards: Rewards{Coin: d.Coin, Gem: d.Gem, Items: items}}
		if !Supported(m) {
			t.Errorf("%s not supported", d.ID)
		}
		if d.Coin == 0 && d.Gem == 0 && len(d.Items) == 0 {
			t.Errorf("%s has no reward", d.ID)
		}
	}
	for id := range TitleRewards {
		if !seen[id] {
			t.Errorf("title milestone %s is not a default", id)
		}
	}
	if len(TitleRewards) != 8 {
		t.Errorf("%d title milestones", len(TitleRewards))
	}
	if len(Defaults) != 46 || perTrack[TrackCharacter] != 16 || perTrack[TrackUpgrade] != 13 || perTrack[TrackCombat] != 12 || perTrack[TrackRank] != 5 {
		t.Fatalf("defaults %d %v", len(Defaults), perTrack)
	}
	if TierOrder("silver") != 2 || TierOrder("master") != 6 || TierOrder("x") != 0 {
		t.Fatal("tier order")
	}
}
