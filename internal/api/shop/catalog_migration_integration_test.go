package shop

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/inventory"
	"battle-squad/internal/shared/database/dbtest"
)

// The default catalog migration is rerun on every deploy: a rerun must keep
// admin edits and add nothing.
func TestDefaultCatalogMigrationIsIdempotent(t *testing.T) {
	const file = "014_default_shop_catalog.up.sql"
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "013_shop_purchase_idempotency.up.sql", file)
	ctx := context.Background()

	if _, err := db.Pool.Exec(ctx, `UPDATE shop_offers SET price_amount = 999 WHERE offer_id = 'item_medkit_x3'`); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	var items, unlocks, products, medkitPrice int
	if err := db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE offer_type = 'item' AND quantity = 3 AND price_currency = 'coin' AND limit_per_player IS NULL),
		       COUNT(*) FILTER (WHERE offer_type = 'character_unlock' AND limit_per_player = 1),
		       (SELECT COUNT(*) FROM iap_products WHERE is_active),
		       (SELECT price_amount FROM shop_offers WHERE offer_id = 'item_medkit_x3')
		FROM shop_offers WHERE is_active`).Scan(&items, &unlocks, &products, &medkitPrice); err != nil {
		t.Fatal(err)
	}
	if items != 8 || unlocks != 6 || products != 3 {
		t.Fatalf("items %d unlocks %d products %d, want 8, 6, 3", items, unlocks, products)
	}
	if medkitPrice != 999 {
		t.Fatalf("rerun overwrote the edited price: %d", medkitPrice)
	}

	// A default unlock offer can be bought once
	svc := NewService(NewRepository(db), economy.NewRepository(), inventory.NewRepository(db), db)
	dbtest.CreatePlayer(t, db, "p1", 0, 600)
	if _, err := svc.Purchase(ctx, "p1", "unlock_spark_gem", "k1"); err != nil {
		t.Fatal(err)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 300 {
		t.Fatalf("gem %d, want 300", gem)
	}
	if _, err := svc.Purchase(ctx, "p1", "unlock_spark_gem", "k2"); err != ErrLimitExceeded {
		t.Fatalf("second unlock: %v, want ErrLimitExceeded", err)
	}
}
