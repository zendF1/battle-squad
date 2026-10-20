package milestone

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

func titleItems(items []RewardItem) []RewardItem {
	var out []RewardItem
	for _, it := range items {
		if it.Kind == RewardKindTitle {
			out = append(out, it)
		}
	}
	return out
}

func ownedTitles(t *testing.T, db *database.PostgresDB, playerID string) string {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(), `SELECT title_id FROM player_titles WHERE player_id = $1 ORDER BY title_id`, playerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return strings.Join(ids, ",")
}

func listed(list ListResponse, id string) bool {
	for _, tr := range list.Tracks {
		for _, n := range tr.Nodes {
			if n.ID == id {
				return true
			}
		}
	}
	return false
}

// GET /milestones serves title items with the name and rarity of their config_titles row;
// a title that is inactive or unknown keeps its node out.
func TestListServesTitleItems(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	ctx := context.Background()

	list, err := svc.List(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	for id, titleID := range TitleRewards {
		items := titleItems(node(t, list, id).Rewards.Items)
		if len(items) != 1 || items[0].ID != titleID || items[0].Name == "" || items[0].Rarity == "" || items[0].Qty != 0 {
			t.Errorf("%s: title items %+v", id, items)
		}
	}
	n := node(t, list, "ms_char_lv_40")
	raw, _ := json.Marshal(n.Rewards.Items)
	if string(raw) != `[{"kind":"stone","level":7,"qty":2},{"kind":"title","id":"veteran","name":"Chiến binh kỳ cựu","rarity":"blue"}]` {
		t.Fatalf("ms_char_lv_40 items %s", raw)
	}
	if n.Rewards.Gem != 50 {
		t.Fatalf("other rewards kept: %+v", n.Rewards)
	}

	exec(t, db, `UPDATE config_titles SET is_active = FALSE WHERE title_id = 'smith'`)
	exec(t, db, `UPDATE missions SET reward_items = '[{"kind":"title","id":"nope"}]' WHERE mission_id = 'ms_char_lv_05'`)
	setCounter(t, db, "p1", CounterUpgradePeak, 10)
	exec(t, db, `INSERT INTO player_characters (player_id, character_id, level) VALUES ('p1', 'rookie', 5)`)
	list, err = svc.List(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if listed(list, "ms_upgrade_10") || listed(list, "ms_char_lv_05") || !listed(list, "ms_upgrade_06") {
		t.Fatal("a node with an inactive or unknown title is served")
	}
	// upgrade 3, 6 (the +10 node and the level 5 node are not servable)
	if list.ClaimableCount != 2 {
		t.Fatalf("claimable %d", list.ClaimableCount)
	}
	_, err = svc.Claim(ctx, "p1", "ms_upgrade_10")
	wantCode(t, err, "NOT_FOUND")
	if ownedTitles(t, db, "p1") != "" {
		t.Fatal("title granted")
	}
}

func TestClaimGrantsTitleOnce(t *testing.T) {
	svc, db := newService(t, 1) // a pool query inside the claim tx would hang
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_characters (player_id, character_id, level) VALUES ('p1', 'tanko', 40)`)
	ctx := context.Background()

	res, err := svc.Claim(ctx, "p1", "ms_char_lv_40")
	if err != nil {
		t.Fatal(err)
	}
	items := titleItems(res.Rewards.Items)
	if len(items) != 1 || items[0] != (RewardItem{Kind: RewardKindTitle, ID: "veteran", Name: "Chiến binh kỳ cựu", Rarity: "blue"}) {
		t.Fatalf("claim rewards %+v", res.Rewards.Items)
	}
	if ownedTitles(t, db, "p1") != "veteran" || stones(t, db, "p1")[7] != 2 || res.Gem != 50 {
		t.Fatalf("granted %q stones %v gem %d", ownedTitles(t, db, "p1"), stones(t, db, "p1"), res.Gem)
	}
	_, err = svc.Claim(ctx, "p1", "ms_char_lv_40")
	wantCode(t, err, "MILESTONE_ALREADY_CLAIMED")
	if n := scalar(t, db, `SELECT COUNT(*) FROM player_titles`); n != 1 {
		t.Fatalf("%d title rows", n)
	}

	// A title already owned (another source) does not fail the claim
	dbtest.CreatePlayer(t, db, "p2", 0, 0)
	exec(t, db, `INSERT INTO player_characters (player_id, character_id, level) VALUES ('p2', 'tanko', 40)`)
	exec(t, db, `INSERT INTO player_titles (player_id, title_id, obtained_at) VALUES ('p2', 'veteran', '2026-01-01T00:00:00Z')`)
	if _, err := svc.Claim(ctx, "p2", "ms_char_lv_40"); err != nil {
		t.Fatal(err)
	}
	if n := scalar(t, db, `SELECT COUNT(*) FROM player_titles WHERE player_id = 'p2' AND obtained_at = '2026-01-01T00:00:00Z'`); n != 1 {
		t.Fatal("owned title row changed")
	}
	if _, gem := dbtest.Balance(t, db, "p2"); gem != 50 {
		t.Fatalf("p2 gem %d", gem)
	}
}

func TestConcurrentTitleClaimGrantsOnce(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterWins, 100)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, errs[i] = svc.Claim(ctx, "p1", "ms_wins_0100")
			} else {
				_, errs[i] = svc.ClaimAll(ctx, "p1")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil && !strings.Contains(err.Error(), "already claimed") {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if ownedTitles(t, db, "p1") != "sharpshooter" {
		t.Fatalf("titles %q", ownedTitles(t, db, "p1"))
	}
	// wins 5 + 25 + 100 once each: gem 0 + 20 + 50
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 70 {
		t.Fatalf("gem %d", gem)
	}
}

func TestClaimAllGrantsTitles(t *testing.T) {
	svc, db := newService(t, 1)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	setCounter(t, db, "p1", CounterWins, 100)
	setCounter(t, db, "p1", CounterMatches, 1000)
	setCounter(t, db, "p1", CounterRankPeak, 6)
	ctx := context.Background()

	res, err := svc.ClaimAll(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range titleItems(res.Rewards.Items) {
		if it.Name == "" || it.Rarity == "" {
			t.Errorf("title item without name / rarity: %+v", it)
		}
		got = append(got, it.ID)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != "grandmaster,old_hand,sharpshooter" {
		t.Fatalf("claim-all titles %v", got)
	}
	if ownedTitles(t, db, "p1") != "grandmaster,old_hand,sharpshooter" {
		t.Fatalf("owned %q", ownedTitles(t, db, "p1"))
	}
	again, err := svc.ClaimAll(ctx, "p1")
	if err != nil || len(again.Claimed) != 0 || len(again.Rewards.Items) != 0 {
		t.Fatalf("second claim-all %+v %v", again, err)
	}
}
