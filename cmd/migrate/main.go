package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

func main() {
	cfg := config.LoadConfig()
	observability.InitLogger(cfg.Env)
	log := observability.Log

	log.Info().Msg("starting database migrations...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	// Read migration file
	migrationPath := filepath.Join("migrations", "001_init_schema.up.sql")
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		log.Fatal().Err(err).Str("path", migrationPath).Msg("failed to read migration file")
	}

	log.Info().Msg("applying 001_init_schema.up.sql...")
	_, err = db.Pool.Exec(ctx, string(content))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to execute migrations SQL script")
	}

	log.Info().Msg("database schema migrated successfully!")
}
