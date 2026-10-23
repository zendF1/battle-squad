# CLAUDE.md

Go turn-based artillery game server: `cmd/api` (REST), `cmd/game` (WebSocket), `cmd/admin`, `cmd/worker`, `cmd/migrate`; shared Postgres + Redis.

## Rules

- API modules: handler → service → repository, types in `model.go`
- Errors: codes in `internal/shared/model/errors.go` + `model.WriteError()`. Logging in handlers: `observability.FromContext(ctx)`
- Mutating player resources: lock rows `FOR UPDATE` inside the tx and check `RowsAffected`; never query via `Pool` while holding a tx (pool deadlock)
- Rank tiers, divisions and seasons live only in `internal/shared/ranked`

## Migrations

- `cmd/migrate` embeds `migrations/*.up.sql` (filename order, each once via `schema_migrations`), then seeds missing rows from `configs/*.yaml` + Go defaults (`internal/shared/migrate`); never edit an applied file, add a new one
- Must be idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`; an untracked DB reruns all once); never `DROP TABLE` in up migrations
- Reference rows a migration adds or changes: keep `configs/*.yaml` / seed defaults at the current values too

## Git

- Commit messages: changes only, no Co-Authored-By
