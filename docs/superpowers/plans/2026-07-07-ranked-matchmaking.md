# Ranked Matchmaking System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an automatic ranked 2v2 matchmaking system with lobby rooms, Redis queue, Elo rating, smart bot AI, and admin-configurable settings.

**Architecture:** Matchmaker goroutine in the game server ticks every few seconds, scanning a Redis sorted set for queue entries with nearby ratings. Players prepare in lobby rooms (select character/items persisted to DB), then enter the queue. Matched players are placed into battle rooms. Post-match, players return to their lobby. Smart bots fill empty slots after timeout.

**Tech Stack:** Go, PostgreSQL (pgx/v5), Redis (go-redis/v9), gorilla/websocket, chi router

**Spec:** `docs/superpowers/specs/2026-07-07-ranked-matchmaking-design.md`

---

## File Structure

```
New files:
  migrations/002_add_loadouts_and_matchmaking.up.sql
  migrations/002_add_loadouts_and_matchmaking.down.sql
  internal/game/lobby/model.go          — LobbyRoom struct, LobbyState, payloads
  internal/game/lobby/lobby.go          — LobbyRoom goroutine event loop
  internal/game/lobby/hub.go            — LobbyHub (registry, Redis sync)
  internal/game/lobby/handler.go        — WebSocket event handler
  internal/game/matchmaker/model.go     — QueueEntry, config structs
  internal/game/matchmaker/queue.go     — Redis queue operations
  internal/game/matchmaker/matchmaker.go — Matchmaker goroutine, tick loop, matching
  internal/game/match/bot_ai.go         — Smart Bot AI (state-based decisions)
  internal/game/match/elo.go            — Elo rating calculation

Modified files:
  cmd/game/main.go                      — Wire lobby hub, matchmaker, composite handler
  internal/game/ws/client.go            — Add LobbyID field
  internal/game/match/reward.go         — Use Elo for 2v2 ranked, bot modifier
  internal/game/match/match.go          — Pass lobby mapping, signal return-to-lobby
  internal/game/room/room.go            — Add LobbyMapping field, ReturnToLobby on match end
  internal/game/room/model.go           — Add LobbyMapping to RoomState
  internal/admin/server.go              — Add matchmaking config routes
  internal/admin/handlers_matchmaking.go — Admin handlers for matchmaking/elo/bot config
  internal/admin/repository.go          — Add game_settings get/upsert for JSON configs
```

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/002_add_loadouts_and_matchmaking.up.sql`
- Create: `migrations/002_add_loadouts_and_matchmaking.down.sql`

- [ ] **Step 1: Create up migration**

```sql
-- migrations/002_add_loadouts_and_matchmaking.up.sql

-- Player loadout (character + items selection, shared across all modes)
CREATE TABLE IF NOT EXISTS player_loadouts (
    player_id    VARCHAR(64) PRIMARY KEY REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    character_id VARCHAR(64) NOT NULL DEFAULT 'rookie',
    items        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Seed matchmaking config into game_settings
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('matchmaking', '{
        "tickInterval": 3,
        "baseRatingRange": 100,
        "expandInterval": 10,
        "expandStep": 50,
        "maxRatingRange": 300,
        "maxWaitTime": 60,
        "botRatingModifier": 0.5,
        "partyRatingStrategy": "max",
        "weightedRatio": 0.7
    }', 'json', 'Matchmaking queue configuration', 'matchmaking'),
    ('elo', '{
        "kFactor": 32,
        "ratingFloor": 0,
        "defaultRating": 1000
    }', 'json', 'Elo rating configuration', 'matchmaking'),
    ('bot_difficulty', '{
        "tiers": {
            "bronze":   {"accuracyError": 15, "powerError": 12, "decisionNoise": 30, "useItemChance": 0.3, "movementSmart": 0.3},
            "silver":   {"accuracyError": 12, "powerError": 10, "decisionNoise": 25, "useItemChance": 0.4, "movementSmart": 0.4},
            "gold":     {"accuracyError": 9,  "powerError": 8,  "decisionNoise": 20, "useItemChance": 0.55, "movementSmart": 0.55},
            "platinum": {"accuracyError": 6,  "powerError": 5,  "decisionNoise": 15, "useItemChance": 0.7, "movementSmart": 0.7},
            "diamond":  {"accuracyError": 4,  "powerError": 3,  "decisionNoise": 8,  "useItemChance": 0.85, "movementSmart": 0.85},
            "master":   {"accuracyError": 2,  "powerError": 2,  "decisionNoise": 5,  "useItemChance": 0.9, "movementSmart": 0.95}
        }
    }', 'json', 'Bot difficulty per rank tier', 'matchmaking')
ON CONFLICT (key) DO NOTHING;
```

- [ ] **Step 2: Create down migration**

```sql
-- migrations/002_add_loadouts_and_matchmaking.down.sql
DROP TABLE IF EXISTS player_loadouts;
DELETE FROM game_settings WHERE key IN ('matchmaking', 'elo', 'bot_difficulty');
```

- [ ] **Step 3: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration applies successfully, tables created

- [ ] **Step 4: Verify**

Run: `psql -c "SELECT * FROM player_loadouts LIMIT 1;" && psql -c "SELECT key FROM game_settings WHERE category = 'matchmaking';"`
Expected: Empty loadouts table, 3 matchmaking settings rows

- [ ] **Step 5: Commit**

```bash
git add migrations/002_add_loadouts_and_matchmaking.up.sql migrations/002_add_loadouts_and_matchmaking.down.sql
git commit -m "feat: add player_loadouts table and matchmaking config seeds"
```

---

### Task 2: Matchmaking Config Models & Loading

**Files:**
- Create: `internal/game/matchmaker/model.go`

- [ ] **Step 1: Create matchmaker model with config structs**

```go
// internal/game/matchmaker/model.go
package matchmaker

import (
	"context"
	"encoding/json"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type MatchmakingConfig struct {
	TickInterval       int     `json:"tickInterval"`
	BaseRatingRange    int     `json:"baseRatingRange"`
	ExpandInterval     int     `json:"expandInterval"`
	ExpandStep         int     `json:"expandStep"`
	MaxRatingRange     int     `json:"maxRatingRange"`
	MaxWaitTime        int     `json:"maxWaitTime"`
	BotRatingModifier  float64 `json:"botRatingModifier"`
	PartyRatingStrategy string `json:"partyRatingStrategy"`
	WeightedRatio      float64 `json:"weightedRatio"`
}

type EloConfig struct {
	KFactor       int `json:"kFactor"`
	RatingFloor   int `json:"ratingFloor"`
	DefaultRating int `json:"defaultRating"`
}

type BotTierConfig struct {
	AccuracyError  float64 `json:"accuracyError"`
	PowerError     float64 `json:"powerError"`
	DecisionNoise  int     `json:"decisionNoise"`
	UseItemChance  float64 `json:"useItemChance"`
	MovementSmart  float64 `json:"movementSmart"`
}

type BotDifficultyConfig struct {
	Tiers map[string]BotTierConfig `json:"tiers"`
}

type QueueEntry struct {
	EntryID      string   `json:"entryId"`
	LobbyID      string   `json:"lobbyId"`
	PlayerIDs    []string `json:"playerIds"`
	Rating       int      `json:"rating"`
	PlayerRatings map[string]int    `json:"playerRatings"`
	PlayerChars  map[string]string  `json:"playerChars"`
	PlayerItems  map[string][]string `json:"playerItems"`
	PlayerNames  map[string]string  `json:"playerNames"`
	TeamSize     int      `json:"teamSize"`
	QueuedAt     int64    `json:"queuedAt"`
	NodeID       string   `json:"nodeId"`
}

func DefaultMatchmakingConfig() MatchmakingConfig {
	return MatchmakingConfig{
		TickInterval:       3,
		BaseRatingRange:    100,
		ExpandInterval:     10,
		ExpandStep:         50,
		MaxRatingRange:     300,
		MaxWaitTime:        60,
		BotRatingModifier:  0.5,
		PartyRatingStrategy: "max",
		WeightedRatio:      0.7,
	}
}

func DefaultEloConfig() EloConfig {
	return EloConfig{
		KFactor:       32,
		RatingFloor:   0,
		DefaultRating: 1000,
	}
}

func DefaultBotDifficultyConfig() BotDifficultyConfig {
	return BotDifficultyConfig{
		Tiers: map[string]BotTierConfig{
			"bronze":   {AccuracyError: 15, PowerError: 12, DecisionNoise: 30, UseItemChance: 0.3, MovementSmart: 0.3},
			"silver":   {AccuracyError: 12, PowerError: 10, DecisionNoise: 25, UseItemChance: 0.4, MovementSmart: 0.4},
			"gold":     {AccuracyError: 9, PowerError: 8, DecisionNoise: 20, UseItemChance: 0.55, MovementSmart: 0.55},
			"platinum": {AccuracyError: 6, PowerError: 5, DecisionNoise: 15, UseItemChance: 0.7, MovementSmart: 0.7},
			"diamond":  {AccuracyError: 4, PowerError: 3, DecisionNoise: 8, UseItemChance: 0.85, MovementSmart: 0.85},
			"master":   {AccuracyError: 2, PowerError: 2, DecisionNoise: 5, UseItemChance: 0.9, MovementSmart: 0.95},
		},
	}
}

func LoadConfigFromDB(db *database.PostgresDB, key string, dest interface{}) error {
	var value string
	err := db.Pool.QueryRow(context.Background(),
		`SELECT value FROM game_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(value), dest)
}

func LoadMatchmakingConfig(db *database.PostgresDB) MatchmakingConfig {
	cfg := DefaultMatchmakingConfig()
	if err := LoadConfigFromDB(db, "matchmaking", &cfg); err != nil {
		observability.Log.Warn().Err(err).Msg("using default matchmaking config")
	}
	return cfg
}

func LoadEloConfig(db *database.PostgresDB) EloConfig {
	cfg := DefaultEloConfig()
	if err := LoadConfigFromDB(db, "elo", &cfg); err != nil {
		observability.Log.Warn().Err(err).Msg("using default elo config")
	}
	return cfg
}

func LoadBotDifficultyConfig(db *database.PostgresDB) BotDifficultyConfig {
	cfg := DefaultBotDifficultyConfig()
	if err := LoadConfigFromDB(db, "bot_difficulty", &cfg); err != nil {
		observability.Log.Warn().Err(err).Msg("using default bot difficulty config")
	}
	return cfg
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/matchmaker/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/matchmaker/model.go
git commit -m "feat: add matchmaker config models and DB loading"
```

---

### Task 3: Add LobbyID to WebSocket Client

**Files:**
- Modify: `internal/game/ws/client.go`

- [ ] **Step 1: Add LobbyID field to Client struct**

In `internal/game/ws/client.go`, add `LobbyID` field to the `Client` struct after `RoomID`:

```go
type Client struct {
	Conn          *websocket.Conn
	Send          chan Message
	PlayerID      string
	AccountID     string
	RoomID        string
	LobbyID       string
	WSHandHandler HandlerInterface
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/ws/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/ws/client.go
git commit -m "feat: add LobbyID field to WebSocket Client"
```

---

### Task 4: Lobby Room — Model & Struct

**Files:**
- Create: `internal/game/lobby/model.go`

- [ ] **Step 1: Create lobby model**

```go
// internal/game/lobby/model.go
package lobby

type LobbyState struct {
	LobbyID      string        `json:"lobbyId"`
	HostPlayerID string        `json:"hostPlayerId"`
	Members      []LobbyMember `json:"members"`
	Status       string        `json:"status"` // preparing, in_queue, in_match
}

type LobbyMember struct {
	PlayerID    string   `json:"playerId"`
	DisplayName string   `json:"displayName"`
	CharacterID string   `json:"characterId"`
	Items       []string `json:"items"`
	Rating      int      `json:"rating"`
	Tier        string   `json:"tier"`
}

type UpdateLoadoutPayload struct {
	CharacterID *string  `json:"characterId,omitempty"`
	Items       []string `json:"items,omitempty"`
}

type InvitePayload struct {
	PlayerID string `json:"playerId"`
}

type JoinLobbyPayload struct {
	LobbyID string `json:"lobbyId"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/lobby/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/lobby/model.go
git commit -m "feat: add lobby room models"
```

---

### Task 5: Lobby Room — Hub (Registry)

**Files:**
- Create: `internal/game/lobby/hub.go`

- [ ] **Step 1: Create lobby hub**

```go
// internal/game/lobby/hub.go
package lobby

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

type LobbyHub struct {
	sync.RWMutex
	lobbies map[string]*LobbyRoom
	db      *database.PostgresDB
	redis   *database.RedisClient
	nodeID  string
}

func NewLobbyHub(db *database.PostgresDB, redis *database.RedisClient, nodeID string) *LobbyHub {
	return &LobbyHub{
		lobbies: make(map[string]*LobbyRoom),
		db:      db,
		redis:   redis,
		nodeID:  nodeID,
	}
}

func (h *LobbyHub) CreateLobby(ctx context.Context, hostPlayerID, hostDisplayName string, rating int, tier string) (*LobbyRoom, error) {
	h.Lock()
	defer h.Unlock()

	// Check if host already has a lobby
	for _, l := range h.lobbies {
		if l.State.HostPlayerID == hostPlayerID {
			return nil, errors.New("you already have a lobby")
		}
	}

	lobbyID := generateID()

	// Load host loadout from DB
	charID, items := loadPlayerLoadout(ctx, h.db, hostPlayerID)

	state := LobbyState{
		LobbyID:      lobbyID,
		HostPlayerID: hostPlayerID,
		Members: []LobbyMember{
			{
				PlayerID:    hostPlayerID,
				DisplayName: hostDisplayName,
				CharacterID: charID,
				Items:       items,
				Rating:      rating,
				Tier:        tier,
			},
		},
		Status: "preparing",
	}

	lobby := NewLobbyRoom(lobbyID, state, h)
	h.lobbies[lobbyID] = lobby

	go lobby.Run()

	return lobby, nil
}

func (h *LobbyHub) FindLobby(lobbyID string) (*LobbyRoom, error) {
	h.RLock()
	defer h.RUnlock()

	lobby, exists := h.lobbies[lobbyID]
	if !exists {
		return nil, errors.New("lobby not found")
	}
	return lobby, nil
}

func (h *LobbyHub) FindLobbyByPlayer(playerID string) *LobbyRoom {
	h.RLock()
	defer h.RUnlock()

	for _, l := range h.lobbies {
		for _, m := range l.State.Members {
			if m.PlayerID == playerID {
				return l
			}
		}
	}
	return nil
}

func (h *LobbyHub) UnregisterLobby(lobbyID string) {
	h.Lock()
	delete(h.lobbies, lobbyID)
	h.Unlock()
}

func loadPlayerLoadout(ctx context.Context, db *database.PostgresDB, playerID string) (string, []string) {
	var charID string
	var itemsJSON []byte
	err := db.Pool.QueryRow(ctx,
		`SELECT character_id, items FROM player_loadouts WHERE player_id = $1`, playerID).
		Scan(&charID, &itemsJSON)
	if err != nil {
		return "rookie", []string{}
	}
	var items []string
	if err := json.Unmarshal(itemsJSON, &items); err != nil {
		return charID, []string{}
	}
	return charID, items
}

func generateID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(b)
}
```

- [ ] **Step 2: Add missing import**

The `loadPlayerLoadout` function uses `json.Unmarshal`, so add `"encoding/json"` to the import block.

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/game/lobby/...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/game/lobby/hub.go
git commit -m "feat: add lobby hub registry"
```

---

### Task 6: Lobby Room — Event Loop

**Files:**
- Create: `internal/game/lobby/lobby.go`

- [ ] **Step 1: Create lobby room with event loop**

```go
// internal/game/lobby/lobby.go
package lobby

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/observability"
)

type lobbyEvent struct {
	client *ws.Client
	msg    ws.Message
	ctx    context.Context
}

type LobbyRoom struct {
	ID      string
	State   LobbyState
	Clients map[string]*ws.Client
	Events  chan lobbyEvent
	hub     *LobbyHub
	ctx     context.Context
	cancel  context.CancelFunc
}

func NewLobbyRoom(lobbyID string, state LobbyState, hub *LobbyHub) *LobbyRoom {
	ctx, cancel := context.WithCancel(context.Background())
	return &LobbyRoom{
		ID:      lobbyID,
		State:   state,
		Clients: make(map[string]*ws.Client),
		Events:  make(chan lobbyEvent, 64),
		hub:     hub,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (l *LobbyRoom) Run() {
	defer func() {
		if err := recover(); err != nil {
			observability.Log.Error().Interface("panic", err).Str("lobbyId", l.ID).Msg("lobby goroutine panic")
		}
		l.hub.UnregisterLobby(l.ID)
	}()

	idleTicker := time.NewTicker(30 * time.Minute)
	defer idleTicker.Stop()

	for {
		select {
		case ev := <-l.Events:
			l.handleEvent(ev)
		case <-idleTicker.C:
			if len(l.Clients) == 0 {
				observability.Log.Info().Str("lobbyId", l.ID).Msg("destroying idle lobby")
				return
			}
		case <-l.ctx.Done():
			return
		}
	}
}

func (l *LobbyRoom) Join(client *ws.Client) {
	l.Events <- lobbyEvent{
		client: client,
		msg:    ws.Message{Event: "__lobby_join"},
		ctx:    context.Background(),
	}
}

func (l *LobbyRoom) Leave(client *ws.Client) {
	l.Events <- lobbyEvent{
		client: client,
		msg:    ws.Message{Event: "__lobby_leave"},
		ctx:    context.Background(),
	}
}

func (l *LobbyRoom) ProcessEvent(ctx context.Context, client *ws.Client, msg ws.Message) {
	l.Events <- lobbyEvent{client: client, msg: msg, ctx: ctx}
}

func (l *LobbyRoom) handleEvent(ev lobbyEvent) {
	switch ev.msg.Event {
	case "__lobby_join":
		l.processJoin(ev.client)
	case "__lobby_leave", "LeaveLobby":
		l.processLeave(ev.client)
	case "UpdateLoadout":
		var payload UpdateLoadoutPayload
		if err := json.Unmarshal(ev.msg.Data, &payload); err == nil {
			l.processUpdateLoadout(ev.ctx, ev.client, payload)
		}
	}
}

func (l *LobbyRoom) processJoin(client *ws.Client) {
	if len(l.State.Members) >= 2 {
		l.sendError(client, "Lobby is full")
		return
	}

	if l.State.Status != "preparing" {
		l.sendError(client, "Lobby is not accepting members")
		return
	}

	// Check if already a member
	for _, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			l.Clients[client.PlayerID] = client
			client.LobbyID = l.ID
			l.broadcastLobbyUpdated()
			return
		}
	}

	l.Clients[client.PlayerID] = client
	client.LobbyID = l.ID

	// Load member info
	var displayName string
	err := l.hub.db.Pool.QueryRow(context.Background(),
		`SELECT display_name FROM player_profiles WHERE player_id = $1`, client.PlayerID).
		Scan(&displayName)
	if err != nil {
		displayName = "Player_" + client.PlayerID[:6]
	}

	charID, items := loadPlayerLoadout(context.Background(), l.hub.db, client.PlayerID)

	// Load rating
	rating, tier := loadPlayerRating(context.Background(), l.hub.db, client.PlayerID)

	l.State.Members = append(l.State.Members, LobbyMember{
		PlayerID:    client.PlayerID,
		DisplayName: displayName,
		CharacterID: charID,
		Items:       items,
		Rating:      rating,
		Tier:        tier,
	})

	l.broadcastLobbyUpdated()
}

func (l *LobbyRoom) processLeave(client *ws.Client) {
	delete(l.Clients, client.PlayerID)
	client.LobbyID = ""

	isHost := client.PlayerID == l.State.HostPlayerID

	// Remove member from state
	for idx, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			l.State.Members = append(l.State.Members[:idx], l.State.Members[idx+1:]...)
			break
		}
	}

	// Host left → destroy lobby, kick remaining members
	if isHost {
		for _, c := range l.Clients {
			c.LobbyID = ""
			l.sendEvent(c, "LobbyDisbanded", map[string]string{"reason": "host left"})
		}
		l.cancel()
		return
	}

	l.broadcastLobbyUpdated()
}

func (l *LobbyRoom) processUpdateLoadout(ctx context.Context, client *ws.Client, payload UpdateLoadoutPayload) {
	// Find member
	memberIdx := -1
	for idx, m := range l.State.Members {
		if m.PlayerID == client.PlayerID {
			memberIdx = idx
			break
		}
	}
	if memberIdx == -1 {
		return
	}

	// Validate and update character
	if payload.CharacterID != nil {
		charID := *payload.CharacterID
		if charID != "rookie" {
			var exists bool
			err := l.hub.db.Pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM player_characters WHERE player_id = $1 AND character_id = $2)`,
				client.PlayerID, charID).Scan(&exists)
			if err != nil || !exists {
				l.sendError(client, "Character not unlocked")
				return
			}
		}
		l.State.Members[memberIdx].CharacterID = charID
	}

	// Validate and update items
	if payload.Items != nil {
		if len(payload.Items) > 3 {
			l.sendError(client, "Max 3 items")
			return
		}
		if len(payload.Items) > 0 {
			rows, err := l.hub.db.Pool.Query(ctx,
				`SELECT item_id FROM inventory_items WHERE player_id = $1 AND item_id = ANY($2) AND quantity > 0 AND (expires_at IS NULL OR expires_at > NOW())`,
				client.PlayerID, payload.Items)
			if err != nil {
				l.sendError(client, "Failed to validate items")
				return
			}
			defer rows.Close()
			ownedSet := make(map[string]bool)
			for rows.Next() {
				var itemID string
				if err := rows.Scan(&itemID); err == nil {
					ownedSet[itemID] = true
				}
			}
			rows.Close()
			for _, itemID := range payload.Items {
				if !ownedSet[itemID] {
					l.sendError(client, fmt.Sprintf("You do not own item: %s", itemID))
					return
				}
			}
		}
		l.State.Members[memberIdx].Items = payload.Items
	}

	// Persist to DB
	member := l.State.Members[memberIdx]
	itemsJSON, _ := json.Marshal(member.Items)
	_, err := l.hub.db.Pool.Exec(ctx,
		`INSERT INTO player_loadouts (player_id, character_id, items, updated_at)
		 VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		 ON CONFLICT (player_id) DO UPDATE SET character_id = $2, items = $3, updated_at = CURRENT_TIMESTAMP`,
		client.PlayerID, member.CharacterID, itemsJSON)
	if err != nil {
		observability.Log.Error().Err(err).Str("playerId", client.PlayerID).Msg("failed to persist loadout")
	}

	l.broadcastLobbyUpdated()
}

// SetStatus updates lobby status (called by matchmaker/handler)
func (l *LobbyRoom) SetStatus(status string) {
	l.State.Status = status
	l.broadcastLobbyUpdated()
}

func (l *LobbyRoom) broadcastLobbyUpdated() {
	payload, err := json.Marshal(l.State)
	if err != nil {
		return
	}
	msg := ws.Message{Event: "LobbyUpdated", Data: payload}
	for _, client := range l.Clients {
		select {
		case client.Send <- msg:
		default:
		}
	}
}

func (l *LobbyRoom) sendError(client *ws.Client, message string) {
	payload, _ := json.Marshal(map[string]string{"error": message})
	select {
	case client.Send <- ws.Message{Event: "LobbyError", Data: payload}:
	default:
	}
}

func (l *LobbyRoom) sendEvent(client *ws.Client, event string, data interface{}) {
	payload, _ := json.Marshal(data)
	select {
	case client.Send <- ws.Message{Event: event, Data: payload}:
	default:
	}
}

func loadPlayerRating(ctx context.Context, db *database.PostgresDB, playerID string) (int, string) {
	var rating int
	var tier string
	err := db.Pool.QueryRow(ctx,
		`SELECT pr.rating, pr.tier FROM player_ranks pr
		 JOIN rank_seasons rs ON pr.season_id = rs.season_id
		 WHERE pr.player_id = $1 AND rs.status = 'active'`, playerID).
		Scan(&rating, &tier)
	if err != nil {
		return 1000, "bronze"
	}
	return rating, tier
}
```

- [ ] **Step 2: Add missing import for database package**

Ensure the import includes `"battle-squad/internal/shared/database"`.

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/game/lobby/...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/game/lobby/lobby.go
git commit -m "feat: add lobby room event loop with loadout management"
```

---

### Task 7: Lobby WebSocket Handler

**Files:**
- Create: `internal/game/lobby/handler.go`

- [ ] **Step 1: Create lobby WebSocket handler**

```go
// internal/game/lobby/handler.go
package lobby

import (
	"context"
	"encoding/json"

	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type WSHandler struct {
	hub *LobbyHub
}

func NewWSHandler(hub *LobbyHub) *WSHandler {
	return &WSHandler{hub: hub}
}

func (h *WSHandler) HandleLobbyMessage(ctx context.Context, client *ws.Client, msg ws.Message) bool {
	switch msg.Event {
	case "CreateLobby":
		h.handleCreateLobby(ctx, client)
		return true

	case "JoinLobby":
		var payload JoinLobbyPayload
		if err := json.Unmarshal(msg.Data, &payload); err != nil {
			h.sendError(client, "INVALID_PAYLOAD", "Invalid payload")
			return true
		}
		h.handleJoinLobby(ctx, client, payload)
		return true

	case "LeaveLobby", "UpdateLoadout":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err != nil {
			client.LobbyID = ""
			return true
		}
		lobby.ProcessEvent(ctx, client, msg)
		return true
	}

	return false // not a lobby event
}

func (h *WSHandler) handleCreateLobby(ctx context.Context, client *ws.Client) {
	// Must not be in a room or lobby already
	if client.LobbyID != "" {
		h.sendError(client, "ALREADY_IN_LOBBY", "You are already in a lobby")
		return
	}
	if client.RoomID != "" {
		h.sendError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	var displayName string
	err := h.hub.db.Pool.QueryRow(ctx,
		`SELECT display_name FROM player_profiles WHERE player_id = $1`, client.PlayerID).
		Scan(&displayName)
	if err != nil {
		displayName = "Player_" + client.PlayerID[:6]
	}

	rating, tier := loadPlayerRating(ctx, h.hub.db, client.PlayerID)

	lobby, err := h.hub.CreateLobby(ctx, client.PlayerID, displayName, rating, tier)
	if err != nil {
		h.sendError(client, "LOBBY_CREATE_FAILED", err.Error())
		return
	}

	lobby.Join(client)
}

func (h *WSHandler) handleJoinLobby(ctx context.Context, client *ws.Client, payload JoinLobbyPayload) {
	if client.LobbyID != "" {
		h.sendError(client, "ALREADY_IN_LOBBY", "Leave your current lobby first")
		return
	}
	if client.RoomID != "" {
		h.sendError(client, "IN_ROOM", "Leave your current room first")
		return
	}

	lobby, err := h.hub.FindLobby(payload.LobbyID)
	if err != nil {
		h.sendError(client, "LOBBY_NOT_FOUND", err.Error())
		return
	}

	lobby.Join(client)
}

func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
	if client.LobbyID != "" {
		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err == nil {
			lobby.Leave(client)
		}
	}
}

func (h *WSHandler) sendError(client *ws.Client, code, message string) {
	errResp := model.ErrorResponse{}
	errResp.Error.Code = code
	errResp.Error.Message = message
	errResp.Error.CorrelationID = "ws-error"

	payload, err := json.Marshal(errResp)
	if err != nil {
		return
	}
	select {
	case client.Send <- ws.Message{Event: "LobbyError", Data: payload}:
	default:
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/lobby/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/lobby/handler.go
git commit -m "feat: add lobby WebSocket handler"
```

---

### Task 8: Matchmaking Queue — Redis Operations

**Files:**
- Create: `internal/game/matchmaker/queue.go`

- [ ] **Step 1: Create Redis queue operations**

```go
// internal/game/matchmaker/queue.go
package matchmaker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"github.com/redis/go-redis/v9"
)

const (
	queueKey       = "matchmaking:queue:2v2"
	entryKeyPrefix = "matchmaking:entry:"
	playerKeyPrefix = "matchmaking:player:"
	entryTTL       = 120 * time.Second
)

type Queue struct {
	redis *database.RedisClient
}

func NewQueue(redis *database.RedisClient) *Queue {
	return &Queue{redis: redis}
}

func (q *Queue) Enqueue(ctx context.Context, entry QueueEntry) error {
	// 1. Add to sorted set with rating as score
	err := q.redis.Client.ZAdd(ctx, queueKey, redis.Z{
		Score:  float64(entry.Rating),
		Member: entry.EntryID,
	}).Err()
	if err != nil {
		return fmt.Errorf("zadd queue: %w", err)
	}

	// 2. Store entry details
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	err = q.redis.Client.Set(ctx, entryKeyPrefix+entry.EntryID, data, entryTTL).Err()
	if err != nil {
		return fmt.Errorf("set entry: %w", err)
	}

	// 3. Map each player to this entry
	for _, playerID := range entry.PlayerIDs {
		err = q.redis.Client.Set(ctx, playerKeyPrefix+playerID, entry.EntryID, entryTTL).Err()
		if err != nil {
			observability.Log.Error().Err(err).Str("playerId", playerID).Msg("failed to set player queue mapping")
		}
	}

	return nil
}

func (q *Queue) Dequeue(ctx context.Context, entryID string, playerIDs []string) error {
	// Remove from sorted set
	q.redis.Client.ZRem(ctx, queueKey, entryID)

	// Remove entry detail
	q.redis.Client.Del(ctx, entryKeyPrefix+entryID)

	// Remove player mappings
	for _, playerID := range playerIDs {
		q.redis.Client.Del(ctx, playerKeyPrefix+playerID)
	}

	return nil
}

func (q *Queue) IsPlayerInQueue(ctx context.Context, playerID string) (bool, string) {
	entryID, err := q.redis.Client.Get(ctx, playerKeyPrefix+playerID).Result()
	if err != nil {
		return false, ""
	}
	return true, entryID
}

func (q *Queue) GetAllEntries(ctx context.Context) ([]QueueEntry, error) {
	// Get all members with scores from sorted set
	results, err := q.redis.Client.ZRangeWithScores(ctx, queueKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrange queue: %w", err)
	}

	entries := make([]QueueEntry, 0, len(results))
	for _, z := range results {
		entryID, ok := z.Member.(string)
		if !ok {
			continue
		}

		data, err := q.redis.Client.Get(ctx, entryKeyPrefix+entryID).Result()
		if err != nil {
			// Entry expired, clean up orphan from sorted set
			q.redis.Client.ZRem(ctx, queueKey, entryID)
			continue
		}

		var entry QueueEntry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

func (q *Queue) CancelByPlayer(ctx context.Context, playerID string) (*QueueEntry, error) {
	entryID, err := q.redis.Client.Get(ctx, playerKeyPrefix+playerID).Result()
	if err != nil {
		return nil, fmt.Errorf("player not in queue")
	}

	// Load entry to get all player IDs
	data, err := q.redis.Client.Get(ctx, entryKeyPrefix+entryID).Result()
	if err != nil {
		return nil, fmt.Errorf("entry not found")
	}

	var entry QueueEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		return nil, fmt.Errorf("unmarshal entry: %w", err)
	}

	q.Dequeue(ctx, entryID, entry.PlayerIDs)
	return &entry, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/matchmaker/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/matchmaker/queue.go
git commit -m "feat: add matchmaking Redis queue operations"
```

---

### Task 9: Elo Rating Calculation

**Files:**
- Create: `internal/game/match/elo.go`

- [ ] **Step 1: Create Elo calculator**

```go
// internal/game/match/elo.go
package match

import (
	"math"
)

type EloParams struct {
	KFactor       int
	RatingFloor   int
	BotModifier   float64 // 0-1, multiplier for matches with bots
	HasBot        bool
}

// CalculateEloChange returns the rating change for a team.
// teamRating and opponentRating are averages of team members' ratings.
// actualScore: 1.0 = win, 0.0 = loss, 0.5 = draw
func CalculateEloChange(teamRating, opponentRating int, actualScore float64, params EloParams) int {
	expected := 1.0 / (1.0 + math.Pow(10, float64(opponentRating-teamRating)/400.0))
	change := float64(params.KFactor) * (actualScore - expected)

	if params.HasBot {
		change *= params.BotModifier
	}

	result := int(math.Round(change))

	return result
}

// TeamAvgRating returns the average rating of a slice of ratings.
func TeamAvgRating(ratings []int) int {
	if len(ratings) == 0 {
		return 1000
	}
	sum := 0
	for _, r := range ratings {
		sum += r
	}
	return sum / len(ratings)
}
```

- [ ] **Step 2: Write test for Elo calculation**

Create `internal/game/match/elo_test.go`:

```go
package match

import "testing"

func TestCalculateEloChange(t *testing.T) {
	params := EloParams{KFactor: 32, RatingFloor: 0, BotModifier: 1.0, HasBot: false}

	// Equal teams, winner gets +16
	change := CalculateEloChange(1200, 1200, 1.0, params)
	if change != 16 {
		t.Errorf("equal teams win: expected 16, got %d", change)
	}

	// Equal teams, loser gets -16
	change = CalculateEloChange(1200, 1200, 0.0, params)
	if change != -16 {
		t.Errorf("equal teams loss: expected -16, got %d", change)
	}

	// Underdog wins (team 1250 beats team 1450)
	change = CalculateEloChange(1250, 1450, 1.0, params)
	if change < 20 {
		t.Errorf("underdog win: expected >20, got %d", change)
	}

	// Favorite wins (team 1450 beats team 1250)
	change = CalculateEloChange(1450, 1250, 1.0, params)
	if change > 12 {
		t.Errorf("favorite win: expected <12, got %d", change)
	}

	// Bot modifier halves the change
	botParams := EloParams{KFactor: 32, RatingFloor: 0, BotModifier: 0.5, HasBot: true}
	change = CalculateEloChange(1200, 1200, 1.0, botParams)
	if change != 8 {
		t.Errorf("bot modifier: expected 8, got %d", change)
	}
}

func TestTeamAvgRating(t *testing.T) {
	avg := TeamAvgRating([]int{1200, 1400})
	if avg != 1300 {
		t.Errorf("expected 1300, got %d", avg)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/game/match/... -v -run TestCalculateElo`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add internal/game/match/elo.go internal/game/match/elo_test.go
git commit -m "feat: add Elo rating calculation with bot modifier"
```

---

### Task 10: Smart Bot AI

**Files:**
- Create: `internal/game/match/bot_ai.go`

- [ ] **Step 1: Create smart bot AI**

```go
// internal/game/match/bot_ai.go
package match

import (
	"math"
	"math/rand"
	"time"

	"battle-squad/internal/game/matchmaker"
)

type SmartBotBrain struct {
	tierConfig matchmaker.BotTierConfig
	rng        *rand.Rand
}

func NewSmartBotBrain(tierConfig matchmaker.BotTierConfig) *SmartBotBrain {
	return &SmartBotBrain{
		tierConfig: tierConfig,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (b *SmartBotBrain) DecideAction(
	botState *BattlePlayerState,
	matchState *MatchState,
) interface{} {
	// 1. Evaluate situation
	hpRatio := float64(botState.HP) / float64(botState.MaxHP)
	target := b.findClosestEnemy(botState, matchState)

	// 2. Score each action
	shootScore := b.scoreShoot(botState, target)
	moveScore := b.scoreMove(botState, target, hpRatio)
	itemScore := b.scoreUseItem(botState, hpRatio)

	// 3. Add noise based on rank tier
	noise := b.tierConfig.DecisionNoise
	shootScore += b.rng.Intn(noise*2+1) - noise
	moveScore += b.rng.Intn(noise*2+1) - noise
	itemScore += b.rng.Intn(noise*2+1) - noise

	// 4. Choose best action
	if itemScore > shootScore && itemScore > moveScore && itemScore > 0 {
		return b.buildUseItemAction(botState)
	}

	if moveScore > shootScore && moveScore > 0 {
		return b.buildMoveAction(botState, target)
	}

	return b.buildShootAction(botState, target, matchState)
}

func (b *SmartBotBrain) findClosestEnemy(bot *BattlePlayerState, state *MatchState) *BattlePlayerState {
	var closest *BattlePlayerState
	minDist := math.MaxFloat64

	for _, p := range state.Players {
		if !p.IsAlive || p.TeamID == bot.TeamID {
			continue
		}
		dx := p.Position.X - bot.Position.X
		dy := p.Position.Y - bot.Position.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < minDist {
			minDist = dist
			closest = p
		}
	}
	return closest
}

func (b *SmartBotBrain) scoreShoot(bot *BattlePlayerState, target *BattlePlayerState) int {
	if target == nil {
		return 30
	}
	dx := math.Abs(target.Position.X - bot.Position.X)
	// Better score when target is at medium range (not too close, not too far)
	if dx > 200 && dx < 1000 {
		return 70
	}
	return 50
}

func (b *SmartBotBrain) scoreMove(bot *BattlePlayerState, target *BattlePlayerState, hpRatio float64) int {
	if bot.MoveEnergy < 20 {
		return 0
	}

	score := 20

	// Move if too close and HP is low
	if target != nil {
		dx := math.Abs(target.Position.X - bot.Position.X)
		if dx < 150 && hpRatio < 0.5 {
			score = 60 // retreat
		}
		if dx > 1000 {
			score = 40 // advance
		}
	}

	// Smart movement chance
	if b.rng.Float64() > b.tierConfig.MovementSmart {
		score = score / 2 // dumb move
	}

	return score
}

func (b *SmartBotBrain) scoreUseItem(bot *BattlePlayerState, hpRatio float64) int {
	if len(bot.Items) == 0 {
		return 0
	}

	// Check if has healing item and HP is low
	for _, item := range bot.Items {
		if item == "medkit" && hpRatio < 0.4 {
			if b.rng.Float64() < b.tierConfig.UseItemChance {
				return 80
			}
		}
	}

	// Other items
	if b.rng.Float64() < b.tierConfig.UseItemChance {
		return 40
	}

	return 10
}

func (b *SmartBotBrain) buildShootAction(bot *BattlePlayerState, target *BattlePlayerState, state *MatchState) ShootAction {
	if target == nil {
		return ShootAction{
			Angle:          45.0,
			Power:          50.0,
			ActionMode:     "weapon",
			ClientTimestamp: time.Now().Unix(),
		}
	}

	// Calculate perfect angle using physics
	dx := target.Position.X - bot.Position.X
	dy := target.Position.Y - bot.Position.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	angleRad := math.Atan2(-dy, dx)
	angleDeg := angleRad * 180.0 / math.Pi
	if angleDeg < 0 {
		angleDeg += 360
	}
	if angleDeg > 180 {
		if dx < 0 {
			angleDeg = 135
		} else {
			angleDeg = 45
		}
	}

	// Apply accuracy error
	angleError := (b.rng.Float64() - 0.5) * 2.0 * b.tierConfig.AccuracyError
	angleDeg += angleError

	// Estimate power
	power := 40.0
	if dist > 800 {
		power = 80.0
	} else if dist > 400 {
		power = 60.0
	}

	// Apply power error
	powerError := (b.rng.Float64() - 0.5) * 2.0 * b.tierConfig.PowerError
	power += powerError
	if power < 20 {
		power = 20
	} else if power > 100 {
		power = 100
	}

	// Wind compensation for smart bots
	if state.Wind.Power > 0 && b.rng.Float64() < b.tierConfig.MovementSmart {
		windCompensation := float64(state.Wind.Direction) * float64(state.Wind.Power) * 2.0
		angleDeg -= windCompensation
	}

	return ShootAction{
		Angle:          angleDeg,
		Power:          power,
		ActionMode:     "weapon",
		ClientTimestamp: time.Now().Unix(),
	}
}

func (b *SmartBotBrain) buildMoveAction(bot *BattlePlayerState, target *BattlePlayerState) MoveAction {
	direction := "right"
	targetX := bot.Position.X + 50

	if target != nil {
		if target.Position.X < bot.Position.X {
			direction = "left"
			targetX = bot.Position.X - 50
		}

		// Retreat if HP is low
		hpRatio := float64(bot.HP) / float64(bot.MaxHP)
		if hpRatio < 0.3 {
			// Move away from target
			if target.Position.X < bot.Position.X {
				direction = "right"
				targetX = bot.Position.X + 60
			} else {
				direction = "left"
				targetX = bot.Position.X - 60
			}
		}
	}

	return MoveAction{
		Direction:      direction,
		TargetX:        targetX,
		ClientTimestamp: time.Now().Unix(),
	}
}

func (b *SmartBotBrain) buildUseItemAction(bot *BattlePlayerState) UseItemAction {
	// Prioritize medkit when HP is low
	hpRatio := float64(bot.HP) / float64(bot.MaxHP)
	for _, item := range bot.Items {
		if item == "medkit" && hpRatio < 0.5 {
			return UseItemAction{
				ItemID:         "medkit",
				ClientTimestamp: time.Now().Unix(),
			}
		}
	}

	// Use first available item
	return UseItemAction{
		ItemID:         bot.Items[0],
		ClientTimestamp: time.Now().Unix(),
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/match/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/match/bot_ai.go
git commit -m "feat: add smart bot AI with rank-based difficulty"
```

---

### Task 11: Matchmaker Engine

**Files:**
- Create: `internal/game/matchmaker/matchmaker.go`

- [ ] **Step 1: Create matchmaker goroutine**

```go
// internal/game/matchmaker/matchmaker.go
package matchmaker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	"sync"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
)

const leaderKey = "matchmaking:leader"

type MatchResult struct {
	Entry1  QueueEntry
	Entry2  QueueEntry
	MapID   string
	HasBot  bool
}

// RoomCreator is implemented by the game server to create battle rooms from match results.
type RoomCreator interface {
	CreateBattleFromMatch(ctx context.Context, result MatchResult, botDiffConfig BotDifficultyConfig, eloConfig EloConfig) error
}

type Matchmaker struct {
	queue       *Queue
	db          *database.PostgresDB
	redis       *database.RedisClient
	nodeID      string
	cfg         MatchmakingConfig
	eloConfig   EloConfig
	botConfig   BotDifficultyConfig
	roomCreator RoomCreator
	mapIDs      []string
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

func NewMatchmaker(
	db *database.PostgresDB,
	redis *database.RedisClient,
	nodeID string,
	roomCreator RoomCreator,
	mapIDs []string,
) *Matchmaker {
	ctx, cancel := context.WithCancel(context.Background())

	cfg := LoadMatchmakingConfig(db)
	eloCfg := LoadEloConfig(db)
	botCfg := LoadBotDifficultyConfig(db)

	return &Matchmaker{
		queue:       NewQueue(redis),
		db:          db,
		redis:       redis,
		nodeID:      nodeID,
		cfg:         cfg,
		eloConfig:   eloCfg,
		botConfig:   botCfg,
		roomCreator: roomCreator,
		mapIDs:      mapIDs,
		ctx:         ctx,
		cancel:      cancel,
	}
}

func (m *Matchmaker) Run() {
	defer func() {
		if r := recover(); r != nil {
			observability.Log.Error().Interface("panic", r).Msg("matchmaker panic recovery")
		}
	}()

	observability.Log.Info().Str("nodeId", m.nodeID).Msg("matchmaker started")

	tickDuration := time.Duration(m.cfg.TickInterval) * time.Second
	if tickDuration < time.Second {
		tickDuration = 3 * time.Second
	}
	ticker := time.NewTicker(tickDuration)
	defer ticker.Stop()

	renewTicker := time.NewTicker(5 * time.Second)
	defer renewTicker.Stop()

	// Reload config periodically
	configTicker := time.NewTicker(30 * time.Second)
	defer configTicker.Stop()

	for {
		select {
		case <-ticker.C:
			if m.tryAcquireLeader() {
				m.tick()
			}

		case <-renewTicker.C:
			m.renewLeader()

		case <-configTicker.C:
			m.reloadConfig()

		case <-m.ctx.Done():
			observability.Log.Info().Msg("matchmaker stopped")
			return
		}
	}
}

func (m *Matchmaker) Stop() {
	m.cancel()
}

func (m *Matchmaker) tryAcquireLeader() bool {
	ok, err := m.redis.Client.SetNX(m.ctx, leaderKey, m.nodeID, 10*time.Second).Result()
	if err != nil {
		return false
	}
	if ok {
		return true
	}
	// Check if we're already the leader
	val, err := m.redis.Client.Get(m.ctx, leaderKey).Result()
	if err != nil {
		return false
	}
	return val == m.nodeID
}

func (m *Matchmaker) renewLeader() {
	val, err := m.redis.Client.Get(m.ctx, leaderKey).Result()
	if err != nil || val != m.nodeID {
		return
	}
	m.redis.Client.Expire(m.ctx, leaderKey, 10*time.Second)
}

func (m *Matchmaker) reloadConfig() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = LoadMatchmakingConfig(m.db)
	m.eloConfig = LoadEloConfig(m.db)
	m.botConfig = LoadBotDifficultyConfig(m.db)
}

func (m *Matchmaker) GetConfig() MatchmakingConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Matchmaker) tick() {
	ctx := m.ctx
	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	entries, err := m.queue.GetAllEntries(ctx)
	if err != nil || len(entries) == 0 {
		return
	}

	now := time.Now().Unix()
	matched := make(map[string]bool)

	// Try to match entries
	for i := 0; i < len(entries); i++ {
		if matched[entries[i].EntryID] {
			continue
		}

		e1 := entries[i]
		waitTime1 := now - e1.QueuedAt
		allowedRange1 := m.calculateAllowedRange(waitTime1, cfg)

		// Check timeout — fill with bot
		if waitTime1 >= int64(cfg.MaxWaitTime) {
			m.matchWithBot(ctx, e1)
			matched[e1.EntryID] = true
			continue
		}

		// Find best match
		for j := i + 1; j < len(entries); j++ {
			if matched[entries[j].EntryID] {
				continue
			}

			e2 := entries[j]
			waitTime2 := now - e2.QueuedAt
			allowedRange2 := m.calculateAllowedRange(waitTime2, cfg)

			ratingDiff := int(math.Abs(float64(e1.Rating - e2.Rating)))
			allowedRange := int(math.Min(float64(allowedRange1), float64(allowedRange2)))

			if ratingDiff <= allowedRange {
				// Match found!
				m.matchEntries(ctx, e1, e2)
				matched[e1.EntryID] = true
				matched[e2.EntryID] = true
				break
			}
		}
	}
}

func (m *Matchmaker) calculateAllowedRange(waitTimeSeconds int64, cfg MatchmakingConfig) int {
	expansions := int(waitTimeSeconds) / cfg.ExpandInterval
	expanded := cfg.BaseRatingRange + expansions*cfg.ExpandStep
	if expanded > cfg.MaxRatingRange {
		expanded = cfg.MaxRatingRange
	}
	return expanded
}

func (m *Matchmaker) matchEntries(ctx context.Context, e1, e2 QueueEntry) {
	// Remove both from queue
	m.queue.Dequeue(ctx, e1.EntryID, e1.PlayerIDs)
	m.queue.Dequeue(ctx, e2.EntryID, e2.PlayerIDs)

	mapID := m.randomMap()

	result := MatchResult{
		Entry1: e1,
		Entry2: e2,
		MapID:  mapID,
		HasBot: false,
	}

	observability.Log.Info().
		Str("entry1", e1.EntryID).
		Str("entry2", e2.EntryID).
		Int("rating1", e1.Rating).
		Int("rating2", e2.Rating).
		Msg("matchmaking: matched two entries")

	m.mu.RLock()
	botCfg := m.botConfig
	eloCfg := m.eloConfig
	m.mu.RUnlock()

	if err := m.roomCreator.CreateBattleFromMatch(ctx, result, botCfg, eloCfg); err != nil {
		observability.Log.Error().Err(err).Msg("matchmaking: failed to create battle room")
	}
}

func (m *Matchmaker) matchWithBot(ctx context.Context, entry QueueEntry) {
	m.queue.Dequeue(ctx, entry.EntryID, entry.PlayerIDs)

	mapID := m.randomMap()

	// Create a fake entry for the bot team
	botEntryID := generateMatchmakerID()
	botEntry := QueueEntry{
		EntryID:      botEntryID,
		LobbyID:      "",
		PlayerIDs:    []string{},
		Rating:       entry.Rating,
		PlayerRatings: map[string]int{},
		PlayerChars:  map[string]string{},
		PlayerItems:  map[string][]string{},
		PlayerNames:  map[string]string{},
		TeamSize:     0, // signals bot team
		QueuedAt:     time.Now().Unix(),
		NodeID:       m.nodeID,
	}

	result := MatchResult{
		Entry1: entry,
		Entry2: botEntry,
		MapID:  mapID,
		HasBot: true,
	}

	observability.Log.Info().
		Str("entryId", entry.EntryID).
		Int("rating", entry.Rating).
		Msg("matchmaking: timeout, filling with bots")

	m.mu.RLock()
	botCfg := m.botConfig
	eloCfg := m.eloConfig
	m.mu.RUnlock()

	if err := m.roomCreator.CreateBattleFromMatch(ctx, result, botCfg, eloCfg); err != nil {
		observability.Log.Error().Err(err).Msg("matchmaking: failed to create bot battle room")
	}
}

func (m *Matchmaker) randomMap() string {
	if len(m.mapIDs) == 0 {
		return "grassland_valley"
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	return m.mapIDs[rng.Intn(len(m.mapIDs))]
}

// EnqueueLobby builds a QueueEntry from lobby data and adds it to the queue.
func (m *Matchmaker) EnqueueLobby(ctx context.Context, lobbyID string, playerIDs []string,
	playerRatings map[string]int, playerChars map[string]string,
	playerItems map[string][]string, playerNames map[string]string) (*QueueEntry, error) {

	m.mu.RLock()
	cfg := m.cfg
	m.mu.RUnlock()

	// Calculate queue rating based on party strategy
	rating := m.calculatePartyRating(playerRatings, cfg)

	entry := QueueEntry{
		EntryID:       generateMatchmakerID(),
		LobbyID:       lobbyID,
		PlayerIDs:     playerIDs,
		Rating:        rating,
		PlayerRatings: playerRatings,
		PlayerChars:   playerChars,
		PlayerItems:   playerItems,
		PlayerNames:   playerNames,
		TeamSize:      len(playerIDs),
		QueuedAt:      time.Now().Unix(),
		NodeID:        m.nodeID,
	}

	if err := m.queue.Enqueue(ctx, entry); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (m *Matchmaker) CancelQueue(ctx context.Context, playerID string) (*QueueEntry, error) {
	return m.queue.CancelByPlayer(ctx, playerID)
}

func (m *Matchmaker) IsPlayerInQueue(ctx context.Context, playerID string) bool {
	inQueue, _ := m.queue.IsPlayerInQueue(ctx, playerID)
	return inQueue
}

func (m *Matchmaker) calculatePartyRating(ratings map[string]int, cfg MatchmakingConfig) int {
	if len(ratings) == 0 {
		return 1000
	}

	if len(ratings) == 1 {
		for _, r := range ratings {
			return r
		}
	}

	values := make([]int, 0, len(ratings))
	for _, r := range ratings {
		values = append(values, r)
	}

	switch cfg.PartyRatingStrategy {
	case "average":
		sum := 0
		for _, v := range values {
			sum += v
		}
		return sum / len(values)

	case "weighted":
		maxR, minR := values[0], values[0]
		for _, v := range values[1:] {
			if v > maxR {
				maxR = v
			}
			if v < minR {
				minR = v
			}
		}
		return int(float64(maxR)*cfg.WeightedRatio + float64(minR)*(1-cfg.WeightedRatio))

	default: // "max"
		maxR := values[0]
		for _, v := range values[1:] {
			if v > maxR {
				maxR = v
			}
		}
		return maxR
	}
}

func generateMatchmakerID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(b)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/game/matchmaker/...`
Expected: Build succeeds

- [ ] **Step 3: Commit**

```bash
git add internal/game/matchmaker/matchmaker.go
git commit -m "feat: add matchmaker engine with expanding range and bot fallback"
```

---

### Task 12: Update Reward System for 2v2 Elo

**Files:**
- Modify: `internal/game/match/reward.go`

- [ ] **Step 1: Add HasBot and Mode fields to support Elo in rewards**

In `internal/game/match/reward.go`, modify `ProcessMatchRewards` to accept additional parameters for Elo-based 2v2 rating. Find the section where `mode == "pvp_1v1"` rating logic is, and extend it:

Replace the existing rating update block (around line 117-135):

```go
		// Update player rank rating
		ratingChange := 0
		newRating := 1000
		newTier := "silver"
		newDivision := 3

		if mode == "pvp_1v1" {
			ratingChange = -20
			if p.IsWinner {
				ratingChange = 25
			} else if p.IsDraw {
				ratingChange = 0
			}

			newRating, newTier, newDivision, err = updatePlayerRank(ctx, tx, p.PlayerID, ratingChange, p.IsWinner, p.IsDraw)
			if err != nil {
				return nil, fmt.Errorf("failed to update rank rating: %w", err)
			}
		}
```

With:

```go
		// Update player rank rating
		ratingChange := 0
		newRating := 1000
		newTier := "silver"
		newDivision := 3

		if mode == "pvp_1v1" {
			ratingChange = -20
			if p.IsWinner {
				ratingChange = 25
			} else if p.IsDraw {
				ratingChange = 0
			}

			newRating, newTier, newDivision, err = updatePlayerRank(ctx, tx, p.PlayerID, ratingChange, p.IsWinner, p.IsDraw)
			if err != nil {
				return nil, fmt.Errorf("failed to update rank rating: %w", err)
			}
		} else if mode == "ranked_2v2" {
			// Elo-based rating for ranked 2v2
			actualScore := 0.0
			if p.IsWinner {
				actualScore = 1.0
			} else if p.IsDraw {
				actualScore = 0.5
			}

			teamRating := teamRatings[p.TeamID]
			opponentTeamID := 1
			if p.TeamID == 1 {
				opponentTeamID = 2
			}
			opponentRating := teamRatings[opponentTeamID]

			ratingChange = CalculateEloChange(teamRating, opponentRating, actualScore, eloParams)

			newRating, newTier, newDivision, err = updatePlayerRank(ctx, tx, p.PlayerID, ratingChange, p.IsWinner, p.IsDraw)
			if err != nil {
				return nil, fmt.Errorf("failed to update rank rating: %w", err)
			}
		}
```

- [ ] **Step 2: Update ProcessMatchRewards signature**

Change the function signature to accept team ratings and elo params:

```go
func ProcessMatchRewards(
	ctx context.Context,
	db *database.PostgresDB,
	economyRepo *economy.Repository,
	matchID string,
	mode string,
	mapID string,
	stats map[string]*PlayerStats,
	playerItems map[string][]string,
	teamRatings map[int]int,      // NEW: teamID → avg rating
	eloParams EloParams,          // NEW: elo configuration
) (map[string]RewardResult, error) {
```

For existing callers (normal PvP), pass empty defaults: `map[int]int{}` and `EloParams{}`.

- [ ] **Step 3: Update existing call sites in match.go**

Find the call to `ProcessMatchRewards` in `checkWinCondition` (around line 923) and add the new parameters:

```go
rewards, err := ProcessMatchRewards(ctx, m.db, m.economyRepo, m.State.MatchID, m.State.Mode, m.State.MapID, stats, playerItems, m.TeamRatings, m.EloParams)
```

Add `TeamRatings map[int]int` and `EloParams EloParams` fields to the `Match` struct. Initialize them as empty defaults in `NewMatch`. The matchmaker will set them when creating ranked matches.

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/game/match/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/game/match/reward.go internal/game/match/match.go
git commit -m "feat: add Elo-based rating for ranked 2v2 in reward system"
```

---

### Task 13: Battle Room Integration — LobbyMapping & ReturnToLobby

**Files:**
- Modify: `internal/game/room/model.go`
- Modify: `internal/game/room/room.go`

- [ ] **Step 1: Add LobbyMapping to RoomState**

In `internal/game/room/model.go`, add `LobbyMapping` field to `RoomState`:

```go
type RoomState struct {
	RoomID       string            `json:"roomId"`
	HostPlayerID string            `json:"hostPlayerId"`
	Mode         string            `json:"mode"`
	MapID        string            `json:"mapId"`
	MaxPlayers   int               `json:"maxPlayers"`
	Players      []RoomPlayer      `json:"players"`
	IsLocked     bool              `json:"isLocked"`
	PasswordHash string            `json:"-"`
	Status       string            `json:"status"`
	IsTutorial   bool              `json:"isTutorial"`
	LobbyMapping map[string]string `json:"-"` // playerID → lobbyID for return-to-lobby
	HasBot       bool              `json:"hasBot"`
}
```

- [ ] **Step 2: Send ReturnToLobby when match ends**

In `internal/game/room/room.go`, update the `Run()` method's `matchDoneCh` case to send `ReturnToLobby` events before destroying the room:

Replace:
```go
		case <-matchDoneCh:
			// Match ended — destroy the room
			observability.Log.Info().Str("roomId", r.ID).Msg("match ended, destroying room")
			return
```

With:
```go
		case <-matchDoneCh:
			// Match ended — notify players to return to their lobbies
			if r.State.LobbyMapping != nil {
				for playerID, lobbyID := range r.State.LobbyMapping {
					if client, ok := r.Clients[playerID]; ok {
						payload, _ := json.Marshal(map[string]string{"lobbyId": lobbyID})
						select {
						case client.Send <- ws.Message{Event: "ReturnToLobby", Data: payload}:
						default:
						}
					}
				}
			}
			observability.Log.Info().Str("roomId", r.ID).Msg("match ended, destroying room")
			return
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/game/room/...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/game/room/model.go internal/game/room/room.go
git commit -m "feat: add lobby mapping and ReturnToLobby on match end"
```

---

### Task 14: Room Creator — Bridge Between Matchmaker and Room Hub

**Files:**
- Modify: `internal/game/room/hub.go`

- [ ] **Step 1: Add CreateBattleFromMatch method to Hub**

This implements the `matchmaker.RoomCreator` interface. Add to `internal/game/room/hub.go`:

```go
func (h *Hub) CreateBattleFromMatch(ctx context.Context, result matchmaker.MatchResult, botDiffConfig matchmaker.BotDifficultyConfig, eloConfig matchmaker.EloConfig) error {
	h.Lock()
	defer h.Unlock()

	roomID := generateID()
	matchID := generateID()

	// Build players from match entries
	var players []RoomPlayer
	lobbyMapping := make(map[string]string)

	// Team 1 from entry1
	for _, pid := range result.Entry1.PlayerIDs {
		players = append(players, RoomPlayer{
			PlayerID:    pid,
			DisplayName: result.Entry1.PlayerNames[pid],
			TeamID:      1,
			CharacterID: result.Entry1.PlayerChars[pid],
			Items:       result.Entry1.PlayerItems[pid],
			IsReady:     true,
			IsHost:      false,
		})
		lobbyMapping[pid] = result.Entry1.LobbyID
	}

	// Team 2 from entry2 (may be empty if bot team)
	for _, pid := range result.Entry2.PlayerIDs {
		players = append(players, RoomPlayer{
			PlayerID:    pid,
			DisplayName: result.Entry2.PlayerNames[pid],
			TeamID:      2,
			CharacterID: result.Entry2.PlayerChars[pid],
			Items:       result.Entry2.PlayerItems[pid],
			IsReady:     true,
			IsHost:      false,
		})
		lobbyMapping[pid] = result.Entry2.LobbyID
	}

	// Determine bot tier from average rating of real players
	avgRating := result.Entry1.Rating
	if len(result.Entry2.PlayerIDs) > 0 {
		avgRating = (result.Entry1.Rating + result.Entry2.Rating) / 2
	}
	botTier := ratingToTier(avgRating)
	botTierConfig := botDiffConfig.Tiers[botTier]
	if botTierConfig == (matchmaker.BotTierConfig{}) {
		botTierConfig = botDiffConfig.Tiers["bronze"]
	}

	// Fill bots for team 2 if needed
	botsNeeded := 2 - len(result.Entry2.PlayerIDs)
	// Also fill bots for team 1 if solo queue
	team1Bots := 2 - len(result.Entry1.PlayerIDs)

	for i := 0; i < team1Bots; i++ {
		botID := "bot_" + generateID()[:8]
		players = append(players, RoomPlayer{
			PlayerID:    botID,
			DisplayName: "Bot " + botID[4:8],
			TeamID:      1,
			CharacterID: "rookie",
			Items:       []string{},
			IsReady:     true,
			IsHost:      false,
		})
	}

	for i := 0; i < botsNeeded; i++ {
		botID := "bot_" + generateID()[:8]
		players = append(players, RoomPlayer{
			PlayerID:    botID,
			DisplayName: "Bot " + botID[4:8],
			TeamID:      2,
			CharacterID: "rookie",
			Items:       []string{},
			IsReady:     true,
			IsHost:      false,
		})
	}

	// Set host to first real player
	if len(players) > 0 {
		players[0].IsHost = true
	}

	roomState := RoomState{
		RoomID:       roomID,
		HostPlayerID: players[0].PlayerID,
		Mode:         "ranked_2v2",
		MapID:        result.MapID,
		MaxPlayers:   4,
		Players:      players,
		Status:       "in_match",
		LobbyMapping: lobbyMapping,
		HasBot:       result.HasBot,
	}

	room := NewRoom(roomID, roomState, h, h.db)
	h.rooms[roomID] = room

	observability.ActiveRooms.Inc()

	go room.Run()

	// Collect clients from lobbies and move them to this room
	// This is done via MatchFound event — clients will connect to the battle room
	// Send MatchFound to all real players
	matchFoundPayload := map[string]interface{}{
		"matchId":  matchID,
		"roomId":   roomID,
		"mapId":    result.MapID,
		"players":  players,
		"hasBot":   result.HasBot,
	}

	// Notify players in entry1's lobby
	h.notifyLobbyPlayers(result.Entry1.LobbyID, "MatchFound", matchFoundPayload)
	// Notify players in entry2's lobby
	if result.Entry2.LobbyID != "" {
		h.notifyLobbyPlayers(result.Entry2.LobbyID, "MatchFound", matchFoundPayload)
	}

	// Build match players and start match directly
	room.startRankedMatch(matchID, botTierConfig, eloConfig, result)

	h.saveToRedis(ctx, &roomState)

	return nil
}

func (h *Hub) notifyLobbyPlayers(lobbyID string, event string, data interface{}) {
	// This will be called by the composite handler to find lobby clients
	// For now, we store a reference to the lobby hub (set during wiring)
	if h.lobbyNotifier != nil {
		h.lobbyNotifier(lobbyID, event, data)
	}
}

func ratingToTier(rating int) string {
	switch {
	case rating < 1000:
		return "bronze"
	case rating < 1200:
		return "silver"
	case rating < 1500:
		return "gold"
	case rating < 1800:
		return "platinum"
	case rating < 2200:
		return "diamond"
	default:
		return "master"
	}
}
```

- [ ] **Step 2: Add lobbyNotifier and imports to Hub struct**

Add `lobbyNotifier func(lobbyID string, event string, data interface{})` field to `Hub` struct and a setter:

```go
type Hub struct {
	sync.RWMutex
	rooms          map[string]*Room
	redis          *database.RedisClient
	db             *database.PostgresDB
	nodeID         string
	lobbyNotifier  func(lobbyID string, event string, data interface{})
}

func (h *Hub) SetLobbyNotifier(fn func(lobbyID string, event string, data interface{})) {
	h.lobbyNotifier = fn
}
```

Add import for `"battle-squad/internal/game/matchmaker"` at the top.

- [ ] **Step 3: Add startRankedMatch to Room**

Add this method to `internal/game/room/room.go`:

```go
func (r *Room) startRankedMatch(matchID string, botTierConfig matchmaker.BotTierConfig, eloConfig matchmaker.EloConfig, matchResult matchmaker.MatchResult) {
	// Build match players
	team1Spawns := []gamedata.SpawnPoint{{X: 200, Y: 0}, {X: 350, Y: 0}}
	team2Spawns := []gamedata.SpawnPoint{{X: 1200, Y: 0}, {X: 1350, Y: 0}}
	if mapCfg, ok := gamedata.Data.Maps[r.State.MapID]; ok {
		mid := len(mapCfg.SpawnPoints) / 2
		team1Spawns = team1Spawns[:0]
		team2Spawns = team2Spawns[:0]
		for i, sp := range mapCfg.SpawnPoints {
			if i < mid {
				team1Spawns = append(team1Spawns, sp)
			} else {
				team2Spawns = append(team2Spawns, sp)
			}
		}
	}

	team1Idx, team2Idx := 0, 0
	matchPlayers := []*match.BattlePlayerState{}

	// Collect team ratings for Elo
	teamRatings := map[int][]int{1: {}, 2: {}}

	for _, p := range r.State.Players {
		var spawnPos match.Vector2
		if p.TeamID == 2 {
			if len(team2Spawns) > 0 {
				sp := team2Spawns[team2Idx%len(team2Spawns)]
				spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
				team2Idx++
			}
		} else {
			if len(team1Spawns) > 0 {
				sp := team1Spawns[team1Idx%len(team1Spawns)]
				spawnPos = match.Vector2{X: sp.X, Y: sp.Y}
				team1Idx++
			}
		}

		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp, defense := 100, 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}

		isBot := len(p.PlayerID) > 4 && p.PlayerID[:4] == "bot_"

		// Get player rating for Elo
		playerRating := 1000
		if !isBot {
			if matchResult.Entry1.PlayerRatings != nil {
				if r, ok := matchResult.Entry1.PlayerRatings[p.PlayerID]; ok {
					playerRating = r
				}
			}
			if matchResult.Entry2.PlayerRatings != nil {
				if r, ok := matchResult.Entry2.PlayerRatings[p.PlayerID]; ok {
					playerRating = r
				}
			}
		}
		teamRatings[p.TeamID] = append(teamRatings[p.TeamID], playerRating)

		matchPlayers = append(matchPlayers, &match.BattlePlayerState{
			PlayerID:      p.PlayerID,
			DisplayName:   p.DisplayName,
			TeamID:        p.TeamID,
			CharacterID:   p.CharacterID,
			HP:            hp,
			MaxHP:         hp,
			Defense:       defense,
			Position:      spawnPos,
			MoveEnergy:    100,
			Items:         p.Items,
			StatusEffects: []match.StatusEffect{},
			IsAlive:       true,
			IsBot:         isBot,
		})
	}

	// Calculate team avg ratings
	avgTeamRatings := map[int]int{}
	for teamID, ratings := range teamRatings {
		avgTeamRatings[teamID] = match.TeamAvgRating(ratings)
	}

	eloParams := match.EloParams{
		KFactor:     eloConfig.KFactor,
		RatingFloor: eloConfig.RatingFloor,
		BotModifier: 1.0,
		HasBot:      r.State.HasBot,
	}
	if r.State.HasBot {
		mmCfg := matchmaker.LoadMatchmakingConfig(r.db)
		eloParams.BotModifier = mmCfg.BotRatingModifier
	}

	r.match = match.NewMatch(
		matchID,
		r.ID,
		r.State.Mode,
		r.State.MapID,
		matchPlayers,
		r.Clients,
		r.db,
		r.hub.redis,
		economy.NewRepository(),
		r.hub,
	)
	r.match.TeamRatings = avgTeamRatings
	r.match.EloParams = eloParams
	r.match.BotTierConfig = botTierConfig

	r.matchDone = r.match.MatchDone

	go r.match.Run()
	r.hub.SyncRoomState(context.Background(), &r.State)
}
```

Add import for `"battle-squad/internal/game/matchmaker"` in room.go.

- [ ] **Step 4: Add TeamRatings, EloParams, and BotTierConfig fields to Match struct**

In `internal/game/match/match.go`, add these fields to the `Match` struct:

```go
type Match struct {
	// ... existing fields ...
	TeamRatings   map[int]int
	EloParams     EloParams
	BotTierConfig matchmaker.BotTierConfig
}
```

Add import for `"battle-squad/internal/game/matchmaker"`.

- [ ] **Step 5: Update bot turn handling to use SmartBotBrain when BotTierConfig is set**

In the bot turn handling code in `match.go`, check if `BotTierConfig` has non-zero values and use `SmartBotBrain` instead of the existing `BotBrain`:

```go
// In the bot turn handling section
if currentPlayer.IsBot {
    var action interface{}
    if m.BotTierConfig != (matchmaker.BotTierConfig{}) {
        smartBot := NewSmartBotBrain(m.BotTierConfig)
        action = smartBot.DecideAction(currentPlayer, &m.State)
    } else {
        botBrain := NewBotBrain("easy")
        action = botBrain.DecideAction(currentPlayer, &m.State)
    }
    // ... inject action into event loop
}
```

- [ ] **Step 6: Verify compilation**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/game/room/hub.go internal/game/room/room.go internal/game/room/model.go internal/game/match/match.go
git commit -m "feat: bridge matchmaker to battle room with Elo and smart bots"
```

---

### Task 15: StartQueue / CancelQueue in Lobby Handler

**Files:**
- Modify: `internal/game/lobby/handler.go`
- Modify: `internal/game/lobby/lobby.go`

- [ ] **Step 1: Add matchmaker reference to lobby handler**

Update `WSHandler` in `internal/game/lobby/handler.go`:

```go
type WSHandler struct {
	hub        *LobbyHub
	matchmaker *matchmaker.Matchmaker
}

func NewWSHandler(hub *LobbyHub, mm *matchmaker.Matchmaker) *WSHandler {
	return &WSHandler{hub: hub, matchmaker: mm}
}
```

Add import for `"battle-squad/internal/game/matchmaker"`.

- [ ] **Step 2: Add StartQueue and CancelQueue handling**

Add these cases to `HandleLobbyMessage`:

```go
	case "StartQueue":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		h.handleStartQueue(ctx, client)
		return true

	case "CancelQueue":
		if client.LobbyID == "" {
			h.sendError(client, "LOBBY_REQUIRED", "You must be in a lobby")
			return true
		}
		h.handleCancelQueue(ctx, client)
		return true
```

- [ ] **Step 3: Implement StartQueue handler**

```go
func (h *WSHandler) handleStartQueue(ctx context.Context, client *ws.Client) {
	lobby, err := h.hub.FindLobby(client.LobbyID)
	if err != nil {
		h.sendError(client, "LOBBY_NOT_FOUND", "Lobby not found")
		return
	}

	// Only host can start queue
	if lobby.State.HostPlayerID != client.PlayerID {
		h.sendError(client, "NOT_HOST", "Only the host can start queue")
		return
	}

	if lobby.State.Status != "preparing" {
		h.sendError(client, "INVALID_STATUS", "Lobby is not in preparing state")
		return
	}

	// Check no one is already in queue
	for _, m := range lobby.State.Members {
		if h.matchmaker.IsPlayerInQueue(ctx, m.PlayerID) {
			h.sendError(client, "ALREADY_IN_QUEUE", "A member is already in queue")
			return
		}
	}

	// Build queue entry data from lobby members
	playerIDs := make([]string, 0, len(lobby.State.Members))
	playerRatings := make(map[string]int)
	playerChars := make(map[string]string)
	playerItems := make(map[string][]string)
	playerNames := make(map[string]string)

	for _, m := range lobby.State.Members {
		playerIDs = append(playerIDs, m.PlayerID)
		playerRatings[m.PlayerID] = m.Rating
		playerChars[m.PlayerID] = m.CharacterID
		playerItems[m.PlayerID] = m.Items
		playerNames[m.PlayerID] = m.DisplayName
	}

	entry, err := h.matchmaker.EnqueueLobby(ctx, lobby.ID, playerIDs, playerRatings, playerChars, playerItems, playerNames)
	if err != nil {
		h.sendError(client, "QUEUE_FAILED", err.Error())
		return
	}

	lobby.State.Status = "in_queue"
	lobby.State.QueueEntryID = entry.EntryID
	lobby.broadcastLobbyUpdated()

	// Send QueueStarted to all members
	payload, _ := json.Marshal(map[string]interface{}{
		"estimatedWait": h.matchmaker.GetConfig().MaxWaitTime,
	})
	for _, c := range lobby.Clients {
		select {
		case c.Send <- ws.Message{Event: "QueueStarted", Data: payload}:
		default:
		}
	}
}

func (h *WSHandler) handleCancelQueue(ctx context.Context, client *ws.Client) {
	lobby, err := h.hub.FindLobby(client.LobbyID)
	if err != nil {
		return
	}

	if lobby.State.Status != "in_queue" {
		h.sendError(client, "NOT_IN_QUEUE", "Lobby is not in queue")
		return
	}

	// Cancel queue for the whole party
	_, cancelErr := h.matchmaker.CancelQueue(ctx, client.PlayerID)
	if cancelErr != nil {
		observability.Log.Warn().Err(cancelErr).Msg("failed to cancel queue")
	}

	lobby.State.Status = "preparing"
	lobby.State.QueueEntryID = ""
	lobby.broadcastLobbyUpdated()

	payload, _ := json.Marshal(map[string]string{"reason": "cancelled"})
	for _, c := range lobby.Clients {
		select {
		case c.Send <- ws.Message{Event: "QueueCancelled", Data: payload}:
		default:
		}
	}
}
```

Add `QueueEntryID` field to `LobbyRoom` struct in `lobby.go` if not already present (it's in the `LobbyState` from `model.go` — add it there if missing).

- [ ] **Step 4: Update lobby disconnect to cancel queue**

In `lobby.go` `processLeave`, add queue cancellation before destroying lobby:

```go
func (l *LobbyRoom) processLeave(client *ws.Client) {
	delete(l.Clients, client.PlayerID)
	client.LobbyID = ""

	isHost := client.PlayerID == l.State.HostPlayerID

	// Cancel queue if in queue
	if l.State.Status == "in_queue" {
		// Queue will be cancelled by the handler's UnregisterFromLobby
		// Reset status for remaining members
		l.State.Status = "preparing"
		l.State.QueueEntryID = ""

		// Notify remaining members
		payload, _ := json.Marshal(map[string]string{"reason": "teammate disconnected"})
		for _, c := range l.Clients {
			select {
			case c.Send <- ws.Message{Event: "QueueCancelled", Data: payload}:
			default:
			}
		}
	}

	// ... rest of existing leave logic
```

- [ ] **Step 5: Update UnregisterFromLobby to cancel queue**

In `handler.go`, update `UnregisterFromLobby`:

```go
func (h *WSHandler) UnregisterFromLobby(client *ws.Client) {
	if client.LobbyID != "" {
		// Cancel queue if player is in one
		if h.matchmaker.IsPlayerInQueue(context.Background(), client.PlayerID) {
			h.matchmaker.CancelQueue(context.Background(), client.PlayerID)
		}

		lobby, err := h.hub.FindLobby(client.LobbyID)
		if err == nil {
			lobby.Leave(client)
		}
	}
}
```

- [ ] **Step 6: Verify compilation**

Run: `go build ./...`
Expected: Build succeeds

- [ ] **Step 7: Commit**

```bash
git add internal/game/lobby/handler.go internal/game/lobby/lobby.go
git commit -m "feat: add StartQueue and CancelQueue with disconnect handling"
```

---

### Task 16: Composite WebSocket Handler

**Files:**
- Modify: `cmd/game/main.go`

- [ ] **Step 1: Create composite handler that routes to lobby or room handler**

Create a composite handler that tries lobby events first, then falls back to room events. Add this directly in `cmd/game/main.go` or as a small wrapper:

```go
type CompositeWSHandler struct {
	lobbyHandler *lobby.WSHandler
	roomHandler  *room.WSHandler
}

func (c *CompositeWSHandler) HandleMessage(ctx context.Context, client *ws.Client, msg ws.Message) {
	// Try lobby handler first
	if c.lobbyHandler.HandleLobbyMessage(ctx, client, msg) {
		return
	}
	// Fall through to room handler
	c.roomHandler.HandleMessage(ctx, client, msg)
}

func (c *CompositeWSHandler) Unregister(client *ws.Client) {
	c.lobbyHandler.UnregisterFromLobby(client)
	c.roomHandler.Unregister(client)
}
```

- [ ] **Step 2: Update main.go initialization to wire everything**

```go
func main() {
	// ... existing config, logger, db, gamedata, redis, jwt setup ...

	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node-game-1"
	}

	// Room hub
	roomHub := room.NewHub(redisClient, db, nodeID)
	roomWSHandler := room.NewWSHandler(roomHub)

	// Lobby hub
	lobbyHub := lobby.NewLobbyHub(db, redisClient, nodeID)

	// Collect available map IDs
	mapIDs := make([]string, 0)
	for mapID := range gamedata.Data.Maps {
		mapIDs = append(mapIDs, mapID)
	}

	// Matchmaker
	mm := matchmaker.NewMatchmaker(db, redisClient, nodeID, roomHub, mapIDs)
	go mm.Run()

	lobbyWSHandler := lobby.NewWSHandler(lobbyHub, mm)

	// Wire lobby notifier so room hub can notify lobby clients
	roomHub.SetLobbyNotifier(func(lobbyID string, event string, data interface{}) {
		l, err := lobbyHub.FindLobby(lobbyID)
		if err != nil {
			return
		}
		payload, _ := json.Marshal(data)
		for _, c := range l.Clients {
			select {
			case c.Send <- ws.Message{Event: event, Data: payload}:
			default:
			}
		}
		l.SetStatus("in_match")
	})

	// Composite handler
	compositeHandler := &CompositeWSHandler{
		lobbyHandler: lobbyWSHandler,
		roomHandler:  roomWSHandler,
	}

	wsServer := ws.NewServer(jwtAccess, db, redisClient, compositeHandler, cfg)

	// ... rest of HTTP server setup unchanged ...

	// Graceful shutdown — stop matchmaker
	// ... in shutdown section add:
	mm.Stop()
}
```

- [ ] **Step 3: Add necessary imports**

Add imports for `"battle-squad/internal/game/lobby"`, `"battle-squad/internal/game/matchmaker"`, and `"encoding/json"`.

- [ ] **Step 4: Verify compilation**

Run: `go build ./cmd/game/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add cmd/game/main.go
git commit -m "feat: wire composite handler, lobby hub, and matchmaker in game server"
```

---

### Task 17: Admin Dashboard — Matchmaking Config Endpoints

**Files:**
- Create: `internal/admin/handlers_matchmaking.go`
- Modify: `internal/admin/server.go`
- Modify: `internal/admin/repository.go`

- [ ] **Step 1: Add JSON setting get/upsert to repository**

In `internal/admin/repository.go`, add:

```go
func (r *Repository) GetJSONSetting(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("get setting %s: %w", key, err)
	}
	return value, nil
}

func (r *Repository) UpsertJSONSetting(ctx context.Context, key, value string) error {
	_, err := r.db.Pool.Exec(ctx,
		`UPDATE game_settings SET value = $1, updated_at = CURRENT_TIMESTAMP WHERE key = $2`,
		value, key)
	if err != nil {
		return fmt.Errorf("update setting %s: %w", key, err)
	}
	return nil
}
```

- [ ] **Step 2: Create matchmaking admin handlers**

```go
// internal/admin/handlers_matchmaking.go
package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleMatchmakingConfigGet(settingKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, err := s.repo.GetJSONSetting(r.Context(), settingKey)
		if err != nil {
			observability.Log.Error().Err(err).Str("key", settingKey).Msg("failed to get config")
			http.Error(w, "Config not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(value))
	}
}

func (s *Server) handleMatchmakingConfigSave(settingKey string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Validate JSON
		var js json.RawMessage
		if err := json.Unmarshal(body, &js); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if err := s.repo.UpsertJSONSetting(r.Context(), settingKey, string(body)); err != nil {
			observability.Log.Error().Err(err).Str("key", settingKey).Msg("failed to save config")
			http.Error(w, "Failed to save", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}
}
```

- [ ] **Step 3: Register routes in server.go**

In `internal/admin/server.go`, add to the `Routes()` method:

```go
	// Matchmaking config
	r.Get("/api/config/matchmaking", s.handleMatchmakingConfigGet("matchmaking"))
	r.Post("/api/config/matchmaking", s.handleMatchmakingConfigSave("matchmaking"))
	r.Get("/api/config/elo", s.handleMatchmakingConfigGet("elo"))
	r.Post("/api/config/elo", s.handleMatchmakingConfigSave("elo"))
	r.Get("/api/config/bot-difficulty", s.handleMatchmakingConfigGet("bot_difficulty"))
	r.Post("/api/config/bot-difficulty", s.handleMatchmakingConfigSave("bot_difficulty"))
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/admin/... && go build ./cmd/admin/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/admin/handlers_matchmaking.go internal/admin/server.go internal/admin/repository.go
git commit -m "feat: add admin dashboard endpoints for matchmaking config"
```

---

### Task 18: Integration — Full Build & Smoke Test

**Files:** None (verification only)

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: All packages compile

- [ ] **Step 2: Run all existing tests**

Run: `go test ./... -v -count=1`
Expected: All existing tests pass (may need to update test calls that now require new parameters in ProcessMatchRewards)

- [ ] **Step 3: Fix any test breakages from ProcessMatchRewards signature change**

Update test calls to include the new `teamRatings` and `eloParams` parameters with empty defaults:

```go
ProcessMatchRewards(ctx, db, economyRepo, matchID, mode, mapID, stats, playerItems, map[int]int{}, match.EloParams{})
```

- [ ] **Step 4: Run migration and start servers**

```bash
go run cmd/migrate/main.go
go run cmd/api/main.go &
go run cmd/game/main.go &
go run cmd/admin/main.go &
```

Expected: All three servers start without errors

- [ ] **Step 5: Verify admin config endpoints**

```bash
curl http://localhost:9000/api/config/matchmaking | jq .
curl http://localhost:9000/api/config/elo | jq .
curl http://localhost:9000/api/config/bot-difficulty | jq .
```

Expected: Returns JSON config for each

- [ ] **Step 6: Commit final cleanup**

```bash
git add -A
git commit -m "fix: resolve compilation and test issues for matchmaking integration"
```

---

## Task Dependency Order

```
Task 1 (migration)
  → Task 2 (config models)
  → Task 3 (client LobbyID)
  → Task 4 (lobby model)
  → Task 5 (lobby hub)
  → Task 6 (lobby event loop)
  → Task 7 (lobby handler)
  → Task 8 (queue Redis ops)
  → Task 9 (Elo calc) — independent, can parallel with 4-8
  → Task 10 (smart bot) — independent, can parallel with 4-8
  → Task 11 (matchmaker engine) — depends on 2, 8
  → Task 12 (reward Elo update) — depends on 9
  → Task 13 (lobby mapping) — depends on 6
  → Task 14 (room creator bridge) — depends on 11, 13
  → Task 15 (start/cancel queue) — depends on 7, 11
  → Task 16 (composite handler) — depends on 7, 14, 15
  → Task 17 (admin config) — independent, can parallel
  → Task 18 (integration test) — depends on all
```

Parallelizable groups:
- **Group A:** Tasks 4-8 (lobby system)
- **Group B:** Tasks 9-10 (Elo + bot AI) — can run parallel with Group A
- **Group C:** Task 17 (admin) — can run parallel with everything
