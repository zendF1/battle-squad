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
- **Disconnect khi đang queue:** Hủy queue cho cả party ngay lập tức, người còn lại nhận thông báo.

### Map Selection

Ranked match map được chọn **random** từ danh sách map available (cấu hình trong gamedata). Matchmaker chọn map khi tạo Battle Room.

---

## 2. Room Architecture

### 2 loại Room

| Aspect | Lobby Room | Battle Room |
|--------|-----------|-------------|
| Mục đích | Phòng chuẩn bị team | Phòng đấu |
| Tạo bởi | Hệ thống (khi vào ranked) | Matchmaker (khi ghép xong) |
| Max players | 1-2 (cùng team) | 4 (2 teams) |
| Chọn char/items | Có | Không (đã chọn từ lobby) |
| Tồn tại | Lâu dài (đến khi host thoát) | Ngắn (trận xong → xóa) |
| Sau trận | Players quay về | Bị xóa |

### Lobby Room Structure

```go
type LobbyRoom struct {
    ID           string
    HostPlayerID string
    Members      map[string]*ws.Client  // 1-2 người
    Status       string                 // "preparing" | "in_queue" | "in_match"
    QueueEntryID string                 // set khi vào queue
    BattleRoomID string                 // set khi match found
    Events       chan lobbyEvent
}
```

Lobby room là goroutine nhẹ trong Hub. Không lưu character/items — đọc từ `player_loadouts` khi cần.

### Lobby Lifecycle

```
CreateLobby
     │
     ▼
 [preparing] ← LeaveLobby (member) / UpdateLoadout / InviteToLobby
     │
     │ StartQueue
     ▼
 [in_queue] ← CancelQueue → [preparing]
     │
     │ MatchFound
     ▼
 [in_match]
     │
     │ Match ended
     ▼
 [preparing]  ← tự động quay về
```

### Battle Room

Chính là Room hiện tại (`internal/game/room/room.go`). Bổ sung field `lobbyMapping map[string]string` (playerID → lobbyID) để biết trận xong chuyển ai về lobby nào.

---

## 3. Data Layer

### Player Loadout — PostgreSQL

```sql
CREATE TABLE player_loadouts (
    player_id    VARCHAR PRIMARY KEY REFERENCES accounts(account_id),
    character_id VARCHAR NOT NULL DEFAULT 'rookie',
    items        JSONB   NOT NULL DEFAULT '[]',
    updated_at   TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

- 1 user = 1 row, dùng chung cho mọi mode
- Đổi character/items trong lobby → update DB ngay
- Regular room cũng đọc loadout này làm default

### Matchmaking Queue — Redis

**Sorted Set (queue):**
```
Key:    "matchmaking:queue:2v2"
Type:   Sorted Set
Score:  rating dùng để ghép (tính theo party strategy config)
Member: queueEntryID
```

**Entry Detail (hash):**
```
Key:    "matchmaking:entry:{queueEntryID}"
Type:   Hash
TTL:    120 giây
Fields:
  - lobbyId         : lobby room ID
  - playerIds       : "player1,player2" hoặc "player1"
  - rating          : rating dùng để ghép
  - player1Rating   : rating thật player 1
  - player2Rating   : rating thật player 2 (nếu party)
  - player1Char     : character ID
  - player2Char     : character ID
  - player1Items    : items JSON
  - player2Items    : items JSON
  - player1Name     : display name
  - player2Name     : display name
  - teamSize        : 1 hoặc 2
  - queuedAt        : unix timestamp
  - nodeId          : game server node
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
    ├── Tick mỗi 2-3 giây (configurable)
    │     1. ZRANGEBYSCORE lấy tất cả entries
    │     2. Sort theo rating
    │     3. Duyệt tuần tự, tìm cặp trong allowed range
    │     4. Ghép được → tạo Battle Room → notify clients
    │     5. Entries quá timeout → điền bot → tạo trận
    │
    └── Node crash → lock expire 10s → node khác lên leader
```

### Matching Algorithm

```
Mỗi tick:
  1. entries = ZRANGEBYSCORE "matchmaking:queue:2v2" -inf +inf
  2. Sort theo score (rating)
  3. Duyệt tuần tự:
     - waitTime = now - entry.queuedAt
     - allowedRange = baseRange + (waitTime / expandInterval) * expandStep
       Ví dụ: 100 + (waitTime/10s) * 50, cap tại maxRange (300)
     - Tìm entry tiếp theo chưa ghép: |rating1 - rating2| ≤ min(allowedRange1, allowedRange2)
     - Ghép được → xóa cả 2 khỏi queue → tạo Battle Room
  4. Entries có waitTime ≥ maxWaitTime (default 60s):
     - Điền Smart Bot vào slot trống → tạo trận
```

### Party Rating Calculation

3 chiến lược, chọn từ admin dashboard:

| Strategy | Công thức |
|----------|----------|
| `max` (default) | `max(player1.rating, player2.rating)` |
| `average` | `(player1.rating + player2.rating) / 2` |
| `weighted` | `player_max.rating * ratio + player_min.rating * (1 - ratio)`, ratio configurable (default 0.7) |

Solo queue: dùng rating của chính player đó.

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

Mỗi lượt bot:

```
1. Evaluate situation
   - HP mình / HP max
   - HP đối thủ gần nhất
   - Khoảng cách đến từng đối thủ
   - Items còn lại
   - Status effects đang active

2. Score mỗi hành động (0-100)
   - shootScore: cao khi đối thủ trong tầm bắn tốt
   - moveScore: cao khi vị trí nguy hiểm hoặc cần tiến lại gần
   - useItemScore: cao khi HP thấp + có heal, hoặc có item lợi thế

3. Thêm noise theo rank
   - Rank thấp: noise lớn → quyết định hay sai
   - Rank cao: noise nhỏ → gần tối ưu

4. Chọn hành động có score cao nhất sau noise
```

### Shooting Accuracy

```
1. Tính góc + lực "hoàn hảo" (trúng chính xác đối thủ, tính cả gió)
2. Thêm error theo rank tier:
   actualAngle = perfectAngle + random(-accuracyError, +accuracyError)
   actualPower = perfectPower + random(-powerError, +powerError)
```

### Bot Rank

Bot nhận tier của trận đấu: trung bình rating các player thật → map sang tier → dùng config tương ứng.

### Difficulty Config (per tier)

| Tier | AccuracyError | PowerError | DecisionNoise | UseItemChance | MovementSmart |
|------|--------------|------------|---------------|---------------|---------------|
| Bronze | 15 | 12 | 30 | 0.3 | 0.3 |
| Silver | 12 | 10 | 25 | 0.4 | 0.4 |
| Gold | 9 | 8 | 20 | 0.55 | 0.55 |
| Platinum | 6 | 5 | 15 | 0.7 | 0.7 |
| Diamond | 4 | 3 | 8 | 0.85 | 0.85 |
| Master | 2 | 2 | 5 | 0.9 | 0.95 |

- `AccuracyError`: sai số góc bắn (độ)
- `PowerError`: sai số lực bắn
- `DecisionNoise`: noise thêm vào action score
- `UseItemChance`: xác suất dùng item đúng lúc
- `MovementSmart`: xác suất di chuyển thông minh

Config lưu trong `app_configs`, key = `"bot_difficulty"`, value = JSON. Admin tune từng tier.

---

## 7. WebSocket Events

### Client → Server

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateLobby` | `{}` | Tạo lobby room |
| `InviteToLobby` | `{playerId}` | Mời player vào lobby |
| `JoinLobby` | `{lobbyId}` | Accept lời mời, join lobby |
| `LeaveLobby` | `{}` | Rời lobby |
| `UpdateLoadout` | `{characterId?, items?}` | Đổi character/items → update DB |
| `StartQueue` | `{}` | Bấm tìm trận |
| `CancelQueue` | `{}` | Hủy tìm trận |

### Server → Client

| Event | Payload | Mô tả |
|-------|---------|-------|
| `LobbyUpdated` | `{lobbyId, host, members[], status}` | Lobby state thay đổi |
| `LobbyInvite` | `{lobbyId, hostName}` | Nhận lời mời từ host |
| `QueueStarted` | `{estimatedWait}` | Đã vào hàng đợi |
| `QueueUpdate` | `{waitTime, currentRange}` | Cập nhật thời gian chờ |
| `QueueCancelled` | `{reason}` | Đã hủy queue |
| `MatchFound` | `{matchId, players[], mapId, hasBot}` | Ghép xong, vào trận |
| `ReturnToLobby` | `{lobbyId}` | Trận xong, quay về lobby |

---

## 8. Admin Dashboard Config

### Endpoints cần thêm

```
GET  /admin/config/matchmaking      — Lấy matchmaking config
PUT  /admin/config/matchmaking      — Cập nhật matchmaking config
GET  /admin/config/elo              — Lấy elo config
PUT  /admin/config/elo              — Cập nhật elo config
GET  /admin/config/bot-difficulty   — Lấy bot difficulty config
PUT  /admin/config/bot-difficulty   — Cập nhật bot difficulty config
```

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

Tất cả lưu trong bảng `app_configs` hiện có.

---

## 9. Edge Cases

| Tình huống | Xử lý |
|-----------|-------|
| Host disconnect khi preparing | Lobby xóa, member kick → main menu |
| Member disconnect khi preparing | Member xóa khỏi lobby, host vẫn ở |
| Host disconnect khi in_queue | Hủy queue cả party, xóa lobby |
| Member disconnect khi in_queue | Hủy queue cả party, xóa member khỏi lobby |
| Host disconnect khi in_match | Trận tiếp tục (match engine xử lý). Lobby xóa. Trận xong member về main menu |
| Member disconnect khi in_match | Trận tiếp tục. Trận xong member không quay lobby (đã disconnect) |
| Player đang trong regular room bấm ranked | Phải rời regular room trước |
| Player đang trong lobby queue lại | Reject, đã trong queue |
| Lobby chỉ có 1 người bấm queue | OK — vào queue như solo, matchmaker ghép với người khác |
| Matchmaker node crash | Leader lock expire 10s → node khác lên |

---

## 10. New Files & Packages

```
internal/game/lobby/
  ├── lobby.go          — LobbyRoom struct, goroutine event loop
  ├── handler.go        — WebSocket event handler (CreateLobby, JoinLobby, etc.)
  ├── model.go          — LobbyState, payloads

internal/game/matchmaker/
  ├── matchmaker.go     — Matchmaker goroutine, tick loop, matching algorithm
  ├── queue.go          — Redis queue operations (add, remove, scan)
  ├── config.go         — Config loading from app_configs
  ├── model.go          — QueueEntry, MatchmakingConfig, EloConfig

internal/game/match/
  ├── bot_ai.go         — Smart Bot AI (state-based decisions, accuracy)
  ├── bot.go            — (existing) idle bot logic for tutorial
  ├── reward.go         — (modify) add Elo 2v2 calculation

internal/admin/
  ├── (existing files)  — Add matchmaking/elo/bot config endpoints

migrations/
  ├── 002_add_loadouts.up.sql    — player_loadouts table
  ├── 002_add_loadouts.down.sql
```
