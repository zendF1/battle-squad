// Package dbtest gives integration tests a throwaway Postgres schema built from
// the real migration files. Tests are skipped unless TEST_POSTGRES_DSN is set.
package dbtest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/migrations"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrations are every up migration cmd/migrate applies (migrations.FS), in filename order.
func Migrations(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(migrations.FS, "*.up.sql")
	if err != nil || len(names) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	return names // fs.Glob sorts
}

// NewSchema applies the named migrations (e.g. "001_init_schema.up.sql") into a
// fresh schema, dropped when the test ends. maxConns <= 0 keeps the pgx default.
func NewSchema(t *testing.T, maxConns int32, migrations ...string) *database.PostgresDB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("it_%d", time.Now().UnixNano())

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
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
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, self, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(self), "..", "..", "..", "..", "migrations")
	for _, name := range migrations {
		sql, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("migration %s: %v", name, err)
		}
	}
	return &database.PostgresDB{Pool: pool}
}

// CreatePlayer inserts an account and profile with the given balances.
func CreatePlayer(t *testing.T, db *database.PostgresDB, playerID string, coin, gem int) {
	t.Helper()
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO accounts (account_id, account_type, status) VALUES ($1, 'guest', 'active')`, "acc_"+playerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO player_profiles (player_id, account_id, display_name, coin, gem) VALUES ($1, $2, $1, $3, $4)`, playerID, "acc_"+playerID, coin, gem); err != nil {
		t.Fatal(err)
	}
}

// Balance returns the player's coin and gem.
func Balance(t *testing.T, db *database.PostgresDB, playerID string) (coin, gem int) {
	t.Helper()
	if err := db.Pool.QueryRow(context.Background(), `SELECT coin, gem FROM player_profiles WHERE player_id = $1`, playerID).Scan(&coin, &gem); err != nil {
		t.Fatal(err)
	}
	return coin, gem
}
