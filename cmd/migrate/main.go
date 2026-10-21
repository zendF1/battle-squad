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

	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN, cfg.DBMaxConns, cfg.DBMinConns)
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
		filepath.Join("migrations", "010_equipment_system.up.sql"),
		filepath.Join("migrations", "011_equipment_slot_unique.up.sql"),
		filepath.Join("migrations", "012_bot_hit_chance.up.sql"),
		filepath.Join("migrations", "013_shop_purchase_idempotency.up.sql"),
		filepath.Join("migrations", "014_default_shop_catalog.up.sql"),
		filepath.Join("migrations", "015_match_history_rating_change.up.sql"),
		filepath.Join("migrations", "016_default_equipment_catalog.up.sql"),
		filepath.Join("migrations", "017_equipment_purchase_idempotency.up.sql"),
		filepath.Join("migrations", "018_coin_exchange_offers.up.sql"),
		filepath.Join("migrations", "019_equipment_design_levels.up.sql"),
		filepath.Join("migrations", "020_gun_weapon_names.up.sql"),
		filepath.Join("migrations", "021_big_numbers_balance.up.sql"),
		filepath.Join("migrations", "022_equipment_sell_rate.up.sql"),
		filepath.Join("migrations", "023_character_identities.up.sql"),
		filepath.Join("migrations", "024_stone_purchase_idempotency.up.sql"),
		filepath.Join("migrations", "025_careful_upgrade.up.sql"),
		filepath.Join("migrations", "026_milestones.up.sql"),
		filepath.Join("migrations", "027_titles.up.sql"),
		filepath.Join("migrations", "028_skill_redesign.up.sql"),
		filepath.Join("migrations", "029_weapon_craters_halved.up.sql"),
		filepath.Join("migrations", "030_rain_tune.up.sql"),
		filepath.Join("migrations", "031_chain_cap.up.sql"),
		filepath.Join("migrations", "032_faster_shells.up.sql"),
		filepath.Join("migrations", "033_chain_one_bomb_less.up.sql"),
		filepath.Join("migrations", "034_daily_missions.up.sql"),
		filepath.Join("migrations", "035_ranked_2v2.up.sql"),
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
