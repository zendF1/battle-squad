package equipment

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

const stonePurchaseMigration = "024_stone_purchase_idempotency.up.sql"

func newServiceOn(db *database.PostgresDB) *Service {
	return NewService(NewRepository(db), economy.NewRepository(), db)
}

func TestBuyStones(t *testing.T) {
	db := dbtest.NewSchema(t, 1, append(append([]string{}, equipmentMigrations...), stonePurchaseMigration)...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 10_000, 1_000)

	// Level 2 costs 250 coin
	code, env := callHandler(t, h.BuyStones, "p1", BuyStoneRequest{StoneLevel: 2, Quantity: 3, IdempotencyKey: "k1"})
	if code != http.StatusOK {
		t.Fatalf("buy: status %d body %v", code, env)
	}
	data, _ := env["data"].(map[string]any)
	if data["stoneLevel"] != 2.0 || data["quantity"] != 3.0 || data["currency"] != "coin" || data["amount"] != 750.0 ||
		data["balance"] != 9_250.0 || data["owned"] != 3.0 || data["replay"] != false {
		t.Fatalf("buy data %v", data)
	}

	// Same key, level and quantity: the first buy back, no second charge
	code, env = callHandler(t, h.BuyStones, "p1", BuyStoneRequest{StoneLevel: 2, Quantity: 3, IdempotencyKey: "k1"})
	data, _ = env["data"].(map[string]any)
	if code != http.StatusOK || data["replay"] != true || data["amount"] != 750.0 || data["balance"] != 9_250.0 || data["owned"] != 3.0 {
		t.Fatalf("replay: status %d body %v", code, env)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 9_250 || stoneCount(t, db, "p1")[2] != 3 {
		t.Fatalf("replay charged or granted again: coin %d stones %v", coin, stoneCount(t, db, "p1"))
	}

	// Same key, another quantity or level
	for _, req := range []BuyStoneRequest{{StoneLevel: 2, Quantity: 4, IdempotencyKey: "k1"}, {StoneLevel: 3, Quantity: 3, IdempotencyKey: "k1"}} {
		code, env = callHandler(t, h.BuyStones, "p1", req)
		if code != http.StatusConflict || errorCode(env) != "IDEMPOTENCY_KEY_REUSED" {
			t.Fatalf("key reuse %v: status %d body %v", req, code, env)
		}
	}

	// Level 7 costs 50 gem
	res, err := svc.BuyStones(ctx, "p1", BuyStoneRequest{StoneLevel: 7, Quantity: 2, IdempotencyKey: "k2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Currency != "gem" || res.Amount != 100 || res.Balance != 900 || res.Owned != 2 || res.Replay {
		t.Fatalf("gem buy %+v", res)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM economy_transactions WHERE player_id = 'p1' AND source = 'buy_stones'
		AND currency = 'gem' AND amount = -100 AND balance_after = 900 AND ref_id = 'stone_7'`); got != 1 {
		t.Fatalf("%d gem buy logs, want 1", got)
	}

	// Refusals change nothing
	refusals := []struct {
		req    BuyStoneRequest
		status int
		code   string
	}{
		{BuyStoneRequest{StoneLevel: 11, Quantity: 1, IdempotencyKey: "r1"}, http.StatusBadRequest, "BAD_REQUEST"}, // merge only
		{BuyStoneRequest{StoneLevel: 13, Quantity: 1, IdempotencyKey: "r2"}, http.StatusNotFound, "NOT_FOUND"},
		{BuyStoneRequest{StoneLevel: 1, Quantity: 0}, http.StatusBadRequest, "BAD_REQUEST"},
		{BuyStoneRequest{StoneLevel: 1, Quantity: 1000}, http.StatusBadRequest, "BAD_REQUEST"},
		{BuyStoneRequest{StoneLevel: 1, Quantity: 1, IdempotencyKey: string(bytes.Repeat([]byte("k"), 65))}, http.StatusBadRequest, "BAD_REQUEST"},
		{BuyStoneRequest{StoneLevel: 6, Quantity: 1, IdempotencyKey: "r3"}, http.StatusBadRequest, "SHOP_INSUFFICIENT_BALANCE"}, // 15 000 coin
	}
	for _, r := range refusals {
		code, env = callHandler(t, h.BuyStones, "p1", r.req)
		if code != r.status || errorCode(env) != r.code {
			t.Fatalf("%+v: status %d body %v, want %d %s", r.req, code, env, r.status, r.code)
		}
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 9_250 || gem != 900 {
		t.Fatalf("refusals charged: coin %d gem %d", coin, gem)
	}
	if got := stoneCount(t, db, "p1"); len(got) != 2 {
		t.Fatalf("refusals granted stones: %v", got)
	}
	// A refused key recorded nothing: it buys once the player can pay
	if _, err := db.Pool.Exec(ctx, `UPDATE player_profiles SET coin = 20000 WHERE player_id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	if res, err := svc.BuyStones(ctx, "p1", BuyStoneRequest{StoneLevel: 6, Quantity: 1, IdempotencyKey: "r3"}); err != nil || res.Replay || res.Balance != 5_000 {
		t.Fatalf("buy after refusal: %+v %v", res, err)
	}

	// No key: every call buys (old clients)
	for i := 0; i < 2; i++ {
		if _, err := svc.BuyStones(ctx, "p1", BuyStoneRequest{StoneLevel: 1, Quantity: 5}); err != nil {
			t.Fatalf("keyless buy %d: %v", i, err)
		}
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 4_000 || stoneCount(t, db, "p1")[1] != 10 {
		t.Fatalf("coin %d stones %v after two keyless 5 × 100-coin buys from 5000", coin, stoneCount(t, db, "p1"))
	}
}

func TestBuyStonesSameKeyConcurrentChargesOnce(t *testing.T) {
	db := dbtest.NewSchema(t, 0, append(append([]string{}, equipmentMigrations...), stonePurchaseMigration)...)
	svc := newServiceOn(db)
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)

	const n = 5
	var wg sync.WaitGroup
	results := make([]*BuyStonesResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.BuyStones(context.Background(), "p1", BuyStoneRequest{StoneLevel: 3, Quantity: 2, IdempotencyKey: "same"})
		}(i)
	}
	wg.Wait()
	replays := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if results[i].Replay {
			replays++
		}
	}
	if replays != n-1 {
		t.Fatalf("%d replays, want %d", replays, n-1)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 8_600 || stoneCount(t, db, "p1")[3] != 2 {
		t.Fatalf("coin %d stones %v, want 8600 and 2 × level 3", coin, stoneCount(t, db, "p1"))
	}
}

// Only zero quantities: refused, nothing changes (it used to roll a free fail
// that reset the piece to its safe zone).
func TestUpgradeRefusesZeroPower(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)
	id, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_helmet_lv10"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE player_equipment SET upgrade_level = 8 WHERE equipment_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO player_stones (player_id, stone_level, quantity) VALUES ('p1', 7, 1)`); err != nil {
		t.Fatal(err)
	}
	for _, stones := range [][]StoneInput{nil, {{StoneLevel: 7, Quantity: 0}}, {{StoneLevel: 7, Quantity: -2}}} {
		code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: stones})
		if code != http.StatusBadRequest || errorCode(env) != "EQUIP_NO_STONES" {
			t.Fatalf("%v: status %d body %v", stones, code, env)
		}
	}
	var level int
	if err := db.Pool.QueryRow(ctx, `SELECT upgrade_level FROM player_equipment WHERE equipment_id = $1`, id).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != 8 || stoneCount(t, db, "p1")[7] != 1 {
		t.Fatalf("level %d stones %v, want +8 and the stone kept", level, stoneCount(t, db, "p1"))
	}
	// Not enough stones: refused, nothing consumed
	code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: []StoneInput{{StoneLevel: 7, Quantity: 1}, {StoneLevel: 6, Quantity: 1}}})
	if code != http.StatusBadRequest || errorCode(env) != "EQUIP_INSUFFICIENT_STONES" || stoneCount(t, db, "p1")[7] != 1 {
		t.Fatalf("insufficient: status %d body %v stones %v", code, env, stoneCount(t, db, "p1"))
	}
	// A real attempt: 729 × 100 / 1200 = 60.75% capped at 45%, the stone consumed
	code, env = callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: []StoneInput{{StoneLevel: 7, Quantity: 1}}})
	data, _ := env["data"].(map[string]any)
	if code != http.StatusOK || data["percent"] != 45.0 || (data["newLevel"] != 9.0 && data["newLevel"] != 6.0) || stoneCount(t, db, "p1")[7] != 0 {
		t.Fatalf("upgrade: status %d body %v", code, env)
	}
}

func TestCatalogMergeRateAndMilestones(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	h := NewHandler(newServiceOn(db))
	_, env := callHandler(t, h.GetCatalog, "p1", nil)
	data, _ := env["data"].(map[string]any)
	ms, _ := data["upgradeMilestones"].([]any)
	if data["mergeRatePerItem"] != 25.0 || len(ms) != 4 {
		t.Fatalf("mergeRatePerItem %v milestones %v, want the defaults", data["mergeRatePerItem"], ms)
	}
	want := [][2]float64{{6, 0.10}, {10, 0.20}, {14, 0.40}, {16, 1.00}}
	for i, m := range ms {
		mm := m.(map[string]any)
		if mm["level"] != want[i][0] || mm["bonus"] != want[i][1] {
			t.Fatalf("milestone %d %v, want %v", i, mm, want[i])
		}
	}
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO game_settings (key, value, value_type, description, category) VALUES
		('equipment.merge_rate_per_item', '30', 'number', '', 'equipment'), ('equipment.milestone_10_bonus', '0.25', 'number', '', 'equipment')`); err != nil {
		t.Fatal(err)
	}
	_, env = callHandler(t, h.GetCatalog, "p1", nil)
	data, _ = env["data"].(map[string]any)
	ms, _ = data["upgradeMilestones"].([]any)
	if data["mergeRatePerItem"] != 30.0 || ms[1].(map[string]any)["bonus"] != 0.25 {
		t.Fatalf("configured: mergeRatePerItem %v milestones %v", data["mergeRatePerItem"], ms)
	}
}
