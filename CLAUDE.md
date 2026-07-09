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

**Server binaries:** `cmd/api/` (REST :8080), `cmd/game/` (WebSocket :8081), `cmd/admin/` (HTML dashboard :9000), `cmd/worker/`, `cmd/migrate/`. API and Game are separate processes — crash isolation, independent scaling.

**`internal/shared/`** — config, database (pgx/v5 + Redis), auth (JWT), middleware, model (error codes), observability (zerolog).

**`internal/api/`** — Domain modules following handler → service → repository pattern: auth, player, economy, inventory, shop, iap, giftcode, mission, rank, moderation, appconfig.

**`internal/game/`** — ws (WebSocket server), room (hub + room goroutines), lobby (ranked matchmaking lobbies), matchmaker (Redis leader election, rating-based matching, bot fallback), match (turn-based engine with physics, Elo, bot AI), gamedata (YAML/DB config loader).

**`internal/admin/`** — HTML template CRUD for game configs, player management, matchmaking tuning, map editor, brick type editor.

### Match lifecycle

**Regular PvP:** Room → ready → Match goroutine → turn-based loop (20s/turn) → shoot/move/items → physics → damage → terrain destruction → win check → rewards.

**Ranked 2v2:** CreateLobby → loadout → StartQueue → matchmaker matches → battle room → match → Elo update → ReturnToLobby.

### Data layer

- **PostgreSQL** — All persistent data. Schema in `migrations/001_init_schema.up.sql`.
- **Redis** — Sessions, room registry, leaderboards, feature flags, matchmaking queue, leader lock.

## Conventions

- Module pattern: each API domain has `handler.go` (HTTP handlers), `service.go` (business logic), `repository.go` (DB queries), `model.go` (types).
- Logging: use `observability.Log` (zerolog). Use `observability.FromContext(ctx)` inside request handlers for correlation ID propagation.
- Errors: use structured error codes from `internal/shared/model/errors.go` with `model.WriteError()`.
- Game data configs are YAML files in `configs/` loaded once at game server startup via `gamedata.LoadGameData("configs")`.
- Bot turns are handled by injecting events into the match event channel after a delay. Tutorial bots use `BotBrain` (idle/simple), ranked bots use `SmartBotBrain` (state-based AI with rank-tier difficulty from `game_settings`).
- Game server WebSocket uses a `CompositeWSHandler` that routes lobby events first, then falls through to room events.
- Matchmaking config (tick intervals, rating ranges, Elo K-factor, bot difficulty per tier) is stored in `game_settings` table and hot-reloaded every 30 seconds by the matchmaker.
- Room deletion triggers: match ends (via `MatchDone` channel), all players leave, or tutorial room user leaves.

## Map System (Grid v2)

Maps use a tile-based grid system. Each map has `gridWidth x gridHeight` cells of `cellSize` pixels.

**Key structures:**
- `config_maps` table: map_id (PK), name, grid_width, grid_height, cell_size, tiles (JSONB 2D array of brick_type_id integers), spawn_points (JSONB array of {x,y}), min_rank_tier, default_wind_power_range
- `config_brick_types` table: brick_type_id (SERIAL PK), name, image_id, destructible, border (JSONB polygon with top/right/bottom/left edges), color
- `gamedata.MapConfig` — loaded from DB via `LoadGameDataFromDB()`, includes `MinRankTier`
- `match/terrain.go` — `NewTerrain()` converts tiles + brick borders into pixel-level collision mask. Falls back to `generateLegacyTerrain()` if tiles are empty.

**Admin dashboard map editing:**
- Single editor page at `/maps/editor?id=xxx` (no separate edit form)
- Editor handles both metadata (name, grid size, wind, min_rank_tier, description) and canvas (tiles, spawn points)
- Canvas renders brick polygon shapes from border data, not just colored squares
- Save: `PUT /api/maps/save` accepts all fields in one JSON request
- Brick types managed at `/brick-types` with polygon border editor at `/brick-types/editor`

**Rank-based map selection:**
- Each map has `min_rank_tier` (bronze/silver/gold/platinum/diamond/master)
- Matchmaker filters maps: `randomMapForRating(maxRating)` selects from maps where `tierIndex(map.minRankTier) <= tierIndex(playerTier)`
- Uses highest-ranked player in the match to determine eligible maps
- Tier thresholds: bronze <1000, silver 1000-1199, gold 1200-1499, platinum 1500-1799, diamond 1800-2199, master 2200+

**Current status:**
- Server terrain engine supports both grid v2 (tiles) and legacy (sine curves) — auto-detects based on tiles data
- All 3 default maps have `tiles: []` (empty) → still using legacy terrain generation
- Maps need actual tile data created via admin editor to use grid v2
- `MatchStarted` WS event sends `MatchState` with `mapId` only — client must know how to render the map

## Migrations

- Migrator at `cmd/migrate/main.go` runs ALL migrations every time (no state tracking)
- All migrations MUST be idempotent: use `IF NOT EXISTS`, `ON CONFLICT DO NOTHING`, `ADD COLUMN IF NOT EXISTS`
- Never use `DROP TABLE` in up migrations — it destroys data on re-run
- Current migrations: 001-009. Next: 010

## Rank System

- 6 tiers: bronze (<1000), silver (1000), gold (1200), platinum (1500), diamond (1800), master (2200)
- `ratingToTier()` exists in both `room/hub.go` and `matchmaker/matchmaker.go` (duplicated to avoid circular deps)
- Elo rating with configurable K-factor, bot modifier (0.5x), floor at 0
- Season-based ranking with reward claims

## git
- Git commit messages: Record only the changes; do not sign (no Co-Authored-By).
