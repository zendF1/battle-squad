# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Battle Squad is a turn-based artillery/shooting game server written in Go. It consists of two separate server processes (API Server and Game Server) sharing PostgreSQL and Redis.

## Build & Run

```bash
# Start infrastructure (Postgres + Redis)
docker-compose up -d

# Run database migrations
go run cmd/migrate/main.go

# Run API server (REST on :8080)
go run cmd/api/main.go

# Run Game server (WebSocket on :8081)
go run cmd/game/main.go

# Run Admin dashboard (HTML + API on :9000)
go run cmd/admin/main.go

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/game/match/... -v

# Run a single test
go test ./internal/game/match/... -v -run TestDamageCalculation
```

## Configuration

All config is via environment variables with sensible defaults for local dev (see `internal/shared/config/config.go`). Key vars: `POSTGRES_DSN`, `REDIS_ADDR`, `JWT_SECRET`, `API_PORT` (default 8080), `GAME_PORT` (default 8081), `ADMIN_PORT` (default 9000), `APP_ENV`, `NODE_ID`.

## Architecture

**Four server binaries + a migration tool:**
- `cmd/api/` — REST API server (chi router). Handles auth, player profiles, economy, shop, IAP, gift codes, missions, ranking, moderation, app config.
- `cmd/game/` — WebSocket game server (gorilla/websocket). Handles real-time rooms, lobbies, matchmaking, and matches.
- `cmd/admin/` — Admin dashboard server (chi router, HTML templates). Game config CRUD, player management, matchmaking tuning.
- `cmd/worker/` — Background worker for async tasks.
- `cmd/migrate/` — Applies SQL migrations from `migrations/`.

**Why separate processes:** A bug or crash in the API server doesn't kill active matches, and vice versa. They can be deployed and scaled independently.

### Key packages under `internal/`

**`internal/shared/`** — Code shared by both servers:
- `config/` — Env-based config loading
- `database/` — Postgres (pgx/v5 pool) and Redis client wrappers
- `auth/` — JWT manager (sign/verify)
- `middleware/` — Chi middleware: auth, rate limiting, correlation ID, version check
- `model/` — Structured error codes and error response helpers
- `observability/` — Zerolog logger, health check endpoints (/healthz, /readyz, /livez)
- `circuitbreaker/`, `featureflag/`, `idempotency/` — Resilience patterns

**`internal/api/`** — API server domain modules, each following handler → service → repository pattern:
- `auth/` — Guest/provider login, JWT refresh, logout, link provider
- `player/` — Profile CRUD, account deletion
- `economy/` — Coin/Gem ledger (Credit/Debit within DB transactions)
- `inventory/`, `shop/`, `iap/`, `giftcode/` — Commerce
- `mission/`, `rank/`, `moderation/`, `appconfig/` — Game systems

**`internal/game/`** — Game server modules:
- `ws/` — WebSocket server, client read/write pumps, message envelope. Client has `RoomID` and `LobbyID` fields.
- `room/` — Room hub (registry with Redis sync) and room goroutines. Each room is a goroutine. Hub also implements `matchmaker.RoomCreator` to create battle rooms from matchmaking results.
- `lobby/` — Lobby room system for ranked matchmaking. Players create lobbies (max 2 per team), choose loadouts, then queue for matches. Lobby goroutines follow the same actor pattern as rooms.
- `matchmaker/` — Ranked matchmaking engine. Runs as a goroutine with Redis leader election (multi-node safe). Scans Redis sorted set queue every few seconds, matches entries by expanding rating range, fills bots after timeout. Config (tick interval, rating range, Elo params, bot difficulty) loaded from `game_settings` DB table.
- `match/` — Match engine. Each match runs as a goroutine with its own event loop, panic recovery, and watchdog timer (2 min idle → no-contest). Includes Elo rating calculation (`elo.go`) and smart bot AI (`bot_ai.go`) with rank-based difficulty.
- `gamedata/` — Loads static YAML configs from `configs/` at startup (characters, weapons, skills, items, maps)

**`internal/admin/`** — Admin dashboard: HTML template-based CRUD for game configs (characters, weapons, skills, items, maps, shop offers), player management (ban/unban), matchmaking config API endpoints, dev tools.

### Match lifecycle

**Regular PvP:** Room → all players ready → host starts → Match goroutine spawns → turn-based loop (20s per turn) → shoot/move/use items → projectile physics simulation → damage calculation → terrain destruction → win condition check → rewards → cleanup.

**Ranked 2v2:** CreateLobby → choose loadout (persisted to `player_loadouts`) → StartQueue → matchmaker finds opponent → creates battle room → match runs → Elo rating update → ReturnToLobby.

Key match events (WS): `Move`, `Shoot`, `UseItem`, `EndTurn`, `Reconnect`, `Leave` (client→server); `MatchStarted`, `TurnStarted`, `ProjectileResult`, `PlayerDamaged`, `MatchEnded`, `MatchStateSync` (server→client).

Key lobby/matchmaking events (WS): `CreateLobby`, `JoinLobby`, `LeaveLobby`, `UpdateLoadout`, `StartQueue`, `CancelQueue` (client→server); `LobbyUpdated`, `QueueStarted`, `QueueCancelled`, `MatchFound`, `ReturnToLobby` (server→client).

### Data layer

- **PostgreSQL** — All persistent data (accounts, players, inventory, transactions, match history, bans, etc.). Schema in `migrations/001_init_schema.up.sql`.
- **Redis** — Sessions, room registry (`rooms:active` hash), leaderboards (sorted sets), feature flags, idempotency keys, matchmaking queue (`matchmaking:queue:2v2` sorted set + entry hashes), matchmaker leader lock.

## Conventions

- Module pattern: each API domain has `handler.go` (HTTP handlers), `service.go` (business logic), `repository.go` (DB queries), `model.go` (types).
- Logging: use `observability.Log` (zerolog). Use `observability.FromContext(ctx)` inside request handlers for correlation ID propagation.
- Errors: use structured error codes from `internal/shared/model/errors.go` with `model.WriteError()`.
- Game data configs are YAML files in `configs/` loaded once at game server startup via `gamedata.LoadGameData("configs")`.
- Bot turns are handled by injecting events into the match event channel after a delay. Tutorial bots use `BotBrain` (idle/simple), ranked bots use `SmartBotBrain` (state-based AI with rank-tier difficulty from `game_settings`).
- Game server WebSocket uses a `CompositeWSHandler` that routes lobby events first, then falls through to room events.
- Matchmaking config (tick intervals, rating ranges, Elo K-factor, bot difficulty per tier) is stored in `game_settings` table and hot-reloaded every 30 seconds by the matchmaker.
- Room deletion triggers: match ends (via `MatchDone` channel), all players leave, or tutorial room user leaves.
