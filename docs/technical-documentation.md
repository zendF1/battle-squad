# Battle Squad — Server Technical Documentation

**Version:** 1.0.0 | **Last Updated:** 2026-07-07 | **Language:** Go 1.22+

---

## 1. System Overview

Battle Squad là game bắn súng theo lượt (turn-based artillery). Server gồm 4 process độc lập + 1 migration tool:

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│  API Server  │  │ Game Server  │  │   Worker     │  │    Admin     │
│  REST :8080  │  │   WS :8081   │  │  (cron jobs) │  │  Web :9000   │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │                 │
       └────────────┬────┴─────────────────┴─────────────────┘
                    │
          ┌─────────┴─────────┐
          │   PostgreSQL 15   │
          │   Redis 7         │
          └───────────────────┘
```

| Process | Port | Vai trò |
|---------|------|---------|
| API Server | 8080 | REST API: auth, profile, economy, shop, IAP, missions, ranking, moderation |
| Game Server | 8081 | WebSocket: lobby, matchmaking, rooms, matches (real-time) |
| Worker | — | Background jobs: ban expiry (5m), daily reset (1h), account deletion (1h) |
| Admin Dashboard | 9000 | Web UI + API: game config CRUD, player management, matchmaking tuning |
| Migrate | — | Chạy 1 lần: apply SQL migrations từ `migrations/` |

**Tại sao tách process:** API crash không ảnh hưởng trận đấu đang chơi. Mỗi process scale độc lập.

---

## 2. Tech Stack

| Thành phần | Công nghệ | Version |
|-----------|-----------|---------|
| Language | Go | 1.22+ |
| HTTP Router | chi/v5 | 5.3.0 |
| WebSocket | gorilla/websocket | 1.5.3 |
| Database | PostgreSQL (pgx/v5) | 15 / 5.10.0 |
| Cache/Queue | Redis (go-redis/v9) | 7 / 9.21.0 |
| Auth | JWT (golang-jwt/v5) | 5.3.1 |
| Logging | zerolog | 1.35.1 |
| Metrics | Prometheus client_golang | 1.23.2 |
| Config | YAML (gopkg.in/yaml.v3) | 3.0.1 |
| Crypto | golang.org/x/crypto | 0.53.0 |
| Rate Limit | golang.org/x/time | 0.15.0 |

---

## 3. Configuration

Tất cả config qua environment variables, có default cho local dev.

| Env Var | Default | Mô tả |
|---------|---------|-------|
| `APP_ENV` | `development` | Environment (development/production) |
| `API_PORT` | `8080` | API server port |
| `GAME_PORT` | `8081` | Game server port |
| `ADMIN_PORT` | `9000` | Admin dashboard port |
| `POSTGRES_DSN` | `postgres://postgres:postgres@localhost:5432/battlesquad?sslmode=disable` | PostgreSQL connection |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | _(empty)_ | Redis password |
| `REDIS_DB` | `0` | Redis database index |
| `JWT_SECRET` | `super-secret-battle-squad-key-2026` | JWT signing secret |
| `APP_VERSION` | `1.0.0` | App version |
| `PROTOCOL_VERSION` | `1` | Protocol version (force update nếu client < server) |
| `NODE_ID` | `node-game-1` | Game server node ID (multi-node) |

---

## 4. Project Structure

```
cmd/
├── api/main.go           REST API server
├── game/main.go          WebSocket game server
├── admin/main.go         Admin dashboard
├── worker/main.go        Background jobs
└── migrate/main.go       Database migration tool

internal/
├── api/                  API server modules (handler → service → repository)
│   ├── auth/             Guest/OAuth login, JWT refresh, provider linking
│   ├── player/           Profile CRUD, account deletion (7-day grace)
│   ├── economy/          Coin/Gem ledger (Credit/Debit trong DB transaction)
│   ├── inventory/        Player items query
│   ├── shop/             Shop offers, purchase (idempotent)
│   ├── iap/              In-app purchase, receipt verification
│   ├── giftcode/         Gift code redemption
│   ├── mission/          Daily missions, achievements, claim rewards
│   ├── rank/             Elo rating, leaderboard, seasons, season rewards
│   ├── moderation/       Player reports, ban/unban (admin only)
│   ├── appconfig/        Version policy, remote config
│   ├── matchhistory/     Match history query
│   ├── rooms/            Room listing (đọc từ Redis)
│   └── dev/              Dev tools (clear rooms, reset data)
│
├── game/                 Game server modules
│   ├── ws/               WebSocket server, Client, Message envelope
│   ├── lobby/            Ranked lobby system (1-2 người/team)
│   ├── matchmaker/       Matchmaking engine (Redis queue, leader election)
│   ├── room/             Room hub + goroutines, battle room creation
│   ├── match/            Match engine: physics, damage, terrain, bots, rewards
│   └── gamedata/         Load game configs (YAML/DB)
│
├── admin/                Admin dashboard (chi router, HTML templates)
│   ├── server.go         Routes + template rendering
│   ├── repository.go     DB queries
│   ├── handlers_*.go     Handlers (dashboard, config, players, devtools, matchmaking)
│   ├── seed.go           Idempotent config seeding
│   └── templates/        Embedded HTML templates
│
├── shared/               Code shared giữa tất cả servers
│   ├── config/           Environment-based config loading
│   ├── database/         PostgreSQL pool + Redis client wrappers
│   ├── auth/             JWT manager (sign/verify)
│   ├── middleware/       Auth, CorrelationID, VersionCheck, RateLimiter
│   ├── model/            Structured error codes + ErrorResponse
│   ├── observability/    Zerolog logger, health checks, Prometheus metrics
│   ├── circuitbreaker/   Circuit breaker pattern
│   ├── featureflag/      Redis-based feature flags
│   └── idempotency/      Request deduplication
│
└── worker/               Background job scheduler
    ├── scheduler.go      Job scheduler with panic recovery
    └── jobs/             ban_expire, daily_reset, account_deletion

configs/                  YAML game data (characters, weapons, skills, items, maps, physics)
migrations/               SQL migration files (001-005)
```

---

## 5. API Server — REST Endpoints

### Public (không cần auth)

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| POST | `/auth/guest-login` | GuestLogin | Login bằng device ID, tạo account nếu chưa có |
| POST | `/auth/provider-login` | ProviderLogin | Login bằng Google/Apple OAuth token |
| POST | `/auth/refresh` | RefreshToken | Đổi refresh token lấy access token mới |
| POST | `/auth/logout` | Logout | Blacklist refresh token trong Redis |
| GET | `/app/version-policy?platform=` | GetVersionPolicy | Policy version cho android/ios |
| GET | `/app/config` | GetRemoteConfig | Remote config (API URL, WS URL, features) |
| GET | `/healthz` | Healthz | Health check |
| GET | `/readyz` | Readyz | Readiness check (DB + Redis) |
| GET | `/livez` | Livez | Liveness check |
| GET | `/metrics` | Prometheus | Prometheus metrics |

### Protected (cần Bearer token)

**Player & Account:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/player/profile` | GetProfile | Lấy profile (name, level, coin, gem) |
| PUT | `/player/profile` | UpdateProfile | Đổi display name (max 50 chars) |
| POST | `/account/deletion/request` | RequestAccountDeletion | Yêu cầu xóa account (7 ngày grace) |
| POST | `/account/deletion/cancel` | CancelAccountDeletion | Hủy yêu cầu xóa |
| GET | `/account/deletion/status` | GetAccountDeletionStatus | Trạng thái xóa account |
| POST | `/auth/link-provider` | LinkProvider | Liên kết Google/Apple vào account |

**Economy & Shop:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/player/inventory` | GetInventory | Danh sách items đang có |
| GET | `/shop/offers` | GetOffers | Các offer đang active |
| POST | `/shop/purchase` | Purchase | Mua offer (idempotent, header `X-Idempotency-Key`) |
| GET | `/shop/purchases` | GetPurchases | Lịch sử mua hàng |
| GET | `/iap/products` | GetProducts | Danh sách IAP products |
| POST | `/iap/verify` | VerifyReceipt | Verify receipt từ App Store/Play Store |
| POST | `/giftcode/redeem` | Redeem | Nhập gift code |

**Game Systems:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/missions/daily` | GetDailyMissions | Nhiệm vụ hàng ngày + tiến độ |
| GET | `/missions/achievements` | GetAchievements | Thành tích + tiến độ |
| POST | `/missions/claim` | ClaimReward | Nhận thưởng nhiệm vụ |
| GET | `/rank/me` | GetRankMe | Rating, tier, division của mình |
| GET | `/rank/leaderboard?page=&limit=` | GetLeaderboard | Bảng xếp hạng (default: page=1, limit=20) |
| GET | `/rank/seasons/current` | GetCurrentSeason | Mùa giải hiện tại |
| POST | `/rank/reward/claim` | ClaimReward | Nhận thưởng cuối mùa |
| GET | `/player/match-history?page=&limit=` | GetHistory | Lịch sử trận đấu |
| GET | `/rooms?mode=&page=&limit=` | GetRooms | Danh sách phòng đang chờ |

**Moderation (admin only):**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| POST | `/report/player` | CreateReport | Báo cáo player |
| POST | `/moderation/ban` | BanPlayer | Ban player (cần role admin) |
| POST | `/moderation/ban/revoke` | RevokeBan | Gỡ ban (cần role admin) |

**Dev only (APP_ENV=development):**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| POST | `/dev/clear-rooms` | ClearRooms | Xóa tất cả rooms trong Redis |
| POST | `/dev/reset-data` | ResetData | Reset toàn bộ player data |

### Middleware Pipeline

```
Request → CORS → RealIP → CorrelationID → Recoverer → VersionCheck → Metrics → RateLimiter(10/s, burst 20) → [AuthMiddleware] → Handler
```

---

## 6. Game Server — WebSocket Protocol

### Connection

```
ws://host:8081/ws?token={JWT}&protocolVersion={int}
```

Server kiểm tra: JWT valid → account không bị ban → protocol version ≥ minimum → upgrade WebSocket.

### Message Envelope

```json
{
  "event": "EventName",
  "data": { ... },
  "seq": 1,
  "timestamp": 1720000000,
  "correlationId": "abc123"
}
```

### Routing: CompositeWSHandler

```
Client message → CompositeWSHandler
    │
    ├── LobbyHandler.HandleLobbyMessage() → return true nếu là lobby event
    │     ├── CreateLobby, JoinLobby, LeaveLobby
    │     ├── UpdateLoadout, StartQueue, CancelQueue
    │     └── return false → không phải lobby event
    │
    └── RoomHandler.HandleMessage() → xử lý room/match events
          ├── CreateRoom, JoinRoom, QuickPlay
          └── Forward to Room → Forward to Match
```

### Lobby Events (Ranked Matchmaking)

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateLobby` | `{}` | Tạo lobby room |
| `JoinLobby` | `{lobbyId}` | Join lobby theo ID |
| `LeaveLobby` | `{}` | Rời lobby |
| `UpdateLoadout` | `{characterId?, items?}` | Đổi character/items → lưu DB |
| `StartQueue` | `{}` | Host bấm tìm trận |
| `CancelQueue` | `{}` | Hủy tìm trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `LobbyUpdated` | `LobbyState` | State thay đổi |
| `LobbyError` | `{error: {code, message}}` | Lỗi |
| `LobbyDisbanded` | `{reason}` | Host rời → lobby giải tán |
| `QueueStarted` | `{estimatedWait}` | Đã vào hàng đợi |
| `QueueCancelled` | `{reason}` | Hủy queue |
| `MatchFound` | `{matchId, roomId, mapId, players[], hasBot}` | Ghép xong |
| `ReturnToLobby` | `{lobbyId}` | Trận xong, quay về lobby |

### Room Events

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateRoom` | `{mode, mapId, password?}` | Tạo phòng (pvp_1v1, pvp_2v2) |
| `JoinRoom` | `{roomId, password?}` | Vào phòng |
| `QuickPlay` | `{}` | Chơi tutorial với bot idle |
| `ChangeTeam` | `{teamId}` | Đổi team (1 hoặc 2) |
| `SelectCharacter` | `{characterId}` | Chọn nhân vật |
| `SelectItems` | `{items[]}` | Chọn items (max 3) |
| `Ready` | `{}` | Sẵn sàng (toggle) |
| `StartMatch` | `{}` | Host bắt đầu trận |
| `Leave` | `{}` | Rời phòng/trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `RoomUpdated` | `RoomState` | Phòng thay đổi |
| `RoomError` | `{error: {code, message}}` | Lỗi phòng |

### Match Events

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `Move` | `{direction, targetX}` | Di chuyển trái/phải |
| `Shoot` | `{angle, power, actionMode, itemId?}` | Bắn (weapon hoặc skill) |
| `UseItem` | `{itemId, targetPosition?}` | Dùng item |
| `EndTurn` | `{}` | Kết thúc lượt |
| `Reconnect` | `{}` | Kết nối lại sau disconnect |
| `Leave` | `{}` | Rời trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `MatchStarted` | `{matchId, players, turnOrder, wind, mapId}` | Trận bắt đầu |
| `TurnStarted` | `{currentPlayerId, turnIndex, wind, timeLeft}` | Lượt mới |
| `TurnTimerTick` | `{timeLeft}` | Đếm ngược (mỗi 1s) |
| `PlayerMoved` | `{playerId, position}` | Player di chuyển |
| `ProjectileResult` | `{path[], hitPlayerId?, explosionPoint, terrainDestroyed}` | Kết quả bắn |
| `PlayerDamaged` | `{playerId, damage, hp, isAlive}` | Player bị damage |
| `StatusEffectApplied` | `{effectId, targetPlayerId, duration}` | Buff/debuff |
| `TerrainDestroyed` | `{center, radius}` | Terrain bị phá |
| `MatchEnded` | `{winningTeam, rewards}` | Trận kết thúc |
| `MatchStateSync` | `MatchState` | Full sync khi reconnect |

---

## 7. Match Engine

### Lifecycle

```
Room (waiting) → Host StartMatch → Match goroutine spawns
    │
    ├── broadcastMatchStarted()
    ├── startTurn() → broadcast TurnStarted
    │
    ├── Event Loop (select):
    │   ├── Events channel → handleEvent (Move/Shoot/UseItem/EndTurn/Leave)
    │   ├── 1s ticker → TurnTimerTick, auto-endTurn khi hết 20s
    │   ├── 30s watchdog → 2 phút không activity → endAsNoContest
    │   └── ctx.Done → cleanup
    │
    ├── checkWinCondition() → team nào hết người alive → MatchEnded
    ├── ProcessMatchRewards() → EXP, coins, rating update
    ├── signalDone() → close MatchDone channel → Room biết để cleanup
    │
    └── Panic recovery → endAsNoContest, log stack trace
```

### Turn Flow

```
startTurn()
  1. Tick status effects (decrement duration, remove expired)
  2. Apply terrain damage (lava: 5 HP/turn)
  3. Reset move energy = 100
  4. Update wind (random power + direction, trừ khi WindStopper active)
  5. Broadcast TurnStarted
  6. Nếu bot → delay 1-2s → DecideAction → inject event

Player actions trong turn:
  - Move: tiêu MoveEnergy, update position, check fall damage
  - Shoot: 1 lần/turn, simulate projectile physics, calculate damage, destroy terrain
  - UseItem: medkit (+30 HP), teleport, wind_stopper, shot modifiers
  - EndTurn: chuyển sang player tiếp theo
```

### Projectile Physics

```
SimulateProjectile(startPos, angle, power, wind, weaponConfig):
  velocity = power * 6.0 tại angle
  timeStep = 0.02s
  gravity = 200.0 px/s²
  windForce = direction * power * 30.0 * weaponWindInfluence

  Loop (max 6 giây):
    velocity.X += windForce * timeStep
    velocity.Y += gravity * timeStep
    position += velocity * timeStep

    Check collision:
      - Terrain: IsSolid(x, y) → explode
      - Player: distance < 24px hitRadius → hit
      - Out of bounds → discard

    Record path mỗi 0.05s cho client animation
```

### Damage Calculation

```
Explosion damage:
  distance = dist(explosionCenter, playerPosition)
  if distance >= explosionRadius → 0 damage
  multiplier = 1.0 - (distance / explosionRadius)  // linear falloff
  rawDamage = weaponDamage * multiplier
  finalDamage = rawDamage * (100 / (100 + playerDefense))

Fall damage:
  threshold = 150px
  if fallDistance > threshold:
    damage = (fallDistance - 150) * 0.25
```

### Terrain System

3 built-in maps:

| Map | Đặc điểm |
|-----|---------|
| `grassland_valley` | Đồi thoải (sine wave) |
| `frozen_peak` | Đỉnh nhọn (multiple sine frequencies) |
| `steel_base` | Platform bậc thang |

Terrain zones đặc biệt:
- `lava`: 5 damage/turn khi đứng
- `ice`: Trượt (giảm friction)
- `fragile`: Sập sau khi bị bắn

Methods: `IsSolid(x,y)`, `DestroyCircle(cx,cy,radius)`, `GetLandingY(x,maxY)`, `GetTerrainTypeAt(x,y)`

### Items & Status Effects

**Immediate items:**

| Item | Hiệu ứng |
|------|----------|
| `medkit` | +30 HP (cap tại MaxHP) |
| `teleport` | Dịch chuyển đến vị trí target |
| `wind_stopper` | Triệt tiêu gió 2 lượt (global) |

**Shot modifiers (active trong lượt hiện tại):**

| Item | Hiệu ứng |
|------|----------|
| `power_shot` | Damage ×1.5 |
| `drill_bomb` | Đạn xuyên terrain |
| `spider_net` | Target MoveEnergy cap 50 |
| `freeze_bomb` | Target MoveEnergy = 0 |
| `air_strike` | Đạn rơi từ trên xuống tại tọa độ X |

### Reward System

Sau mỗi trận:

| Loại | Thắng | Thua |
|------|-------|------|
| EXP cơ bản | 80 (50+30) | 50 |
| Coins | 50 (30+20) | 30 |
| Damage bonus | +5% damage dealt | +5% |
| Kill bonus | +10/kill | +10/kill |

**Rating (chỉ ranked):**
- `pvp_1v1`: cố định +25/-20
- `ranked_2v2`: Elo formula `K * (actual - expected)`, K=32, điều chỉnh theo chênh lệch rating. Trận có bot: rating change × `botRatingModifier` (default 0.5)

---

## 8. Ranked Matchmaking System

### Flow

```
CreateLobby → chọn loadout → StartQueue → Matchmaker ghép → MatchFound → Battle Room → trận đấu → ReturnToLobby
```

### Lobby Room

- 1-2 người cùng team, actor pattern (goroutine + event channel)
- Character/items lưu trong `player_loadouts` DB table
- Status: `preparing` → `in_queue` → `in_match` → `preparing` (loop)
- Host rời → lobby giải tán, member nhận `LobbyDisbanded`

### Matchmaker Engine

- Goroutine chạy trong game server, tick mỗi 3s (configurable)
- Redis leader election (`SETNX` TTL 10s) cho multi-node
- Hot-reload config mỗi 30s từ `game_settings` DB

**Queue (Redis):**
- Sorted set `matchmaking:queue:2v2` (score = rating)
- Entry detail: `matchmaking:entry:{id}` (JSON string, TTL 120s)
- Player mapping: `matchmaking:player:{id}` (string, TTL 120s)

**Algorithm:**
1. Lấy tất cả entries, đã sorted theo rating
2. Expanding range: `baseRange(100) + (waitTime/10s) × expandStep(50)`, max 300
3. Tìm cặp có |rating₁ - rating₂| ≤ min(allowedRange₁, allowedRange₂)
4. Timeout (default 60s) → điền Smart Bot

**Party rating:** 3 strategies (config từ admin): `max` (default), `average`, `weighted`

### Smart Bot AI

State-based decision making, difficulty theo rank tier:

1. Evaluate: HP ratio, khoảng cách đến enemy, items còn
2. Score 3 actions: shoot, move, useItem
3. Thêm noise (Bronze: ±30, Master: ±5)
4. Chọn action score cao nhất
5. Bắn: tính góc hoàn hảo + error (Bronze: ±15°, Master: ±2°) + wind compensation

---

## 9. Admin Dashboard

### Web UI Routes

| Method | Path | Mô tả |
|--------|------|-------|
| GET | `/` | Dashboard (active rooms, total players) |
| GET/POST | `/characters/*` | CRUD nhân vật |
| GET/POST | `/weapons/*` | CRUD vũ khí |
| GET/POST | `/skills/*` | CRUD skills |
| GET/POST | `/items/*` | CRUD items |
| GET/POST | `/maps/*` | CRUD maps |
| GET/POST | `/physics` | Physics settings |
| GET/POST | `/shop/*` | CRUD shop offers |
| GET/POST | `/players` | Player list, ban/unban |
| GET/POST | `/devtools/*` | Clear rooms, reset data, seed config |

### Config API (JSON)

| Method | Path | Key | Mô tả |
|--------|------|-----|-------|
| GET/POST | `/api/config/matchmaking` | `matchmaking` | Matchmaking params |
| GET/POST | `/api/config/elo` | `elo` | Elo rating params |
| GET/POST | `/api/config/bot-difficulty` | `bot_difficulty` | Bot AI difficulty per tier |

---

## 10. Worker — Background Jobs

| Job | Interval | Mô tả |
|-----|----------|-------|
| BanExpireJob | 5 phút | Expire bans đã hết `ends_at`, reactivate accounts |
| DailyResetJob | 1 giờ | Reset daily mission progress (1 lần/ngày UTC, Redis dedup) |
| AccountDeletionJob | 1 giờ | Anonymize + mark deleted cho accounts quá grace period |

---

## 11. Database Schema

30 tables, 10 domain groups. Chi tiết: `docs/database-erd.html` (interactive) hoặc `docs/database-erd.md`.

### Core tables

| Table | PK | Mô tả |
|-------|-----|-------|
| `accounts` | `account_id` | Tài khoản (guest/google/apple, role, status) |
| `auth_identities` | `identity_id` | Login providers (1 account : N identities) |
| `player_profiles` | `player_id` | Profile (name, level, coin, gem) — **central entity** |
| `player_loadouts` | `player_id` | Loadout mặc định (character + items) |

### Economy & Inventory

| Table | PK | Mô tả |
|-------|-----|-------|
| `player_characters` | `(player_id, character_id)` | Nhân vật đã unlock |
| `inventory_items` | `(player_id, item_id)` | Items đang có |
| `inventory_reservations` | `reservation_id` | Items reserved cho trận đấu |
| `economy_transactions` | `transaction_id` | Ledger coin/gem |

### Ranking

| Table | PK | Mô tả |
|-------|-----|-------|
| `rank_seasons` | `season_id` | Mùa giải (status: upcoming→active→ended→closed) |
| `player_ranks` | `(player_id, season_id)` | Rating, tier, wins/losses per season |
| `season_reward_claims` | `claim_id` | Thưởng cuối mùa (1 lần/season) |

### Match

| Table | PK | Mô tả |
|-------|-----|-------|
| `match_histories` | `(match_id, player_id)` | Kết quả trận đấu |
| `match_snapshots` | `match_id` | Snapshot state cho crash recovery |
| `match_event_logs` | `id` | Event log chi tiết (debug) |

### Dynamic Config

| Table | PK | Mô tả |
|-------|-----|-------|
| `game_settings` | `key` | Key-value config (matchmaking, elo, bot_difficulty) |
| `config_characters/weapons/skills/items/maps` | `*_id` | Game data (admin editable) |

---

## 12. Redis Data Structures

| Key Pattern | Type | TTL | Mô tả |
|-------------|------|-----|-------|
| `rooms:active` | Hash | — | Room registry (field=roomID, value=metadata JSON) |
| `matchmaking:queue:2v2` | Sorted Set | — | Matchmaking queue (score=rating) |
| `matchmaking:entry:{id}` | String (JSON) | 120s | Queue entry detail |
| `matchmaking:player:{id}` | String | 120s | Player → entry mapping |
| `matchmaking:leader` | String | 10s | Leader election lock |
| `leaderboard:current` | Sorted Set | — | Current season leaderboard |
| `session:{token}` | String | — | Refresh token blacklist |
| `feature:{flag}` | String | — | Feature flags |
| `idempotency:{key}` | String | 300s | Request deduplication |

---

## 13. Error Codes

| Code | HTTP | Mô tả |
|------|------|-------|
| `INTERNAL_SERVER_ERROR` | 500 | Server error |
| `AUTH_UNAUTHORIZED` | 401 | Token missing/invalid |
| `AUTH_FORBIDDEN` | 403 | Access denied |
| `AUTH_ADMIN_REQUIRED` | 403 | Cần role admin |
| `AUTH_ACCOUNT_BANNED` | 403 | Account bị ban |
| `BAD_REQUEST` | 400 | Invalid parameters |
| `NOT_FOUND` | 404 | Resource not found |
| `APP_FORCE_UPDATE` | 426 | Client cần update |
| `SHOP_INSUFFICIENT_BALANCE` | 400 | Không đủ tiền |
| `SHOP_ITEM_OUT_OF_STOCK` | 409 | Hết hàng |
| `MATCH_NOT_YOUR_TURN` | 403 | Chưa đến lượt |
| `MATCH_ALREADY_SHOT` | 409 | Đã bắn rồi |
| `RATE_LIMIT_EXCEEDED` | 429 | Quá nhiều request (10/s) |

---

## 14. Deployment

### Docker Compose

```yaml
services:
  postgres:  # PostgreSQL 15, port 5432
  redis:     # Redis 7, port 6379
  api:       # API server, port 8080
  game:      # Game server, port 8081
  worker:    # Background worker
```

Network: `battlesquad_network` (bridge)

### Startup Order

```
1. docker-compose up -d (Postgres + Redis)
2. go run cmd/migrate/main.go
3. go run cmd/api/main.go
4. go run cmd/game/main.go
5. go run cmd/admin/main.go
6. go run cmd/worker/main.go (optional)
```

### Graceful Shutdown

Tất cả servers: bắt SIGINT/SIGTERM → 15s timeout → force shutdown. Game server dừng matchmaker trước.

---

## 15. Key Design Patterns

| Pattern | Ở đâu | Mô tả |
|---------|-------|-------|
| Actor model | Room, Lobby, Match | Mỗi instance = 1 goroutine + event channel |
| Handler → Service → Repository | API modules | Tách concerns: HTTP, business logic, DB |
| Leader election | Matchmaker | Redis SETNX cho multi-node, chỉ 1 node chạy matchmaker |
| Composite handler | Game server WS | Lobby handler thử trước, fall through sang room handler |
| Panic recovery | Match, Room | Defer recover() + cleanup, không crash cả server |
| Watchdog timer | Match | 2 phút không activity → auto terminate |
| Idempotency | Shop purchases | Header `X-Idempotency-Key` + Redis TTL 300s |
| Circuit breaker | Shared | Resilience cho external calls |
| Hot reload | Matchmaker | Config reload mỗi 30s từ DB |
