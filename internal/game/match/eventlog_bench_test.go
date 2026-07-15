//go:build integration

package match

import (
	"context"
	"os"
	"testing"
	"time"

	"battle-squad/internal/shared/database"
)

func connectBenchDB(b *testing.B) *database.PostgresDB {
	b.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		b.Skip("POSTGRES_DSN not set, skipping integration benchmark")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := database.NewPostgresDB(ctx, dsn, 10, 2)
	if err != nil {
		b.Fatalf("failed to connect to postgres: %v", err)
	}
	return db
}

func BenchmarkEventLogInsertBatch(b *testing.B) {
	db := connectBenchDB(b)
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		matchID := "bench-match-" + time.Now().Format("150405.000")
		el := NewEventLogger(matchID, db)
		el.Start(ctx)

		for j := 0; j < 50; j++ {
			el.Log("BenchEvent", "player1", map[string]int{"seq": j})
		}

		time.Sleep(600 * time.Millisecond)
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}
