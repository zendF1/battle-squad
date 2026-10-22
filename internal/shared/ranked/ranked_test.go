package ranked_test

import (
	"context"
	"sync"
	"testing"

	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/ranked"
)

func TestTierAndDivision(t *testing.T) {
	cases := []struct {
		rating   int
		tier     string
		division int
	}{
		{0, "bronze", 3}, {332, "bronze", 3}, {333, "bronze", 2}, {999, "bronze", 1},
		{1000, "silver", 3}, {1065, "silver", 3}, {1066, "silver", 2}, {1199, "silver", 1},
		{1200, "gold", 3}, {1499, "gold", 1},
		{1500, "platinum", 3}, {1799, "platinum", 1},
		{1800, "diamond", 3}, {2199, "diamond", 1},
		{2200, "master", 1}, {3000, "master", 1},
	}
	for _, c := range cases {
		tier, div := ranked.TierAndDivision(c.rating)
		if tier != c.tier || div != c.division || ranked.Tier(c.rating) != c.tier {
			t.Errorf("%d: %s %d, want %s %d", c.rating, tier, div, c.tier, c.division)
		}
	}
}

func TestTierOrder(t *testing.T) {
	for i, tier := range []string{"bronze", "silver", "gold", "platinum", "diamond", "master"} {
		if ranked.TierIndex(tier) != i {
			t.Errorf("TierIndex(%s) = %d", tier, ranked.TierIndex(tier))
		}
	}
	if ranked.TierIndex("unknown") != 0 || ranked.TierIndex("") != 0 {
		t.Error("unknown tiers count as bronze")
	}
	if ranked.CompareTiers("gold", "silver") <= 0 || ranked.CompareTiers("silver", "silver") != 0 || ranked.CompareTiers("bronze", "master") >= 0 {
		t.Error("CompareTiers order")
	}
}

func TestDefaultEloConfig(t *testing.T) {
	c := ranked.DefaultEloConfig()
	if c.DefaultRating != 1000 || c.RatingFloor != 0 || c.KFactor != 32 {
		t.Fatalf("%+v", c)
	}
	if tier, div := ranked.TierAndDivision(c.DefaultRating); tier != "silver" || div != 3 {
		t.Fatalf("default rating is %s %d, want silver 3", tier, div)
	}
}

// No active season: one "Mùa 1" is created, even by racing callers; afterwards
// the active row is returned as is, and an ended season makes the next "Mùa 2".
func TestEnsureActiveSeason(t *testing.T) {
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql")
	ctx := context.Background()

	var wg sync.WaitGroup
	got := make([]ranked.Season, 8)
	errs := make([]error, 8)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = ranked.EnsureActiveSeason(ctx, db.Pool)
		}(i)
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if got[i].SeasonID != "season_1" || got[i].Name != "Mùa 1" || got[i].Status != "active" || !got[i].EndsAt.After(got[i].StartsAt) {
			t.Fatalf("season %+v", got[i])
		}
	}
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM rank_seasons`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d seasons (%v)", n, err)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE rank_seasons SET status = 'ended'`); err != nil {
		t.Fatal(err)
	}
	s, err := ranked.EnsureActiveSeason(ctx, db.Pool)
	if err != nil || s.SeasonID != "season_2" || s.Name != "Mùa 2" {
		t.Fatalf("next season %+v %v", s, err)
	}

	// A configured active season is kept
	if _, err := db.Pool.Exec(ctx, `UPDATE rank_seasons SET status = 'ended';
		INSERT INTO rank_seasons (season_id, name, starts_at, ends_at, status) VALUES ('s_custom', 'Custom', now(), now() + interval '1 day', 'active')`); err != nil {
		t.Fatal(err)
	}
	if s, err := ranked.EnsureActiveSeason(ctx, db.Pool); err != nil || s.SeasonID != "s_custom" {
		t.Fatalf("custom season %+v %v", s, err)
	}
}

func TestLoadEloConfig(t *testing.T) {
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql")
	ctx := context.Background()
	if c, err := ranked.LoadEloConfig(ctx, db.Pool); err == nil || c != ranked.DefaultEloConfig() {
		t.Fatalf("missing row: %+v %v", c, err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO game_settings (key, value) VALUES ('elo', '{"kFactor":40,"defaultRating":1200}')`); err != nil {
		t.Fatal(err)
	}
	c, err := ranked.LoadEloConfig(ctx, db.Pool)
	if err != nil || c.KFactor != 40 || c.DefaultRating != 1200 || c.RatingFloor != 0 {
		t.Fatalf("%+v %v", c, err)
	}
}

// oldTierAndDivision is the switch TierAndDivision was before ranked.Bands (and the Godot
// client's rank_model.of_rating): the table must keep every rating's rank.
func oldTierAndDivision(rating int) (string, int) {
	div := func(base, width int) int {
		return min(max(3-(rating-base)/width, 1), 3)
	}
	switch {
	case rating < 1000:
		return "bronze", div(0, 333)
	case rating < 1200:
		return "silver", div(1000, 66)
	case rating < 1500:
		return "gold", div(1200, 100)
	case rating < 1800:
		return "platinum", div(1500, 100)
	case rating < 2200:
		return "diamond", div(1800, 133)
	default:
		return "master", 1
	}
}

func TestBandsKeepEveryRank(t *testing.T) {
	for rating := -1000; rating <= 4000; rating++ {
		gotTier, gotDiv := ranked.TierAndDivision(rating)
		wantTier, wantDiv := oldTierAndDivision(rating)
		if gotTier != wantTier || gotDiv != wantDiv {
			t.Fatalf("rating %d: %s %d, want %s %d", rating, gotTier, gotDiv, wantTier, wantDiv)
		}
	}
	if len(ranked.Bands()) != len(ranked.Tiers) {
		t.Fatal("a band per tier")
	}
	for i, b := range ranked.Bands() {
		if b.Tier != ranked.Tiers[i] {
			t.Fatalf("band %d is %s, tier %s", i, b.Tier, ranked.Tiers[i])
		}
	}
}
