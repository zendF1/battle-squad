package room

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newReservePool connects to TEST_POSTGRES_DSN with a throwaway schema holding
// minimal inventory tables, dropped when the test ends.
func newReservePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("it_reserve_%d", time.Now().UnixNano())

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, fmt.Sprintf(`
		CREATE SCHEMA %[1]s;
		CREATE TABLE %[1]s.inventory_items (
			player_id TEXT, item_id TEXT, quantity INT NOT NULL, expires_at TIMESTAMPTZ,
			PRIMARY KEY (player_id, item_id));
		CREATE TABLE %[1]s.inventory_reservations (
			reservation_id TEXT PRIMARY KEY, player_id TEXT, match_id TEXT, item_id TEXT,
			quantity INT NOT NULL, status TEXT NOT NULL);
	`, schema)); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", schema))
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestReserveItemTxConcurrentMatches(t *testing.T) {
	pool := newReservePool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_items (player_id, item_id, quantity) VALUES ('p1', 'medkit', 1)`); err != nil {
		t.Fatal(err)
	}

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txA.Rollback(ctx)
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer txB.Rollback(ctx)

	var pidB int
	if err := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); err != nil {
		t.Fatal(err)
	}

	// A reserves the only medkit and holds the lock
	if err := reserveItemTx(ctx, txA, "p1", "matchA", "medkit", 1); err != nil {
		t.Fatalf("first reservation failed: %v", err)
	}

	// B starts while A is uncommitted and must block on the inventory lock
	errB := make(chan error, 1)
	go func() { errB <- reserveItemTx(ctx, txB, "p1", "matchB", "medkit", 1) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := pool.QueryRow(ctx,
			`SELECT COALESCE(wait_event_type = 'Lock', false) FROM pg_stat_activity WHERE pid = $1`, pidB,
		).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second reservation never blocked on the inventory lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-errB; err == nil {
		txB.Commit(ctx)
		var reserved int
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE status = 'reserved'`).Scan(&reserved)
		t.Fatalf("second match reserved the same medkit (%d reservations for 1 item)", reserved)
	}
}

func TestReserveItemTxShortageIsLoadoutError(t *testing.T) {
	pool := newReservePool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_items (player_id, item_id, quantity) VALUES ('p1', 'power_shot', 1)`); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		itemID string
		count  int
	}{
		{"medkit", 1},     // not owned
		{"power_shot", 2}, // owned, not enough
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = reserveItemTx(ctx, tx, "p1", "m1", tc.itemID, tc.count)
		tx.Rollback(ctx)
		var loadoutErr *LoadoutError
		if !errors.As(err, &loadoutErr) || loadoutErr.PlayerID != "p1" {
			t.Fatalf("%s x%d: err = %v, want LoadoutError for p1", tc.itemID, tc.count, err)
		}
	}
}
