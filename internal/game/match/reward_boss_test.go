package match

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// The reward table scales with the effective damage up to its caps, adds the win bonus, and
// rolls one drop per damagePerDropRoll (at most maxDropRolls) plus winDropRolls.
func TestBossRewardAmountsScaleWithDamage(t *testing.T) {
	tb := gamedata.DefaultBosses()[0].Rewards.Bonus // 100 + 60/1000 dmg (max 1500) +400 win; exp 40 + 25/1000 (max 600) +120
	rng := func() *rand.Rand { return rand.New(rand.NewSource(1)) }
	g0, e0, i0 := BossRewardAmounts(tb, 0, false, rng())
	g1, e1, i1 := BossRewardAmounts(tb, 5000, false, rng())
	g2, e2, _ := BossRewardAmounts(tb, 10000, false, rng())
	gw, ew, iw := BossRewardAmounts(tb, 10000, true, rng())
	gc, ec, _ := BossRewardAmounts(tb, 1e7, false, rng())
	if g0 != 100 || e0 != 40 || len(i0) != 0 || g1 != 400 || e1 != 165 || g2 != 700 || e2 != 290 || gw != 1100 || ew != 410 ||
		gc != 1500 || ec != 600 {
		t.Fatalf("gold %d %d %d %d %d, exp %d %d %d %d %d", g0, g1, g2, gw, gc, e0, e1, e2, ew, ec)
	}
	count := func(items []BossRewardItem) int {
		n := 0
		for _, it := range items {
			n += it.Quantity
		}
		return n
	}
	// 5000 damage: 2 rolls; won 10000: 4 (cap) + 1
	_ = i1
	total1, totalW := 0, 0
	for seed := int64(0); seed < 200; seed++ {
		_, _, a := BossRewardAmounts(tb, 5000, false, rand.New(rand.NewSource(seed)))
		_, _, b := BossRewardAmounts(tb, 10000, true, rand.New(rand.NewSource(seed)))
		total1 += count(a)
		totalW += count(b)
	}
	if total1 == 0 || totalW <= total1*2 {
		t.Fatalf("drops: 5000 dmg %d, won 10000 dmg %d (200 runs)", total1, totalW)
	}
	_ = iw
	if f := BossEffectiveDamage(1000, 500, 0.3); f != 1150 {
		t.Fatalf("effective %v", f)
	}
}

// processBossRewards: gold (coin), EXP and drops by damage, the bonus table for the first
// dailyBonusRuns runs of the UTC+7 day per boss and the normal one after, a leaver gets nothing
// and spends no run, bots write nothing; history mode boss:<id>.
func TestBossRewardsAndBonusRunsDB(t *testing.T) {
	db := newMatchSchema(t)
	ctx := context.Background()
	for _, id := range []string{"p1", "p2", "p3"} {
		dbtest.CreatePlayer(t, db, id, 0, 0)
	}
	boss := gamedata.DefaultBosses()[0]
	day1 := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC) // 17:00 UTC+7
	run := func(now time.Time, stats ...*PlayerStats) map[string]RewardResult {
		t.Helper()
		byID := map[string]*PlayerStats{}
		for _, s := range stats {
			byID[s.PlayerID] = s
		}
		res, err := processBossRewards(ctx, db, economy.NewRepository(), "m_"+now.String(), boss, boss.MapID, byID, nil, 100, now,
			rand.New(rand.NewSource(7)))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := run(day1,
		&PlayerStats{PlayerID: "p1", CharacterID: "rookie", TeamID: 1, IsWinner: true, BossDamage: 9000, MinionDamage: 1000, Counts: true},
		&PlayerStats{PlayerID: "p2", CharacterID: "tanko", TeamID: 1, IsWinner: true, BossDamage: 1000},
		&PlayerStats{PlayerID: "p3", CharacterID: "flora", TeamID: 1, Left: true, BossDamage: 3000},
		&PlayerStats{PlayerID: "bot_x", CharacterID: "spark", TeamID: 1, IsWinner: true, BossDamage: 5000, IsBot: true},
		&PlayerStats{PlayerID: "boss_steel_robot", TeamID: 2, IsBot: true},
	)
	if len(res) != 3 {
		t.Fatalf("rewards for %d players", len(res))
	}
	p1, p2, p3 := res["p1"], res["p2"], res["p3"]
	// p1: effective 9300 → 100 + 558 + 400 win gold, 40 + 233 + 120 exp; p2: 1000 → 100 + 60 + 400, 40 + 25 + 120
	if p1.BossReward == nil || p1.CoinGained != 1058 || p1.ExpGained != 393 || !p1.BonusRun || p1.BonusRunsLeft != 2 || p1.BossDamage != 9000 ||
		p1.MinionDamage != 1000 || len(p1.Items) == 0 {
		t.Fatalf("p1 %+v %+v", p1, p1.BossReward)
	}
	if p2.CoinGained != 560 || p2.ExpGained != 185 || p2.CoinGained >= p1.CoinGained || !p2.BonusRun {
		t.Fatalf("p2 %+v", p2)
	}
	if p3.CoinGained != 0 || p3.ExpGained != 0 || p3.BonusRun || p3.BonusRunsLeft != 3 || len(p3.Items) != 0 {
		t.Fatalf("leaver %+v %+v", p3, p3.BossReward)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 1058 {
		t.Fatalf("p1 coin %d", coin)
	}
	items := 0
	for _, it := range p1.Items {
		var q int
		if err := db.Pool.QueryRow(ctx, `SELECT quantity FROM inventory_items WHERE player_id = 'p1' AND item_id = $1 AND source = 'boss_reward'`,
			it.ItemID).Scan(&q); err != nil || q != it.Quantity {
			t.Fatalf("inventory %s: %d %v", it.ItemID, q, err)
		}
		items += it.Quantity
	}
	var mode, result string
	if err := db.Pool.QueryRow(ctx, `SELECT mode, result FROM match_histories WHERE player_id = 'p1'`).Scan(&mode, &result); err != nil ||
		mode != "boss:steel_robot" || result != "win" {
		t.Fatalf("history %s %s %v", mode, result, err)
	}
	var botRows int
	_ = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM match_histories WHERE player_id LIKE 'bot_%' OR player_id LIKE 'boss_%'`).Scan(&botRows)
	if botRows != 0 {
		t.Fatalf("%d bot history rows", botRows)
	}

	// p1's runs 2 and 3 of the day are bonus runs, the 4th uses the normal table
	small := &PlayerStats{PlayerID: "p1", CharacterID: "rookie", TeamID: 1, BossDamage: 2000}
	for i, want := range []struct {
		bonus bool
		left  int
		gold  int
	}{{true, 1, 220}, {true, 0, 220}, {false, 0, 60}} {
		r := run(day1.Add(time.Duration(i+1)*time.Minute), small)["p1"]
		if r.BonusRun != want.bonus || r.BonusRunsLeft != want.left || r.CoinGained != want.gold {
			t.Fatalf("run %d: %+v %+v", i+2, r, r.BossReward)
		}
	}
	var runs int
	if err := db.Pool.QueryRow(ctx, `SELECT runs FROM player_boss_runs WHERE player_id = 'p1' AND boss_id = 'steel_robot' AND day = '2026-10-22'`).
		Scan(&runs); err != nil || runs != 4 {
		t.Fatalf("runs %d %v", runs, err)
	}
	// the UTC+7 day ends at 17:00 UTC: a bonus run again
	if r := run(time.Date(2026, 10, 22, 17, 0, 0, 0, time.UTC), small)["p1"]; !r.BonusRun || r.BonusRunsLeft != 2 {
		t.Fatalf("next day %+v", r.BossReward)
	}
	// another boss counts its own runs
	other := gamedata.DefaultBosses()[1]
	byID := map[string]*PlayerStats{"p1": small}
	r, err := processBossRewards(ctx, db, economy.NewRepository(), "m_crab", other, other.MapID, byID, nil, 100, day1, rand.New(rand.NewSource(1)))
	if err != nil || !r["p1"].BonusRun || r["p1"].BonusRunsLeft != 2 {
		t.Fatalf("lava crab %+v %v", r["p1"].BossReward, err)
	}
}
