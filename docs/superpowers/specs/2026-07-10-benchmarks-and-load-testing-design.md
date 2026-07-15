# Benchmarks & Load Testing

## Overview

Add comprehensive benchmarks and load testing to measure game server performance:

1. **Game logic benchmarks** — in-memory, pure Go, no infra needed
2. **DB benchmark** — batch INSERT throughput, needs Postgres
3. **WebSocket benchmark** — connection + message throughput, in-process server
4. **Load test CLI** — E2E simulate N players through full game flow

## 1. Game Logic Benchmarks

File: `internal/game/match/bench_test.go`

No build tags — runs with plain `go test -bench`.

### Benchmarks

- `BenchmarkSimulateProjectile` — fire 1 projectile at 45 degrees, power 80, compute full physics path until hit/OOB. Uses grassland_valley map with legacy terrain.
- `BenchmarkDestroyCircle` — destroy terrain in circle radius 30px at center of map. Pre-create terrain, benchmark only the DestroyCircle call.
- `BenchmarkNewTerrain` — create terrain from MapConfig. Two sub-benchmarks: `Legacy` (empty tiles, sine curves) and `GridV2` (filled tiles array with brick types).
- `BenchmarkIsSolid` — single point lookup on terrain bitset. Pre-create terrain, benchmark IsSolid at random coordinates.
- `BenchmarkWalkTo` — simulate player walking 200px horizontally on terrain. Pre-create terrain, benchmark WalkTo call.

### Setup pattern

```go
func BenchmarkXxx(b *testing.B) {
    terrain := NewTerrain(mapConfig)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // single operation
    }
}
```

## 2. Event Log Batch INSERT Benchmark

File: `internal/game/match/eventlog_bench_test.go`

Build tag: `//go:build integration`

Needs `POSTGRES_DSN` env var pointing to a running Postgres with `match_event_logs` table.

### Benchmarks

- `BenchmarkEventLogInsertBatch` — create EventLogger, log 50 events, wait for flush, measure throughput. Each iteration: start logger → log N events → cancel context → wait for drain.

### Setup

```go
func BenchmarkEventLogInsertBatch(b *testing.B) {
    dsn := os.Getenv("POSTGRES_DSN")
    if dsn == "" {
        b.Skip("POSTGRES_DSN not set")
    }
    db := connectDB(dsn)
    defer db.Close()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // create logger, log 50 events, flush
    }
}
```

## 3. WebSocket Benchmark

File: `internal/game/ws/bench_test.go`

Build tag: `//go:build integration`

Starts an in-process HTTP server with WebSocket upgrade (no auth for benchmark), connects with gorilla/websocket client.

### Benchmarks

- `BenchmarkWSConnect` — measure WebSocket handshake + upgrade time. Each iteration: dial → close.
- `BenchmarkWSMessageThroughput` — single connection, send+receive N messages as fast as possible. Echo handler for benchmark. Measure messages/sec.

### Setup

Start `httptest.NewServer` with a simple WebSocket echo handler in `TestMain` or benchmark setup.

## 4. Load Test CLI

File: `cmd/loadtest/main.go`

Standalone binary, runs against a live server.

### Flags

```
-server    string  WebSocket server URL (default "ws://localhost:8081/ws")
-api       string  API server URL (default "http://localhost:8080")
-players   int     Number of concurrent players (default 100)
-duration  duration  Test duration (default 60s)
```

### Player simulation flow

Each goroutine:
1. `POST /auth/guest-login` with random deviceInstallId → get JWT token
2. WebSocket connect with `Authorization: Bearer <token>`
3. Send `QuickPlay` event → enter tutorial room with idle bot
4. Loop until duration expires:
   - Wait for `TurnStarted` where it's our turn
   - Random action: 70% shoot (random angle 30-60, power 60-100), 30% move (random direction)
   - Send action, wait for response
5. Disconnect

### Report output

```
--- Load Test Results ---
Duration:           60s
Players:            100
Connections OK:     98
Connection Errors:  2
Messages Sent:      4523
Messages Received:  12847
Avg Latency:        12ms
P95 Latency:        45ms
Peak Concurrent:    98
Errors:             3
```

### Error handling

- Connection failures counted but don't stop test
- Player goroutine recovers from panics, logs error, continues
- Graceful shutdown on SIGINT

## 5. Makefile Targets

Add to existing `Makefile`:

```makefile
bench:
	go test -bench=. -benchmem -count=3 ./internal/game/match/...

bench-integration:
	go test -bench=. -benchmem -tags=integration ./internal/game/match/... ./internal/game/ws/...

loadtest:
	go run cmd/loadtest/main.go -players 100 -duration 60s
```

## Files to create/modify

| File | Action |
|------|--------|
| `internal/game/match/bench_test.go` | Create — game logic benchmarks |
| `internal/game/match/eventlog_bench_test.go` | Create — DB batch INSERT benchmark |
| `internal/game/ws/bench_test.go` | Create — WebSocket benchmarks |
| `cmd/loadtest/main.go` | Create — E2E load test CLI |
| `Makefile` | Modify — add bench/loadtest targets |
