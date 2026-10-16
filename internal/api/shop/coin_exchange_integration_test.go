package shop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
)

const coinExchangeMigration = "018_coin_exchange_offers.up.sql"

func newCoinExchangeService(t *testing.T) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "013_shop_purchase_idempotency.up.sql", coinExchangeMigration)
	return NewService(NewRepository(db), economy.NewRepository(), inventory.NewRepository(db), db), db
}

func TestCoinPackExchangesGemForCoin(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	dbtest.CreatePlayer(t, db, "p1", 7, 120)
	ctx := context.Background()

	p, err := svc.Purchase(ctx, "p1", "coin_pack_100", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if p.PriceCurrency != "gem" || p.PriceAmount != 100 || p.QuantityGranted != 10_000 {
		t.Fatalf("purchase = %+v, want 100 gem -> 10000 coin", p)
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 10_007 || gem != 20 {
		t.Fatalf("coin %d gem %d, want 10007, 20", coin, gem)
	}

	// Both legs are logged against the offer
	rows, err := db.Pool.Query(ctx, `
		SELECT currency, amount, balance_after FROM economy_transactions
		WHERE player_id = 'p1' AND source = 'shop_purchase' AND ref_id = 'coin_pack_100'
		ORDER BY currency`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type leg struct {
		currency      string
		amount, after int
	}
	var legs []leg
	for rows.Next() {
		var l leg
		if err := rows.Scan(&l.currency, &l.amount, &l.after); err != nil {
			t.Fatal(err)
		}
		legs = append(legs, l)
	}
	want := []leg{{"coin", 10_000, 10_007}, {"gem", -100, 20}}
	if len(legs) != 2 || legs[0] != want[0] || legs[1] != want[1] {
		t.Fatalf("economy legs = %+v, want %+v", legs, want)
	}

	// Nothing is added to the inventory
	var items int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_items WHERE player_id = 'p1'`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if items != 0 {
		t.Fatalf("inventory rows = %d, want 0", items)
	}
}

// The gem debit and the coin credit share one transaction: if the credit
// fails the debit is rolled back and no purchase is recorded.
func TestCoinPackCreditFailureRollsBackDebit(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	dbtest.CreatePlayer(t, db, "p1", 0, 50)
	ctx := context.Background()

	if _, err := db.Pool.Exec(ctx, `
		CREATE FUNCTION fail_coin_credit() RETURNS trigger AS $$
		BEGIN
			IF NEW.currency = 'coin' AND NEW.amount > 0 THEN
				RAISE EXCEPTION 'coin credit blocked';
			END IF;
			RETURN NEW;
		END $$ LANGUAGE plpgsql;
		CREATE TRIGGER fail_coin_credit BEFORE INSERT ON economy_transactions
			FOR EACH ROW EXECUTE FUNCTION fail_coin_credit();`); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Purchase(ctx, "p1", "coin_pack_50", "k1"); err == nil {
		t.Fatal("purchase succeeded, want the credit failure")
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 0 || gem != 50 {
		t.Fatalf("coin %d gem %d, want 0, 50", coin, gem)
	}
	if got := countPurchases(t, db, "p1"); got != 0 {
		t.Fatalf("purchase rows = %d, want 0", got)
	}
}

func TestCoinPackInsufficientGem(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	dbtest.CreatePlayer(t, db, "p1", 300, 9)

	_, err := svc.Purchase(context.Background(), "p1", "coin_pack_10", "k1")
	var appErr model.AppError
	if err == nil || !errors.As(purchaseError(err), &appErr) || appErr.Code != "SHOP_INSUFFICIENT_BALANCE" {
		t.Fatalf("err = %v, want SHOP_INSUFFICIENT_BALANCE", err)
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 300 || gem != 9 {
		t.Fatalf("coin %d gem %d, want 300, 9", coin, gem)
	}
	if got := countPurchases(t, db, "p1"); got != 0 {
		t.Fatalf("purchase rows = %d, want 0", got)
	}
}

func TestCoinPackIdempotentReplayCreditsOnce(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	dbtest.CreatePlayer(t, db, "p1", 0, 100)

	const n = 5
	var wg sync.WaitGroup
	results := make([]*ShopPurchase, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.Purchase(context.Background(), "p1", "coin_pack_10", "same-key")
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if results[i].PurchaseID != results[0].PurchaseID || results[i].QuantityGranted != 1_000 {
			t.Fatalf("request %d got %+v, want replay of %+v", i, results[i], results[0])
		}
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 1_000 || gem != 90 {
		t.Fatalf("coin %d gem %d, want 1000, 90", coin, gem)
	}
	if got := countPurchases(t, db, "p1"); got != 1 {
		t.Fatalf("purchase rows = %d, want 1", got)
	}

	// A new key exchanges again: coin packs have no per-player limit
	if _, err := svc.Purchase(context.Background(), "p1", "coin_pack_10", "other-key"); err != nil {
		t.Fatal(err)
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 2_000 || gem != 80 {
		t.Fatalf("coin %d gem %d, want 2000, 80", coin, gem)
	}
}

// A coin pack priced in coin is a catalog error: rejected before any debit.
func TestCoinPackPricedInCoinIsRejected(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	dbtest.CreatePlayer(t, db, "p1", 5_000, 100)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity)
		VALUES ('coin_for_coin', 'coin', 'coin_pack', 'coin', 100, 1000)`); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Purchase(ctx, "p1", "coin_for_coin", "k1")
	if !errors.Is(err, ErrCoinPackMisconfigured) {
		t.Fatalf("err = %v, want ErrCoinPackMisconfigured", err)
	}
	var appErr model.AppError
	if errors.As(purchaseError(err), &appErr) {
		t.Fatalf("mapped to client error %s, want a server error", appErr.Code)
	}
	if coin, gem := dbtest.Balance(t, db, "p1"); coin != 5_000 || gem != 100 {
		t.Fatalf("coin %d gem %d, want 5000, 100", coin, gem)
	}
}

// Migration 018 is rerun on every deploy: a rerun keeps admin edits and adds
// nothing; the down migration removes only unedited, never-bought packs.
func TestCoinExchangeMigrationIsIdempotent(t *testing.T) {
	svc, db := newCoinExchangeService(t)
	ctx := context.Background()
	readMigration := func(name string) string {
		sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(sql)
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE shop_offers SET quantity = 1200 WHERE offer_id = 'coin_pack_10'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, readMigration(coinExchangeMigration)); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	got := map[string][2]int{}
	rows, err := db.Pool.Query(ctx, `
		SELECT offer_id, price_amount, quantity FROM shop_offers
		WHERE offer_type = 'coin_pack' AND item_id = 'coin' AND price_currency = 'gem'
		  AND limit_per_player IS NULL AND is_active`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		var price, qty int
		if err := rows.Scan(&id, &price, &qty); err != nil {
			t.Fatal(err)
		}
		got[id] = [2]int{price, qty}
	}
	rows.Close()
	want := map[string][2]int{
		"coin_pack_10":  {10, 1200}, // admin edit kept
		"coin_pack_50":  {50, 5_000},
		"coin_pack_100": {100, 10_000},
		"coin_pack_500": {500, 50_000},
	}
	if len(got) != len(want) {
		t.Fatalf("coin packs = %v, want %v", got, want)
	}
	for id, w := range want {
		if got[id] != w {
			t.Fatalf("%s = %v, want %v", id, got[id], w)
		}
	}

	// Down: coin_pack_10 (edited) and coin_pack_50 (bought) stay
	dbtest.CreatePlayer(t, db, "p1", 0, 50)
	if _, err := svc.Purchase(ctx, "p1", "coin_pack_50", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, readMigration("018_coin_exchange_offers.down.sql")); err != nil {
		t.Fatalf("down: %v", err)
	}
	var left []string
	rows, err = db.Pool.Query(ctx, `SELECT offer_id FROM shop_offers WHERE offer_type = 'coin_pack' ORDER BY offer_id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		left = append(left, id)
	}
	rows.Close()
	if len(left) != 2 || left[0] != "coin_pack_10" || left[1] != "coin_pack_50" {
		t.Fatalf("after down = %v, want [coin_pack_10 coin_pack_50]", left)
	}
}
