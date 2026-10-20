package milestone

import "fmt"

// Default is a seeded milestone row (migrations/026 and admin SeedMilestones write the same).
type Default struct {
	ID          string
	Track       string
	Target      string
	Required    int
	SortOrder   int
	Coin        int
	Gem         int
	Items       []RewardItem
	Description string
}

func stone(level, qty int) []RewardItem {
	return []RewardItem{{Kind: RewardKindStone, Level: level, Qty: qty}}
}

// Defaults lists the seeded milestones, all account-wide.
var Defaults = func() []Default {
	var out []Default
	add := func(d Default) { out = append(out, d) }

	// A. A character (any) reaches level N
	charLevels := []struct {
		level, coin, gem int
		items            []RewardItem
	}{
		{5, 1000, 0, nil}, {10, 2000, 0, stone(4, 3)}, {15, 0, 20, nil}, {20, 0, 30, stone(6, 2)},
		{25, 5000, 0, nil}, {30, 0, 40, stone(7, 1)}, {35, 8000, 0, nil}, {40, 0, 50, stone(7, 2)},
		{45, 10000, 0, nil}, {50, 0, 60, stone(8, 1)}, {55, 12000, 0, nil}, {60, 0, 100, stone(9, 1)},
		{65, 15000, 0, nil}, {70, 0, 80, stone(8, 2)}, {75, 20000, 0, nil}, {80, 0, 300, stone(10, 1)},
	}
	for _, c := range charLevels {
		add(Default{ID: fmt.Sprintf("ms_char_lv_%02d", c.level), Track: TrackCharacter, Target: TargetCharLevel,
			Required: c.level, SortOrder: c.level, Coin: c.coin, Gem: c.gem, Items: c.items})
	}

	// B. A piece reaches +N; a weapon reaches +14 / +16; one character wears a full set at +N
	upgrades := []struct {
		level, gem int
		items      []RewardItem
	}{
		{3, 0, stone(3, 5)}, {6, 20, stone(6, 3)}, {10, 50, stone(8, 2)}, {12, 0, stone(9, 1)},
		{14, 150, stone(10, 1)}, {16, 500, stone(11, 1)},
	}
	for _, u := range upgrades {
		add(Default{ID: fmt.Sprintf("ms_upgrade_%02d", u.level), Track: TrackUpgrade, Target: TargetUpgradePeak,
			Required: u.level, SortOrder: 100 + u.level, Gem: u.gem, Items: u.items})
	}
	add(Default{ID: "ms_weapon_14", Track: TrackUpgrade, Target: TargetWeaponPeak, Required: 14, SortOrder: 214, Gem: 100,
		Description: "Mở khoá hào quang tím sau lưng"})
	add(Default{ID: "ms_weapon_16", Track: TrackUpgrade, Target: TargetWeaponPeak, Required: 16, SortOrder: 216, Gem: 300,
		Description: "Mở khoá hào quang đỏ sau lưng"})
	sets := []struct {
		level, gem int
		items      []RewardItem
	}{
		{3, 0, stone(5, 3)}, {6, 50, stone(7, 2)}, {10, 150, stone(9, 2)}, {14, 300, stone(10, 2)}, {16, 1000, stone(11, 2)},
	}
	for _, s := range sets {
		add(Default{ID: fmt.Sprintf("ms_set_%02d", s.level), Track: TrackUpgrade, Target: fmt.Sprintf("%s%d", SetTargetPrefix, s.level),
			Required: SlotCount, SortOrder: 300 + s.level, Gem: s.gem, Items: s.items})
	}

	// C. Counted matches, wins, most wins with one character
	combat := []struct {
		id, target      string
		n, coin, gem, o int
		items           []RewardItem
	}{
		{"ms_matches_0010", TargetMatches, 10, 1000, 0, 0, nil},
		{"ms_matches_0050", TargetMatches, 50, 3000, 10, 0, nil},
		{"ms_matches_0100", TargetMatches, 100, 0, 20, 0, stone(7, 1)},
		{"ms_matches_0300", TargetMatches, 300, 0, 50, 0, stone(8, 2)},
		{"ms_matches_1000", TargetMatches, 1000, 0, 200, 0, stone(10, 1)},
		{"ms_wins_0005", TargetWins, 5, 1500, 0, 10000, nil},
		{"ms_wins_0025", TargetWins, 25, 5000, 20, 10000, nil},
		{"ms_wins_0100", TargetWins, 100, 0, 50, 10000, stone(8, 1)},
		{"ms_wins_0500", TargetWins, 500, 0, 150, 10000, stone(9, 1)},
		{"ms_char_wins_0010", TargetCharWins, 10, 2000, 0, 20000, nil},
		{"ms_char_wins_0050", TargetCharWins, 50, 0, 30, 20000, stone(7, 1)},
		{"ms_char_wins_0200", TargetCharWins, 200, 0, 100, 20000, stone(9, 1)},
	}
	for _, c := range combat {
		add(Default{ID: c.id, Track: TrackCombat, Target: c.target, Required: c.n, SortOrder: c.o + c.n,
			Coin: c.coin, Gem: c.gem, Items: c.items})
	}

	// D. Lifetime peak rank tier
	ranks := []struct {
		tier      string
		coin, gem int
		items     []RewardItem
	}{
		{"silver", 2000, 0, nil}, {"gold", 0, 30, stone(7, 1)}, {"platinum", 0, 50, stone(7, 2)},
		{"diamond", 0, 100, stone(8, 1)}, {"master", 0, 200, stone(9, 1)},
	}
	for _, r := range ranks {
		add(Default{ID: "ms_rank_" + r.tier, Track: TrackRank, Target: TargetRankPeak, Required: TierOrder(r.tier),
			SortOrder: TierOrder(r.tier), Coin: r.coin, Gem: r.gem, Items: r.items})
	}
	return out
}()
