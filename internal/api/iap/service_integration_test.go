package iap

import (
	"context"
	"errors"
	"sync"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

func newIAPService(t *testing.T, mockStore bool) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql")
	_, err := db.Pool.Exec(context.Background(), `
		INSERT INTO iap_products (product_id, platform_sku_id, gem_amount, bonus_gem_amount, price_usd)
		VALUES ('gems_small', '{"android":"gem.small","ios":"gem.small"}', 100, 10, 0.99),
		       ('gems_large', '{"android":"gem.large","ios":"gem.large"}', 1000, 200, 9.99)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return NewService(NewRepository(db), economy.NewRepository(), db, mockStore), db
}

func TestVerifyReceiptRejectedWhenStoreNotConfigured(t *testing.T) {
	svc, db := newIAPService(t, false)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)

	_, err := svc.VerifyReceipt(context.Background(), "p1", "gems_small", "android", "any_token")
	if !errors.Is(err, ErrStoreNotConfigured) {
		t.Fatalf("err = %v, want ErrStoreNotConfigured", err)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 0 {
		t.Fatalf("gem = %d, want 0", gem)
	}
}

// Client mock tokens share a long prefix ("mock_token_<ms>"); each must still
// be its own store transaction.
func TestMockTokensWithSharedPrefixAreDistinct(t *testing.T) {
	svc, db := newIAPService(t, true)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	ctx := context.Background()

	for _, token := range []string{"mock_token_1760500000001", "mock_token_1760500000002"} {
		if _, err := svc.VerifyReceipt(ctx, "p1", "gems_small", "android", token); err != nil {
			t.Fatalf("%s: %v", token, err)
		}
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 220 {
		t.Fatalf("gem = %d, want 220", gem)
	}
}

func TestReplayedReceiptCreditsOnce(t *testing.T) {
	svc, db := newIAPService(t, true)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	dbtest.CreatePlayer(t, db, "p2", 0, 0)
	ctx := context.Background()

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.VerifyReceipt(ctx, "p1", "gems_small", "ios", "receipt-1")
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 110 {
		t.Fatalf("p1 gem = %d, want 110", gem)
	}

	if _, err := svc.VerifyReceipt(ctx, "p2", "gems_small", "ios", "receipt-1"); !errors.Is(err, ErrTransactionNotOwned) {
		t.Fatalf("p2 err = %v, want ErrTransactionNotOwned", err)
	}
	if _, gem := dbtest.Balance(t, db, "p2"); gem != 0 {
		t.Fatalf("p2 gem = %d, want 0", gem)
	}

	if _, err := svc.VerifyReceipt(ctx, "p1", "gems_large", "ios", "receipt-1"); err == nil {
		t.Fatal("replaying a receipt for another product succeeded")
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 110 {
		t.Fatalf("p1 gem after product mismatch = %d, want 110", gem)
	}
}
