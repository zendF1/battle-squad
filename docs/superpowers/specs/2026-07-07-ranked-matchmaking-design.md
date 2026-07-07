# Ranked Matchmaking System — Design Spec

**Date:** 2026-07-07
**Mode:** Ranked 2v2 (default)
**Approach:** Matchmaker goroutine + Redis queue (single-node with leader election for multi-node)

---

## 1. User Flow

### Full Flow

```
1. Player vào ranked mode → server tạo Lobby Room
2. (Optional) Mời đồng đội → đồng đội join Lobby Room
3. Mỗi người chọn character/items trong lobby → update vào DB (player_loadouts)
4. Host bấm "Tìm trận" → cả lobby vào matchmaking queue
5. Matchmaker ghép 2 lobby có rating gần nhau
6. Ghép xong → server tạo Battle Room (4 người) → trận bắt đầu ngay
7. Hết timeout chờ (default 60s) → điền Smart Bot vào slot trống
8. Trận kết thúc → Battle Room xóa → players quay về Lobby Room của host team mình
```

### Queue Support

- **Solo queue:** 1 người tạo lobby, vào queue một mình. Matchmaker ghép với solo khác hoặc party.
- **Party queue:** 2 người cùng lobby, vào queue cùng nhau. Matchmaker ghép với party khác hoặc 2 solo.
- **Disconnect khi đang queue:** Hủy queue cho cả party ngay lập tức, người còn lại nhận thông báo `QueueCancelled` với reason `"teammate disconnected"`.

### Map Selection

Ranked match map được chọn **random** từ danh sách map available (`gamedata.Data.Maps`). Matchmaker chọn map khi tạo Battle Room.

---

## 2. Room Architecture

### 2 loại Room

| Aspect | Lobby Room | Battle Room |
|--------|-----------|-------------|
| Mục đích | Phòng chuẩn bị team | Phòng đấu |
| Tạo bởi | Hệ thống (khi vào ranked) | Matchmaker (khi ghép xong) |
| Max players | 1-2 (cùng team) | 4 (2 teams) |
| Chọn char/items | Có | Không (đã chọn từ lobby) |
| Tồn tại | Lâu dài (đến khi host thoát, hoặc idle 30 phút) | Ngắn (trận xong → xóa) |
| Sau trận | Players quay về | Bị xóa |

### Lobby Room Structure

```go
type LobbyRoom struct {
    ID      string
    State   LobbyState
    Clients map[string]*ws.Client  // active WebSocket connections
    Events  chan lobbyEvent
    hub     *LobbyHub
    mu      sync.RWMutex
    ctx     context.Context
    cancel  context.CancelFunc
}

type LobbyState struct {
    LobbyID      string        `json:"lobbyId"`
    HostPlayerID string        `json:"hostPlayerId"`
    Members      []LobbyMember `json:"members"`
    Status       string        `json:"status"`       // "preparing" | "in_queue" | "in_match"
    QueueEntryID string        `json:"-"`             // set khi vào queue
}

type LobbyMember struct {
    PlayerID    string   `json:"playerId"`
    DisplayName string   `json:"displayName"`
    CharacterID string   `json:"characterId"`
    Items       []string `json:"items"`
    Rating      int      `json:"rating"`
    Tier        string   `json:"tier"`
}
```

- Lobby room là goroutine nhẹ trong `LobbyHub` (actor pattern, giống Room).
- Character/items được lưu trong `player_loadouts` DB table, đồng thời cached trong `LobbyMember` để broadcast cho client.
- Khi `UpdateLoadout` → validate ownership → update DB → update member state → broadcast `LobbyUpdated`.

### Lobby Lifecycle

```
CreateLobby
     │
     ▼
 [preparing] ← LeaveLobby (member) / UpdateLoadout / JoinLobby
     │
     │ StartQueue
     ▼
 [in_queue] ← CancelQueue → [preparing]
     │
     │ MatchFound (từ matchmaker)
     ▼
 [in_match]
     │
     │ Match ended → ReturnToLobby
     ▼
 [preparing]  ← tự động quay về
```

### Battle Room

Chính là `Room` hiện tại (`internal/game/room/room.go`). Bổ sung:
- `LobbyMapping map[string]string` trong `RoomState` — playerID → lobbyID, để biết trận xong chuyển ai về lobby nào.
- `HasBot bool` trong `RoomState` — đánh dấu trận có bot.
- Khi match kết thúc (qua `MatchDone` channel), gửi `ReturnToLobby` event cho mỗi player với `lobbyId` tương ứng trước khi xóa room.

---

## 3. Data Layer

### Player Loadout — PostgreSQL

```sql
CREATE TABLE player_loadouts (
    player_id    VARCHAR(64) PRIMARY KEY REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    character_id VARCHAR(64) NOT NULL DEFAULT 'rookie',
    items        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at   TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

- 1 user = 1 row, dùng chung cho mọi mode
- Đổi character/items trong lobby → update DB ngay (upsert)
- Khi tạo lobby → đọc loadout từ DB làm default cho member

Migration file: `migrations/005_add_loadouts_and_matchmaking.up.sql`

### Matchmaking Queue — Redis

**Sorted Set (queue):**
```
Key:    "matchmaking:queue:2v2"
Type:   Sorted Set
Score:  rating dùng để ghép (tính theo party strategy config)
Member: queueEntryID
```

**Entry Detail (JSON string, không phải hash):**
```
Key:    "matchmaking:entry:{queueEntryID}"
Type:   String (JSON-serialized QueueEntry)
TTL:    120 giây
```

QueueEntry struct:
```go
type QueueEntry struct {
    EntryID       string              `json:"entryId"`
    LobbyID       string              `json:"lobbyId"`
    PlayerIDs     []string            `json:"playerIds"`
    Rating        int                 `json:"rating"`
    PlayerRatings map[string]int      `json:"playerRatings"`
    PlayerChars   map[string]string   `json:"playerChars"`
    PlayerItems   map[string][]string `json:"playerItems"`
    PlayerNames   map[string]string   `json:"playerNames"`
    TeamSize      int                 `json:"teamSize"`
    QueuedAt      int64               `json:"queuedAt"`
    NodeID        string              `json:"nodeId"`
}
```

**Player → Queue Mapping:**
```
Key:    "matchmaking:player:{playerID}"
Type:   String
Value:  queueEntryID
TTL:    120 giây
```

**Leader Lock:**
```
Key:    "matchmaking:leader"
Type:   String (SET NX EX 10)
Value:  nodeId
```

---

## 4. Matchmaker Engine

### Architecture

Matchmaker chạy như 1 goroutine trong game server. Multi-node sử dụng Redis leader election — chỉ 1 node chạy matchmaker tại một thời điểm.

```
Matchmaker goroutine
    │
    ├── Acquire leader lock (Redis SETNX, TTL 10s, renew mỗi 5s)
    │
    ├── Tick mỗi N giây (configurable, default 3)
    │     1. GetAllEntries từ Redis sorted set
    │     2. Entries đã sorted theo rating (ascending)
    │     3. Duyệt tuần tự, tìm cặp trong allowed range
    │     4. Ghép được → tạo Battle Room → notify clients
    │     5. Entries quá timeout → điền bot → tạo trận
    │
    ├── Config reload mỗi 30 giây (hot-reload từ game_settings DB)
    │
    └── Node crash → lock expire 10s → node khác lên leader
```

### Matching Algorithm

```
Mỗi tick:
  1. entries = GetAllEntries() (đọc sorted set + entry details từ Redis)
  2. Duyệt tuần tự:
     - waitTime = now - entry.queuedAt
     - allowedRange = baseRange + (waitTime / expandInterval) * expandStep
       Ví dụ: 100 + (waitTime/10s) * 50, cap tại maxRange (300)
     - Kiểm tra timeout trước: nếu waitTime >= maxWaitTime → matchWithBot
     - Tìm entry tiếp theo chưa ghép: |rating1 - rating2| ≤ min(allowedRange1, allowedRange2)
     - Ghép được → Dequeue cả 2 → gọi roomCreator.CreateBattleFromMatch
  3. Entries quá maxWaitTime (default 60s):
     - Dequeue entry → tạo MatchResult với bot team (TeamSize=0, empty PlayerIDs)
     - Gọi roomCreator.CreateBattleFromMatch → room hub tự điền bot players
```

### Party Rating Calculation

3 chiến lược, chọn từ admin dashboard (`partyRatingStrategy` trong matchmaking config):

| Strategy | Công thức |
|----------|----------|
| `max` (default) | `max(ratings...)` |
| `average` | `sum(ratings) / len(ratings)` |
| `weighted` | `maxRating * weightedRatio + avgRating * (1 - weightedRatio)`, ratio configurable (default 0.7) |

Solo queue: dùng rating của chính player đó.

### RoomCreator Interface

Matchmaker giao tiếp với Room Hub qua interface:

```go
type RoomCreator interface {
    CreateBattleFromMatch(ctx context.Context, result MatchResult, botDiffConfig BotDifficultyConfig, eloConfig EloConfig) error
}
```

`room.Hub` implement interface này. Khi match found:
1. Tạo Room với `Mode = "ranked_2v2"`, `Status = "in_match"`
2. Build players từ 2 entries, điền bot cho slot trống
3. Set `LobbyMapping` cho return-to-lobby
4. Gửi `MatchFound` cho lobby clients qua `lobbyNotifier` callback
5. Gọi `room.startRankedMatch()` để khởi tạo match engine

---

## 5. Elo Rating System (2v2)

### Công thức

```
1. Team rating = trung bình rating 2 player
   teamA_rating = (playerA1.rating + playerA2.rating) / 2
   teamB_rating = (playerB1.rating + playerB2.rating) / 2

2. Expected score
   expectedA = 1 / (1 + 10^((teamB_rating - teamA_rating) / 400))
   expectedB = 1 - expectedA

3. Rating change
   K = 32 (configurable)
   Win:  actualScore = 1
   Loss: actualScore = 0
   Draw: actualScore = 0.5

   ratingChange = round(K * (actualScore - expectedScore))

4. Bot modifier (nếu trận có bot)
   ratingChange = round(ratingChange * botRatingModifier)
   botRatingModifier default = 0.5 (configurable)
```

### Implementation

```go
// internal/game/match/elo.go
type EloParams struct {
    KFactor     int
    RatingFloor int
    BotModifier float64
    HasBot      bool
}

func CalculateEloChange(teamRating, opponentRating int, actualScore float64, params EloParams) int
func TeamAvgRating(ratings []int) int
```

Rating update xảy ra trong `ProcessMatchRewards()` (`reward.go`) khi `mode == "ranked_2v2"`.

Match struct lưu `TeamRatings map[int]int` và `EloParams` để truyền vào reward processing.

### Đặc điểm

- Cùng team → cùng rating change (không phân biệt performance cá nhân)
- Thắng team mạnh hơn → được nhiều, mất ít
- Thắng team yếu hơn → được ít, mất nhiều
- Rating floor: 0 (không âm)
- Default rating: 1000
- Trận có bot: rating change giảm theo % configurable

### Ví dụ

```
Team A: (1200 + 1300) / 2 = 1250
Team B: (1400 + 1500) / 2 = 1450

expectedA = 0.24, expectedB = 0.76

Team A thắng (upset): +24 mỗi người Team A, -24 mỗi người Team B
Team B thắng (expected): +8 mỗi người Team B, -8 mỗi người Team A
```

---

## 6. Smart Bot AI

### Decision System — State-based

```go
// internal/game/match/bot_ai.go
type SmartBotBrain struct {
    tierConfig matchmaker.BotTierConfig
    rng        *rand.Rand
}

func NewSmartBotBrain(tierConfig matchmaker.BotTierConfig) *SmartBotBrain
func (b *SmartBotBrain) DecideAction(botState *BattlePlayerState, matchState *MatchState) interface{}
```

Mỗi lượt bot:

```
1. Evaluate situation
   - HP mình / HP max (hpRatio)
   - Đối thủ alive gần nhất (findClosestEnemy)
   - Khoảng cách đến đối thủ
   - Items còn lại

2. Score mỗi hành động (float64)
   - shootScore: 80 khi target ở medium range (200-1000px), thấp hơn khi quá gần/xa
   - moveScore: cao khi quá gần + HP thấp (retreat), hoặc quá xa (advance). Nhân với MovementSmart
   - useItemScore: cao khi HP < 50% + có medkit + qua UseItemChance probability check

3. Thêm noise theo rank
   - noise = random trong [-DecisionNoise, +DecisionNoise]
   - Rank thấp (Bronze, noise=30): quyết định hay sai
   - Rank cao (Master, noise=5): gần tối ưu

4. Chọn hành động có score cao nhất sau noise
   - Ưu tiên: useItem > shoot > move (khi score bằng nhau)
```

### Shooting Accuracy

```
1. Tính góc "hoàn hảo" dựa trên atan2(dy, dx)
2. Bù gió (wind compensation): nếu random < MovementSmart/2, bù góc theo hướng/lực gió
3. Thêm error theo rank tier:
   actualAngle = perfectAngle + random(-AccuracyError, +AccuracyError)
   actualPower = basePower + random(-PowerError, +PowerError)
4. Clamp power trong [20, 100]
```

### Bot Rank

Bot nhận tier của trận đấu: `ratingToTier(avgRating)` — trung bình rating 2 entries → map sang tier → dùng BotTierConfig tương ứng.

```go
func ratingToTier(rating int) string {
    switch {
    case rating < 1000:  return "bronze"
    case rating < 1200:  return "silver"
    case rating < 1500:  return "gold"
    case rating < 1800:  return "platinum"
    case rating < 2200:  return "diamond"
    default:             return "master"
    }
}
```

### Difficulty Config (per tier)

| Tier | AccuracyError | PowerError | DecisionNoise | UseItemChance | MovementSmart |
|------|--------------|------------|---------------|---------------|---------------|
| Bronze | 15 | 12 | 30 | 0.3 | 0.3 |
| Silver | 12 | 10 | 25 | 0.4 | 0.4 |
| Gold | 9 | 8 | 20 | 0.55 | 0.55 |
| Platinum | 6 | 5 | 15 | 0.7 | 0.7 |
| Diamond | 4 | 3 | 8 | 0.85 | 0.85 |
| Master | 2 | 2 | 5 | 0.9 | 0.95 |

Config lưu trong `game_settings`, key = `"bot_difficulty"`, value = JSON. Admin tune từng tier qua dashboard.

---

## 7. WebSocket Events

### Client → Server

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateLobby` | `{}` | Tạo lobby room, host tự join |
| `JoinLobby` | `{lobbyId}` | Join lobby theo ID |
| `LeaveLobby` | `{}` | Rời lobby |
| `UpdateLoadout` | `{characterId?, items?}` | Đổi character/items → validate → update DB → broadcast |
| `StartQueue` | `{}` | Host bấm tìm trận → vào matchmaking queue |
| `CancelQueue` | `{}` | Hủy tìm trận |

### Server → Client

| Event | Payload | Mô tả |
|-------|---------|-------|
| `LobbyUpdated` | `LobbyState` (full) | Lobby state thay đổi (join/leave/loadout/status) |
| `LobbyError` | `{error: {code, message}}` | Lỗi lobby operation |
| `LobbyDisbanded` | `{reason}` | Lobby bị giải tán (host rời) |
| `QueueStarted` | `{estimatedWait}` | Đã vào hàng đợi |
| `QueueCancelled` | `{reason}` | Đã hủy queue. Reason: `"cancelled"` hoặc `"teammate disconnected"` |
| `MatchFound` | `{matchId, roomId, mapId, players[], hasBot}` | Ghép xong, vào trận |
| `ReturnToLobby` | `{lobbyId}` | Trận xong, quay về lobby |

### Routing

Game server sử dụng `CompositeWSHandler` để route events:

```go
type CompositeWSHandler struct {
    lobbyHandler *lobby.WSHandler
    roomHandler  *room.WSHandler
}
```

1. Lobby handler thử xử lý trước (`HandleLobbyMessage` returns bool)
2. Nếu không phải lobby event → fall through sang room handler
3. Disconnect → `UnregisterFromLobby` + `Unregister` (room)

---

## 8. Admin Dashboard Config

### Endpoints

```
GET  /api/config/matchmaking      — Lấy matchmaking config (JSON)
POST /api/config/matchmaking      — Cập nhật matchmaking config
GET  /api/config/elo              — Lấy elo config
POST /api/config/elo              — Cập nhật elo config
GET  /api/config/bot-difficulty   — Lấy bot difficulty config
POST /api/config/bot-difficulty   — Cập nhật bot difficulty config
```

Handler pattern: `handleMatchmakingConfigGet(key)` / `handleMatchmakingConfigSave(key)` — generic cho mọi config key. Save handler validate JSON trước khi upsert.

Tất cả lưu trong bảng `game_settings` (key = config name, value = JSON string).

### Config Structures

**Matchmaking Config** (key: `"matchmaking"`):
```json
{
  "tickInterval": 3,
  "baseRatingRange": 100,
  "expandInterval": 10,
  "expandStep": 50,
  "maxRatingRange": 300,
  "maxWaitTime": 60,
  "botRatingModifier": 0.5,
  "partyRatingStrategy": "max",
  "weightedRatio": 0.7
}
```

**Elo Config** (key: `"elo"`):
```json
{
  "kFactor": 32,
  "ratingFloor": 0,
  "defaultRating": 1000
}
```

**Bot Difficulty Config** (key: `"bot_difficulty"`):
```json
{
  "tiers": {
    "bronze":   {"accuracyError": 15, "powerError": 12, "decisionNoise": 30, "useItemChance": 0.3, "movementSmart": 0.3},
    "silver":   {"accuracyError": 12, "powerError": 10, "decisionNoise": 25, "useItemChance": 0.4, "movementSmart": 0.4},
    "gold":     {"accuracyError": 9,  "powerError": 8,  "decisionNoise": 20, "useItemChance": 0.55, "movementSmart": 0.55},
    "platinum": {"accuracyError": 6,  "powerError": 5,  "decisionNoise": 15, "useItemChance": 0.7, "movementSmart": 0.7},
    "diamond":  {"accuracyError": 4,  "powerError": 3,  "decisionNoise": 8,  "useItemChance": 0.85, "movementSmart": 0.85},
    "master":   {"accuracyError": 2,  "powerError": 2,  "decisionNoise": 5,  "useItemChance": 0.9, "movementSmart": 0.95}
  }
}
```

Matchmaker hot-reload config mỗi 30 giây từ DB.

---

## 9. Edge Cases

| Tình huống | Xử lý |
|-----------|-------|
| Host disconnect khi preparing | Lobby xóa, member nhận `LobbyDisbanded` → main menu |
| Member disconnect khi preparing | Member xóa khỏi lobby, host vẫn ở, broadcast `LobbyUpdated` |
| Host disconnect khi in_queue | `UnregisterFromLobby` cancel queue, lobby xóa |
| Member disconnect khi in_queue | Cancel queue cả party, member xóa, `QueueCancelled` gửi cho người còn lại |
| Host disconnect khi in_match | Trận tiếp tục (match engine xử lý Leave). Lobby xóa. Trận xong member về main menu |
| Member disconnect khi in_match | Trận tiếp tục. Trận xong member không quay lobby (đã disconnect) |
| Player đang trong regular room bấm CreateLobby | Reject: `"Leave your current room first"` |
| Player đã có lobby bấm CreateLobby | Reject: `"You are already in a lobby"` |
| Lobby chỉ có 1 người bấm StartQueue | OK — vào queue như solo, matchmaker ghép với người khác |
| Non-host bấm StartQueue | Reject: `"Only the host can start queue"` |
| Lobby đang in_queue bấm StartQueue lại | Reject: `"Lobby is not in preparing state"` |
| Matchmaker node crash | Leader lock expire 10s → node khác lên qua SETNX |
| Redis entry expired (TTL 120s) | GetAllEntries tự cleanup orphan từ sorted set (ZREM) |
| Idle lobby 30 phút không ai | Lobby goroutine tự destroy |

---

## 10. Files & Packages

```
internal/game/lobby/
  ├── model.go          — LobbyState, LobbyMember, payloads
  ├── hub.go            — LobbyHub (registry), loadPlayerLoadout, loadPlayerRating
  ├── lobby.go          — LobbyRoom goroutine event loop, join/leave/updateLoadout
  ├── handler.go        — WSHandler: CreateLobby, JoinLobby, LeaveLobby, UpdateLoadout, StartQueue, CancelQueue

internal/game/matchmaker/
  ├── model.go          — QueueEntry, MatchmakingConfig, EloConfig, BotDifficultyConfig, config loading
  ├── queue.go          — Redis queue operations (Enqueue, Dequeue, GetAllEntries, CancelByPlayer)
  ├── matchmaker.go     — Matchmaker goroutine, tick loop, matching algorithm, leader election, RoomCreator interface

internal/game/match/
  ├── elo.go            — EloParams, CalculateEloChange, TeamAvgRating
  ├── elo_test.go       — Unit tests for Elo calculation
  ├── bot_ai.go         — SmartBotBrain (state-based decisions, rank-based accuracy)
  ├── bot.go            — (existing) idle BotBrain for tutorial
  ├── reward.go         — (modified) ProcessMatchRewards supports ranked_2v2 Elo
  ├── match.go          — (modified) added TeamRatings, EloParams fields to Match struct

internal/game/room/
  ├── hub.go            — (modified) CreateBattleFromMatch, SetLobbyNotifier, ratingToTier
  ├── room.go           — (modified) startRankedMatch, ReturnToLobby on match end
  ├── model.go          — (modified) LobbyMapping, HasBot in RoomState

internal/game/ws/
  ├── client.go         — (modified) added LobbyID field

internal/admin/
  ├── handlers_matchmaking.go — GET/POST handlers for matchmaking/elo/bot config
  ├── server.go         — (modified) added 6 config API routes
  ├── repository.go     — (modified) added GetJSONSetting, UpsertJSONSetting

cmd/game/
  ├── main.go           — (modified) CompositeWSHandler, LobbyHub, Matchmaker wiring

migrations/
  ├── 005_add_loadouts_and_matchmaking.up.sql   — player_loadouts table + config seeds
  ├── 005_add_loadouts_and_matchmaking.down.sql
```

---

## 11. Future Improvements (Not Implemented)

Các tính năng đã thiết kế trong brainstorming nhưng chưa implement:

- **`InviteToLobby` / `LobbyInvite`**: Mời player cụ thể vào lobby qua notification. Hiện tại player phải biết lobbyId để JoinLobby.
- **`QueueUpdate`**: Gửi periodic update cho client về thời gian chờ và rating range hiện tại. Hiện tại client chỉ nhận `QueueStarted` và `QueueCancelled`/`MatchFound`.
- **BattleRoomID trên LobbyState**: Track battle room ID trên lobby. Hiện tại dùng reverse mapping (`LobbyMapping` trên RoomState).
