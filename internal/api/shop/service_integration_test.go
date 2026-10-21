package shop

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

func newShopService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, "001_init_schema.up.sql", "013_shop_purchase_idempotency.up.sql", "034_daily_missions.up.sql")
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO shop_offers (offer_id, item_id, offer_type, price_currency, price_amount, quantity, limit_per_player) VALUES
			('medkit_x1', 'medkit', 'item', 'coin', 100, 1, NULL),
			('medkit_once', 'medkit', 'item', 'coin', 100, 1, 1),
			('potion_x5_twice', 'potion', 'bundle', 'coin', 100, 5, 2),
			('unlock_ninja', 'ninja', 'character_unlock', 'gem', 50, 1, NULL)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(NewRepository(db), economy.NewRepository(), inventory.NewRepository(db), db), db
}

func countPurchases(t *testing.T, db *database.PostgresDB, playerID string) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM shop_purchases WHERE player_id = $1`, playerID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestPurchaseLimitHoldsUnderConcurrency(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Purchase(context.Background(), "p1", "medkit_once", fmt.Sprintf("key-%d", i))
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrLimitExceeded):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("successful purchases = %d, want 1", ok)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 9_900 {
		t.Fatalf("coin = %d, want 9900", coin)
	}
}

func TestPurchaseSameKeyChargesOnce(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 1_000, 0)

	const n = 5
	var wg sync.WaitGroup
	results := make([]*ShopPurchase, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.Purchase(context.Background(), "p1", "medkit_x1", "same-key")
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if results[i].PurchaseID != results[0].PurchaseID {
			t.Fatalf("request %d got purchase %s, want %s", i, results[i].PurchaseID, results[0].PurchaseID)
		}
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 900 {
		t.Fatalf("coin = %d, want 900", coin)
	}
	if got := countPurchases(t, db, "p1"); got != 1 {
		t.Fatalf("purchase rows = %d, want 1", got)
	}
}

func TestPurchaseFailureIsNotRememberedForKey(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 50, 0)
	ctx := context.Background()

	if _, err := svc.Purchase(ctx, "p1", "medkit_x1", "retry-key"); err == nil || err.Error() != "insufficient coin balance" {
		t.Fatalf("first attempt err = %v, want insufficient coin balance", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE player_profiles SET coin = 100 WHERE player_id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	p, err := svc.Purchase(ctx, "p1", "medkit_x1", "retry-key")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if p.PurchaseID == "" || p.CreatedAt.IsZero() {
		t.Fatalf("retry returned incomplete purchase: %+v", p)
	}
	var qty int
	if err := db.Pool.QueryRow(ctx, `SELECT quantity FROM inventory_items WHERE player_id = 'p1' AND item_id = 'medkit'`).Scan(&qty); err != nil {
		t.Fatal(err)
	}
	if qty != 1 {
		t.Fatalf("medkit quantity = %d, want 1", qty)
	}
}

func TestPurchaseKeyReusedForOtherOffer(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 1_000, 100)
	ctx := context.Background()

	if _, err := svc.Purchase(ctx, "p1", "medkit_x1", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Purchase(ctx, "p1", "unlock_ninja", "k"); !errors.Is(err, ErrIdempotencyKeyMismatch) {
		t.Fatalf("err = %v, want ErrIdempotencyKeyMismatch", err)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 100 {
		t.Fatalf("gem = %d, want 100", gem)
	}
}

func TestPurchaseOwnedCharacterIsRejectedWithoutCharge(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 100)
	ctx := context.Background()

	if _, err := svc.Purchase(ctx, "p1", "unlock_ninja", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Purchase(ctx, "p1", "unlock_ninja", "k2"); !errors.Is(err, ErrCharacterAlreadyOwned) {
		t.Fatalf("err = %v, want ErrCharacterAlreadyOwned", err)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 50 {
		t.Fatalf("gem = %d, want 50", gem)
	}
	if got := countPurchases(t, db, "p1"); got != 1 {
		t.Fatalf("purchase rows = %d, want 1", got)
	}
}

// With every connection held by a purchase tx, no purchase may need a second
// connection from the pool, or they all wait on each other.
func TestPurchaseDoesNotExhaustPool(t *testing.T) {
	svc, db := newShopService(t, 2)
	const n = 8
	for i := 0; i < n; i++ {
		dbtest.CreatePlayer(t, db, fmt.Sprintf("p%d", i), 1_000, 0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.Purchase(ctx, fmt.Sprintf("p%d", i), "medkit_once", "k")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("player %d: %v", i, err)
		}
	}
}

// LimitPerPlayer counts purchases, not granted quantity.
func TestPurchaseLimitCountsPurchasesNotQuantity(t *testing.T) {
	svc, db := newShopService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 1_000, 0)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := svc.Purchase(ctx, "p1", "potion_x5_twice", fmt.Sprintf("k%d", i)); err != nil {
			t.Fatalf("purchase %d: %v", i, err)
		}
	}
	if _, err := svc.Purchase(ctx, "p1", "potion_x5_twice", "k2"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("third purchase err = %v, want ErrLimitExceeded", err)
	}
}
