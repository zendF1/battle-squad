# Battle Squad — Server Implementation Plan v2

Kế hoạch triển khai code server cho game Battle Squad dựa trên 4 tài liệu spec.

---

## Technology Stack

| Thành phần | Lựa chọn |
|---|---|
| Language | **Go (Golang)** |
| Real-time Protocol | **WebSocket** (per room/match) — port riêng |
| REST API | **HTTP** — port riêng |
| Database | **PostgreSQL** |
| Cache / Session / Room Registry | **Redis** |
| Serialization | **JSON** (giai đoạn đầu) |
| HTTP Router | **chi** hoặc **gin** |
| WebSocket | **gorilla/websocket** hoặc **nhooyr.io/websocket** |
| DB Driver | **pgx/v5** + **sqlc** hoặc **GORM** |
| Config | **YAML** / **envconfig** |
| Migration | **golang-migrate** |
| Auth | **JWT** (access + refresh token) |

---

## Kiến trúc Tổng Quan — 3 Processes

```
┌──────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  ┌── API Server ──────┐  ┌── Game Server ────┐  ┌── Worker ──────┐  │
│  │  cmd/api/main.go    │  │ cmd/game/main.go  │  │ cmd/worker/    │  │
│  │  REST :8080         │  │ WS :8081          │  │ main.go        │  │
│  │                     │  │                   │  │                │  │
│  │  Auth               │  │ WebSocket server  │  │ Refund check   │  │
│  │  Player Profile     │  │ Room goroutines   │  │ Season reward  │  │
│  │  Inventory          │  │ Match goroutines  │  │ Account delete │  │
│  │  Shop Coin/Gem      │  │ Turn management   │  │ Daily reset    │  │
│  │  IAP Verify         │  │ Shooting/Physics  │  │ Ban expire     │  │
│  │  Gift Code          │  │ Terrain           │  │ Anomaly detect │  │
│  │  Economy Ledger     │  │ Item/Status FX    │  │ Snapshot clean │  │
│  │  Mission            │  │ Wind/Bot          │  │                │  │
│  │  Rank/Leaderboard   │  │ Reward write      │  │ Cron jobs      │  │
│  │  Moderation         │  │ Recovery          │  │ Chạy ngầm      │  │
│  │  App Config         │  │                   │  │                │  │
│  │  Health/Metrics     │  │ Health/Metrics    │  │                │  │
│  └─────────┬───────────┘  └────────┬──────────┘  └───────┬────────┘  │
│            │                       │                      │          │
│            └───────────────────────┼──────────────────────┘          │
│                                    │                                  │
│               ┌── PostgreSQL ──────┴───── Redis ──┐                  │
│               │         Shared data layer          │                  │
│               └────────────────────────────────────┘                  │
└──────────────────────────────────────────────────────────────────────┘
```

### Tại sao tách 3 processes?

| Vấn đề | Monolith 1 process | Tách 3 processes ✅ |
|---|---|---|
| Bug shop panic | Crash toàn bộ, mất trận đang chơi | Chỉ API server restart, trận vẫn chạy |
| IAP verify timeout | Block goroutine, lag game | API server chậm, game không ảnh hưởng |
| Deploy fix auth | Restart → mất hết trận | Chỉ restart API server |
| Game server OOM | Mất cả shop, login | Chỉ mất trận, login/shop vẫn OK |
| Scale | Scale cả cục | Scale game server theo CCU, API theo RPS |

### Giao tiếp giữa 3 processes

Shared DB + Redis pub/sub, không cần gRPC/message queue phức tạp:

| Scenario | Cách giao tiếp |
|---|---|
| Match kết thúc → cập nhật coin/exp/rank | Game Server ghi trực tiếp vào DB |
| Match kết thúc → cập nhật mission progress | Game Server publish event qua Redis → API Server subscribe |
| Player login → lấy profile để vào room | Game Server đọc DB trực tiếp |
| Player bị ban → kick khỏi match | API Server set ban trong DB → Game Server periodic check |
| Shop mua item → inventory cập nhật | API Server ghi DB → Game Server đọc khi player vào room |

---

## Project Structure

```
battle-squad/
├── cmd/
│   ├── api/                        # API Server binary (REST :8080)
│   │   └── main.go
│   ├── game/                       # Game Server binary (WS :8081)
│   │   └── main.go
│   └── worker/                     # Worker Server binary (background jobs)
│       └── main.go
│
├── internal/
│   ├── shared/                     # Shared code giữa 3 processes
│   │   ├── config/
│   │   │   └── config.go           # App config (YAML + env)
│   │   ├── database/
│   │   │   ├── postgres.go         # PostgreSQL pool (pgx/v5)
│   │   │   └── redis.go            # Redis client wrapper
│   │   ├── middleware/
│   │   │   ├── auth.go             # JWT validation
│   │   │   ├── correlation.go      # Correlation ID injection
│   │   │   ├── version_check.go    # Client version check
│   │   │   └── ratelimit.go        # Rate limiting
│   │   ├── model/                  # Shared domain models
│   │   │   ├── account.go
│   │   │   ├── player.go
│   │   │   ├── inventory.go
│   │   │   ├── economy.go
│   │   │   └── errors.go           # Structured error codes
│   │   ├── observability/
│   │   │   ├── health.go           # /healthz, /readyz, /livez
│   │   │   ├── metrics.go          # Prometheus metrics
│   │   │   └── logger.go           # Structured logging
│   │   ├── circuitbreaker/
│   │   │   └── breaker.go          # Circuit breaker cho DB/Redis/external API
│   │   ├── featureflag/
│   │   │   └── flags.go            # Feature flags (Redis-backed)
│   │   └── idempotency/
│   │       └── idempotency.go      # Idempotency key check
│   │
│   ├── api/                        # API Server modules
│   │   ├── auth/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go            # Account, AuthIdentity, Session
│   │   ├── player/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── inventory/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── shop/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── iap/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── verifier.go         # Google/Apple receipt verification
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── economy/
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── giftcode/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── mission/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── rank/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   ├── moderation/
│   │   │   ├── handler.go
│   │   │   ├── service.go
│   │   │   ├── repository.go
│   │   │   └── model.go
│   │   └── appconfig/
│   │       ├── handler.go
│   │       ├── service.go
│   │       └── model.go
│   │
│   ├── game/                       # Game Server modules
│   │   ├── ws/
│   │   │   ├── server.go           # WS upgrade, auth, ban check
│   │   │   ├── client.go           # Per-connection, read/write goroutines
│   │   │   ├── message.go          # Message envelope
│   │   │   └── protocol.go         # Event types registry
│   │   ├── room/
│   │   │   ├── hub.go              # Room registry (Redis-backed)
│   │   │   ├── room.go             # Room goroutine + state
│   │   │   ├── handler.go          # WS event handlers
│   │   │   └── model.go
│   │   ├── match/
│   │   │   ├── match.go            # Match goroutine + lifecycle
│   │   │   ├── turn.go             # Turn management
│   │   │   ├── movement.go         # Movement system
│   │   │   ├── shooting.go         # Shooting & projectile simulation
│   │   │   ├── collision.go        # Collision detection
│   │   │   ├── damage.go           # Damage calculation
│   │   │   ├── terrain.go          # Terrain destruction & special effects
│   │   │   ├── item_effect.go      # Item effects (8 MVP items)
│   │   │   ├── status_effect.go    # Status effects (Freeze, Net, Heal, WindStop)
│   │   │   ├── wind.go             # Wind system
│   │   │   ├── bot.go              # Bot AI (Easy/Normal)
│   │   │   ├── reward.go           # Post-match rewards
│   │   │   ├── recovery.go         # Match state snapshot/recovery
│   │   │   ├── eventlog.go         # Match event log (replay & debug)
│   │   │   ├── watchdog.go         # Watchdog timer cho trận zombie
│   │   │   └── model.go
│   │   ├── gamedata/
│   │   │   ├── loader.go           # Load YAML configs at startup
│   │   │   ├── character.go
│   │   │   ├── weapon.go
│   │   │   ├── skill.go
│   │   │   ├── item.go
│   │   │   └── mapconfig.go
│   │   └── matchhistory/
│   │       ├── service.go
│   │       ├── repository.go
│   │       └── model.go
│   │
│   └── worker/                     # Worker Server modules
│       ├── scheduler.go            # Job scheduler
│       └── jobs/
│           ├── refund_check.go     # IAP refund/chargeback check
│           ├── season_reward.go    # End-of-season reward grant
│           ├── account_deletion.go # Account deletion after grace period
│           ├── daily_reset.go      # Daily mission reset
│           ├── ban_expire.go       # Expire temporary bans
│           ├── anomaly_detect.go   # Economy anomaly detection
│           └── snapshot_cleanup.go # Old match snapshot cleanup
│
├── migrations/                     # SQL migration files
│   ├── 001_accounts.up.sql
│   ├── 001_accounts.down.sql
│   ├── 002_players.up.sql
│   ├── ...
│
├── configs/                        # Game data configs (YAML)
│   ├── characters.yaml
│   ├── weapons.yaml
│   ├── skills.yaml
│   ├── items.yaml
│   ├── maps.yaml
│   ├── rewards.yaml
│   ├── missions.yaml
│   └── bot_difficulty.yaml
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile.api
├── Dockerfile.game
├── Dockerfile.worker
├── docker-compose.yml              # Chạy cả 3 process + PostgreSQL + Redis
└── README.md
```

---

## Operational Excellence — 10 Patterns

### Pattern 1: Panic Recovery Per Match

Mỗi Room/Match goroutine có `recover()` riêng. 1 trận panic → chỉ trận đó kết thúc no-contest, hàng trăm trận khác vẫn chạy.

```go
func (m *Match) Run() {
    defer func() {
        if r := recover(); r != nil {
            log.Error().Str("matchId", m.ID).
                Interface("panic", r).
                Str("stack", string(debug.Stack())).
                Msg("match panic - ending as no-contest")
            m.EndAsNoContest() // Refund items
            metrics.MatchPanicCounter.Inc()
        }
    }()
    // Match logic...
}
```

### Pattern 2: Graceful Shutdown

Khi deploy version mới, server không kill ngay:
1. `/readyz` trả 503 → load balancer ngưng gửi traffic mới.
2. Đợi các trận kết thúc tự nhiên (timeout 5-10 phút).
3. Snapshot các trận chưa xong vào DB.
4. Đóng connections, tắt process.

→ **Zero-downtime deploy, không ai mất trận.**

### Pattern 3: Circuit Breaker

Khi DB/Redis/external API chết → fail fast ngay thay vì hàng nghìn goroutine đồng loạt chờ timeout → OOM.

- **Closed**: hoạt động bình thường.
- **Open**: trả lỗi ngay, không gọi dependency.
- **Half-Open**: thử 1 request, OK → Closed, fail → Open tiếp.

Áp dụng cho: PostgreSQL, Redis, Google Play API, Apple StoreKit API.

### Pattern 4: Correlation ID

Mỗi request/action mang 1 ID duy nhất, truyền xuyên suốt tất cả log:

```
[correlationId=req-abc-123 playerId=p1] POST /shop/purchase
[correlationId=req-abc-123] UPDATE player_profiles SET gem = gem - 100
[correlationId=req-abc-123] PUBLISH currency_updated p1
```

Cho match: thêm `matchId` + `turnIndex` vào mọi log.

```bash
# Debug: tìm tất cả log liên quan 1 request lỗi
grep "req-abc-123" /var/log/*.log
```

### Pattern 5: Feature Flags (Redis-backed)

Tắt/bật tính năng runtime không cần restart/deploy:

| Scenario | Hành động |
|---|---|
| Bug shop crash | Tắt `shop_enabled`, fix từ từ |
| Apple IAP API lỗi | Tắt `iap_ios_enabled`, thông báo user |
| Test ranked mode | Bật `ranked_mode` cho 5% user |
| Seasonal event | Bật `lunar_new_year_shop` đúng giờ |

### Pattern 6: Retry + Idempotency

Mọi operation quan trọng (purchase, IAP verify, reward grant) có idempotency key. Gọi nhiều lần do lỗi mạng → kết quả như 1 lần, không cộng trùng Gem/Coin.

### Pattern 7: Structured Error Codes

Mọi lỗi trả về error code cụ thể + correlationId:

```json
{
  "error": {
    "code": "SHOP_INSUFFICIENT_BALANCE",
    "message": "Không đủ Coin",
    "details": { "required": 500, "current": 320 },
    "correlationId": "req-abc-123"
  }
}
```

Error code registry: `SHOP_*`, `MATCH_*`, `AUTH_*`, `IAP_*`, `APP_*`.

### Pattern 8: Health Check 3 Tầng

| Endpoint | Dùng cho | Trả về |
|---|---|---|
| `GET /healthz` | Kubernetes liveness | Process sống = 200 |
| `GET /readyz` | Load balancer | DB + Redis OK = 200 |
| `GET /livez` | Monitoring dashboard | Chi tiết từng dependency + CCU + active matches |

### Pattern 9: Match Event Log

Mọi action trong trận ghi vào event log theo sequence number. Dùng để:
- **Debug**: admin replay lại trận bị report.
- **Detect cheat**: so sánh event log với expected behavior.
- **Recovery**: replay từ snapshot + events.
- **Analytics**: phân tích meta game.

### Pattern 10: Watchdog Timer

Nếu match goroutine bị stuck (deadlock, infinite loop) quá 2 phút không có activity → tự kết thúc no-contest, refund items, log alert.

---

## Phân Pha Triển Khai

### Phase 1: Foundation & Infrastructure

> Mục tiêu: Khung xương 3 binaries, DB/Redis, config, health, middleware, shared patterns.

#### Shared infrastructure (`internal/shared/`)
- **config.go**: Parse config từ YAML + env. DB DSN, Redis addr, JWT secret, ports, log level.
- **postgres.go**: PostgreSQL connection pool (pgx/v5) + health check + circuit breaker wrapper.
- **redis.go**: Redis client wrapper (go-redis/v9). Session, room registry, leaderboard, feature flags.
- **health.go**: `/healthz`, `/readyz`, `/livez` endpoints.
- **metrics.go**: Prometheus metrics (CCU, active matches, RPS, latency, error rate, goroutine count).
- **logger.go**: Structured logger (zerolog/zap) với correlationId, playerId, matchId, serverNodeId.
- **correlation.go**: Middleware inject/propagate correlation ID.
- **auth.go**: JWT access token validation middleware.
- **version_check.go**: Check `X-App-Version`, `X-Protocol-Version` headers.
- **ratelimit.go**: Rate limit per player/IP.
- **breaker.go**: Circuit breaker cho DB/Redis/external API.
- **flags.go**: Feature flags reader (Redis-backed).
- **idempotency.go**: Idempotency key check.
- **errors.go**: Structured error codes registry + error response builder.

#### 3 Entry points
- **cmd/api/main.go**: Bootstrap API server, register REST routes, start HTTP :8080.
- **cmd/game/main.go**: Bootstrap Game server, start WS :8081, init room hub.
- **cmd/worker/main.go**: Bootstrap Worker, init scheduler, register jobs.

#### Migrations (`migrations/`)
Tất cả SQL migration files cho toàn bộ schema.

**Database tables:**

| Table | Mô tả |
|---|---|
| `accounts` | Account đăng nhập |
| `auth_identities` | Provider identity (guest/google/apple) |
| `player_profiles` | Profile người chơi |
| `inventory_items` | Kho item |
| `inventory_reservations` | Reserve item khi vào match |
| `shop_offers` | Sản phẩm bán Coin/Gem |
| `shop_purchases` | Lịch sử mua shop |
| `iap_products` | Sản phẩm IAP |
| `payment_transactions` | Giao dịch IAP |
| `economy_transactions` | Ledger Coin/Gem |
| `gift_codes` | Gift codes |
| `gift_code_redemptions` | Lịch sử redeem |
| `missions` | Cấu hình mission |
| `mission_progress` | Tiến độ mission player |
| `player_reports` | Report |
| `account_bans` | Bans |
| `rank_seasons` | Mùa giải |
| `player_ranks` | Rank player theo mùa |
| `season_reward_claims` | Claim reward mùa |
| `match_histories` | Lịch sử trận |
| `match_snapshots` | Snapshot match state |
| `match_recovery_logs` | Event log cho recovery |
| `match_event_logs` | Event log cho replay/debug |
| `client_version_policies` | Version policy |
| `feature_flags` | Feature flags |
| `idempotency_keys` | Idempotency tracking |

#### docker-compose.yml
Chạy cả 3 process + PostgreSQL + Redis cho local development.

---

### Phase 2: Auth, Account & Player Profile

> Mục tiêu: Guest login, Google/Apple login, link provider, JWT session, account deletion, player profile.

#### API Server — `internal/api/auth/`

**Models:**
- `Account` — accountId, accountType (`guest`|`google`|`apple`|`linked`), status (`active`|`banned`|`deleted`|`pending_deletion`), primaryPlayerId, createdAt, lastLoginAt, deletedAt
- `AuthIdentity` — identityId, accountId, provider, providerUserId, emailHash, createdAt, lastUsedAt
- `Session` — sessionId, accountId, accessToken, refreshToken, expiresAt, revokedAt

**REST Endpoints:**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| POST | `/auth/guest-login` | ❌ | Guest login → tạo Account + AuthIdentity + PlayerProfile |
| POST | `/auth/provider-login` | ❌ | Google/Apple login (verify ID token server-side) |
| POST | `/auth/link-provider` | ✅ | Nâng cấp guest → link Google/Apple |
| POST | `/auth/refresh` | ❌ | Refresh access token |
| POST | `/auth/logout` | ✅ | Revoke current session |
| POST | `/auth/logout-all` | ✅ | Revoke tất cả sessions |

**Logic:**
- Access token JWT short-lived (15-30 min), Refresh token long-lived (30 ngày).
- Link provider: thêm AuthIdentity mới, đổi accountType → `linked`, giữ nguyên playerId.
- Ban check tại login → trả `AUTH_ACCOUNT_BANNED` + ban info.

#### API Server — `internal/api/player/`

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/player/profile` | ✅ | Lấy profile |
| PUT | `/player/profile` | ✅ | Cập nhật displayName |
| POST | `/account/deletion/request` | ✅ | Yêu cầu xóa (grace period 7-30 ngày) |
| GET | `/account/deletion/status` | ✅ | Xem trạng thái |
| POST | `/account/deletion/cancel` | ✅ | Hủy yêu cầu xóa |

---

### Phase 3: Game Data Config & Economy

> Mục tiêu: Load static configs, inventory, economy ledger, shop Coin/Gem, IAP verify, gift code.

#### Game Data Configs (`configs/` + `internal/game/gamedata/`)
- Load từ YAML at startup: CharacterConfig, WeaponConfig, SkillConfig, ItemConfig, MapConfig.
- Validate cross-references.
- 4 nhân vật: Rookie, Tanko, Spark, Flora.
- 8 items: Medkit, Teleport, Power Shot, Drill Bomb, Spider Net, Freeze Bomb, Air Strike, Wind Stopper.
- 3 maps: Grassland Valley, Frozen Peak, Steel Base.

#### API Server — Economy & Commerce

**`internal/api/economy/`**
- EconomyTransaction: log mọi thay đổi Coin/Gem.
- `Credit()` / `Debit()` — atomic trong cùng DB transaction.

**`internal/api/inventory/`**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/player/inventory` | ✅ | Lấy inventory |

**`internal/api/shop/`**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/shop/offers` | ✅ | Danh sách offer |
| POST | `/shop/purchase` | ✅ | Mua → trừ Coin/Gem + cộng inventory (cùng DB tx) |
| GET | `/shop/purchases` | ✅ | Lịch sử mua |

**`internal/api/iap/`**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/iap/products` | ✅ | Danh sách IAP |
| POST | `/iap/verify` | ✅ | Verify receipt → cộng Gem (idempotent) |

**`internal/api/giftcode/`**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| POST | `/giftcode/redeem` | ✅ | Redeem code |

---

### Phase 4: WebSocket Infrastructure & Room System

> Mục tiêu: WS server trên Game Server, connection management, room lifecycle.

#### Game Server — `internal/game/ws/`
- **server.go**: HTTP upgrade → WS, authenticate JWT, check ban, check client version.
- **client.go**: Per-connection struct, read/write goroutines, heartbeat/ping-pong, correlationId propagation.
- **message.go**: Envelope `{ event, data, seq, timestamp, correlationId }`.
- **protocol.go**: Event types registry.

#### Game Server — `internal/game/room/`
- **hub.go**: Room registry (Redis-backed cho multi-node).
- **room.go**: Mỗi room = 1 goroutine, có panic recovery.

**WS Events (Client → Server):**

| Event | Mô tả |
|---|---|
| `CreateRoom` | Tạo phòng (mode, mapId, maxPlayers, password) |
| `JoinRoom` | Vào phòng |
| `LeaveRoom` | Rời phòng |
| `ChangeTeam` | Đổi đội |
| `SelectCharacter` | Chọn nhân vật |
| `SelectItems` | Chọn items (max 3 PvP) |
| `Ready` | Sẵn sàng |
| `StartMatch` | Host bắt đầu |

**WS Events (Server → Client):**

| Event | Mô tả |
|---|---|
| `RoomUpdated` | Trạng thái phòng cập nhật |
| `RoomList` | Danh sách phòng |
| `RoomError` | Lỗi |

**REST bổ sung (trên API Server):**

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/rooms` | ✅ | Danh sách phòng (đọc từ Redis) |

---

### Phase 5: Match System — Core Gameplay

> Mục tiêu: Toàn bộ match engine trên Game Server.

#### `internal/game/match/`

##### `match.go` — Match Lifecycle
- Khởi tạo MatchState từ RoomState.
- Mỗi match = 1 goroutine với **panic recovery** + **watchdog timer**.
- Match event log ghi mọi action theo sequence number.
- Graceful shutdown: snapshot state khi server shutdown.

##### `turn.go` — Turn Management
- TurnStarted: xác định current player, reset moveEnergy, apply start-turn effects, update wind, start 20s timer.
- TurnEnded: sau shoot/timeout/disconnect/skip.
- Turn order: 1v1 xen kẽ A→B, 2v2 xen kẽ A1→B1→A2→B2.

##### `movement.go` — Movement
- MoveAction → validate moveEnergy, terrain collision.
- Giới hạn quãng đường tối đa nếu hết energy (không reject toàn bộ).
- Nhiều lần di chuyển/lượt.
- Broadcast PlayerMoved.

##### `shooting.go` — Shooting & Projectile
- ShootAction → validate đúng lượt, còn sống, chưa bắn, angle/power hợp lệ.
- Projectile simulation fixed timestep:
  ```
  velocity += gravity * deltaTime
  velocity += windForce / projectileMass * deltaTime
  position += velocity * deltaTime
  ```
- Multi-projectile (Triple Shot), drill (Drill Bomb), air strike.

##### `collision.go` — Collision Detection
- Check: Terrain, Player, Boundary.
- Xác định explosion center.

##### `damage.go` — Damage Calculation
- Explosion: `finalDamage = baseDamage * clamp(1 - distance/radius, 0, 1)`
- Defense: `finalDamage = rawDamage * (100 / (100 + defense))`
- Death → remove from turn order → check win condition.
- Fall damage khi rơi từ cao.

##### `terrain.go` — Terrain Destruction
- Bitmap mask, crater theo bán kính.
- Sync delta (không full state).
- Special terrain: Lava (damage/turn), Ice (trượt), Fragile (sập 1 hit).
- Terrain collapse → player rơi → fall damage.

##### `item_effect.go` — 8 MVP Items
Medkit, Teleport, Power Shot, Drill Bomb, Spider Net, Freeze Bomb, Air Strike, Wind Stopper.

##### `status_effect.go` — Status Effects
Freeze, Net, Heal, Wind Stop. Duration tracking theo turn.

##### `wind.go` — Wind System
Random mỗi lượt, direction (-1/0/1), power (0-4). Wind Stopper override.

##### `bot.go` — Bot AI
Easy/Normal. Chọn mục tiêu gần nhất, tính hướng + sai số, dùng item cơ bản.

##### `reward.go` — Post-Match Rewards
```
baseExp=50, winBonusExp=30, damageBonusExp=totalDamage*0.05, killBonusExp=killCount*10
baseCoin=30, winBonusCoin=20
```
Ghi MatchHistory, cập nhật profile (exp/coin/level), publish mission events, update PlayerRank.

##### `recovery.go` — Match Persistence
Snapshot tại MatchStarted, TurnEnded, TerrainUpdated lớn, MatchEnded. Redis hot state, PostgreSQL snapshot bền.

##### `eventlog.go` — Match Event Log
Ghi mọi action theo seq cho replay/debug/cheat detection.

##### `watchdog.go` — Watchdog Timer
Match idle > 2 phút → end as no-contest.

**WS Events (Client → Server) — In-Match:**

| Event | Mô tả |
|---|---|
| `Move` | Di chuyển |
| `UseItem` | Dùng item |
| `Shoot` | Bắn (weapon/skill/item) |
| `EndTurn` | Kết thúc lượt sớm |
| `Reconnect` | Kết nối lại |

**WS Events (Server → Client) — In-Match:**

| Event | Mô tả |
|---|---|
| `MatchStarted` | Full initial state |
| `TurnStarted` | Current player, wind, timer |
| `PlayerMoved` | Kết quả di chuyển |
| `ItemUsed` | Item dùng |
| `ProjectileResult` | Trajectory, impacts |
| `TerrainUpdated` | Delta terrain |
| `PlayerDamaged` | Damage received |
| `PlayerKilled` | Player hạ gục |
| `StatusEffectApplied` | Effect mới |
| `StatusEffectRemoved` | Effect hết |
| `TurnEnded` | Lượt kết thúc |
| `WindUpdated` | Gió mới |
| `MatchEnded` | Kết quả + rewards |
| `MatchStateSync` | Full state (reconnect) |

---

### Phase 6: Mission, Ranking & Moderation

#### API Server — `internal/api/mission/`

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/missions/daily` | ✅ | Daily missions (3/ngày) |
| POST | `/missions/claim` | ✅ | Nhận thưởng |
| GET | `/missions/achievements` | ✅ | Achievements |

Events: MatchCompleted, MatchWon, DamageDealt, ItemUsed, EnemyKilled, TerrainDestroyed.

#### API Server — `internal/api/rank/`

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/rank/me` | ✅ | Rank hiện tại |
| GET | `/rank/leaderboard` | ✅ | Bảng xếp hạng (Redis sorted set, phân trang) |
| GET | `/rank/seasons/current` | ✅ | Season active |
| POST | `/rank/reward/claim` | ✅ | Nhận thưởng mùa |

Tiers: Bronze → Silver → Gold → Platinum → Diamond → Master.

#### API Server — `internal/api/moderation/`

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| POST | `/report/player` | ✅ | Player report |
| GET | `/moderation/reports` | ✅ Admin | Xem reports |
| POST | `/moderation/ban` | ✅ Admin | Tạo ban |
| POST | `/moderation/ban/revoke` | ✅ Admin | Gỡ ban |

Ban enforcement: check tại REST login + WS connect + periodic check trong match.

---

### Phase 7: Client Version Control & App Config

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/app/version-policy` | ❌ | Check force update |
| GET | `/app/config` | ❌ | Remote config |

Middleware check version ở mọi REST + WS handshake.

---

### Phase 8: Match History & Analytics

#### API Server — `internal/game/matchhistory/`

| Method | Path | Auth | Mô tả |
|---|---|---|---|
| GET | `/player/match-history` | ✅ | Lịch sử trận (phân trang) |

---

## Worker Server — Background Jobs

| Job | Schedule | Mô tả |
|---|---|---|
| `refund_check` | Mỗi 15-30 phút | Check IAP refund/chargeback từ store |
| `season_reward` | Khi season kết thúc | Grant reward cuối mùa |
| `account_deletion` | Mỗi giờ | Xóa/ẩn danh account hết grace period |
| `daily_reset` | 00:00 UTC | Reset daily missions |
| `ban_expire` | Mỗi 5 phút | Expire temporary bans |
| `anomaly_detect` | Mỗi giờ | Phát hiện Gem/Coin bất thường |
| `snapshot_cleanup` | Hàng ngày | Xóa match snapshots cũ |

---

## Full REST API Summary

### Auth & Account (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| POST | `/auth/guest-login` | ❌ |
| POST | `/auth/provider-login` | ❌ |
| POST | `/auth/link-provider` | ✅ |
| POST | `/auth/refresh` | ❌ |
| POST | `/auth/logout` | ✅ |
| POST | `/auth/logout-all` | ✅ |

### Player (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| GET | `/player/profile` | ✅ |
| PUT | `/player/profile` | ✅ |
| GET | `/player/inventory` | ✅ |
| GET | `/player/match-history` | ✅ |
| POST | `/account/deletion/request` | ✅ |
| GET | `/account/deletion/status` | ✅ |
| POST | `/account/deletion/cancel` | ✅ |

### Shop & Economy (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| GET | `/shop/offers` | ✅ |
| POST | `/shop/purchase` | ✅ |
| GET | `/shop/purchases` | ✅ |
| GET | `/iap/products` | ✅ |
| POST | `/iap/verify` | ✅ |
| POST | `/giftcode/redeem` | ✅ |

### Mission & Rank (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| GET | `/missions/daily` | ✅ |
| POST | `/missions/claim` | ✅ |
| GET | `/missions/achievements` | ✅ |
| GET | `/rank/me` | ✅ |
| GET | `/rank/leaderboard` | ✅ |
| GET | `/rank/seasons/current` | ✅ |
| POST | `/rank/reward/claim` | ✅ |

### Moderation (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| POST | `/report/player` | ✅ |
| GET | `/moderation/reports` | ✅ Admin |
| POST | `/moderation/ban` | ✅ Admin |
| POST | `/moderation/ban/revoke` | ✅ Admin |

### System (API Server :8080)
| Method | Path | Auth |
|---|---|---|
| GET | `/app/version-policy` | ❌ |
| GET | `/app/config` | ❌ |
| GET | `/healthz` | ❌ |
| GET | `/readyz` | ❌ |
| GET | `/livez` | ❌ |
| GET | `/metrics` | ❌ (internal) |
| GET | `/rooms` | ✅ |

### Game Server (:8081)
| Protocol | Path | Auth |
|---|---|---|
| WebSocket | `/ws` | ✅ JWT |
| GET | `/healthz` | ❌ |
| GET | `/readyz` | ❌ |
| GET | `/livez` | ❌ |
| GET | `/metrics` | ❌ (internal) |

---

## Open Questions

> [!IMPORTANT]
> 1. **HTTP Router**: `chi` (lightweight) hay `gin` (full-featured)?
> 2. **DB query**: `sqlc` (type-safe generated code) hay `GORM` (ORM)?
> 3. **WebSocket library**: `gorilla/websocket` hay `nhooyr.io/websocket`?
> 4. **Logger**: `zerolog` hay `zap`?
> 5. **Deployment target**: GCP/AWS/DO? (ảnh hưởng IAP webhook + infra)
> 6. **Admin auth**: Cùng JWT hay API key riêng?

---

## Verification Plan

### Automated Tests
```bash
# Unit tests
go test ./internal/... -v -cover

# Integration tests (testcontainers: PostgreSQL + Redis)
go test ./internal/... -tags=integration -v

# Match engine tests
go test ./internal/game/match/... -v -run TestProjectileSimulation
go test ./internal/game/match/... -v -run TestTerrainDestruction
go test ./internal/game/match/... -v -run TestDamageCalculation
```

### Manual Verification
- WS client → tạo room → join → ready → start → chơi full 1v1.
- Guest login → link Google → re-login không mất data.
- IAP verify flow với mock receipt.
- Disconnect/reconnect giữa trận.
- Crash 1 match (inject panic) → các match khác vẫn chạy.
- Shutdown API server → trận trên Game server vẫn tiếp tục.
- Load test: nhiều room đồng thời.
