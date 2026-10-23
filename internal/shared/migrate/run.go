package migrate

import (
	"context"
	"fmt"
	"io/fs"

	"battle-squad/internal/shared/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Options configure Run and Plan.
type Options struct {
	Migrations fs.FS // the *.up.sql files (migrations.FS)
	Configs    fs.FS // the reference game data YAML (configs.FS)
	Production bool  // config.IsProduction(APP_ENV)
	// The first admin dashboard login, created only while admin_users is empty
	AdminEmail, AdminPassword string
	AllowChanged              bool // see ApplyOptions
}

// Result is what Run changed.
type Result struct {
	Applied []string       // migration files applied, in order
	Seeded  int64          // reference rows inserted
	Admin   AdminBootstrap // the admin bootstrap outcome
}

// Run brings the database up to date, idempotently (a second run changes nothing):
//  1. the pending migration files, each once, in filename order;
//  2. the reference game data seed: missing rows only, so admin edits are never overwritten;
//  3. the first admin login from the bootstrap credentials.
//
// There are no dev-only fixtures: the migrations and the seed hold reference data only, the
// same in every environment. Everything runs on one connection holding the migrate lock.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (Result, error) {
	var res Result
	migs, err := Load(opts.Migrations)
	if err != nil {
		return res, err
	}
	conn, release, err := Lock(ctx, pool)
	if err != nil {
		return res, err
	}
	defer release()

	if res.Applied, err = ApplyMigrations(ctx, conn, migs, ApplyOptions{AllowChanged: opts.AllowChanged}); err != nil {
		return res, err
	}
	if res.Seeded, err = SeedAll(ctx, conn, opts.Configs); err != nil {
		return res, fmt.Errorf("seed reference data: %w", err)
	}
	if res.Admin, err = BootstrapAdmin(ctx, conn, opts.AdminEmail, opts.AdminPassword); err != nil {
		return res, fmt.Errorf("admin bootstrap: %w", err)
	}
	logAdmin(res.Admin, opts.Production)
	return res, nil
}

func logAdmin(a AdminBootstrap, production bool) {
	log := observability.Log
	switch {
	case a == AdminCreated:
		log.Info().Msg("created the first admin dashboard login (ADMIN_BOOTSTRAP_EMAIL)")
	case a == AdminMissing && production:
		log.Warn().Msg("no admin dashboard login exists and ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD are unset: nobody can sign in to cmd/admin until cmd/migrate runs with them")
	case a == AdminMissing:
		log.Info().Msg("no admin dashboard login (set ADMIN_BOOTSTRAP_EMAIL / ADMIN_BOOTSTRAP_PASSWORD to create one); outside production cmd/admin stays open without one")
	}
}

// Plan is what Run would do, without changing anything.
type Plan struct {
	States      []State
	SeedMissing int64 // rows the seed would insert; -1 when it can only tell after the pending files
	NoAdmin     bool  // admin_users is empty (or does not exist yet)
}

// Inspect reads the plan (-status, -dry-run).
func Inspect(ctx context.Context, pool *pgxpool.Pool, opts Options) (Plan, error) {
	var p Plan
	migs, err := Load(opts.Migrations)
	if err != nil {
		return p, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return p, err
	}
	defer conn.Release()
	if p.States, err = Status(ctx, conn, migs); err != nil {
		return p, err
	}
	p.SeedMissing = -1
	p.NoAdmin = true
	if len(Pending(p.States)) == 0 {
		if p.SeedMissing, err = SeedMissing(ctx, conn, opts.Configs); err != nil {
			return p, err
		}
		var count int
		if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
			return p, err
		}
		p.NoAdmin = count == 0
	}
	return p, nil
}
