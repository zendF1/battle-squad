# Performance Improvements & Horizontal Scaling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Increase game server capacity by tuning connection pools, reducing terrain memory 8x with bitset, batching event log inserts, and adding multi-node deployment config.

**Architecture:** Config-driven pool sizes passed through to DB/Redis constructors. Terrain masks switch from `[]bool` to `[]uint64` bitset with helper functions. EventLogger buffers events and flushes in batches. Nginx reverse proxy with ip_hash for sticky WebSocket sessions.

**Tech Stack:** Go (pgx/v5, go-redis/v9), Docker, Nginx

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/shared/config/config.go` | Modify | Add pool size env vars |
| `internal/shared/database/postgres.go` | Modify | Accept pool params |
| `internal/shared/database/redis.go` | Modify | Accept pool params |
| `cmd/api/main.go` | Modify | Pass pool config |
| `cmd/game/main.go` | Modify | Pass pool config |
| `cmd/admin/main.go` | Modify | Pass pool config |
| `cmd/worker/main.go` | Modify | Pass pool config |
| `cmd/migrate/main.go` | Modify | Pass pool config |
| `internal/game/match/terrain.go` | Modify | Bitset masks |
| `internal/game/match/polygon.go` | Modify | Bitset scanline fill |
| `internal/game/match/eventlog.go` | Modify | Batch INSERT |
| `deploy/nginx.conf` | Create | Reverse proxy config |
| `docker-compose.yml` | Modify | Multi-node game servers |
| `docs/scaling-guide.md` | Create | Deployment guide |

---

### Task 1: Config — Add pool size env vars

**Files:**
- Modify: `internal/shared/config/config.go`

- [ ] **Step 1: Add pool config fields to Config struct and LoadConfig**

```go
// In Config struct, add after RedisDB:
	DBMaxConns    int
	DBMinConns    int
	RedisPoolSize int
	RedisMinIdle  int
```

In `LoadConfig()`, after `redisDB` parsing, add:

```go
	dbMaxConns := getEnvInt("DB_MAX_CONNS", 50)
	dbMinConns := getEnvInt("DB_MIN_CONNS", 10)
	redisPoolSize := getEnvInt("REDIS_POOL_SIZE", 100)
	redisMinIdle := getEnvInt("REDIS_MIN_IDLE", 20)
```

Add to the returned struct:

```go
		DBMaxConns:    dbMaxConns,
		DBMinConns:    dbMinConns,
		RedisPoolSize: redisPoolSize,
		RedisMinIdle:  redisMinIdle,
```

Add helper function:

```go
func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/shared/config/...`

- [ ] **Step 3: Commit**

```bash
git add internal/shared/config/config.go
git commit -m "feat: add DB/Redis pool size config via env vars"
```

---

### Task 2: Database — Accept pool params

**Files:**
- Modify: `internal/shared/database/postgres.go`
- Modify: `internal/shared/database/redis.go`

- [ ] **Step 1: Update NewPostgresDB signature and use params**

Replace the function:

```go
func NewPostgresDB(ctx context.Context, dsn string, maxConns, minConns int) (*PostgresDB, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres dsn: %w", err)
	}

	config.MaxConns = int32(maxConns)
	config.MinConns = int32(minConns)
	config.MaxConnIdleTime = 30 * time.Minute
	config.MaxConnLifetime = 1 * time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresDB{Pool: pool}, nil
}
```

- [ ] **Step 2: Update NewRedisClient signature and use params**

Replace the function:

```go
func NewRedisClient(addr, password string, db, poolSize, minIdle int) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     poolSize,
		MinIdleConns: minIdle,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &RedisClient{Client: client}, nil
}
```

- [ ] **Step 3: Update all callers**

**cmd/api/main.go** — change:
```go
db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN)
```
to:
```go
db, err := database.NewPostgresDB(ctx, cfg.PostgresDSN, cfg.DBMaxConns, cfg.DBMinConns)
```

And change:
```go
redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
```
to:
```go
redisClient, err := database.NewRedisClient(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.RedisPoolSize, cfg.RedisMinIdle)
```

Apply the same changes to:
- `cmd/game/main.go`
- `cmd/admin/main.go`
- `cmd/worker/main.go`
- `cmd/migrate/main.go`

- [ ] **Step 4: Verify build**

Run: `go build ./...`

- [ ] **Step 5: Commit**

```bash
git add internal/shared/database/postgres.go internal/shared/database/redis.go cmd/api/main.go cmd/game/main.go cmd/admin/main.go cmd/worker/main.go cmd/migrate/main.go
git commit -m "feat: configurable DB/Redis connection pool sizes (50/100 defaults)"
```

---

### Task 3: Terrain Bitset

**Files:**
- Modify: `internal/game/match/terrain.go`
- Modify: `internal/game/match/polygon.go`

- [ ] **Step 1: Replace Terrain struct and add bitset helpers**

In `terrain.go`, replace the Terrain struct:

```go
type Terrain struct {
	Width            int
	Height           int
	Mask             []uint64
	DestructibleMask []uint64
	Zones            []TerrainZone
}
```

Add helper functions after the struct:

```go
func maskSize(width, height int) int {
	return (width*height + 63) / 64
}

func bitIndex(x, y, width int) (int, uint64) {
	idx := y*width + x
	return idx / 64, uint64(1) << (idx % 64)
}

func getBit(mask []uint64, x, y, width int) bool {
	word, bit := bitIndex(x, y, width)
	return mask[word]&bit != 0
}

func setBit(mask []uint64, x, y, width int) {
	word, bit := bitIndex(x, y, width)
	mask[word] |= bit
}

func clearBit(mask []uint64, x, y, width int) {
	word, bit := bitIndex(x, y, width)
	mask[word] &^= bit
}
```

- [ ] **Step 2: Update NewTerrain allocation**

Change:
```go
	t := &Terrain{
		Width:            width,
		Height:           height,
		Mask:             make([]bool, width*height),
		DestructibleMask: make([]bool, width*height),
	}
```
to:
```go
	t := &Terrain{
		Width:            width,
		Height:           height,
		Mask:             make([]uint64, maskSize(width, height)),
		DestructibleMask: make([]uint64, maskSize(width, height)),
	}
```

- [ ] **Step 3: Update tile fill in NewTerrain**

Replace the square fallback fill (lines 76-83):
```go
				} else {
					for py := offsetY; py < offsetY+cs && py < height; py++ {
						for px := offsetX; px < offsetX+cs && px < width; px++ {
							setBit(t.Mask, px, py, width)
							if destructible {
								setBit(t.DestructibleMask, px, py, width)
							}
						}
					}
				}
```

- [ ] **Step 4: Update generateLegacyTerrain**

Replace the inner loop:
```go
		for y := 0; y < t.Height; y++ {
			if float64(y) >= terrainHeight {
				setBit(t.Mask, x, y, t.Width)
				setBit(t.DestructibleMask, x, y, t.Width)
			}
		}
```

- [ ] **Step 5: Update IsSolid**

Replace:
```go
	return t.Mask[iy*t.Width+ix]
```
with:
```go
	return getBit(t.Mask, ix, iy, t.Width)
```

- [ ] **Step 6: Update DestroyCircle**

Replace the inner check:
```go
			if dx*dx+dy*dy <= radius*radius {
				if getBit(t.Mask, x, y, t.Width) && getBit(t.DestructibleMask, x, y, t.Width) {
					clearBit(t.Mask, x, y, t.Width)
					destroyedAny = true
				}
			}
```

- [ ] **Step 7: Update GetLandingY**

Replace:
```go
		if t.Mask[y*t.Width+ix] {
```
with:
```go
		if getBit(t.Mask, ix, y, t.Width) {
```

- [ ] **Step 8: Update WalkTo**

Replace:
```go
			if t.Mask[y*t.Width+nextX] {
```
with:
```go
			if getBit(t.Mask, nextX, y, t.Width) {
```

- [ ] **Step 9: Update polygon.go scanlineFillPolygon**

Change signature from:
```go
func scanlineFillPolygon(
	mask []bool,
	destructibleMask []bool,
```
to:
```go
func scanlineFillPolygon(
	mask []uint64,
	destructibleMask []uint64,
```

Replace the inner fill loop:
```go
			for x := xStart; x < xEnd; x++ {
				px := offsetX + x
				py := offsetY + y
				if px >= 0 && px < width && py >= 0 && py < width*1000 {
					setBit(mask, px, py, width)
					if destructible {
						setBit(destructibleMask, px, py, width)
					}
				}
			}
```

Remove the old bounds check `idx < len(mask)` since `setBit` handles the indexing.

- [ ] **Step 10: Run existing tests**

Run: `go test ./internal/game/match/... -v`
Expected: All tests PASS (TestNewTerrainFromTiles, TestTerrainDestruction, TestLegacyTerrainFallback, TestPolygonTerrain, TestDamageCalculation, TestEloCalculation, TestProjectileSimulation)

- [ ] **Step 11: Commit**

```bash
git add internal/game/match/terrain.go internal/game/match/polygon.go
git commit -m "perf: replace terrain []bool masks with []uint64 bitset (8x memory reduction)"
```

---

### Task 4: Batch Event Logging

**Files:**
- Modify: `internal/game/match/eventlog.go`

- [ ] **Step 1: Rewrite EventLogger with batch buffer**

Replace the entire file:

```go
package match

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

const (
	eventBatchSize     = 50
	eventFlushInterval = 500 * time.Millisecond
)

type eventLogEntry struct {
	eventType string
	playerID  string
	data      json.RawMessage
}

type EventLogger struct {
	matchID string
	db      *database.PostgresDB
	events  chan eventLogEntry
	seq     int64
}

func NewEventLogger(matchID string, db *database.PostgresDB) *EventLogger {
	return &EventLogger{
		matchID: matchID,
		db:      db,
		events:  make(chan eventLogEntry, 256),
	}
}

func (el *EventLogger) Start(ctx context.Context) {
	go func() {
		buffer := make([]eventLogEntry, 0, eventBatchSize)
		ticker := time.NewTicker(eventFlushInterval)
		defer ticker.Stop()

		flush := func() {
			if len(buffer) == 0 {
				return
			}
			el.insertBatch(buffer)
			buffer = buffer[:0]
		}

		for {
			select {
			case entry, ok := <-el.events:
				if !ok {
					flush()
					return
				}
				buffer = append(buffer, entry)
				if len(buffer) >= eventBatchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-ctx.Done():
				// Drain remaining events
				for {
					select {
					case entry := <-el.events:
						buffer = append(buffer, entry)
					default:
						flush()
						return
					}
				}
			}
		}
	}()
}

func (el *EventLogger) Log(eventType, playerID string, data interface{}) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			observability.Log.Warn().Err(err).Str("eventType", eventType).Msg("eventlog: failed to marshal data")
			return
		}
		raw = b
	}

	entry := eventLogEntry{
		eventType: eventType,
		playerID:  playerID,
		data:      raw,
	}

	select {
	case el.events <- entry:
	default:
		observability.Log.Warn().Str("matchId", el.matchID).Str("eventType", eventType).Msg("eventlog: channel full, dropping event")
	}
}

func (el *EventLogger) insertBatch(entries []eventLogEntry) {
	if len(entries) == 0 {
		return
	}

	ctx := context.Background()

	// Build multi-row INSERT: INSERT INTO match_event_logs VALUES ($1,$2,$3,$4,$5,NOW()), ($6,...), ...
	var b strings.Builder
	b.WriteString("INSERT INTO match_event_logs (match_id, seq, event_type, player_id, data, created_at) VALUES ")

	args := make([]interface{}, 0, len(entries)*5)
	for i, entry := range entries {
		seq := atomic.AddInt64(&el.seq, 1)
		if i > 0 {
			b.WriteString(", ")
		}
		base := i * 5
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, CURRENT_TIMESTAMP)", base+1, base+2, base+3, base+4, base+5)
		args = append(args, el.matchID, seq, entry.eventType, entry.playerID, entry.data)
	}

	_, err := el.db.Pool.Exec(ctx, b.String(), args...)
	if err != nil {
		observability.Log.Error().Err(err).
			Str("matchId", el.matchID).
			Int("batchSize", len(entries)).
			Msg("eventlog: failed to insert batch")
	}
}
```

- [ ] **Step 2: Verify build and tests**

Run: `go build ./internal/game/match/...`
Run: `go test ./internal/game/match/... -v`
Expected: Build succeeds, all tests pass

- [ ] **Step 3: Commit**

```bash
git add internal/game/match/eventlog.go
git commit -m "perf: batch INSERT for match event logging (flush every 50 events or 500ms)"
```

---

### Task 5: Nginx Config & Docker Compose Multi-Node

**Files:**
- Create: `deploy/nginx.conf`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Create deploy/nginx.conf**

```nginx
upstream game_servers {
    ip_hash;
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
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_read_timeout 86400s;
        proxy_send_timeout 86400s;
    }

    location /healthz {
        proxy_pass http://game_servers;
    }
}
```

- [ ] **Step 2: Update docker-compose.yml**

Replace the `game` service and add nginx:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    container_name: battlesquad_postgres
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: battlesquad
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
    networks:
      - battlesquad_network

  redis:
    image: redis:7-alpine
    container_name: battlesquad_redis
    ports:
      - "6379:6379"
    volumes:
      - redis_data:/data
    networks:
      - battlesquad_network

  api:
    build:
      context: .
      dockerfile: Dockerfile.api
    ports:
      - "8080:8080"
    environment:
      - POSTGRES_DSN=postgres://postgres:postgres@postgres:5432/battlesquad?sslmode=disable
      - REDIS_ADDR=redis:6379
    depends_on:
      - postgres
      - redis
    networks:
      - battlesquad_network

  game1:
    build:
      context: .
      dockerfile: Dockerfile.game
    environment:
      - POSTGRES_DSN=postgres://postgres:postgres@postgres:5432/battlesquad?sslmode=disable
      - REDIS_ADDR=redis:6379
      - NODE_ID=game-1
    depends_on:
      - postgres
      - redis
    networks:
      - battlesquad_network

  game2:
    build:
      context: .
      dockerfile: Dockerfile.game
    environment:
      - POSTGRES_DSN=postgres://postgres:postgres@postgres:5432/battlesquad?sslmode=disable
      - REDIS_ADDR=redis:6379
      - NODE_ID=game-2
    depends_on:
      - postgres
      - redis
    networks:
      - battlesquad_network

  nginx:
    image: nginx:alpine
    ports:
      - "8081:8081"
    volumes:
      - ./deploy/nginx.conf:/etc/nginx/conf.d/default.conf:ro
    depends_on:
      - game1
      - game2
    networks:
      - battlesquad_network

  worker:
    build:
      context: .
      dockerfile: Dockerfile.worker
    environment:
      - POSTGRES_DSN=postgres://postgres:postgres@postgres:5432/battlesquad?sslmode=disable
      - REDIS_ADDR=redis:6379
    depends_on:
      - postgres
      - redis
    networks:
      - battlesquad_network

volumes:
  postgres_data:
  redis_data:

networks:
  battlesquad_network:
    driver: bridge
```

- [ ] **Step 3: Commit**

```bash
git add deploy/nginx.conf docker-compose.yml
git commit -m "infra: add Nginx reverse proxy and multi-node game server config"
```

---

### Task 6: Scaling Guide

**Files:**
- Create: `docs/scaling-guide.md`

- [ ] **Step 1: Write scaling guide**

```markdown
# Scaling Guide

## Architecture Overview

Battle Squad server gồm 3 process độc lập:
- **API Server** (:8080) — REST API, stateless, scale tự do
- **Game Server** (:8081) — WebSocket, stateful (rooms/matches in memory), cần sticky sessions
- **Worker** — Background jobs, chạy 1 instance

## Single Node (Development)

```bash
docker-compose up -d postgres redis
go run cmd/api/main.go
go run cmd/game/main.go
go run cmd/worker/main.go
```

## Multi-Node (Production)

### Game Server Scaling

Game server sử dụng actor model — mỗi room/match là 1 goroutine. Room chỉ tồn tại trên node tạo nó.

**Yêu cầu:**
- Mỗi node cần `NODE_ID` duy nhất (dùng cho matchmaker leader election)
- WebSocket connections phải sticky — cùng client luôn kết nối cùng node
- Nginx ip_hash đảm bảo sticky sessions

**Deploy:**
```bash
docker-compose up -d
```

Docker Compose mặc định chạy 2 game nodes (`game1`, `game2`) phía sau Nginx.

**Thêm node:** Thêm service trong `docker-compose.yml` và cập nhật `deploy/nginx.conf` upstream.

### Matchmaker Leader Election

Chỉ 1 game node chạy matchmaker tại mỗi thời điểm. Sử dụng Redis lock (`matchmaking:leader`) với TTL 10s. Node giữ lock sẽ chạy matching tick. Nếu node chết, lock tự expire và node khác lấy lại.

Không cần cấu hình gì — tự động hoạt động khi có nhiều nodes.

### Connection Pool Tuning

| Env Var | Default | Mô tả |
|---------|---------|-------|
| `DB_MAX_CONNS` | 50 | Max PostgreSQL connections per process |
| `DB_MIN_CONNS` | 10 | Min idle connections |
| `REDIS_POOL_SIZE` | 100 | Max Redis connections per process |
| `REDIS_MIN_IDLE` | 20 | Min idle Redis connections |

**Lưu ý:** Mỗi process có pool riêng. 3 game nodes × 50 DB conns = 150 connections tổng. PostgreSQL mặc định max 100 connections — cần tăng `max_connections` trong `postgresql.conf`.

### Capacity Estimates

| Config | Rooms đồng thời |
|--------|-----------------|
| 1 node, 4GB RAM, DB pool 50 | ~300 rooms |
| 2 nodes, 8GB RAM, DB pool 50 | ~600 rooms |
| 4 nodes, 16GB RAM, DB pool 100 | ~1200 rooms |

Bottleneck chính: PostgreSQL connections và RAM (terrain mask ~180KB per match sau bitset optimization).

### What Scales / What Doesn't

| Component | Scale? | Ghi chú |
|-----------|--------|---------|
| API Server | Horizontal | Stateless, thêm instances tùy ý |
| Game Server | Horizontal | Sticky sessions required, mỗi node cần NODE_ID |
| Worker | Single | 1 instance, chạy cron jobs |
| PostgreSQL | Vertical | Connection pooling, hoặc dùng PgBouncer |
| Redis | Vertical | Single instance đủ cho hầu hết cases |
```

- [ ] **Step 2: Commit**

```bash
git add docs/scaling-guide.md
git commit -m "docs: add horizontal scaling guide"
```

---

### Task 7: Final Verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... 2>&1 | tail -20`
Expected: All tests PASS

- [ ] **Step 2: Verify full build**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 3: Update .env.example with new vars**

Add to `.env.example`:

```
# Connection pool tuning
DB_MAX_CONNS=50
DB_MIN_CONNS=10
REDIS_POOL_SIZE=100
REDIS_MIN_IDLE=20
```

- [ ] **Step 4: Commit**

```bash
git add .env.example
git commit -m "docs: add pool config vars to .env.example"
```
