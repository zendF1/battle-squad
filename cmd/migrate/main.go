// Command migrate brings the database to the current schema and reference game data (see
// internal/shared/migrate). The migrations and configs are embedded: the binary runs alone.
//
//	migrate                 apply pending migrations, seed missing reference rows, bootstrap the admin
//	migrate -status         list every migration file and whether it is applied
//	migrate -dry-run        list what a run would do, change nothing
//	migrate -allow-changed  accept applied files whose content changed (record the new checksum)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"battle-squad/configs"
	"battle-squad/internal/shared/config"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/migrate"
	"battle-squad/internal/shared/observability"
	"battle-squad/migrations"
)

func main() {
	status := flag.Bool("status", false, "list every migration file and whether it is applied")
	dryRun := flag.Bool("dry-run", false, "list the pending migrations and missing seed rows, change nothing")
	allowChanged := flag.Bool("allow-changed", false, "accept applied migration files whose checksum changed")
	flag.Parse()

	cfg := config.LoadConfig()
	observability.InitLogger(cfg.Env)
	log := observability.Log

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	maxConns := cfg.DBMaxConns
	if maxConns < 2 {
		maxConns = 2
	}
	db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN, maxConns, 0)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to Postgres")
	}
	defer db.Close()

	opts := migrate.Options{
		Migrations:    migrations.FS,
		Configs:       configs.FS,
		Production:    config.IsProduction(cfg.Env),
		AdminEmail:    os.Getenv("ADMIN_BOOTSTRAP_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
		AllowChanged:  *allowChanged,
	}

	if *status || *dryRun {
		plan, err := migrate.Inspect(ctx, db.Pool, opts)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to read the migration status")
		}
		printPlan(plan, *status)
		return
	}

	log.Info().Msg("migrating the database...")
	res, err := migrate.Run(ctx, db.Pool, opts)
	if err != nil {
		log.Fatal().Err(err).Strs("applied", res.Applied).Msg("migration failed")
	}
	log.Info().Int("migrations_applied", len(res.Applied)).Int64("seed_rows_inserted", res.Seeded).
		Str("admin", string(res.Admin)).Msg("database is up to date")
}

func printPlan(p migrate.Plan, all bool) {
	pending := 0
	for _, s := range p.States {
		switch {
		case s.Changed:
			fmt.Printf("CHANGED  %s (applied %s, the file differs since)\n", s.Name, s.AppliedAt.Format(time.RFC3339))
		case !s.Applied:
			pending++
			fmt.Printf("pending  %s\n", s.Name)
		case all:
			fmt.Printf("applied  %s (%s)\n", s.Name, s.AppliedAt.Format(time.RFC3339))
		}
	}
	fmt.Printf("%d of %d migration files pending\n", pending, len(p.States))
	if p.SeedMissing < 0 {
		fmt.Println("seed: missing reference rows are counted once the pending files are applied")
	} else {
		fmt.Printf("seed: %d missing reference rows to insert\n", p.SeedMissing)
	}
	if p.NoAdmin {
		fmt.Println("admin: no admin dashboard login yet (ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD create one)")
	}
}
