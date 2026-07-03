# Battle Squad

Game server cho Battle Squad — game bắn súng theo lượt (turn-based artillery) hỗ trợ PvP 1v1 và 2v2 thời gian thực.

## Tổng quan

Battle Squad server gồm **3 process** chạy độc lập, chia sẻ chung PostgreSQL và Redis:

| Process | Port | Vai trò |
|---------|------|---------|
| **API Server** | `:8080` | REST API — đăng nhập, shop, inventory, mission, ranking, moderation |
| **Game Server** | `:8081` | WebSocket — phòng chơi, trận đấu thời gian thực, physics engine |
| **Worker** | — | Background jobs — expire ban, reset daily mission, xóa account |

Lợi ích tách process: bug crash API không ảnh hưởng trận đang chơi, deploy/scale độc lập từng service.

## Tech Stack

- **Go 1.25** — Language chính
- **chi** — HTTP router (API Server)
- **gorilla/websocket** — WebSocket (Game Server)
- **pgx/v5** — PostgreSQL driver
- **go-redis/v9** — Redis client
- **golang-jwt/v5** — JWT authentication
- **zerolog** — Structured logging
- **prometheus/client_golang** — Metrics

## Yêu cầu

- Go 1.25+
- Docker & Docker Compose
- Make (optional)

---

## Chạy Local (Development)

### 1. Khởi động PostgreSQL + Redis

```bash
make docker-up
# hoặc
docker-compose up -d postgres redis
```

### 2. Chạy migration

```bash
make migrate
# hoặc
go run ./cmd/migrate
```

### 3. Chạy 3 servers (mỗi cái 1 terminal)

```bash
# Terminal 1 — API Server
make run-api

# Terminal 2 — Game Server
make run-game

# Terminal 3 — Worker
make run-worker
```

API Server sẵn sàng tại `http://localhost:8080`, Game Server tại `ws://localhost:8081/ws`.

### Verify nhanh

```bash
# Health check
curl http://localhost:8080/healthz

# Guest login
curl -X POST http://localhost:8080/auth/guest-login \
  -H "Content-Type: application/json" \
  -d '{"deviceInstallId": "test-device-001"}'

# Xem metrics
curl http://localhost:8080/metrics
```

---

## Chạy Production (Docker Compose)

### All-in-one

```bash
docker-compose up -d --build
```

Lệnh này build và chạy tất cả 5 services: postgres, redis, api, game, worker.

### Build riêng từng image

```bash
docker build -f Dockerfile.api -t battlesquad-api .
docker build -f Dockerfile.game -t battlesquad-game .
docker build -f Dockerfile.worker -t battlesquad-worker .
```

### Biến môi trường (Production)

| Biến | Mô tả | Mặc định |
|------|--------|----------|
| `APP_ENV` | Môi trường (`development`, `production`) | `development` |
| `API_PORT` | Port API Server | `8080` |
| `GAME_PORT` | Port Game Server | `8081` |
| `POSTGRES_DSN` | PostgreSQL connection string | `postgres://postgres:postgres@localhost:5432/battlesquad?sslmode=disable` |
| `REDIS_ADDR` | Redis address | `localhost:6379` |
| `REDIS_PASSWORD` | Redis password | _(empty)_ |
| `REDIS_DB` | Redis database number | `0` |
| `JWT_SECRET` | Secret key cho JWT token | _(phải thay đổi cho production)_ |
| `APP_VERSION` | Phiên bản ứng dụng | `1.0.0` |
| `PROTOCOL_VERSION` | Phiên bản protocol tối thiểu | `1` |
| `NODE_ID` | ID node cho Game Server (multi-node) | `node-game-1` |

**Quan trọng:** Luôn đặt `JWT_SECRET` riêng và `APP_ENV=production` khi deploy thật.

---

## Cấu trúc Project

```
battle-squad/
├── cmd/
│   ├── api/main.go          # API Server entry point
│   ├── game/main.go         # Game Server entry point
│   ├── worker/main.go       # Worker entry point
│   └── migrate/main.go      # Database migration tool
├── internal/
│   ├── shared/              # Code dùng chung
│   │   ├── auth/            # JWT sign/verify
│   │   ├── config/          # Environment config
│   │   ├── database/        # Postgres + Redis clients
│   │   ├── middleware/       # Auth, rate limit, correlation ID, version check
│   │   ├── model/           # Error codes
│   │   ├── observability/   # Logger, health checks, Prometheus metrics
│   │   ├── circuitbreaker/  # Circuit breaker pattern
│   │   ├── featureflag/     # Feature flags (Redis-backed)
│   │   └── idempotency/     # Idempotency key manager
│   ├── api/                 # API Server modules (handler → service → repository)
│   │   ├── auth/            # Login, JWT refresh, link provider
│   │   ├── player/          # Profile, account deletion
│   │   ├── economy/         # Coin/Gem ledger
│   │   ├── inventory/       # Item management + reservations
│   │   ├── shop/            # Shop purchase (idempotent)
│   │   ├── iap/             # In-app purchase verification
│   │   ├── giftcode/        # Gift code redemption
│   │   ├── mission/         # Daily missions + achievements
│   │   ├── rank/            # Elo ranking + leaderboard
│   │   ├── moderation/      # Player reports + ban (admin only)
│   │   ├── appconfig/       # Version policy + remote config
│   │   ├── matchhistory/    # Match history (paginated)
│   │   └── rooms/           # Room list (from Redis)
│   ├── game/                # Game Server modules
│   │   ├── ws/              # WebSocket server + client pumps
│   │   ├── room/            # Room hub + room goroutines
│   │   ├── match/           # Match engine (physics, damage, terrain, items, skills, bot AI)
│   │   └── gamedata/        # YAML config loader
│   └── worker/              # Background jobs
│       ├── scheduler.go
│       └── jobs/            # ban_expire, daily_reset, account_deletion
├── configs/                 # Game data (YAML)
│   ├── characters.yaml      # 4 nhân vật: Rookie, Tanko, Spark, Flora
│   ├── weapons.yaml         # 4 vũ khí
│   ├── skills.yaml          # 4 kỹ năng (triple_shot, heavy_bomb, shock_field, healing_bloom)
│   ├── items.yaml           # 8 items (medkit, teleport, power_shot, drill_bomb, ...)
│   └── maps.yaml            # 3 maps với terrain đặc biệt (lava, ice, fragile)
├── migrations/              # SQL schema
├── Makefile
├── Dockerfile.api
├── Dockerfile.game
├── Dockerfile.worker
└── docker-compose.yml
```

---

## API Endpoints

### Public (không cần auth)

| Method | Path | Mô tả |
|--------|------|--------|
| POST | `/auth/guest-login` | Đăng nhập guest |
| POST | `/auth/provider-login` | Đăng nhập Google/Apple |
| POST | `/auth/refresh` | Refresh access token |
| GET | `/app/version-policy` | Kiểm tra version |
| GET | `/app/config` | Remote config |
| GET | `/healthz` | Liveness probe |
| GET | `/readyz` | Readiness probe |
| GET | `/livez` | Chi tiết health |
| GET | `/metrics` | Prometheus metrics |

### Protected (cần Bearer token)

| Method | Path | Mô tả |
|--------|------|--------|
| POST | `/auth/link-provider` | Link Google/Apple vào guest account |
| POST | `/auth/logout` | Đăng xuất |
| GET | `/player/profile` | Xem profile |
| PUT | `/player/profile` | Cập nhật display name |
| GET | `/player/inventory` | Xem inventory |
| GET | `/player/match-history` | Lịch sử trận đấu |
| GET | `/shop/offers` | Danh sách shop |
| POST | `/shop/purchase` | Mua item/nhân vật |
| GET | `/iap/products` | Danh sách IAP |
| POST | `/iap/verify` | Xác thực IAP receipt |
| POST | `/giftcode/redeem` | Đổi gift code |
| GET | `/missions/daily` | Nhiệm vụ ngày |
| GET | `/missions/achievements` | Thành tích |
| POST | `/missions/claim` | Nhận thưởng mission |
| GET | `/rank/me` | Rank hiện tại |
| GET | `/rank/leaderboard` | Bảng xếp hạng |
| POST | `/rank/reward/claim` | Nhận thưởng mùa |
| GET | `/rooms` | Danh sách phòng chơi |
| POST | `/report/player` | Report người chơi |
| POST | `/moderation/ban` | Ban (admin) |
| POST | `/moderation/ban/revoke` | Unban (admin) |

### Game Server (WebSocket)

| Protocol | Path | Auth |
|----------|------|------|
| WebSocket | `/ws?token=<JWT>` | JWT trong query param hoặc Authorization header |

**Room events:** `CreateRoom`, `JoinRoom`, `LeaveRoom`, `ChangeTeam`, `SelectCharacter`, `SelectItems`, `Ready`, `StartMatch`

**Match events:** `Move`, `Shoot`, `UseItem`, `EndTurn`, `Reconnect`, `Leave`

---

## Game Mechanics

### Nhân vật (4)

| ID | Vai trò | HP | Damage | Defense | Skill |
|----|---------|-----|--------|---------|-------|
| rookie | Balanced | 100 | 50 | 50 | Triple Shot |
| tanko | Tank | 130 | 45 | 80 | Heavy Bomb |
| spark | DPS | 90 | 75 | 40 | Shock Field |
| flora | Support | 95 | 55 | 60 | Healing Bloom |

### Items (8)

| Item | Loại | Hiệu ứng |
|------|------|-----------|
| Medkit | Consumable | Hồi 30 HP |
| Teleport | Consumable | Dịch chuyển tức thì |
| Power Shot | Modifier | x1.5 damage lượt này |
| Drill Bomb | Modifier | Đạn xuyên terrain, nổ lần 2 |
| Spider Net | Modifier | Giảm move energy đối thủ |
| Freeze Bomb | Modifier | Đóng băng đối thủ 1 lượt (không bắn, không di chuyển) |
| Air Strike | Modifier | Đạn rơi từ trên xuống |
| Wind Stopper | Modifier | Triệt tiêu gió 2 lượt |

### Terrain đặc biệt

| Loại | Hiệu ứng |
|------|-----------|
| Lava | 5 damage mỗi lượt khi đứng trên |
| Ice | Trượt thêm 50px khi di chuyển |
| Fragile | Bán kính nổ x2 |

### Ranking

Bronze → Silver → Gold → Platinum → Diamond → Master. Mỗi tier chia 3 division. Rating thay đổi +25 (thắng) / -20 (thua) sau mỗi trận PvP.

---

## Makefile Commands

```bash
make build-all      # Build tất cả binaries vào bin/
make test           # Chạy toàn bộ tests
make test-cover     # Chạy tests với coverage report
make lint           # Go vet
make migrate        # Chạy database migration
make docker-up      # Docker compose up
make docker-down    # Docker compose down
make clean          # Xóa bin/ và coverage files
```

---

## Health Checks

| Endpoint | Mục đích | Response |
|----------|----------|----------|
| `GET /healthz` | Liveness — process sống | Luôn 200 |
| `GET /readyz` | Readiness — DB + Redis OK | 200 hoặc 503 |
| `GET /livez` | Chi tiết — latency từng dependency | JSON với status + checks |

Cả API Server và Game Server đều expose 3 endpoint này.

---

## Monitoring

Prometheus metrics tại `/metrics` trên cả 2 servers:

- `http_requests_total` — Tổng HTTP requests (method, path, status)
- `http_request_duration_seconds` — Latency histogram
- `active_ws_connections` — WebSocket connections hiện tại
- `active_rooms` / `active_matches` — Phòng và trận đang chạy
- `match_started_total` / `match_ended_total` — Tổng trận
- `match_panic_total` — Số trận bị panic (nên = 0)

---

## License

Private — All rights reserved.
