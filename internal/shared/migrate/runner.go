// Package migrate brings a database to the current schema and reference game data: the
// migration files (applied once each, tracked in schema_migrations), the reference-data seed
// (configs/*.yaml and the Go defaults, missing rows only) and the first admin login. One path
// for every environment: production runs what dev and CI run.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"battle-squad/internal/shared/observability"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// lockKey is the pg_advisory_lock key that keeps two cmd/migrate runs from interleaving.
const lockKey int64 = 0x62735f6d6967 // "bs_mig"

const createTracking = `CREATE TABLE IF NOT EXISTS schema_migrations (
    filename TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
)`

// Migration is one up file.
type Migration struct {
	Name     string // e.g. 001_init_schema.up.sql
	Checksum string // sha256 hex of SQL
	SQL      string
}

// Load reads every *.up.sql file at the root of fsys (migrations.FS), in filename order.
func Load(fsys fs.FS) ([]Migration, error) {
	names, err := fs.Glob(fsys, "*.up.sql")
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, errors.New("no *.up.sql migration files")
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, n := range names {
		raw, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		out = append(out, Migration{Name: path.Base(n), Checksum: hex.EncodeToString(sum[:]), SQL: string(raw)})
	}
	return out, nil
}

// Names are the files' names, in order.
func Names(migs []Migration) []string {
	out := make([]string, len(migs))
	for i, m := range migs {
		out[i] = m.Name
	}
	return out
}

// State is one migration file against the database.
type State struct {
	Migration
	Applied   bool
	AppliedAt time.Time
	Changed   bool // applied with another checksum (the file was edited afterwards)
}

// Pending are the states not applied yet.
func Pending(states []State) []State {
	var out []State
	for _, s := range states {
		if !s.Applied {
			out = append(out, s)
		}
	}
	return out
}

// Changed are the applied states whose file changed since.
func Changed(states []State) []State {
	var out []State
	for _, s := range states {
		if s.Changed {
			out = append(out, s)
		}
	}
	return out
}

// querier is a *pgxpool.Conn or pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Status compares migs with schema_migrations (every file pending when the table is missing).
func Status(ctx context.Context, q querier, migs []Migration) ([]State, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, err
	}
	type rec struct {
		sum string
		at  time.Time
	}
	applied := map[string]rec{}
	if exists {
		rows, err := q.Query(ctx, `SELECT filename, checksum, applied_at FROM schema_migrations`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var name string
			var r rec
			if err := rows.Scan(&name, &r.sum, &r.at); err != nil {
				rows.Close()
				return nil, err
			}
			applied[name] = r
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	out := make([]State, len(migs))
	for i, m := range migs {
		out[i] = State{Migration: m}
		if r, ok := applied[m.Name]; ok {
			out[i].Applied, out[i].AppliedAt, out[i].Changed = true, r.at, r.sum != m.Checksum
		}
	}
	return out, nil
}

// ApplyOptions tune ApplyMigrations.
type ApplyOptions struct {
	// AllowChanged accepts applied files whose checksum changed: the new checksum is recorded,
	// the file is not rerun. Without it a changed file is a hard error.
	AllowChanged bool
}

// ApplyMigrations applies every pending file in order, each in its own transaction with its
// schema_migrations row. A database migrated before the tracking table existed has every file
// pending: they all rerun once (each is idempotent) and get recorded. It returns the applied
// names. conn must hold the migrate lock (Lock).
func ApplyMigrations(ctx context.Context, conn *pgxpool.Conn, migs []Migration, opts ApplyOptions) ([]string, error) {
	log := observability.Log
	if _, err := conn.Exec(ctx, createTracking); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}
	states, err := Status(ctx, conn, migs)
	if err != nil {
		return nil, err
	}
	if changed := Changed(states); len(changed) > 0 {
		names := Names(stateMigrations(changed))
		if !opts.AllowChanged {
			return nil, fmt.Errorf("applied migration files changed since they ran (%s): add a new migration instead, or rerun with -allow-changed to accept them", strings.Join(names, ", "))
		}
		for _, s := range changed {
			log.Warn().Str("file", s.Name).Msg("accepting a changed applied migration (-allow-changed): recording its new checksum, not rerunning it")
			if _, err := conn.Exec(ctx, `UPDATE schema_migrations SET checksum = $2 WHERE filename = $1`, s.Name, s.Checksum); err != nil {
				return nil, err
			}
		}
	}
	pending := Pending(states)
	if len(pending) == len(states) && len(states) > 0 {
		var legacy bool
		if err := conn.QueryRow(ctx, `SELECT to_regclass('accounts') IS NOT NULL`).Scan(&legacy); err != nil {
			return nil, err
		}
		if legacy {
			log.Info().Int("files", len(pending)).Msg("existing database without migration tracking: rerunning every (idempotent) migration once and recording it")
		}
	}
	var applied []string
	for _, s := range pending {
		start := time.Now()
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, s.SQL); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename, checksum) VALUES ($1, $2)`, s.Name, s.Checksum)
			return err
		}); err != nil {
			return applied, fmt.Errorf("migration %s: %w", s.Name, err)
		}
		applied = append(applied, s.Name)
		log.Info().Str("file", s.Name).Dur("took", time.Since(start)).Msg("applied migration")
	}
	return applied, nil
}

func stateMigrations(states []State) []Migration {
	out := make([]Migration, len(states))
	for i, s := range states {
		out[i] = s.Migration
	}
	return out
}

// Lock takes a pool connection holding the migrate advisory lock (waiting for another run to
// finish); release calls the unlock and gives the connection back.
func Lock(ctx context.Context, pool *pgxpool.Pool) (*pgxpool.Conn, func(), error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, nil, err
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		conn.Release()
		return nil, nil, fmt.Errorf("migrate lock: %w", err)
	}
	return conn, func() {
		conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, lockKey)
		conn.Release()
	}, nil
}
