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

	migrations := []string{
		filepath.Join("migrations", "001_init_schema.up.sql"),
		filepath.Join("migrations", "002_add_account_role.up.sql"),
		filepath.Join("migrations", "003_add_match_event_logs.up.sql"),
		filepath.Join("migrations", "004_admin_config_tables.up.sql"),
		filepath.Join("migrations", "005_add_loadouts_and_matchmaking.up.sql"),
		filepath.Join("migrations", "006_character_progression.up.sql"),
		filepath.Join("migrations", "007_map_editor.up.sql"),
		filepath.Join("migrations", "008_brick_border_v2.up.sql"),
		filepath.Join("migrations", "009_map_rank_tier.up.sql"),
	}

	for _, migrationPath := range migrations {
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			log.Fatal().Err(err).Str("path", migrationPath).Msg("failed to read migration file")
		}

		log.Info().Str("file", filepath.Base(migrationPath)).Msg("applying migration...")
		_, err = db.Pool.Exec(ctx, string(content))
		if err != nil {
			log.Fatal().Err(err).Str("file", filepath.Base(migrationPath)).Msg("failed to execute migration")
		}
	}

	log.Info().Msg("database schema migrated successfully!")
}
