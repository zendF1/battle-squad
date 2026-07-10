package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var (
	serverURL  string
	apiURL     string
	numPlayers int
	duration   time.Duration
)

type stats struct {
	connectOK  int64
	connectErr int64
	msgSent    int64
	msgRecv    int64
	errors     int64
	latencies  []time.Duration
	latencyMu  sync.Mutex
}

func main() {
	flag.StringVar(&serverURL, "server", "ws://localhost:9091/ws", "WebSocket server URL")
	flag.StringVar(&apiURL, "api", "http://localhost:9090", "API server URL")
	flag.IntVar(&numPlayers, "players", 100, "Number of concurrent players")
	flag.DurationVar(&duration, "duration", 60*time.Second, "Test duration")
	flag.Parse()

	fmt.Printf("--- Battle Squad Load Test ---\n")
	fmt.Printf("Server:   %s\n", serverURL)
	fmt.Printf("API:      %s\n", apiURL)
	fmt.Printf("Players:  %d\n", numPlayers)
	fmt.Printf("Duration: %s\n", duration)
	fmt.Println()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
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
			runPlayer(ctx, id, deadline, s)
		}(i)
		time.Sleep(time.Duration(50+rand.Intn(150)) * time.Millisecond)
	}

	wg.Wait()
	printReport(s)
}

func guestLogin(apiBase string, playerID int) (string, error) {
	// Retry up to 3 times with backoff (rate limiter may reject)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(500+rand.Intn(1000)) * time.Millisecond)
		}
		body, _ := json.Marshal(map[string]string{
			"deviceInstallId": fmt.Sprintf("loadtest-device-%d-%d", playerID, time.Now().UnixNano()),
		})
		resp, err := http.Post(apiBase+"/auth/guest-login", "application/json", bytes.NewReader(body))
		if err != nil {
			continue
		}

		if resp.StatusCode == 429 {
			resp.Body.Close()
			continue
		}

		var result struct {
			Token string `json:"accessToken"`
		}
		err = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()
		if err != nil {
			continue
		}
		if result.Token == "" {
			continue
		}
		return result.Token, nil
	}
	return "", fmt.Errorf("login failed after 3 retries")
}

func runPlayer(ctx context.Context, id int, deadline time.Time, s *stats) {
	token, err := guestLogin(apiURL, id)
	if err != nil {
		atomic.AddInt64(&s.connectErr, 1)
		fmt.Printf("[player %d] login failed: %v\n", id, err)
		return
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(serverURL, header)
	if err != nil {
		atomic.AddInt64(&s.connectErr, 1)
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		fmt.Printf("[player %d] ws dial failed (status %d): %v\n", id, status, err)
		return
	}
	atomic.AddInt64(&s.connectOK, 1)
	defer conn.Close()

	// Handle server pings to keep connection alive
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	conn.SetPingHandler(func(msg string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(msg), time.Now().Add(5*time.Second))
	})

	// Start QuickPlay
	sendMsg(conn, "QuickPlay", nil, s)

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, raw, err := conn.ReadMessage()
		if err != nil {
			atomic.AddInt64(&s.errors, 1)
			if !time.Now().After(deadline) {
				fmt.Printf("[player %d] read error: %v\n", id, err)
			}
			return
		}
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		atomic.AddInt64(&s.msgRecv, 1)

		var msg struct {
			Event string          `json:"event"`
			Data  json.RawMessage `json:"data"`
		}
		json.Unmarshal(raw, &msg)

		switch msg.Event {
		case "TurnStarted":
			start := time.Now()
			if rand.Float64() < 0.7 {
				angle := 30.0 + rand.Float64()*30.0
				power := 60.0 + rand.Float64()*40.0
				sendMsg(conn, "Shoot", map[string]interface{}{
					"angle":      angle,
					"power":      power,
					"actionMode": "weapon",
				}, s)
			} else {
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

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
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
		p95idx := int(float64(len(lats)) * 0.95)
		p99idx := int(float64(len(lats)) * 0.99)
		if p95idx >= len(lats) {
			p95idx = len(lats) - 1
		}
		if p99idx >= len(lats) {
			p99idx = len(lats) - 1
		}
		fmt.Printf("Avg Latency:        %s\n", avg)
		fmt.Printf("P95 Latency:        %s\n", lats[p95idx])
		fmt.Printf("P99 Latency:        %s\n", lats[p99idx])
	}
}
