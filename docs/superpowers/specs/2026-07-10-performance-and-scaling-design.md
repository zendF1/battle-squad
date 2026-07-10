# Performance Improvements & Horizontal Scaling

## Overview

Four changes to improve game server capacity and enable multi-node deployment:

1. **Connection pool tuning** — DB pool 25→50, Redis pool 50→100, configurable via env vars
2. **Terrain bitset** — Replace `[]bool` masks with `[]uint64` bitset, reduce memory 8x per match
3. **Batch event logging** — Buffer match events, flush in batches instead of single INSERT per event
4. **Horizontal scaling guide** — Nginx + Docker Compose config for multi-node game server deployment

## 1. Connection Pool Tuning

### Config changes

Add env vars to `config.go`:

```
DB_MAX_CONNS     (default: 50)
DB_MIN_CONNS     (default: 10)
REDIS_POOL_SIZE  (default: 100)
REDIS_MIN_IDLE   (default: 20)
```

Add fields to `Config` struct: `DBMaxConns int`, `DBMinConns int`, `RedisPoolSize int`, `RedisMinIdle int`.

### Database

`postgres.go` — accept config params, use them instead of hardcoded values:

```go
config.MaxConns = int32(cfg.DBMaxConns)   // was 25
config.MinConns = int32(cfg.DBMinConns)   // was 5
```

Signature change: `NewPostgresDB(ctx, dsn string)` → `NewPostgresDB(ctx, dsn string, maxConns, minConns int)`.

### Redis

`redis.go` — accept config params:

```go
PoolSize:     cfg.RedisPoolSize,  // was 50
MinIdleConns: cfg.RedisMinIdle,   // was 10
```

Signature change: `NewRedisClient(addr, password string, db int)` → `NewRedisClient(addr, password string, db, poolSize, minIdle int)`.

### Callers

All `cmd/*/main.go` files that create DB/Redis connections need to pass the new params from config.

## 2. Terrain Bitset

### Current

`terrain.go`:
```go
Mask             []bool  // 1 byte per pixel
DestructibleMask []bool  // 1 byte per pixel
```

Map 1600x900 = 1,440,000 bools = ~2.88MB per match (2 arrays).

### New

Replace with packed `[]uint64`:

```go
Mask             []uint64  // 1 bit per pixel
DestructibleMask []uint64  // 1 bit per pixel
```

Map 1600x900 = 1,440,000 bits = 22,500 uint64s = ~360KB per match (2 arrays). **8x reduction**.

### Helper functions

Add to `terrain.go`:

```go
func bitIndex(x, y, width int) (int, uint64) {
    idx := y*width + x
    return idx / 64, uint64(1) << (idx % 64)
}

func (t *Terrain) getBit(mask []uint64, x, y int) bool {
    word, bit := bitIndex(x, y, t.Width)
    return mask[word]&bit != 0
}

func (t *Terrain) setBit(mask []uint64, x, y int) {
    word, bit := bitIndex(x, y, t.Width)
    mask[word] |= bit
}

func (t *Terrain) clearBit(mask []uint64, x, y int) {
    word, bit := bitIndex(x, y, t.Width)
    mask[word] &^= bit
}
```

### Methods to update

All methods that access `Mask[idx]` or `DestructibleMask[idx]` need to use the helper functions:

- `NewTerrain` — tile fill and legacy terrain generation
- `IsSolid` — `getBit(t.Mask, ix, iy)`
- `DestroyCircle` — `getBit` + `clearBit`
- `GetLandingY` — `getBit(t.Mask, ix, y)`
- `WalkTo` — `getBit(t.Mask, nextX, y)`
- `generateLegacyTerrain` — `setBit`
- `polygon.go: scanlineFillPolygon` — `setBit` on both masks

Allocation: `make([]uint64, (width*height+63)/64)` instead of `make([]bool, width*height)`.

### Tests

Existing terrain tests (`terrain_test.go`, `physics_test.go`) must still pass — they test via public API (`IsSolid`, `DestroyCircle`, `NewTerrain`), so no test changes needed if the API stays the same.

## 3. Batch Event Logging

### Current

`eventlog.go` — each event does 1 `INSERT INTO match_event_logs` immediately. A busy match (20+ turns, 4 players) can produce 100+ events = 100+ DB round-trips.

### New

Buffer events and flush in batches:

```go
type EventLogger struct {
    matchID    string
    db         *database.PostgresDB
    events     chan eventLogEntry
    seq        int64
    buffer     []eventLogEntry  // new: batch buffer
    bufferSize int              // new: max batch size before flush (default: 50)
    flushTimer *time.Ticker     // new: flush interval (default: 500ms)
}
```

### Flush logic

In the `Start` goroutine:

```
loop:
  select:
    case entry from events channel → append to buffer, if len(buffer) >= bufferSize → flush
    case tick from flushTimer → if len(buffer) > 0 → flush
    case ctx.Done → drain channel into buffer → flush → return
```

### Batch INSERT

```go
func (el *EventLogger) flush() {
    if len(el.buffer) == 0 { return }

    // Build batch query: INSERT INTO match_event_logs VALUES ($1,$2,$3,$4,$5,NOW()), ($6,...), ...
    // Or use pgx.Batch for multiple statements
    // Assign sequential seq numbers from atomic counter

    el.buffer = el.buffer[:0]
}
```

Use a single multi-row INSERT for efficiency. pgx supports `CopyFrom` for bulk inserts but a multi-VALUES INSERT is simpler and sufficient for batches of 50.

### Config

- `batchSize: 50` — flush when buffer reaches this
- `flushInterval: 500ms` — flush timer
- Hardcoded constants (not env vars — these are internal tuning params)

## 4. Horizontal Scaling Guide

### Files

- Create `docs/scaling-guide.md` — deployment guide
- Create `deploy/nginx.conf` — Nginx reverse proxy config
- Modify `docker-compose.yml` — add scaling profile

### Nginx config

```nginx
upstream game_servers {
    ip_hash;  # sticky sessions for WebSocket
    server game1:8081;
    server game2:8081;
}

server {
    listen 8081;
    location /ws {
        proxy_pass http://game_servers;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

`ip_hash` ensures the same client IP always hits the same game server — important because rooms and matches only exist in memory on the node that created them.

### Docker Compose additions

Add Nginx service and multiple game server instances:

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "8081:8081"
    volumes:
      - ./deploy/nginx.conf:/etc/nginx/conf.d/default.conf
    depends_on:
      - game1
      - game2

  game1:
    build: { dockerfile: Dockerfile.game }
    environment:
      - NODE_ID=game-1
    # no port exposure — Nginx handles it

  game2:
    build: { dockerfile: Dockerfile.game }
    environment:
      - NODE_ID=game-2
```

### Scaling guide content

- How multi-node works: each game server is independent, matchmaker uses Redis leader election (only 1 active leader), rooms/matches live on the node that created them
- Sticky sessions requirement: WebSocket connections must stay on the same node
- What scales: game servers (rooms, matches, WebSocket connections)
- What doesn't scale: PostgreSQL and Redis are shared (single instance or managed service)
- NODE_ID: must be unique per instance, used for matchmaker leader election
- Adding more nodes: add to nginx upstream + docker-compose, no code changes needed

## Files to modify

| File | Change |
|------|--------|
| `internal/shared/config/config.go` | Add pool config fields + env vars |
| `internal/shared/database/postgres.go` | Accept pool params |
| `internal/shared/database/redis.go` | Accept pool params |
| `cmd/api/main.go` | Pass pool config |
| `cmd/game/main.go` | Pass pool config |
| `cmd/admin/main.go` | Pass pool config |
| `cmd/worker/main.go` | Pass pool config |
| `cmd/migrate/main.go` | Pass pool config |
| `internal/game/match/terrain.go` | Bitset mask |
| `internal/game/match/polygon.go` | Bitset scanline fill |
| `internal/game/match/eventlog.go` | Batch INSERT |
| `docker-compose.yml` | Multi-node services |
| `deploy/nginx.conf` | Create: reverse proxy config |
| `docs/scaling-guide.md` | Create: deployment guide |
