# CLAUDE.md

Go turn-based artillery game server: `cmd/api` (REST), `cmd/game` (WebSocket), `cmd/admin`, `cmd/worker`, `cmd/migrate`; shared Postgres + Redis.

## Rules

- API modules: handler → service → repository, types in `model.go`
- Errors: codes in `internal/shared/model/errors.go` + `model.WriteError()`. Logging in handlers: `observability.FromContext(ctx)`
- Mutating player resources: lock rows `FOR UPDATE` inside the tx and check `RowsAffected`; never query via `Pool` while holding a tx (pool deadlock)
- Rank tiers, divisions and seasons live only in `internal/shared/ranked`

## Migrations

- `cmd/migrate/main.go` reruns ALL files from a hardcoded list — register new files there
- Must be idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`); never `DROP TABLE` in up migrations

## Git

- Commit messages: changes only, no Co-Authored-By
