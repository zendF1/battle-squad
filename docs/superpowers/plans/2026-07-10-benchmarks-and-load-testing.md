# Benchmarks & Load Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add comprehensive benchmarks for game logic, DB, WebSocket, and a standalone load test CLI to measure server capacity.

**Architecture:** Game logic benchmarks are pure Go (`go test -bench`). DB and WS benchmarks use `//go:build integration` tag and need infra. Load test CLI simulates N concurrent players through the full game flow against a live server.

**Tech Stack:** Go testing.B, gorilla/websocket client, net/http/httptest

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/game/match/bench_test.go` | Create | Game logic benchmarks (projectile, terrain, walk) |
| `internal/game/match/eventlog_bench_test.go` | Create | DB batch INSERT benchmark |
| `internal/game/ws/bench_test.go` | Create | WebSocket connect + message throughput |
| `cmd/loadtest/main.go` | Create | E2E load test CLI |
| `Makefile` | Modify | Add bench/loadtest targets |

---

### Task 1: Game Logic Benchmarks

**Files:**
- Create: `internal/game/match/bench_test.go`

- [ ] **Step 1: Create benchmark file**

```go
package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

var benchTerrain *Terrain
var benchMapConfig = gamedata.MapConfig{
	MapID:  "grassland_valley",
	Width:  1600,
	Height: 900,
}

func setupBenchTerrain() *Terrain {
	if benchTerrain == nil {
		benchTerrain = NewTerrain(benchMapConfig)
	}
	return benchTerrain
}

func BenchmarkNewTerrainLegacy(b *testing.B) {
	cfg := gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTerrain(cfg)
	}
}

func BenchmarkNewTerrainGridV2(b *testing.B) {
	tiles := make([][]int, 56)
	for r := 0; r < 56; r++ {
		tiles[r] = make([]int, 100)
		if r > 30 {
			for c := 0; c < 100; c++ {
				tiles[r][c] = 1
			}
		}
	}
	cfg := gamedata.MapConfig{
		MapID:      "bench_grid",
		GridWidth:  100,
		GridHeight: 56,
		CellSize:   16,
		Tiles:      tiles,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewTerrain(cfg)
	}
}

func BenchmarkIsSolid(b *testing.B) {
	t := setupBenchTerrain()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.IsSolid(float64(i%1600), float64(400+i%500))
	}
}

func BenchmarkDestroyCircle(b *testing.B) {
	cfg := gamedata.MapConfig{
		MapID:  "grassland_valley",
		Width:  1600,
		Height: 900,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t := NewTerrain(cfg)
		t.DestroyCircle(800, 600, 30)
	}
}

func BenchmarkWalkTo(b *testing.B) {
	t := setupBenchTerrain()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t.WalkTo(400, 550, 600)
	}
}

func BenchmarkSimulateProjectile(b *testing.B) {
	t := setupBenchTerrain()
	weapon := gamedata.WeaponConfig{
		WeaponID:         "basic_rocket",
		Damage:           60,
		ExplosionRadius:  40,
		TerrainDamage:    50,
		ProjectileWeight: 1.0,
		WindInfluence:    1.0,
		MultiHit:         1,
	}
	wind := WindState{Direction: 1, Power: 2.0}
	players := map[string]*BattlePlayerState{
		"p1": {PlayerID: "p1", TeamID: 1, Position: Vector2{X: 400, Y: 500}, IsAlive: true},
		"p2": {PlayerID: "p2", TeamID: 2, Position: Vector2{X: 1200, Y: 500}, IsAlive: true},
	}
	origin := Vector2{X: 400, Y: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SimulateProjectile("p1", 1, origin, 45.0, 80.0, weapon, wind, t, players, false)
	}
}
```

- [ ] **Step 2: Run benchmarks**

Run: `go test ./internal/game/match/... -bench=. -benchmem -count=1 -run=^$`
Expected: All 6 benchmarks run and report ns/op + allocs

- [ ] **Step 3: Commit**

```bash
git add internal/game/match/bench_test.go
git commit -m "bench: add game logic benchmarks (terrain, projectile, walk, destroy)"
```

---

### Task 2: Event Log DB Benchmark

**Files:**
- Create: `internal/game/match/eventlog_bench_test.go`

- [ ] **Step 1: Create integration benchmark file**

```go
//go:build integration

package match

import (
	"context"
	"os"
	"testing"
	"time"

	"battle-squad/internal/shared/database"
)

func connectBenchDB(b *testing.B) *database.PostgresDB {
	b.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		b.Skip("POSTGRES_DSN not set, skipping integration benchmark")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := database.NewPostgresDB(ctx, dsn, 10, 2)
	if err != nil {
		b.Fatalf("failed to connect to postgres: %v", err)
	}
	return db
}

func BenchmarkEventLogInsertBatch(b *testing.B) {
	db := connectBenchDB(b)
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		matchID := "bench-match-" + time.Now().Format("150405.000")
		el := NewEventLogger(matchID, db)
		el.Start(ctx)

		for j := 0; j < 50; j++ {
			el.Log("BenchEvent", "player1", map[string]int{"seq": j})
		}

		// Give flush time to complete
		time.Sleep(600 * time.Millisecond)
		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Verify it compiles (skipped without tag)**

Run: `go build ./internal/game/match/...`
Expected: Builds (file is excluded without integration tag)

Run: `go test ./internal/game/match/... -bench=BenchmarkEventLog -run=^$ -count=1`
Expected: No benchmarks run (build tag excludes the file)

- [ ] **Step 3: Commit**

```bash
git add internal/game/match/eventlog_bench_test.go
git commit -m "bench: add event log batch INSERT integration benchmark"
```

---

### Task 3: WebSocket Benchmarks

**Files:**
- Create: `internal/game/ws/bench_test.go`

- [ ] **Step 1: Create WebSocket benchmark file**

```go
//go:build integration

package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// echoHandler is a minimal WS handler for benchmarking
type echoHandler struct{}

func (h *echoHandler) HandleMessage(ctx context.Context, client *Client, msg Message) {
	client.Send <- msg
}

func (h *echoHandler) Unregister(client *Client) {}

func startBenchServer() (*httptest.Server, string) {
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := &Client{
			Conn:          conn,
			Send:          make(chan Message, 256),
			PlayerID:      "bench-player",
			WSHandHandler: &echoHandler{},
		}
		go client.WritePump()
		go client.ReadPump()
	})

	server := httptest.NewServer(handler)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func BenchmarkWSConnect(b *testing.B) {
	server, wsURL := startBenchServer()
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatalf("dial failed: %v", err)
		}
		conn.Close()
	}
}

func BenchmarkWSMessageRoundtrip(b *testing.B) {
	server, wsURL := startBenchServer()
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		b.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	msg := Message{Event: "ping", Data: json.RawMessage(`{"ts":1}`)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(msg)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		_, _, err := conn.ReadMessage()
		if err != nil {
			b.Fatalf("read failed: %v", err)
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/game/ws/...`
Expected: Builds (excluded without integration tag)

- [ ] **Step 3: Commit**

```bash
git add internal/game/ws/bench_test.go
git commit -m "bench: add WebSocket connect and message roundtrip benchmarks"
```

---

### Task 4: Load Test CLI

**Files:**
- Create: `cmd/loadtest/main.go`

- [ ] **Step 1: Create load test CLI**

```go
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var (
	serverURL string
	apiURL    string
	numPlayers int
	duration  time.Duration
)

type stats struct {
	connectOK     int64
	connectErr    int64
	msgSent       int64
	msgRecv       int64
	errors        int64
	latencies     []time.Duration
	latencyMu     sync.Mutex
}

func main() {
	flag.StringVar(&serverURL, "server", "ws://localhost:8081/ws", "WebSocket server URL")
	flag.StringVar(&apiURL, "api", "http://localhost:8080", "API server URL")
	flag.IntVar(&numPlayers, "players", 100, "Number of concurrent players")
	flag.DurationVar(&duration, "duration", 60*time.Second, "Test duration")
	flag.Parse()

	fmt.Printf("--- Battle Squad Load Test ---\n")
	fmt.Printf("Server:   %s\n", serverURL)
	fmt.Printf("API:      %s\n", apiURL)
	fmt.Printf("Players:  %d\n", numPlayers)
	fmt.Printf("Duration: %s\n", duration)
	fmt.Println()

	ctx, stop := signal.NotifyContext(nil, syscall.SIGINT, syscall.SIGTERM)
	_ = ctx
	defer stop()

	s := &stats{}
	var wg sync.WaitGroup
	deadline := time.Now().Add(duration)

	for i := 0; i < numPlayers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					atomic.AddInt64(&s.errors, 1)
				}
			}()
			runPlayer(id, deadline, s)
		}(i)
		// Stagger connections
		time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	}

	wg.Wait()
	printReport(s)
}

func guestLogin(playerID int) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"deviceInstallId": fmt.Sprintf("loadtest-device-%d-%d", playerID, time.Now().UnixNano()),
	})
	resp, err := http.Post(apiURL+"/auth/guest-login", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Token, nil
}

func runPlayer(id int, deadline time.Time, s *stats) {
	token, err := guestLogin(id)
	if err != nil {
		atomic.AddInt64(&s.connectErr, 1)
		return
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, _, err := websocket.DefaultDialer.Dial(serverURL, header)
	if err != nil {
		atomic.AddInt64(&s.connectErr, 1)
		return
	}
	atomic.AddInt64(&s.connectOK, 1)
	defer conn.Close()

	// Start QuickPlay
	qp, _ := json.Marshal(map[string]string{"event": "QuickPlay"})
	sendMsg(conn, "QuickPlay", nil, s)
	_ = qp

	// Read loop with actions
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for time.Now().Before(deadline) {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			atomic.AddInt64(&s.errors, 1)
			return
		}
		atomic.AddInt64(&s.msgRecv, 1)

		var msg struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		json.Unmarshal(raw, &msg)

		switch msg.Event {
		case "TurnStarted":
			var turn struct {
				CurrentPlayerID string `json:"currentPlayerId"`
			}
			json.Unmarshal(msg.Data, &turn)

			// Check if it's our turn (we don't know our playerID from WS, just try shooting)
			start := time.Now()
			if rand.Float64() < 0.7 {
				// Shoot
				angle := 30.0 + rand.Float64()*30.0
				power := 60.0 + rand.Float64()*40.0
				sendMsg(conn, "Shoot", map[string]interface{}{
					"angle":     angle,
					"power":     power,
					"actionMode": "weapon",
				}, s)
			} else {
				// Move
				dir := "right"
				if rand.Float64() < 0.5 {
					dir = "left"
				}
				sendMsg(conn, "Move", map[string]interface{}{
					"direction": dir,
					"targetX":   float64(200 + rand.Intn(1200)),
				}, s)
			}
			lat := time.Since(start)
			s.latencyMu.Lock()
			s.latencies = append(s.latencies, lat)
			s.latencyMu.Unlock()
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	}
}

func sendMsg(conn *websocket.Conn, event string, data interface{}, s *stats) {
	var rawData json.RawMessage
	if data != nil {
		rawData, _ = json.Marshal(data)
	}
	msg, _ := json.Marshal(map[string]interface{}{
		"event": event,
		"data":  rawData,
	})
	if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
		atomic.AddInt64(&s.errors, 1)
		return
	}
	atomic.AddInt64(&s.msgSent, 1)
}

func printReport(s *stats) {
	fmt.Println()
	fmt.Println("--- Load Test Results ---")
	fmt.Printf("Duration:           %s\n", duration)
	fmt.Printf("Players:            %d\n", numPlayers)
	fmt.Printf("Connections OK:     %d\n", atomic.LoadInt64(&s.connectOK))
	fmt.Printf("Connection Errors:  %d\n", atomic.LoadInt64(&s.connectErr))
	fmt.Printf("Messages Sent:      %d\n", atomic.LoadInt64(&s.msgSent))
	fmt.Printf("Messages Received:  %d\n", atomic.LoadInt64(&s.msgRecv))
	fmt.Printf("Errors:             %d\n", atomic.LoadInt64(&s.errors))

	s.latencyMu.Lock()
	lats := s.latencies
	s.latencyMu.Unlock()

	if len(lats) > 0 {
		sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
		var total time.Duration
		for _, l := range lats {
			total += l
		}
		avg := total / time.Duration(len(lats))
		p95 := lats[int(float64(len(lats))*0.95)]
		p99 := lats[int(float64(len(lats))*0.99)]
		fmt.Printf("Avg Latency:        %s\n", avg)
		fmt.Printf("P95 Latency:        %s\n", p95)
		fmt.Printf("P99 Latency:        %s\n", p99)
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./cmd/loadtest/...`
Expected: Builds successfully

- [ ] **Step 3: Commit**

```bash
git add cmd/loadtest/main.go
git commit -m "feat: add E2E load test CLI (cmd/loadtest)"
```

---

### Task 5: Makefile Targets

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Add bench and loadtest targets**

Add after the `clean` target:

```makefile

bench:
	go test -bench=. -benchmem -count=3 -run=^$$ ./internal/game/match/...

bench-integration:
	go test -bench=. -benchmem -count=1 -tags=integration -run=^$$ ./internal/game/match/... ./internal/game/ws/...

loadtest:
	go run ./cmd/loadtest -players 100 -duration 60s
```

Update the `.PHONY` line at the top to include the new targets:

```makefile
.PHONY: build-api build-game build-worker build-migrate build-all run-api run-game run-worker test test-cover lint migrate docker-up docker-down clean bench bench-integration loadtest
```

- [ ] **Step 2: Verify**

Run: `make bench`
Expected: Runs game logic benchmarks

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build: add bench, bench-integration, and loadtest Makefile targets"
```

---

### Task 6: Final Verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... 2>&1 | grep -E "^(ok|FAIL)"`
Expected: All PASS

- [ ] **Step 2: Run benchmarks**

Run: `go test -bench=. -benchmem -count=1 -run=^$ ./internal/game/match/...`
Expected: 6 benchmarks report results

- [ ] **Step 3: Verify build of all binaries**

Run: `go build ./...`
Expected: Clean build
