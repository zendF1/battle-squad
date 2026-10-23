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
| `IAP_MOCK_ENABLED` | `false` | Accept fake IAP purchase tokens (testing only; ignored when `APP_ENV=production`) |
| `DEV_UNLOCK_ALL` | `false` | Game server: QuickPlay may use every character and item without owning them, and items are not reserved or consumed (testing only; ignored when `APP_ENV=production`) |
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
├── migrate/main.go       Database migration tool
└── mapgen/main.go        Ghi migration 036 / 038 + configs/maps.yaml, bricks.yaml từ internal/game/mapgen (-preview: PNG)

internal/
├── api/                  API server modules (handler → service → repository)
│   ├── auth/             Guest/OAuth login, JWT refresh, provider linking
│   ├── player/           Profile CRUD, account deletion (7-day grace)
│   ├── economy/          Coin/Gem ledger (Credit/Debit trong DB transaction)
│   ├── inventory/        Player items query
│   ├── shop/             Shop offers, purchase (idempotent)
│   ├── iap/              In-app purchase, receipt verification
│   ├── giftcode/         Gift code redemption
│   ├── mission/          Nhiệm vụ ngày (UTC+7 periods, activity chests), achievements
│   ├── milestone/        Mốc thưởng: one-time progress rewards, stat counters
│   ├── title/            Danh hiệu: owned titles, equip
│   ├── rank/             Elo rating, leaderboard, seasons, season rewards
│   ├── moderation/       Player reports, ban/unban (admin only)
│   ├── appconfig/        Version policy, remote config
│   ├── maps/             GET /maps, /maps/{id}: map catalogue cho client sync
│   ├── gamedata/         GET /gamedata: mọi luật / số / text client cần (docs/gamedata-contract.md)
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
│   ├── mapgen/           Layout 16 map (primitive vẽ tay) → tiles, migration, YAML
│   ├── battlestats/      Chỉ số vào trận (gốc + điểm + equipment), dùng chung cho room và GET /player/characters
│   └── gamedata/         Snapshot game data (DB, fallback YAML): config, luật (rules.*), tuning skill, text; mỗi trận giữ snapshot lúc bắt đầu
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

configs/                  YAML game data (characters, weapons, skills, items, maps, bricks); mirror của DB cho test / fallback
migrations/               SQL migration files (001-039)
```

---

## 5. API Server — REST Endpoints

### Response Envelope

Mọi response REST (trừ `/healthz`, `/readyz`, `/livez`, `/metrics`) đều cùng một dạng, ghi qua `model.WriteOK` / `model.WriteJSON` / `model.WriteError` / `model.WriteServiceError`:

```json
{ "success": true, "data": { ... } }
{ "success": false, "error": { "code": "SHOP_ITEM_OUT_OF_STOCK", "message": "...", "details": {}, "correlationId": "abc123" } }
```

- `data` bị bỏ khi endpoint không có gì để trả (ví dụ logout).
- Lỗi không nằm trong registry được log và trả `INTERNAL_SERVER_ERROR`, không lộ message nội bộ.

### Public (không cần auth)

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| POST | `/auth/guest-login` | GuestLogin | Login bằng device ID, tạo account nếu chưa có |
| POST | `/auth/provider-login` | ProviderLogin | Login bằng Google/Apple OAuth token |
| POST | `/auth/refresh` | RefreshToken | Đổi refresh token lấy access token mới |
| POST | `/auth/logout` | Logout | Blacklist refresh token trong Redis |
| GET | `/app/version-policy?platform=` | GetVersionPolicy | Policy version cho android/ios |
| GET | `/app/config` | GetRemoteConfig | Remote config (API URL, WS URL, features). `defaultCharacterSelect`, `activeItems` (thứ tự túi đồ), `clientParams.windScaleFactor` / `physicsTimeStep` lấy từ game data snapshot API đang phục vụ (chỉ giữ cho client cũ; luật đầy đủ ở `/gamedata`), `gamedataVersion` = version của `/gamedata` |
| GET | `/maps` | maps.List | Danh sách map để client sync (nguồn: `config_maps` + `config_brick_types`), xem **Maps** ở mục 7. Header `ETag` = `version`; gửi `If-None-Match` với ETag đã cache → `304` khi không đổi |
| GET | `/gamedata` | gamedata.Get | Public. Mọi luật, số và text tiếng Việt client cần (vật lý, aim aid, vũ khí, skill + template mô tả, nhân vật, item, status effect, rank, level, trang bị, matchmaking, mode, theme) để client không hardcode gì; hợp đồng đầy đủ ở `docs/gamedata-contract.md`. `version` = hash nội dung = `ETag`; `If-None-Match` khớp → `304`. Build một lần mỗi snapshot (reload khi admin lưu). `?version=<v>` → đúng game data của version đó (bản một trận đang chơi báo trong `gamedataVersion`, lưu ở `gamedata_versions`), không có → `404 GAMEDATA_VERSION_NOT_FOUND` |
| GET | `/maps/{mapId}` | maps.Get | Một map kèm `tiles` (lưới brick id, `[]` với map legacy) và `spawnPoints`, để vẽ preview; không có → `MAP_NOT_FOUND` 404 |
| GET | `/healthz` | Healthz | Health check |
| GET | `/readyz` | Readyz | Readiness check (DB + Redis) |
| GET | `/livez` | Livez | Liveness check |
| GET | `/metrics` | Prometheus | Prometheus metrics |

### Protected (cần Bearer token)

**Player & Account:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/player/profile` | GetProfile | Lấy profile (name, level, coin, gem, `equippedTitle: {id, name, rarity} \| null` — danh hiệu đang đeo, null khi không đeo / không còn sở hữu / đã tắt; luôn có field) |
| PUT | `/player/profile` | UpdateProfile | Đổi display name: body `{"displayName"}`, trim khoảng trắng hai đầu, 1–20 ký tự (đếm rune, không phải byte). Rỗng/quá dài → 400 `PLAYER_UPDATE_FAILED` |
| POST | `/account/deletion/request` | RequestAccountDeletion | Yêu cầu xóa account (7 ngày grace) |
| POST | `/account/deletion/cancel` | CancelAccountDeletion | Hủy yêu cầu xóa |
| GET | `/account/deletion/status` | GetAccountDeletionStatus | Trạng thái xóa account |
| POST | `/auth/link-provider` | LinkProvider | Liên kết Google/Apple vào account |

**Economy & Shop:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/player/inventory` | GetInventory | Danh sách items đang có |
| GET | `/shop/offers` | GetOffers | Các offer đang active |
| POST | `/shop/purchase` | Purchase | Mua offer: body `{offerId, idempotencyKey}` (key ≤ 64); cùng key + offer trả lại purchase cũ, không trừ tiền lần nữa. Không đủ tiền → 400 `SHOP_INSUFFICIENT_BALANCE` |
| GET | `/shop/purchases` | GetPurchases | Lịch sử mua hàng |
| GET | `/iap/products` | GetProducts | Danh sách IAP products |
| POST | `/iap/verify` | VerifyReceipt | Verify receipt từ App Store/Play Store |
| POST | `/giftcode/redeem` | Redeem | Nhập gift code |

**Character Progression:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/player/characters` | GetCharacters | `{characters, resetStatsCost: {currency, amount}, baseStats, statMultipliers}`: nhân vật đã sở hữu (level, exp, `statPoints`, 6 chỉ số `bonus*`, `stats` = chỉ số vào trận), phí tẩy điểm (cùng config `character_progression` mà reset-stats trừ), chỉ số gốc của mọi nhân vật và giá trị mỗi điểm — xem [Character Stat Bonuses](#character-stat-bonuses) |
| POST | `/player/character/allocate-stats` | AllocateStats | Cộng điểm `{characterId, hp, damage, mobility, defense, skillPower, terrainDamage}` |
| POST | `/player/character/reset-stats` | ResetStats | Tẩy điểm `{characterId}`: xoá mọi điểm đã cộng và đặt `statPoints = (level − 1) × pointsPerLevel` theo config hiện tại (đổi `pointsPerLevel` thì nhân vật tẩy điểm sẽ nhận số điểm theo luật mới), phí `resetCostAmount` `resetCostCurrency` (seed: 500 coin). Khoá profile rồi row nhân vật `FOR UPDATE`, kiểm tra, trừ tiền và reset trong một transaction — reset đồng thời chỉ bị trừ tiền một lần |

**Offer types** (`offerType` trong `shop_offers`, admin chọn trong dropdown):

| `offerType` | Khi mua | `quantityGranted` |
|-------------|---------|-------------------|
| `item`, `bundle` | Cộng `quantity` item `itemId` vào inventory | số item |
| `character_unlock` | Mở nhân vật `itemId`; đã có → 409 `SHOP_ALREADY_OWNED`, không trừ tiền | 1 |
| `coin_pack` | Đổi kim cương ra xu: trừ `priceAmount` gem và cộng `quantity` coin trong cùng một transaction (2 dòng `economy_transactions`, `source = shop_purchase`, `ref_id = offerId`). Bắt buộc `itemId = coin`, `priceCurrency = gem`, `quantity > 0`; sai cấu hình → 500 trước khi trừ tiền (admin cũng chặn khi lưu) | số xu |

**Default shop catalog** (`migrations/014_default_shop_catalog.up.sql`, `ON CONFLICT DO NOTHING` nên chạy lại không ghi đè chỉnh sửa từ admin):

| `offerId` | `itemId` | Loại | Giá | Số lượng | Giới hạn/người |
|-----------|----------|------|-----|----------|----------------|
| `item_medkit_x3` | `medkit` | item | 300 coin | 3 | — |
| `item_teleport_x3` | `teleport` | item | 400 coin | 3 | — |
| `item_power_shot_x3` | `power_shot` | item | 400 coin | 3 | — |
| `item_drill_bomb_x3` | `drill_bomb` | item | 450 coin | 3 | — |
| `item_spider_net_x3` | `spider_net` | item | 450 coin | 3 | — |
| `item_freeze_bomb_x3` | `freeze_bomb` | item | 500 coin | 3 | — |
| `item_air_strike_x3` | `air_strike` | item | 600 coin | 3 | — |
| `item_wind_stopper_x3` | `wind_stopper` | item | 300 coin | 3 | — |
| `unlock_tanko_coin` / `unlock_tanko_gem` | `tanko` | character_unlock | 5000 coin / 300 gem | 1 | 1 |
| `unlock_spark_coin` / `unlock_spark_gem` | `spark` | character_unlock | 5000 coin / 300 gem | 1 | 1 |
| `unlock_flora_coin` / `unlock_flora_gem` | `flora` | character_unlock | 5000 coin / 300 gem | 1 | 1 |

`rookie` là nhân vật mặc định, không bán.

Gói đổi kim cương ra xu (`migrations/018_coin_exchange_offers.up.sql`, `ON CONFLICT DO NOTHING`): tỉ giá gốc 1 gem = 100 coin, không bonus, không giới hạn/người. Client tính nhãn bonus so với tỉ giá gốc (chỉ hiện khi gói lời hơn 100 xu/kim cương).

| `offerId` | `itemId` | Loại | Giá | Số lượng (coin) | Giới hạn/người |
|-----------|----------|------|-----|-----------------|----------------|
| `coin_pack_10` | `coin` | coin_pack | 10 gem | 1000 | — |
| `coin_pack_50` | `coin` | coin_pack | 50 gem | 5000 | — |
| `coin_pack_100` | `coin` | coin_pack | 100 gem | 10000 | — |
| `coin_pack_500` | `coin` | coin_pack | 500 gem | 50000 | — |

Down migration chỉ xoá gói còn đúng giá trị seed và chưa ai mua.

| `productId` | `platformSkuId` (android = ios) | Gem | Bonus | Giá |
|-------------|--------------------------------|-----|-------|-----|
| `gems_100` | `com.battlesquad.gems_100` | 100 | 0 | $0.99 |
| `gems_550` | `com.battlesquad.gems_550` | 500 | 50 | $4.99 |
| `gems_1200` | `com.battlesquad.gems_1200` | 1000 | 200 | $9.99 |

**Equipment (trang bị):**

Slot: `weapon`, `armor`, `helmet`, `pants`, `boots`, `gloves`. Mỗi món sở hữu (`player_equipment`) có `upgradeLevel` 0–16, tối đa 2 ô ngọc, và được mặc cho **một** nhân vật (`equippedOn`); mỗi (nhân vật, slot) chỉ một món. Chỉ số equipment là điểm (đổi bằng `statMultipliers`, crit là %), vào `stats.equipmentPoints` / `stats.fromEquipment` của `GET /player/characters` và vào trận (xem [Chỉ số hiển thị](#chỉ-số-hiển-thị-get-playercharacters)).

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/equipment/catalog` | GetCatalog | Toàn bộ catalog: `{items, upgradeRates, setBonuses, stones, sellRate, mergeRatePerItem, upgradeMilestones, weaponUpgradeCostMultiplier}` (`upgradeRates[].carefulGemCost` / `upgradeRates[].carefulBonusPercent`: giá kim cương và bonus tương đối (%) của nâng cấp cẩn thận ở bậc đó, một trong hai = 0 → không có; `weaponUpgradeCostMultiplier`: vũ khí cần upgradeCost × hệ số này power (game_settings `equipment.weapon_upgrade_cost_multiplier`, mặc định 3); `sellRate`: tỉ lệ hoàn khi bán, xem `/player/equipment/sell`; `mergeRatePerItem`: % thành công mỗi viên khi ghép (game_settings `equipment.merge_rate_per_item`, mặc định 25); `upgradeMilestones`: `[{level, bonus}]` cho +6/+10/+14/+16 (game_settings `equipment.milestone_<N>_bonus`, mặc định 0.10/0.20/0.40/1.00) — client tính chỉ số sau nâng cấp đúng như `battlestats`; items gồm cả `normal` và `crafted`, **cả item đã tắt** với `isActive: false` — món người chơi đã sở hữu vẫn cần tên/chỉ số; client lọc `isActive` cho shop). Client cache được, chỉ đổi khi admin sửa config |
| GET | `/player/equipment` | GetEquipment | Trang bị đang có (mới nhất trước). Tên/chỉ số lấy từ catalog theo `itemId` |
| POST | `/player/equipment/equip` | EquipItem | `{equipmentId, characterId}` → `{"success":true}`. Slot của nhân vật đã có món khác thì món cũ tự tháo. Món đang mặc (cho bất kỳ nhân vật nào) → 409 `EQUIP_ALREADY_EQUIPPED`: tháo trước rồi mặc lại |
| POST | `/player/equipment/unequip` | UnequipItem | `{equipmentId}` → `{"success":true}`; món không đang mặc → 409 `EQUIP_NOT_EQUIPPED` |
| POST | `/player/equipment/upgrade` | Upgrade | `{equipmentId, stones: [{stoneLevel, quantity}], careful?, expectedLevel?}` → `{success, newLevel, percent, equipmentId, careful, gemSpent, gemBalance?}`. Tỷ lệ đá = min(Σ power × quantity × 100 / (upgradeCost × hệ số), maxPercent), hệ số = `weaponUpgradeCostMultiplier` (3) cho slot `weapon`, 1 cho món khác; `careful: true` nhân tỷ lệ đá (đã cap) với (1 + `carefulBonusPercent`/100) của bậc, làm tròn 2 số lẻ, tối đa 100, và trừ `carefulGemCost` kim cương của bậc (log `economy_transactions` source `careful_upgrade`, ref equipmentId) dù thành công hay thất bại; `percent` trả về là tỷ lệ cuối dùng để roll; `gemBalance` chỉ có khi careful. `expectedLevel`: cấp client đang thấy — khác cấp hiện tại → 409 `EQUIP_MODIFIED`, không trừ gì (retry mù sau khi mất response không roll thêm bậc sau). Mọi đá gửi lên bị tiêu; thất bại → `failResetTo`. Lỗi (không đổi gì): `stones` rỗng hoặc tổng power 0 (chỉ toàn quantity ≤ 0) → 400 `EQUIP_NO_STONES`; đã +16 → 400 `EQUIP_MAX_UPGRADE`; thiếu đá → 400 `EQUIP_INSUFFICIENT_STONES`; careful mà giá hoặc bonus của bậc = 0 → 400 `EQUIP_CAREFUL_UNAVAILABLE`; careful mà thiếu kim cương → 400 `SHOP_INSUFFICIENT_BALANCE`; cấp đổi giữa chừng / khác `expectedLevel` → 409 `EQUIP_MODIFIED` |
| POST | `/player/equipment/dismantle` | Dismantle | `{equipmentId}` → `{"success":true}`; hoàn 50% power đá đã dùng từ safe zone gần nhất. Món đang mặc → 409 `EQUIP_EQUIPPED`. Món đã khoá (`isLocked`) vẫn tách được — client dùng làm "Vứt" |
| POST | `/player/equipment/sell` | Sell | `{equipmentId}` → `{currency, amount, balance, stones: [{stoneLevel, quantity}]}`. Làm đúng như dismantle (tháo ngọc về kho, hoàn đá theo safe zone, xoá món) và cộng thêm `amount` = floor(giá mua × `sellRate`) của tiền mua món đó (`priceCoin` > 0 → coin, không thì gem); `balance` = số dư mới của tiền đó, `stones` = đá hoàn (rỗng nếu không). `sellRate` = game_settings `equipment.sell_rate` (migration 022, mặc định 0.25; thiếu/sai/ngoài (0, 1] → 0.25). Log `economy_transactions` source `sell_equipment`, `ref_id` = itemId. Lỗi (không đổi gì): món đã từng mặc (`isLocked`, gồm mọi món đang mặc) → 409 `EQUIP_LOCKED`; không sở hữu / đã bán → 403 `EQUIP_NOT_OWNED`; item không còn config, giá 0 (crafted) hoặc tiền hoàn làm tròn về 0 → 400 `EQUIP_NOT_SELLABLE` |
| POST | `/player/equipment/socket` | SocketGem | `{equipmentId, slotIndex (1\|2), gemId}` |
| POST | `/player/equipment/unsocket` | UnsocketGem | `{equipmentId, slotIndex}` |
| GET | `/player/stones` · `/player/gems` · `/player/materials` | GetStones · GetGems · GetMaterials | `[{stoneLevel, quantity}]` · `[{gemId, gemType, gemLevel}]` · `[{materialId, quantity}]` (kèm `playerId`) |
| GET | `/shop/equipment?characterId=` | GetShopEquipment | Item `normal` active dùng được cho nhân vật đó: đồ dùng chung (`characterId` null: áo, mũ, quần, giày, găng) + vũ khí của nhân vật đó (`<character>_weapon_lvN`). Không có `characterId` → chỉ đồ dùng chung |
| POST | `/shop/equipment/buy` | BuyEquipment | `{itemId, idempotencyKey}` → `{equipmentId}`. Trả `priceCoin` (hoặc `priceGem` nếu coin = 0). Key ≤ 64, tuỳ chọn: cùng key + item trả lại `equipmentId` của lần mua đầu, không trừ tiền lần nữa; key đã dùng cho item khác → 409 `IDEMPOTENCY_KEY_REUSED`; lần mua lỗi không lưu key nên retry chạy lại từ đầu. Không gửi key → mỗi request mua một món (như cũ). Không đủ tiền → 400 `SHOP_INSUFFICIENT_BALANCE`; item không có/đã tắt → 404 `EQUIP_NOT_FOUND`; item crafted, thiếu `itemId` hoặc key > 64 → 400 `BAD_REQUEST`. Khoá profile `FOR UPDATE` nên các lần mua của một người chạy tuần tự |
| GET/POST | `/shop/stones`, `/shop/stones/buy` | GetShopStones, BuyStones | `{stoneLevel, quantity (1–999), idempotencyKey?}` → `{stoneLevel, quantity, currency, amount, balance, owned, replay}` (`amount` = giá × quantity, `balance` = số dư mới của tiền đó, `owned` = số đá cấp đó đang có). `idempotencyKey` (≤ 64, tuỳ chọn; bảng `stone_purchases`, migration 024): gửi lại cùng key + cấp + số lượng trả về lần mua đầu với `replay: true`, không trừ tiền lần nữa; cùng key khác cấp/số lượng → 409 `IDEMPOTENCY_KEY_REUSED`; lần mua bị từ chối không ghi key. Đá `merge_only` → 400 `BAD_REQUEST`; cấp không có → 404 `NOT_FOUND`; thiếu tiền → 400 `SHOP_INSUFFICIENT_BALANCE`. Log `economy_transactions` source `buy_stones`, `ref_id` = `stone_<cấp>` |
| GET/POST | `/shop/gems`, `/shop/gems/buy` | GetShopGems, BuyGems | `{gemType, gemLevel (1–6), quantity}`; lv1–3 = level×200 coin, lv4–6 = (level−3)×30 gem |
| GET/POST | `/shop/materials`, `/shop/materials/buy` | GetShopMaterials, BuyMaterials | `{materialId, quantity}`, giá `priceGem` |
| POST | `/merge/stone` · `/merge/gem` | MergeStones · MergeGems | `{stoneLevel, count}` · `{gemType, gemLevel, count}`, count 2–4 → `{success, message}` |
| GET | `/crafting/recipes` | GetRecipes | `[{recipeId, resultItemId, materials: [{materialId, quantity}], isActive}]` |
| POST | `/crafting/craft` | Craft | `{recipeId}` → `{equipmentId}` |

`GET /equipment/catalog` (`tier`, `characterId` bị bỏ khi null):

```json
{
  "items": [
    {"itemId": "rookie_weapon_lv10", "name": "Iron Bazooka", "slot": "weapon", "category": "normal", "requiredLevel": 10,
     "characterId": "rookie", "gemSlots": 1, "statHp": 0, "statDamage": 8, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 500, "priceGem": 0, "isActive": true},
    {"itemId": "normal_weapon_lv10", "name": "Iron Blaster", "slot": "weapon", "category": "normal", "requiredLevel": 10,
     "gemSlots": 1, "statHp": 0, "statDamage": 8, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 500, "priceGem": 0, "isActive": false},
    {"itemId": "crafted_weapon_silver", "name": "Silver Blaster", "slot": "weapon", "category": "crafted", "tier": "silver",
     "requiredLevel": 15, "gemSlots": 2, "statHp": 0, "statDamage": 10, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 0, "priceGem": 0, "isActive": true}
  ],
  "upgradeRates": [{"fromLevel": 0, "toLevel": 1, "upgradeCost": 1, "maxPercent": 80, "failResetTo": 0, "carefulGemCost": 2, "carefulBonusPercent": 30}],
  "setBonuses": [{"tier": "silver", "piecesRequired": 2, "bonusHpPct": 3, "bonusDmgPct": 0, "bonusDefPct": 0, "bonusCritPct": 0}],
  "stones": [{"stoneLevel": 1, "power": 1, "priceCoin": 100, "priceGem": 0, "source": "coin_shop"}],
  "sellRate": 0.25,
  "mergeRatePerItem": 25,
  "upgradeMilestones": [{"level": 6, "bonus": 0.1}, {"level": 10, "bonus": 0.2}, {"level": 14, "bonus": 0.4}, {"level": 16, "bonus": 1}],
  "weaponUpgradeCostMultiplier": 3
}
```

`GET /player/equipment` (`tier`, `gemSlot1`, `gemSlot2`, `equippedOn` bị bỏ khi null):

```json
[{"equipmentId": "6f1c…", "playerId": "p1", "itemId": "normal_weapon_lv10", "slot": "weapon", "category": "normal",
  "upgradeLevel": 0, "isEquipped": true, "equippedOn": "tanko", "isLocked": true, "createdAt": "2026-10-15T10:00:00Z"}]
```

Điều kiện mặc: sở hữu nhân vật (`CHARACTER_NOT_FOUND`), `characterId` của item null hoặc trùng (`EQUIP_WRONG_CHARACTER`), level nhân vật ≥ `requiredLevel` (`EQUIP_LEVEL_REQUIRED`). Mặc/tháo, cường hoá, khảm ngọc, tách và chỉ số vào trận không xét `isActive`: item đã tắt chỉ không bán nữa, món đã có vẫn dùng bình thường. Level nhân vật tối đa = level cuối của bảng `character_levels` (mặc định 80, xem [Reward System](#reward-system)).

**Default equipment catalog** (`migrations/016_default_equipment_catalog.up.sql`, cùng dữ liệu với `cmd/admin` `SeedEquipmentConfig`; `ON CONFLICT DO NOTHING` nên chạy lại không ghi đè chỉnh sửa từ admin):

Level yêu cầu theo `docs/equipment-system-design.md` (là **level nhân vật**); hậu tố `lvN` của item normal chính là level yêu cầu:

| Tier | Item id | Lv yêu cầu | Nguồn | Ô ngọc |
|------|---------|-----------|-------|--------|
| Iron | `normal_<slot>_lv10` (5 slot không phải weapon) · `<character>_weapon_lv10` | 10 | Shop (coin) | 1 |
| Steel | `normal_<slot>_lv20` · `<character>_weapon_lv20` | 20 | Shop (coin) | 1 |
| Mithril | `normal_<slot>_lv30` · `<character>_weapon_lv30` | 30 | Shop (coin) | 1 |
| Dragon | `normal_<slot>_lv40` · `<character>_weapon_lv40` | 40 | Shop (coin) | 1 |
| Silver | `crafted_<slot>_silver` (× 6 slot) | 15 | Chế tạo `recipe_<slot>_silver` | 2 |
| Gold | `crafted_<slot>_gold` | 35 | Chế tạo | 2 |
| Titan | `crafted_<slot>_titan` | 55 | Chế tạo | 2 |
| Diamond | `crafted_<slot>_diamond` | 75 | Chế tạo | 2 |

Đồ thường dùng chung cho mọi nhân vật, **trừ vũ khí**: mỗi nhân vật có 4 vũ khí riêng (`characterId` = nhân vật; súng, tên "<Iron|Steel|Mithril|Dragon> <súng>": Rookie Bazooka, Tanko Cannon, Spark Shock Rifle, Flora Seed Launcher; chỉ số và giá bằng tier vũ khí chung). Đồ crafted dùng chung (vũ khí crafted: Silver/Gold/Titan/Diamond Blaster). 4 vũ khí chung `normal_weapon_lv10..40` (Iron/Steel/Mithril/Dragon Blaster) được `migrations/019_equipment_design_levels.up.sql` tắt (`is_active = false`): không bán nữa, món đã mua vẫn mặc được cho mọi nhân vật và vẫn có trong catalog.

Migration 019 (chạy lại mỗi lần deploy, mọi bước có điều kiện nên chạy lại không đổi gì và không ghi đè chỉnh sửa admin):
1. Trả level yêu cầu về thiết kế cho DB còn giữ bảng sai 1/4/7/10 (normal) và 2/5/8/10 (crafted) của bản 016 cũ: mỗi slot chỉ được sửa khi **cả 4** dòng của slot (normal lv10..lv40, hoặc crafted silver..diamond) vẫn đúng giá trị đã remap; slot admin đã sửa một dòng thì giữ nguyên cả slot. 016 không còn bước remap.
2. Mở rộng `character_levels` tới level 80 (xem [Reward System](#reward-system)), chỉ khi giá trị còn đúng nguyên văn bảng 10 level của migration 006.
3. Tắt 4 vũ khí chung nếu còn đúng như seed, và chỉ khi chưa có vũ khí riêng nào (nên chỉ chạy một lần: admin bật lại thì lần sau không tắt nữa); rồi thêm 16 vũ khí riêng (`ON CONFLICT DO NOTHING`).

`migrations/020_gun_weapon_names.up.sql` đổi tên vũ khí kiếm (bản 016/019 đầu) sang súng như trên — chỉ dòng còn đúng tên cũ (admin đã đổi tên thì giữ), chạy lại không đổi gì; down trả tên cũ cho dòng còn đúng tên súng (chạy trước down 019/016).

Down của 019 xoá vũ khí riêng còn đúng seed và không ai sở hữu, bật lại vũ khí chung còn đúng seed, trả bảng 10 level nếu còn đúng bảng 80 level; không đổi level yêu cầu. Chạy down 019 trước down 016.

Giá normal (coin) — weapon (mỗi nhân vật) 500/1500/4000/10000, armor 400/1200/3500/8000, helmet 300/900/2500/6000, pants 350/1000/3000/7000, boots 300/800/2200/5500, gloves 250/750/2000/5000. Ngoài ra: 16 mức cường hoá (`config_upgrade_rates`), 12 cấp đá (1–6 coin, 7–10 gem, 11–12 chỉ ghép), ngọc `hp`/`damage`/`defense`/`critical` lv1–10, set bonus 2/4/6 món cho mỗi tier crafted, 20 nguyên liệu (5 mỗi tier) và 24 công thức (5 nguyên liệu của tier × 10/20/30/40, weapon × 15/30/45/60). JSON nguyên liệu của công thức dùng key `materialId`; công thức cũ của admin dùng `material_id` vẫn đọc được và được migration đổi key (giữ các key khác; `quantity` dạng số/chuỗi số được làm tròn thành số nguyên, giá trị khác giữ nguyên). Down migration chỉ xoá item/nguyên liệu/công thức còn đúng như seed và không ai sở hữu; không đụng `config_upgrade_rates`, `config_stones`, `config_gems`, `config_set_bonuses`.

**Game Systems:**

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/boss/runs` | GetRuns | Lượt thưởng lớn còn lại hôm nay mỗi boss: `{day, resetAt, bosses:[{bossId, dailyBonusRuns, runsToday, bonusRunsLeft}]}` — xem §8a |
| GET | `/missions/daily` | GetDailyMissions | Nhiệm vụ ngày: `DailyState` `{day, dayResetAt, week, weekResetAt, activity, weeklyActivity, missions:[{id, title, target, required, progress, activity, goto, state}], chests:[{id, track, threshold, state, rewards}], weekly:[…]}`; lần gọi đầu trong ngày hoàn thành `daily_login` — xem §7 Nhiệm vụ ngày |
| GET | `/missions/achievements` | GetAchievements | Thành tích + tiến độ |
| POST | `/missions/claim` | ClaimMission | `{missionId}` → `{activity, state}` (điểm năng động nhận được + `DailyState` mới); chưa xong `MISSION_NOT_DONE` 400, đã nhận `MISSION_ALREADY_CLAIMED` 409, không có (kể cả mốc thưởng) `MISSION_NOT_FOUND` 404 |
| POST | `/missions/chests/claim` | ClaimChest | `{chestId}` → `{rewards:{coin, gem, items}, state}`; chưa đủ năng động `CHEST_LOCKED` 400, đã nhận kỳ này `CHEST_ALREADY_CLAIMED` 409, không có `CHEST_NOT_FOUND` 404 |
| GET | `/milestones` | List | Mốc thưởng: `{tracks:[{track, nodes:[{id, track, target, required, progress, state, rewards:{coin, gem, items}, description}]}], claimableCount}` — xem §7 Mốc thưởng |
| GET | `/milestones/summary` | Summary | `{claimableCount}` cho badge ở sảnh |
| POST | `/milestones/claim` | Claim | `{id}` → `{claimed:[id], rewards, coin, gem, stones:[{stoneLevel, quantity}], claimableCount}`; đã nhận `MILESTONE_ALREADY_CLAIMED` 409, chưa đạt `MILESTONE_NOT_REACHED` 400, không có `NOT_FOUND` |
| POST | `/milestones/claim-all` | ClaimAll | Nhận mọi mốc đang nhận được trong 1 tx, cùng dạng kết quả (rewards cộng dồn); không có gì → `claimed: []` |
| GET | `/player/titles` | List | Danh hiệu: `{titles:[{id, name, rarity, description, owned, obtainedAt}], equipped: {id, name, rarity} \| null}` — chỉ danh hiệu đang bật, theo `sort_order` rồi id; `obtainedAt` RFC3339 UTC hoặc null; xem §7 Danh hiệu |
| POST | `/player/titles/equip` | Equip | `{titleId}` (null / thiếu / `""` = tháo) → `{equipped: {id, name, rarity} \| null}`; chưa sở hữu, không tồn tại hoặc đã tắt `TITLE_NOT_OWNED` 403 (không đổi gì); JSON sai `BAD_REQUEST` |
| GET | `/rank/me` | GetRankMe | `{playerId, displayName, seasonId, rating, tier, division, wins, losses, draws, winStreak, highestTier, updatedAt, rankPos, season: {seasonId, name, endsAt}}` của mùa hiện tại. `rankPos` = vị trí (1-based) trong bảng xếp hạng mùa (rating ↓, wins ↓, player_id), `0` khi chưa có dòng `player_ranks`; khi đó rating = `elo.defaultRating` (1000 = silver, division 3), highestTier = tier đó. `endsAt` RFC3339 |
| GET | `/rank/leaderboard?page=&limit=` | GetLeaderboard | Bảng xếp hạng (default: page=1, limit=20), `rankPos` mỗi dòng |
| GET | `/rank/seasons/current` | GetCurrentSeason | Mùa giải hiện tại (dòng thật; chưa có mùa active thì tạo "Mùa N", xem §8 Mùa giải) |
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
    │     ├── ListOnlinePlayers, InviteToLobby, RespondInvite, KickMember, SetReady, SetLobbyQueue
    │     └── return false → không phải lobby event
    │
    └── RoomHandler.HandleMessage() → xử lý room/match events
          ├── CreateRoom, JoinRoom, QuickPlay
          └── Forward to Room → Forward to Match
```

### Chế độ chơi và hàng đợi (màn Chọn chế độ)

Màn Chọn chế độ của client dựng hoàn toàn từ `GET /gamedata` `modes` (xem docs/gamedata-contract.md §4.17): mục `modes.categories` (`config_mode_categories`: PvP, Boss, theo `sort_order`), trong mỗi mục các `modes.lobby` có `category` đó. Thêm chế độ = thêm dòng ở admin Texts → Game Modes / Mode Categories, không sửa code (trừ khi cần hàng đợi mới).

| `config_game_modes` (lobby) | Tên | queue | bot | category | icon | party | ranked |
|---|---|---|---|---|---|---|---|
| `casual` | Đấu thường 2vs2 | `casual_2v2` | | pvp | `swords` | 2 | false |
| `ranked` | Đấu hạng 2vs2 | `ranked_2v2` | | pvp | `trophy` | 2 | true |
| `bot` | Đấu máy | | `ai` | pvp | `robot` | 1 | false |
| `practice` | Luyện tập | | | pvp | `target` | 1 | false |
| `boss_steel_robot` / `boss_lava_crab` / `boss_ice_witch` | Rô-bốt Thép / Cua Dung Nham / Phù Thuỷ Băng | `boss:<bossId>` | | boss | `boss` | 4 | false |

Mục Boss có một chế độ mỗi boss đang bật (§8a; migration 046). `enabled = false` → chế độ không có trong `modes.lobby`. Match mode `casual_2v2` ("Đấu thường 2vs2") ở `modes.match` cho lịch sử / kết quả.

**Hàng đợi** (`matchmaker.AllQueues`): `ranked_2v2`, `casual_2v2` và `boss:<bossId>` của mỗi boss đang bật (§8a). Mode của trận ghép = queue của nó (`MatchFound.mode`, `MatchStarted.mode`, `match_histories.mode`). Admin lưu một chế độ lobby có queue khác, `ranked` khác queue (`ranked` bật đúng khi queue `ranked_2v2`), `party_size` > 1 khi không có queue (hoặc > 2), hoặc `category` không có trong Mode Categories → lỗi, không lưu (`checkGameModes`); xoá một category đang dùng cũng bị từ chối.

**Đấu thường 2vs2 (`casual_2v2`)**: ghép y như Đấu hạng — cùng `planMatches` (party / solo, chênh rating, bot lấp sau `botFillAfter`, bot đa dạng theo `rules.bots`), dùng rating ẩn của người chơi (rating xếp hạng mùa active, không có → `elo.defaultRating`) để ghép và chọn map / độ khó bot. Khác Đấu hạng:

- Không Elo, không đổi rating / điểm hạng, không ghi `player_ranks` (không thắng / thua mùa, không mốc `rank_peak` / `rating_peak`); `MatchEnded.rewards` có `ratingChange: 0` và không có các trường rank (như mọi trận không xếp hạng); lịch sử `rating_change = 0`.
- EXP / coin như mọi trận (50 + 30 thắng + sát thương + hạ gục; coin 30 + 20 thắng; rời trận 0), đếm milestone và nhiệm vụ ngày như trận ghép (`CountsForMilestones`, ≥ 3 lượt hành động). Phòng tự tạo `pvp_2v2` cùng EXP / coin nhưng không đếm milestone / nhiệm vụ.
- Rời trận giữa chừng: thua, 0 EXP / coin (không mất rating).

### Lobby Events (Matchmaking: Đấu hạng / Đấu thường 2vs2)

Mọi lobby event trừ `CreateLobby` / `JoinLobby` / `ListOnlinePlayers` / `RespondInvite` chạy trên goroutine của lobby theo đúng thứ tự client gửi, nên client gửi liền `CreateLobby` → `UpdateLoadout` → `StartQueue` được (`CreateLobby` gán `client.LobbyID` ngay; `RespondInvite` accept cũng vậy, nên gửi liền `UpdateLoadout` / `SetReady` sau đó được).

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateLobby` | `{queue?}` | Tạo lobby (host) với hàng đợi `queue` (`ranked_2v2` / `casual_2v2`; không gửi → `ranked_2v2`; khác → `INVALID_QUEUE`). Đã là host của lobby đang `preparing` → trả lại lobby đó, đổi sang `queue` (`LobbyUpdated`); lobby solo còn `in_match` mà mình đã rời trận → mở lại (`preparing`), đổi queue và trả lại; lobby không còn → tạo mới; còn lại `ALREADY_IN_LOBBY` |
| `JoinLobby` | `{lobbyId}` | Join lobby theo ID (tối đa `party_size` của chế độ: 2 với 2v2, 4 với boss) |
| `LeaveLobby` | `{}` | Rời lobby (đang `in_queue` thì rời hàng đợi trước). Host rời → giải tán |
| `UpdateLoadout` | `{characterId?, items?}` | Đổi character/items khi `preparing`; cùng luật QuickPlay (`room.ValidateLoadout`: nhân vật chơi được và sở hữu, ≤ 3 item đã biết trong giới hạn mỗi trận, đang có) → lưu `player_loadouts`. Sai → `LobbyError` `LOADOUT_INVALID` |
| `StartQueue` | `{}` | Host bấm tìm trận (lobby `preparing`, member đã `ready`, không thì `MEMBER_NOT_READY`); đọc lại rating của các thành viên rồi vào hàng đợi `queue` của lobby |
| `SetLobbyQueue` | `{queue}` | Host đổi chế độ của lobby (Đấu thường ↔ Đấu hạng) khi `preparing`: member gửi → `NOT_HOST`, lobby không `preparing` → `INVALID_STATUS`, queue lạ → `INVALID_QUEUE`, thiếu `queue` → `INVALID_PAYLOAD`. Thành công → mọi người nhận `LobbyUpdated` (`queue` mới); member giữ nguyên `ready` |
| `CancelQueue` | `{}` | Hủy tìm trận |
| `ListOnlinePlayers` | `{query?}` | Danh sách người đang online (xem Party) → `OnlinePlayers` |
| `InviteToLobby` | `{playerId}` | Mời một người online vào lobby `preparing` chưa đủ 2 người → `InviteSent` (mình) + `LobbyInvite` (người được mời) |
| `RespondInvite` | `{inviteId, accept}` | Người được mời trả lời; accept = vào lobby như `JoinLobby` |
| `KickMember` | `{playerId}` | Host mời member ra (lobby `preparing`) |
| `SetReady` | `{ready}` | Member (không phải host) sẵn sàng / huỷ sẵn sàng (lobby `preparing`) |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `LobbyUpdated` | `LobbyState` `{lobbyId, hostPlayerId, queue, members:[LobbyMember], status}` | State thay đổi; `queue` = hàng đợi `StartQueue` vào (`ranked_2v2` / `casual_2v2`, member hiện chế độ host chọn); status `preparing` / `in_queue` / `in_match`. `LobbyMember` `{playerId, displayName, characterId, items, rating, tier, division, level, ready, gun: {kind, tier}}` (xem Party) |
| `LobbyError` | `{error: {code, message, correlationId}}` | Lỗi (mọi lỗi lobby, cả lỗi trong lobby room — trước đây event `Error`) |
| `LobbyDisbanded` | `{reason}` | Host rời / idle 30 phút → lobby giải tán |
| `QueueStarted` | `{estimatedWait}` | Đã vào hàng đợi; `estimatedWait` = `botFillAfter` (giây, 20) — chậm nhất lúc đó bot được điền |
| `QueueCancelled` | `{reason}` | `cancelled` (CancelQueue) / `teammate disconnected` |
| `MatchFound` | xem dưới | Ghép xong, gửi cho mọi người thật trong trận (cùng payload) |
| `ReturnToLobby` | `{lobbyId}` | Trận xong; lobby về `preparing` (kèm `LobbyUpdated`) |
| `OnlinePlayers` | `{players:[OnlinePlayer]}` | Trả lời `ListOnlinePlayers` |
| `InviteSent` | `{playerId, expiresInSec}` | Lời mời đã gửi (cho người mời) |
| `LobbyInvite` | `{inviteId, lobbyId, mode, expiresInSec, from: OnlinePlayer}` | Có lời mời (cho người được mời); `mode` = `queue` của lobby (`ranked_2v2` / `casual_2v2`, client lấy tên từ `modes.lobby` có queue đó); `from` = người mời |
| `InviteExpired` | `{playerId}` (người mời) / `{inviteId}` (người được mời) | Hết `inviteTimeoutSec` chưa trả lời; người được mời cũng nhận `{inviteId}` khi lobby đã đủ người hoặc giải tán |
| `InviteDeclined` | `{playerId, displayName}` | Người được mời từ chối (cho người mời) |
| `LobbyKicked` | `{lobbyId}` | Bị host mời ra; `client.LobbyID` đã xoá, những người còn lại nhận `LobbyUpdated` |

**`MatchFound`** (`room.MatchFound`):

```json
{"roomId":"...","matchId":"...","mapId":"ice_bridge","mapName":"Cầu băng","mapTheme":"ice","mode":"ranked_2v2",
 "teams":[{"teamId":1,"players":[{"playerId":"p1","displayName":"Smoke","characterId":"spark",
   "isBot":false,"level":40,"rating":1050,"tier":"silver","division":2,"titleId":""}, ...]},
  {"teamId":2,"players":[...]}]}
```

`mode` = queue của trận (`ranked_2v2` hoặc `casual_2v2`). Mỗi team đủ 2 người: người thật trước (theo thứ tự entry), rồi bot. `level` = level tài khoản (`player_profiles.level`), `titleId` = danh hiệu đang đeo (`""` = không). Bot: `playerId` `bot_xxxxxxxx`, `displayName` `Bot xxxx`, `characterId` ngẫu nhiên trong các nhân vật của game data (các bot một trận khác nhau khi đủ nhân vật), `level` = trung bình level người thật ± `rules.bots levelSpread` (3), `rating` = trung bình rating người thật (tier/division theo rating đó), `titleId` `""`. Xem Bot AI (§8).

**Luồng client sau `MatchFound`** (không cần gửi gì để vào trận):

1. `MatchFound` → ngay sau đó `LobbyUpdated` (status `in_match`). Server đã đưa client vào battle room (`client.RoomID`).
2. `matchStartDelay` giây sau (config `matchmaking`, mặc định 3 s) → `MatchStarted` (snapshot như mọi trận, mode = queue: `ranked_2v2` / `casual_2v2`), rồi `TurnStarted` và các match event bình thường. Client buffer match event nếu màn trận chưa sẵn sàng.
3. Kết thúc → `MatchEnded {winningTeam, rewards}` (`ranked_2v2`: rewards có rating, xem §7 Reward System; `casual_2v2`: không), rồi `ReturnToLobby {lobbyId}` + `LobbyUpdated` (`preparing`). Lobby vẫn còn: `StartQueue` lại được, hoặc `LeaveLobby` (lobby solo → giải tán); `CreateLobby` lần sau trả lại đúng lobby đó.
4. `Leave` (hoặc mất kết nối) trong trận = rời trận: thua (Đấu hạng: thua toàn bộ rating), không coin/EXP; client về Home rồi nên gửi `LeaveLobby` (hoặc `CreateLobby` lại sau khi trận đã kết thúc / lobby solo).
5. `CancelQueue` thua race: nếu matchmaker đã lấy entry, server không gửi `QueueCancelled` mà `MatchFound` đến ngay sau đó → trận vẫn diễn ra.

### Party: mời đồng đội (Đấu hạng / Đấu thường 2vs2)

Không có hệ bạn bè: lời mời gửi tới **người đang online** trên game node (đang mở WebSocket), chọn từ danh sách có ô tìm tên.

**Presence** (`lobby/presence.go`): map `playerId → *ws.Client` trong bộ nhớ, đăng ký khi kết nối (`ws.ConnectHandler.Register`), xoá khi kết nối đó đóng (`Unregister`); một người nhiều kết nối → giữ kết nối mới nhất. Trạng thái:

- `in_match`: đang ở trong room (`client.RoomID != ""`)
- `in_lobby`: ở lobby 2 người, hoặc lobby đang `in_queue`
- `idle`: còn lại (host một mình trong lobby `preparing` vẫn là `idle`)

**Giới hạn: chỉ một game node.** Presence và lời mời theo node (như lobby); người kết nối node khác coi như offline. Chưa có presence liên node.

`OnlinePlayer` `{playerId, displayName, level, tier, division, characterId, status}`: `level` = `player_profiles.level`; `tier` / `division` theo rating mùa active (không có → `elo.defaultRating`); `characterId` = nhân vật trong lobby nếu đang ở lobby, không thì `player_loadouts` (mặc định `rookie`).

**`ListOnlinePlayers {query?}`** → `OnlinePlayers {players}`: mọi người online trừ mình; `query` khớp `displayName` (chuỗi con, không phân biệt hoa thường) hoặc đúng `playerId`. Sắp xếp: `idle` trước, rồi rating gần mình nhất; tối đa 30. Tên, level, nhân vật, rating đọc một query cho cả danh sách.

**`InviteToLobby {playerId}`** (member của lobby, thường là host). Kiểm tra theo thứ tự:

1. đang ở lobby → không thì `LOBBY_REQUIRED`
2. lobby `preparing` → `INVALID_STATUS`
3. lobby < 2 người → `LOBBY_FULL`
4. người được mời online → `PLAYER_OFFLINE`
5. người được mời `idle` → `PLAYER_BUSY`
6. lobby chưa mời người này (lời mời còn hạn) → `INVITE_PENDING`
7. không tự mời mình → `INVALID_PAYLOAD`

Thành công: người mời nhận `InviteSent {playerId, expiresInSec}`, người được mời nhận `LobbyInvite {inviteId, lobbyId, mode, expiresInSec, from: OnlinePlayer}` (`mode` = `queue` của lobby lúc mời). Sau `inviteTimeoutSec` (`game_settings matchmaking`, mặc định 30, admin trang Matchmaking; GET /gamedata `matchmaking.inviteTimeoutSec`) chưa trả lời → `InviteExpired {playerId}` cho người mời, `InviteExpired {inviteId}` cho người được mời. Lobby đủ 2 người (ai đó vào) hoặc giải tán → các lời mời còn lại bị huỷ: `InviteExpired {inviteId}` cho từng người được mời.

**`RespondInvite {inviteId, accept}`** (người được mời):

- Từ chối: người mời nhận `InviteDeclined {playerId, displayName}`; lời mời bị xoá (mời lại được).
- Đồng ý, kiểm tra: lời mời còn (`INVITE_EXPIRED`), lobby còn (`LOBBY_NOT_FOUND`), `preparing` (`INVALID_STATUS`), còn chỗ (`LOBBY_FULL`), mình không ở room (`IN_ROOM`). Đang là host một mình của lobby khác (không `in_queue`) → server cho rời lobby đó trước (giải tán, không gửi `LobbyDisbanded`); đang ở lobby có người khác / đang tìm trận → `ALREADY_IN_LOBBY`. Rồi vào lobby như `JoinLobby`: mọi người nhận `LobbyUpdated`, member mới `ready = false`.

**`KickMember {playerId}`**: chỉ host (`NOT_HOST`), lobby `preparing` (`INVALID_STATUS`), `playerId` là member khác host (`INVALID_PAYLOAD`). Member bị mời ra nhận `LobbyKicked {lobbyId}` (`client.LobbyID` xoá), những người còn lại nhận `LobbyUpdated`.

**`SetReady {ready}`**: member không phải host (host gửi → `INVALID_PAYLOAD`; host luôn `ready: true`), lobby `preparing` (`INVALID_STATUS`) → `LobbyUpdated`. `StartQueue` với member chưa sẵn sàng → `MEMBER_NOT_READY`. `ready` về `false` khi vào lobby; giữ nguyên qua `UpdateLoadout` và qua các trận (Chơi tiếp của host chạy lại `StartQueue` ngay).

**`StartQueue` khi có 2 người**: cả hai nhận `LobbyUpdated` (`in_queue`) + `QueueStarted`; `MatchFound` / `ReturnToLobby` gửi tới mọi client của lobby. Member huỷ tìm trận bằng `LeaveLobby` (server rời hàng đợi cho cả lobby, host nhận `QueueCancelled {reason: "teammate disconnected"}` + `LobbyUpdated`); host huỷ bằng `CancelQueue`.

**`LobbyMember`**:

```json
{"playerId":"p2","displayName":"Smoke","characterId":"tanko","items":["shield"],"rating":1050,"tier":"silver","division":2,
 "level":12,"ready":false,"gun":{"kind":"cannon","tier":"mithril"}}
```

- `level`: `player_profiles.level`; `ready`: xem `SetReady`.
- `gun {kind, tier}`: súng nhân vật `characterId` cầm ngoài trận, đúng cặp client tính bằng `EquipmentModel.worn_gun` (`lobby.gunOf`). Vũ khí đang mặc trên nhân vật (`player_equipment` slot `weapon`, `equipped_on = characterId`, join `config_equipment_items`):
  - `kind`: vũ khí riêng của nhân vật (`character_id`) → `gunKind` của nhân vật đó; không thì từ loại trong tên (`"Iron Shock Rifle"` → type word `shock rifle` → `shock_rifle`, `config_item_type_words`); không phải gun kind có trong `config_gun_kinds` → `blaster`.
  - `tier`: `tier` của item; không thì chữ đầu của tên (`Iron` → `iron`); không thì hậu tố id (`_lv30` → `mithril`, `config_equipment_tiers.id_suffix`); không thì tier đầu tiên cùng loại (normal / crafted). Item không còn trong catalog → đoán từ id như client (`crafted_weapon_<tier>`, `<character>_weapon_lvN`).
  - Không mặc vũ khí: `{kind: gunKind của nhân vật (không có → của nhân vật mặc định), tier: "basic"}` (`"basic"` = súng cơ bản, `Art.GUN_BASIC` của client).
  - Đọc khi vào lobby, làm mới (cùng `level`) mỗi `UpdateLoadout`.

**Mã lỗi lobby (`LobbyError {error: {code, message}}`)** của party: `LOBBY_REQUIRED`, `LOBBY_NOT_FOUND`, `INVALID_STATUS`, `LOBBY_FULL`, `PLAYER_OFFLINE`, `PLAYER_BUSY`, `INVITE_PENDING`, `INVITE_EXPIRED`, `INVALID_PAYLOAD`, `IN_ROOM`, `ALREADY_IN_LOBBY`, `NOT_HOST`, `MEMBER_NOT_READY`, `INVALID_QUEUE` (`CreateLobby` / `SetLobbyQueue` với queue matchmaker không chạy), `PARTY_TOO_LARGE` (lobby đông hơn `party_size` của queue mới, §8a). Như các mã lobby khác, chúng là chuỗi trong package `lobby` (`sendLobbyError`); riêng `INVALID_QUEUE` và `PARTY_TOO_LARGE` có trong `model/errors.go` (`ErrInvalidQueue`, `ErrPartyTooLarge`).

### Room Events

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateRoom` | `{mode, mapId, password?}` | Tạo phòng (pvp_1v1, pvp_2v2) |
| `JoinRoom` | `{roomId, password?}` | Vào phòng |
| `QuickPlay` | `{bot?: "ai", characterId?: string, items?: string[]}` | Chơi tutorial: mặc định bot idle (đứng yên, map `grassland_valley`), `bot: "ai"` cho bot AI di chuyển và bắn (map ngẫu nhiên trong mọi map; nhân vật, cấp quanh level người chơi, súng và item theo `rules.bots`, xem Bot AI). `characterId` mặc định `rookie`, `items` mặc định `[]`. Loadout được kiểm tra như `SelectCharacter`/`SelectItems` (nhân vật hợp lệ và đã sở hữu, tối đa 3 item, item có trong game data, mỗi item ≤ `maxUsePerMatch`, có trong `inventory_items`) trước khi tạo phòng; item được reserve như `StartMatch` của phòng custom và bị trừ khi dùng trong trận. Sai loadout → `RoomError` code `LOADOUT_INVALID` và không tạo phòng; nếu reserve thất bại (thiếu số lượng, item bị trận khác giữ) cũng trả `LOADOUT_INVALID`, lỗi reserve khác trả `MATCH_START_FAILED`, và phòng đã tạo bị huỷ. `DEV_UNLOCK_ALL` bỏ qua kiểm tra sở hữu và không reserve |
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
| `Move` | `{direction, targetX, seq?}` | Di chuyển trái/phải; `seq` (tuỳ chọn) được trả lại trong `PlayerMoved` |
| `Shoot` | `{angle, power, actionMode, itemId?}` | Bắn (weapon hoặc skill) |
| `UseItem` | `{itemId, targetPosition?}` | Dùng item |
| `EndTurn` | `{}` | Kết thúc lượt |
| `SelectAction` | `{actionMode, itemId?}` | Người tới lượt vừa chọn (arm) trong HUD: `weapon`, `skill` hoặc `item` + `itemId`. Không commit gì, `Shoot` vẫn mang lựa chọn thật; đổi ý thoải mái, `weapon` = bỏ chọn. Server chỉ nhận từ người tới lượt, trong lượt, khi chưa có phát bắn đang resolve; `skill` cần skill energy 100 và không bị stun / freeze; `item` cần còn item đó trong túi và là item bắn (`power_shot`, `drill_bomb`, `spider_net`, `freeze_bomb`, `air_strike`, `teleport`; teleport cần di chuyển được). Item dùng ngay (medkit, wind_stopper...) vẫn đi `UseItem`. Sai thì bỏ qua im lặng. Không bị rate limit 150 ms của các action khác; server gộp broadcast (xem `ActionSelected`). Server cũ bỏ qua event này |
| `Reconnect` | `{}` | Kết nối lại sau disconnect |
| `Leave` | `{}` | Rời trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `MatchStarted` | `MatchState` (`{matchId, roomId, mode, mapId, mapName, mapTheme, turnIndex, currentPlayerId, wind, players, status, turnOrder, turnTimeLeft, activeEffects}`) + `terrain` + `gamedataVersion` | Trận bắt đầu. `mapName` là tên map tiếng Việt (`config_maps.name`), `mapTheme` cách client vẽ map (một trong `MapThemes`, mặc định `grassland`); `MatchStateSync` cũng có hai field này. `gamedataVersion` (string, 16 hex) = `version` của `GET /gamedata` cho game data trận này chơi suốt trận (cùng một snapshot, cùng một hàm build); khác bản client đã cache → client tải `GET /gamedata?version=<gamedataVersion>` trước khi cho ngắm / hành động. `MatchStateSync` cũng có |
| `TurnStarted` | `{currentPlayerId, turnIndex, wind, moveEnergy, maxMoveEnergy, skillEnergy, turnTimeLeft, skipped?}` | Lượt mới; `moveEnergy` là năng lượng của người tới lượt (đã trừ net / root / stun / freeze), `maxMoveEnergy` là mức đầy của họ. `turnTimeLeft` (số giây) = độ dài lượt (`game_settings match.turn_time_seconds` của trận, 20); sau đó `TurnTimerTick` đếm ngược. `skipped` (`"stun"` / `"freeze"`, chỉ có khi bị) = người tới lượt mất trọn lượt: server bỏ mọi Move/Shoot/UseItem và tự kết thúc lượt sau `rules.timings skipTurnDelaySec` (2 s) |
| `TurnTimerTick` | `{timeLeft}` | Đếm ngược (mỗi 1s) |
| `PlayerMoved` | `{playerId, position, moveEnergy, seq?, outOfMap?}` | Player di chuyển; có `seq` khi trả lời `Move` có `seq`. `outOfMap: true`: bước vào hố thủng tới đáy map, bước đi dừng ở mép hố, `position.y` = chiều cao map; ngay sau là `PlayerDamaged` `out_of_map` |
| `PlayerAiming` | `{playerId, angle, durationMs}` | Player đang ngắm trước khi bắn. `angle` cùng quy ước với `Shoot.angle` và là đúng góc sẽ bắn (phát cố tình trượt thì là góc trượt); `ShotResolved` của player đó tới sau khoảng `durationMs`. Không gửi power. Hiện chỉ gửi cho bot; nếu lượt đổi (timeout) trong lúc ngắm thì không có phát bắn |
| `ActionSelected` | `{playerId, actionMode, skillId?, itemId?}` | Người tới lượt đang chọn gì (gửi mọi player): `skill` (+ `skillId`), `item` (+ `itemId`), `weapon` (bỏ chọn). Client hiện badge trên đầu player đó và giữ tới khi `weapon`, lượt đổi (`TurnStarted`) hoặc hết replay của phát bắn. Chỉ gửi khi đổi so với lần gửi trước trong lượt, tối đa 1 lần / 100 ms / player (`selectBroadcastInterval`; đổi trong cửa sổ thì gửi lựa chọn mới nhất lúc hết cửa sổ). Server reset lựa chọn ở đầu lượt và khi phát bắn resolve, không gửi gì thêm. Bot: gửi ngay trước `PlayerAiming` khi bot sắp dùng skill hoặc item bắn |
| `ShotResolved` | `{shooterId, actionMode, skillId?, itemId?, skillEnergy, skillEnergies, projectiles[], events[], duration}` | Toàn bộ kết quả một lượt bắn (weapon, skill, teleport). `skillEnergy` = năng lượng skill của người bắn sau phát bắn; `skillEnergies` = `{playerId: energy}` của mọi player sau phát bắn (người bị địch đánh trúng +10, xem Kỹ năng) |
| `ItemUsed` | `{playerId, itemId, players, wind}` | Item bị tiêu hao |
| `PlayerDamaged` | `[{playerId, damage, hp, shield, isAlive, type}]` | Damage ngoài phát bắn. Đầu lượt của người tới lượt: `type` `lava` (75) hoặc `burn` (10% máu tối đa), không qua defense, khiên chịu trước (`shield` = khiên còn lại); gửi trước `TurnStarted`, nếu chết thì không có `TurnStarted` cho họ: server kiểm tra thắng thua rồi chuyển lượt. `type` `out_of_map`: rơi khỏi đáy map là chết ngay (damage = máu + khiên còn lại), sau `PlayerMoved` `outOfMap`; server khoá lượt trong thời gian rơi tự do + 1.5 s rồi kiểm tra thắng thua và chuyển lượt |
| `MatchEnded` | `{winningTeam, rewards, stats, mvpPlayerId}` (no-contest: `{winningTeam: 0, result: "no_contest", message, stats, mvpPlayerId: ""}`) | Trận kết thúc, xem §7 Reward System |
| `MatchStateSync` | `MatchState` + `terrain` + `gamedataVersion` + `lastMoveSeq` + `selectedAction?` | Full sync trả lời `Reconnect` (`gamedataVersion` như `MatchStarted`). `selectedAction` (`ActionSelected` của người tới lượt, cả khi broadcast còn đang gộp) chỉ có khi họ đang chọn skill / item, để client vào lại vẫn hiện badge. `lastMoveSeq`: `seq` của `Move` cuối cùng của người gửi `Reconnect` mà match loop đã nhận trong lượt hiện tại, kể cả `Move` bị bỏ (rate limit, đang resolve, không hợp lệ); `0` nếu chưa có hoặc không phải lượt của họ; reset khi lượt mới bắt đầu. Snapshot đã phản ánh mọi `Move` có `seq <= lastMoveSeq` |

**`ShotResolved`:** server tính xong toàn bộ phát bắn và state đã cập nhật trước khi gửi; client chỉ phát lại. `projectiles[]` là đường bay (`path[]` với `time`) của từng viên đạn; `time` luôn tính từ lúc bắn, nên viên phóng trễ (bom liên hoàn, tên lửa) có `path[0].time = launchDelay > 0` và client chỉ hiện nó từ lúc đó. Mỗi projectile: `{projectileId, ownerPlayerId, skillId?, kind?, launchDelay?, path[], hitPlayerId?, explosionPoint?, explosionRadius, craterRadius, bounces?}` (`bounces` 054: xem Đạn nảy); `kind` (chỉ với skill): `flare` (pháo hiệu, đáp không nổ), `missile` (tên lửa rơi từ đỉnh map), `bomb` (bom liên hoàn), `seed` (hạt của Hoa hồi xuân, đáp lên đồng đội `hitPlayerId`, không nổ); thiếu = đạn thường. `events[]` sắp theo `t` (giây kể từ lúc bắn):

| `type` | Trường | Ghi chú |
|--------|--------|---------|
| `explode` | `projectileId, position, radius, crater, terrainVersion` | Khoét terrain tại `t` (cuối đường bay). `crater`: hình hố, luôn `bowl` (lòng chảo, `Terrain.CarveBowl`; mọi vụ nổ, cả bom liên hoàn từ playtest 5); `circle` (hình tròn, `Terrain.DestroyCircle`) không còn được gửi, client vẫn khoét được; thiếu = `bowl`. Không còn `floor` (trước playtest 5 bom liên hoàn giữ 48 px đáy map) |
| `damage` | `playerId, amount, hp, cause, projectileId?, critical?, shield?, elementEffect?` | `cause`: `explosion`, `laser`, `nova`, `fall`, `out_of_map` (boss mode thêm `self_destruct`, `death_blast`). `amount` = máu + khiên mất; `shield` (khiên còn lại) chỉ có khi player có khiên trước cú đánh. `elementEffect` (054): `strong` (hệ người đánh khắc hệ mục tiêu) / `weak` (PvP: hệ mục tiêu khắc hệ người đánh), thiếu = trung tính (Ngũ hành, §7) |
| `heal` | `playerId, amount, hp` | `amount` = máu thực hồi (có thể 0 khi đầy máu) |
| `shield` | `playerId, amount, shield` | Khiên nhận thêm / khiên sau đó (tối đa 50% máu tối đa) |
| `status` | `playerId, effectId, turns` | Hiệu ứng được gắn (`burn`, `root`, `stun`, `net`, `freeze`) |
| `cleanse` | `playerId, effectIds[]` | Hiệu ứng xấu bị gỡ |
| `fall` | `playerId, from, to, duration` | Rơi từ `t` đến `t + duration`; damage rơi nằm ở mốc chạm đất. Một player rơi nhiều lần trong cùng phát bắn thì lần sau bắt đầu khi lần trước chạm đất |
| `die` | `playerId, cause` | |
| `teleport` | `playerId, from, to` | |
| `flare` | `projectileId, position` | Pháo hiệu đáp (Mưa tên lửa): khói đỏ, không damage, không hố |
| `rain` | `position, radius, count` | Tên lửa bắt đầu rơi: quanh `position.x` ± `radius`, `count` quả |
| `summon` | `playerId, minionId, position, hp` | Chế độ boss: lính mới xuất hiện (§8a) |
| `lava` | `zoneId, position, radius, amount, turns` | Chế độ boss: vũng dung nham (`amount` HP đầu lượt, `turns` vòng; `MatchState.lavaZones`) |
| `beam` | `playerId, from, to, duration` | Tia laze từ `from` đến mép map `to`, hiện `duration` giây; xuyên địa hình, không khoét. `t` = thời gian nạp ở nòng (0,3 s); đầu tia quét ra 12000 px/s, mỗi địch trúng (`damage` cause `laser`) lúc đầu tia tới thân nó |
| `nova` | `playerId, position, radius, duration` | (054) Nổ bông tuyết của Tide: tâm `position` (tâm thân Tide), vòng băng lan tới `radius` px trong `duration` giây (`waveSpeed`); `t` = `castTime` (0,35 s). Mỗi địch trong tầm: `damage` cause `nova` rồi `status` `freeze` lúc vòng băng tới thân nó (`t + khoảng cách / waveSpeed`) |

Client thiếu field mới (hoặc server cũ) vẫn chạy: field vắng = mặc định (`kind` rỗng = đạn thường, `launchDelay` 0, `shield` không đổi, `floor` 0 = khoét cả hình tròn).

**Đạn nảy (054, `projectiles[].bounces`):** đạn của vũ khí có `bounces` > 0 (Quả cầu nước của Tide: 1) chạm địa hình lần đầu thì nảy thay vì nổ: `bounces: [{t, position}]` (điểm chạm, pixel trống cuối cùng trước địa hình), vận tốc tách theo pháp tuyến mặt đất (ước lượng từ các pixel đặc quanh điểm chạm) và dọc mặt đất: rời đi với −0,55 × phần pháp tuyến và 0,85 × phần dọc (`rules.physics` `bounceRestitution` / `bounceFriction`). `path[]` có điểm nảy **hai lần** cùng `time` và `position`: lần đầu với vận tốc lúc tới, lần sau với vận tốc lúc rời, nên client nội suy cung gia tốc đều giữa hai mẫu vẽ đúng cả hai đoạn (đoạn dài 0 giữa hai mẫu bị bỏ qua). Chạm lần sau (địa hình hoặc người) thì nổ như thường; trúng thẳng người trước khi nảy thì nổ ngay. Không nảy ở đáy map (sàn ảo); dịch chuyển (teleport) bắn bằng súng nảy không nảy.

`duration` là lúc animation cuối cùng kết thúc; server kết thúc lượt sau `duration + 1.2s`.

**Player trong `players`** (`BattlePlayerState`, cùng shape trong `MatchStarted`, `MatchStateSync`, `ItemUsed`): `{playerId, displayName, teamId, characterId, weaponItemId, weaponUpgradeLevel, title?, hp, maxHp, shield, defense, position{x,y}, moveEnergy, maxMoveEnergy, items[], statusEffects[], isAlive, isBot, damageBonus, critChance, moveEnergyBonus, skillPowerBonus, terrainDamageBonus, skillEnergy, skillRank, damageDealt, killCount, shotsFired, shotsHit}`. `shield` là khiên hiện có (chịu damage trước máu, tối đa 50% `maxHp`, mất khi vỡ hoặc hết trận). `skillRank` là cấp súng dùng cho skill (`WeaponSkillRank(weaponItemId)`, 0..4, xem Kỹ năng). `weaponItemId` là `item_id` của vũ khí nhân vật đang mặc lúc vào trận (VD `rookie_weapon_lv30`, `crafted_weapon_gold`, `normal_weapon_lv10`), `""` khi không mặc vũ khí (bot: súng nó được phát, `""` dưới cấp 10). Room đọc nó cùng câu query equipment khi tính chỉ số (`battlestats.LoadLoadouts`), không tốn thêm gì mỗi phát bắn. Chỉ dùng để client vẽ súng đang cầm; không ảnh hưởng gameplay. `weaponUpgradeLevel` là cấp nâng cấp (`player_equipment.upgrade_level`, 0..16) của vũ khí đó, đọc cùng câu query; `0` khi không mặc vũ khí (bot: cấp cường hoá ngẫu nhiên của súng nó). Chỉ để client vẽ hào quang sau lưng (vũ khí +14: tím, +16: đỏ); client cũ bỏ qua, client mới thiếu field coi là 0. `title` là danh hiệu người chơi đang đeo lúc vào trận, `{id, name, rarity}` (rarity `blue`/`orange`/`purple`/`red`), chỉ khi danh hiệu còn sở hữu và đang bật; không có field khi không đeo và với bot. Room đọc nó lúc tính chỉ số (`battlestats.LoadEquippedTitle`, cả khi chơi nhân vật chưa sở hữu); chỉ để client vẽ biển danh hiệu, không ảnh hưởng gameplay. `maxMoveEnergy` là năng lượng di chuyển nạp lại đầu mỗi lượt của player: năng lượng gốc của nhân vật (`config_characters.mobility`) + `moveEnergyBonus` (điểm, trang bị); client dùng nó làm mức đầy của thanh năng lượng.

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
  1. Reset move energy = maxMoveEnergy (gốc nhân vật 100/80/120/100 + moveEnergyBonus);
     net cap 50, root / stun / freeze → 0
  2. Damage đầu lượt (PlayerDamaged): lava 75, rồi burn 10% máu tối đa; khiên chịu trước.
     Chết → checkWinCondition, rồi chuyển lượt (không TurnStarted cho người chết)
  3. (Status effects giảm 1 lượt ở endTurn, cuối lượt của chính người đó)
  4. Update wind (random power + direction, trừ khi WindStopper active)
  5. Broadcast TurnStarted; stun / freeze → `skipped`, khoá lượt, tự endTurn sau 2s
     (không lên lịch bot)
  6. Nếu bot → delay 1–2.5s (botTurnStartMin/MaxDelay) → botStep → DecideAction
     - Medkit / Wind stopper / Move: xử lý ngay, botStep tiếp sau botFollowUpDelay 1.5s (tối đa 4 bước)
     - Shoot: skill / item bắn → broadcast ActionSelected trước; broadcast PlayerAiming {playerId, angle, durationMs}, bắn sau 1.2–2.4s
       (botAimMin/MaxDelay) qua runOnTurnAfter — bị huỷ nếu lượt đổi/trận kết thúc;
       lúc bắn kiểm tra lại vẫn là lượt bot, bot còn sống, chưa resolving

Player actions trong turn:
  - Move: tiêu MoveEnergy, update position, check fall damage
  - Shoot: 1 lần/turn, simulate projectile physics, calculate damage, destroy terrain
  - UseItem: medkit (+450 HP), teleport, wind_stopper, shot modifiers
  - EndTurn: chuyển sang player tiếp theo
```

### Projectile Physics

```
SimulateProjectile(startPos, angle, power, wind, weaponConfig):
  velocity = power * 9.6 tại angle            // physics.projectile_speed_multiplier
  timeStep = 0.02s                            // physics.time_step
  gravity = 445.0 px/s²                       // physics.gravity
  windForce = direction * power * 67.0 * weaponWindInfluence / projectileWeight   // physics.wind_scale

  Loop (max 4.5 giây):                        // physics.max_flight_seconds
    velocity.X += windForce * timeStep
    velocity.Y += gravity * timeStep
    position += velocity * timeStep

    Check collision:
      - Terrain: IsSolid(x, y) → explode
      - Player: distance < 24px hitRadius → hit
      - Out of bounds → discard

    Record path mỗi 0.05s cho client animation
```

Số vật lý đọc từ `game_settings` (`physics.*`, `gamedata.PhysicsFromSettings`; thiếu dòng nào thì dùng `gamedata.DefaultPhysics()`, cùng giá trị với admin seed), các số còn lại của đường bay từ `rules.physics` (bước kiểm tra va chạm 4 px, điểm bắn 20 px trên chân, ra ngoài 100 / 100 / 50 px) và lực tối đa từ `rules.aim` (xem Game data). Trước đây loader tìm key không có tiền tố `physics.` nên không bao giờ thấy dòng nào và luôn dùng mặc định; `SimulateProjectile` cũng cố định `power × 6` thay vì đọc `projectile_speed_multiplier`.

Migration `032_faster_shells` (playtest: đạn bay chậm, tầm ngắn; chạy lại mỗi deploy, chỉ đổi dòng còn giá trị cũ): tốc độ power × 6 → **9,6** (×1,6), gravity 200 → **445** (×2,23), wind scale 30 → **67** (×2,23, nên gió vẫn lệch cùng tỉ lệ phát bắn), bay tối đa 6 → **4,5 s**. Cùng power bay xa hơn ~15% (tầm ∝ v²/g), phát bắn tới cùng chỗ mất ~0,67 × thời gian (cùng power: ~0,72 ×); hình dạng đường bay tới cùng điểm không đổi. Bước 0,02 s giữ nguyên: một bước bay tới 19 px (power 100) nhưng đoạn đường vẫn được lấy mẫu mỗi ≤ 4 px nên va chạm (tường mỏng, người chơi) như cũ; điểm nổ vẫn nằm trong đất ≤ 4 px.

| | Trước 032 | Sau 032 |
|---|---|---|
| Tầm xa nhất (45°, power 100, đất bằng) | ~1 820 px | ~2 090 px |
| Power cần để bay hết map 1600 px (45°) | ~94 | ~87 |
| Phát 600 px ở 45° | power 57, 2,5 s | power 53, 1,66 s |
| Phát cầu vồng 600 px ở 80° | power 99, 5,9 s | power 92, 3,9 s |
| Bắn thẳng lên power 100 | đỉnh 900 px, 6,0 s (hết giờ bay khi vừa về) | đỉnh 1 030 px, 4,3 s |

Kéo ngắm vẫn 2 px / power (client), power tối đa 100: ở 45° power ~87 đã bay hết map (trước ~94; trước đó cũng đã vượt map), phần power còn lại dùng cho phát cầu vồng dốc (80° cần ~92 cho 600 px). Rơi (`fall`, `fallDuration` = √(2 × quãng / gravity)) cũng theo gravity nên nhanh hơn: ~0,67 × thời gian, quãng rơi và damage rơi không đổi.

**Shot origin (client contract):** mọi phát bắn (weapon, skill, teleport, và phát ngắm thử để chọn cột air strike) bắt đầu từ tâm thân nhân vật, không phải chân:

```
shot origin = position − (0, 20)        // ShotOriginLift = PlayerHeight / 2 = 20
```

`position` là chân (pixel solid đầu tiên dưới chân). Fallback khi có trần thấp: quét lên từ `position.y − 1` tới `position.y − 20` theo cột `position.x`, dừng trước pixel `IsSolid` đầu tiên; origin là pixel free cao nhất đã quét (nếu `position.y − 1` đã solid thì origin = `position`). Ngoài map theo X hoặc trên đỉnh map (y < 0) tính là free. Ngoại lệ: air strike bắn từ `(targetX, 0)`. Va chạm player đo tới cùng điểm tâm thân `position − (0, 20)` (hitRadius 24). Bot dùng cùng origin (`shotOriginAt`). Aim preview của Godot client phải dùng đúng origin này; parity fixture `tests/fixtures/terrain_parity.json` (sinh bởi `TestExportClientFixture`) có `feetX/feetY` (position) và `x/y` (origin) cho mỗi shot, gồm vài case origin bị hạ do trần thấp hoặc chân nằm trong terrain.

### Damage Calculation

```
Explosion damage:
  distance = max(0, dist(explosionCenter, bodyCentre) − hitRadius × bodySize)   // tới MÉP THÂN (blastDistance)
    // bodyCentre = position − (0, shotOriginLift × bodySize), hitRadius = physics.player_hit_radius (24);
    // đúng vòng tròn đạn va chạm (checkSegmentCollisions); bodySize 1 cho người chơi, boss / lính theo config.
    // Mọi đơn vị (người chơi, lính, boss) và mọi vụ nổ (đạn, skill, mưa của boss, lính tự nổ / nổ khi chết)
    // đo cùng cách: hai thân sát nhau cùng ăn nổ, cùng khoảng cách thì cùng damage. Người bị trúng trực tiếp
    // (hitPlayerId) distance = 0. Trước bản sửa này đạn đo tới chân: đạn nổ trên đầu một người tuyết cách chân
    // người tuyết sát bên ~55 px > bán kính 40, nên chỉ con bị trúng mất máu.
  if distance >= explosionRadius → 0 damage
  multiplier = 1.0 - (distance / explosionRadius)  // linear falloff
  rawDamage = (weaponDamage + shooter.damageBonus) * itemFactor * multiplier
    // itemFactor: power_shot ×1.5; skill: base = × skill.damageMultiplier × (1 + skillPowerBonus/100), xem Kỹ năng
  finalDamage = rawDamage * (K / (K + playerDefense))   // K = game_settings combat.defense_k, mặc định 600
  crit: ×1.5 với xác suất critChance%

Crater (khoét terrain, không ảnh hưởng damage):
  craterRadius = weapon.terrainDamage × 0.6 px      // terrainDamage ≤ 0 → explosionRadius (039; 0.8 trước đó)
               × skill (xem Kỹ năng) × fragile ×2 (đạn thường) × (1 + min(terrainDamageBonus, 50)/100)
  // MaxTerrainDamageBonus = 50: điểm terrain_damage không giới hạn (tới ~790 ở level 80), không cap thì hố Tanko
  // 18 px thành ~160 px; ở cap Tanko khoét 27 px (sâu ~19 px). Sát thương nổ không phụ thuộc hố:
  // đạn nổ trên đất cứng không khoét gì nhưng người đứng gần vẫn chịu đủ sát thương (TestExplosionOnHardGroundStillSplashes)
  ProjectileResult.craterRadius = trước terrainDamageBonus; explode.radius = cuối cùng
  Hình hố (Terrain.CarveBowl, client TerrainMask.carve_bowl khớp từng bit), r = explode.radius,
  (dx, dy) = pixel − tâm đã làm tròn, y hướng xuống; khoét pixel destructible khi:
    dx² + (r/k)·dy ≤ r²      // đáy parabol: sâu k·r dưới tâm (k = bowlDepth 0,72; playtest 4: 0,5), rộng r mỗi bên ở độ cao tâm
    dy ≥ −1,5·r              // nắp phẳng (bowlCeiling): đáy chạm nắp ở |dx| = r·√(1 + 1,5/k) ≈ 1,76r
  Bom liên hoàn khoét cùng hình lòng chảo, bán kính = hố vũ khí (không ×1,4, không giữ đáy map).

Fall damage (không qua defense):
  threshold = 150px
  if fallDistance > threshold:
    damage = (fallDistance - 150) * 3.75
```

`ReduceByDefense` (`match/damage.go`) là công thức defense duy nhất: `CalculateExplosionDamage` (trận) và bot (`evalShot`) đều gọi nó. K là `Physics.DefenseK` của game data trận đang chơi (thiếu hoặc ≤ 0 → 600), sửa ở trang Game Rules của admin: trận mới dùng giá trị mới, không cần restart. Defense 50 nhận 600/650 ≈ 92% sát thương, defense 600 nhận 50%.

### Balance ("số lớn")

Migration `021_big_numbers_balance` (chạy lại mỗi deploy, chỉ sửa dòng còn giữ giá trị cũ) và `configs/*.yaml` đổi sang thang số lớn: HP ×15, damage vũ khí ×8, defense giữ nguyên, K 100 → 600. Hiệu ứng % (power_shot ×1.5, `damageMultiplier` skill, skill power/terrain %) giữ nguyên.

| Nhân vật | HP | Damage vũ khí (`config_weapons`, cũng là `config_characters.damage`) | Defense |
|----------|----|------------------------------------------------------------------|---------|
| Rookie (basic_rocket) | 100 → **1.500** | 60 → **480** | 50 |
| Tanko (cannon_ball) | 130 → **1.950** | 75 → **600** | 80 |
| Spark (electric_orb) | 90 → **1.350** | 55 → **440** | 40 |
| Flora (seed_bomb) | 95 → **1.425** | 45 → **360** | 50 |

Các số khác đã đổi:

| Giá trị | Cũ → mới | Ở đâu |
|---------|----------|-------|
| `statMultipliers` hp / damage / defense / mobility / skill_power / terrain_damage | 50/5/5/3/5/3 → **2/5/1/1/1/1** | `character_progression` (021 chỉ đổi khi object còn đúng mặc định cũ), seed 006, `character.DefaultStatMultipliers`. skill_power / terrain_damage là % mỗi điểm (1 = 1%) |
| K defense | 100 → **600** | `combat.defense_k` (021 + admin `SeedSettings`), `gamedata.DefaultDefenseK` |
| Medkit | +30 → **+450** HP | `config_items.value` của `medkit` (game server giờ đọc value, mặc định 450), `items.yaml` |
| Healing Bloom | 30 → **450** HP trước skill power (023: **650**; 028 thay bằng Hoa hồi xuân theo % máu) | `match.skillHealAmount` (đã bỏ) |
| Lava đầu lượt | 5 → **75** HP | `match.lavaDamage` |
| Fall damage | 0,25 → **3,75** HP/px quá 150px | `match.fallDamageMultiplier` |
| HP mặc định nhân vật không có trong game data | 100 → **1.500** | `battlestats.defaultHP` |
| EXP thưởng theo damage gây ra | 1 EXP / 20 damage → **1 EXP / 300 damage** | `match.damagePerBonusExp` (một trận gây ≈15× damage hơn trước) |

Không đổi: `air_strike` value 30 (không code nào đọc), `drill_bomb` 50 (px xuyên), `spider_net` cap 50 move energy, `power_shot` ×1.5, `damageMultiplier` skill (1.25 / 1.3 / 0.9; 023 đổi, xem bên dưới), `fall.damage_per_pixel` / `fall.damage_threshold` trong `game_settings` (lúc đó chưa được đọc; từ 041 match đọc chúng, migration sửa giá trị seed cũ 30 / 0,5 thành 150 / 3,75 đúng như hằng số đã chơi).

Kiểm tra bằng `TestBalanceHitsToKill` (`internal/game/room/balance_integration_test.go`, cần `TEST_POSTGRES_DSN`): chạy mọi migration, catalog thật, `configs/*.yaml`, `getActualStats` của room và `CalculateExplosionDamage`. Rookie bắn Rookie cùng build, trúng trực tiếp, không gió, không crit:

| Build | Max HP | Damage thô | Defense | Sát thương / phát | Số phát hạ |
|-------|--------|-----------|---------|-------------------|-----------|
| Không đồ | 1.500 | 480 | 50 | 443 | 4 |
| Iron +0 (6 món) | 1.660 | 530 | 58 | 483 | 4 |
| Iron +16 | 1.982 | 630 | 74 | 561 | 4 |
| Dragon +0 | 2.370 | 730 | 94 | 631 | 4 |
| Dragon +16 | 4.126 | 1.235 | 182 | 948 | 5 |
| Diamond +16 đủ set | 8.068 | 2.370 | 347 | 1.502 | 6 |
| Lv80, 79 điểm vào HP | 1.658 | 480 | 50 | 443 | 4 |
| Lv80, 79 điểm vào damage | 1.500 | 875 | 50 | 808 | 2 |

Cộng điểm nghiêng về damage: build damage Lv80 hạ Rookie không đồ trong 2 phát và build HP Lv80 trong 3 phát (1.658 / 808); build HP chịu 4 phát của Rookie không đồ (đã chấp nhận).

### Nhân vật (bản sắc, migration 023)

Mỗi nhân vật có một thế mạnh riêng. `migrations/023_character_identities` (chạy lại mỗi deploy; mỗi cột chỉ đổi khi còn giữ giá trị cũ, nên sửa trên admin được giữ) và `configs/*.yaml`:

| | Rookie — Cân bằng | Tanko — Phá địa hình | Spark — Sát thương | Flora — Bền bỉ |
|---|---|---|---|---|
| Role id | `balanced` | `heavy_gunner` → `demolisher` | `controller` → `striker` | `support` → `sustain` |
| HP | 1.500 | 1.950 → **1.650** | 1.350 → **1.250** | 1.425 → **1.900** |
| Damage vũ khí (cũng là `config_characters.damage`) | 480 | 600 → **420** | 440 → **620** | 360 → **380** |
| Defense | 50 | 80 → **60** | 40 → **30** | 50 → **90** |
| Bán kính nổ (damage) | 40 | 50 → **60** | 35 → **30** | 40 |
| Terrain damage vũ khí (cũng là `config_characters.terrain_damage`) | 50 → 25 (029) → **20** (039) | 85 → 130 → 65 (029) → **30** (039) | 30 → 25 → **20** (039) | 30 → **24** (039) |
| Bán kính hố (= terrain damage × 0,8; 039: × 0,6) | 40 → 20 (029) → **12** (039) | 40 → 104 → 52 (029) → **18** (039) | 35 → 20 → **12** | 40 → 24 → **14,4** |
| Gió / nặng | 1,0 / 1,0 | 0,5 / 1,5 | 1,2 → **1,3** / 0,8 | 1,0 / 0,9 |
| Năng lượng di chuyển gốc (`config_characters.mobility`) | 100 | **80** | **120** | 100 |
| Skill (028 thay cả bốn, xem Kỹ năng) | Triple Shot ×1,25 ×3 | Heavy Bomb (bom đào): ×1,3 → **×1,1**, nổ ×1,5 → **×2**, hố **×1,8** | Shock Field ×0,9 → **×1,3** + lưới | Healing Bloom 450 → **650** HP |

- **Năng lượng di chuyển**: trước đây mọi nhân vật 100 (`mobility` 70/40/75/65 chỉ để hiển thị). Giờ `config_characters.mobility` là năng lượng gốc mỗi lượt (`gamedata.BaseMoveEnergy`, ≤ 0 hoặc nhân vật lạ → 100). `battlestats.Base` mang nó trong `Stats.MoveEnergy`; điểm và trang bị cộng vào `MoveEnergyBonus`; `Stats.TurnMoveEnergy()` = tổng. Room đặt `BattlePlayerState.moveEnergy = maxMoveEnergy` khi vào trận, `startTurn` nạp lại `maxMoveEnergy` (state cũ thiếu field → tính lại từ config + bonus). `GET /player/characters` `baseStats.*.moveEnergy` và `stats.moveEnergy` báo đúng số này.
- **Hố**: `terrain_damage` của vũ khí trước đây không được đọc (hố = bán kính nổ). Giờ hố = `terrain_damage × 0,8` px (`match.craterPxPerTerrainDamage`; 50 → 40 px nên hố của Rookie không đổi), tách khỏi bán kính damage. Hố phẳng (bắn xuống đất bằng, `TestCraterDepthByCharacter`): Rookie sâu 17 px, Tanko 40, Spark 17, Flora 20 (0,72 × bán kính hố + ~3 px: đạn đã vào đất một bước lấy mẫu khi nổ; playtest 4 với 0,5: 13 / 29 / 13 / 15; hố tròn trước đó: 23 / 55 / 23 / 27, trước 032: 21 / 53 / 21 / 25). `migrations/029_weapon_craters_halved` (sau playtest: Tanko và Rookie phá đất quá nhiều) giảm nửa terrain damage của `basic_rocket` 50 → 25 và `cannon_ball` 130 → 65 (cả bản hiển thị ở `config_characters`), chỉ dòng còn giữ giá trị cũ: hố phát thường và mọi hố skill (bội số của hố vũ khí) đều còn một nửa; damage và bán kính nổ giữ nguyên. Trước 029: Rookie sâu 41 px, Tanko 105.
- **Hố lòng chảo** (playtest 4: hố tròn có vách gần thẳng đứng ở mép, ai rơi vào không đi ra được): mọi vụ nổ trừ bom liên hoàn khoét `Terrain.CarveBowl` (công thức ở Damage Calculation) thay cho hình tròn. Cùng bán kính r: rộng 2r như cũ, sâu r/2 dưới điểm nổ (tròn: r). Vách dốc nhất trên đất bằng 45° (1:1) ở mép; trên sườn 30° phía dốc lên 60° (1,73:1), gặp mặt đất cách điểm nổ ~1,73r; mọi bậc pixel ≤ 2 px, dưới 3 px người chơi leo được mỗi cột (`TestBowlWallsClimbable`: hố 12 / 20 / 24 / 40 / 52 / 104 px trên đất bằng và sườn ±30°; hố tròn cũ có bậc 8..18 px ở mép trên đất bằng, 17..110 px phía dốc lên). Rookie và Tanko tự bắn xuống chân trên đất bằng và sườn ±30°: rơi 13 / 29 px (tròn: 23 / 55; không mất máu rơi, ngưỡng 150 px), rồi đi ra hai phía bằng năng lượng một lượt (`TestWalkOutOfCrater`; hố tròn: kẹt). Nắp phẳng 1,5r đủ cao để trên sườn tới ~35° mép phía dốc lên không để lại mái nhô; sườn dốc hơn có thể còn bậc hoặc mái ở phía dốc lên, phía dốc xuống vẫn ra được. Nổ trong khối đất (vách, trần) khoét rộng hơn hình tròn: tới 4r ngang, 2r đứng, diện tích ~5,3r² (tròn 3,1r²). Trúng thẳng người (điểm nổ trên thân, ~20..40 px trên mặt đất) khoét ít hơn hẳn: Rookie gần như không chạm đất, Tanko ~5 px (tròn ~30; playtest 5 với k 0,72: ~15 px khi nổ 22 px trên đất). Mảnh đất fragile (×2) giữ ×2 trên hình mới. Bom liên hoàn giữ hình tròn (`crater: circle`): giếng/rãnh của nó vẫn có vách đứng, người rơi xuống giếng không tự đi ra được (playtest 5: bỏ, xem dưới).
- **Hố sâu hơn, bom liên hoàn cùng hố** (playtest 5: hố lòng chảo quá nông; giếng của bom liên hoàn quá to): đáy lòng chảo sâu `bowlDepth` 0,72 × r dưới điểm nổ (0,5 trước đó), vẫn rộng 2r ở độ cao điểm nổ. Một k không cho đúng cả Rookie 18 và Tanko 40 (độ sâu đo = ⌊k·r⌋ + 3: Rookie 18 cần k ≥ 0,75, Tanko 40 cần k < 0,731); 0,72 cho Rookie 17, Tanko 40 (0,75: 18 / 42). Vách: trên đất bằng dốc nhất atan(2k) = 55° ở mép, bậc ≤ 2 px; trên sườn 30° phía dốc lên ~65° (2,1:1), gặp mặt đất cách điểm nổ ~1,48r, có bậc 3 px (vách 2,1:1 không giữ được ≤ 2 px mỗi cột), vừa bằng 3 px người chơi leo được. Rookie / Tanko tự bắn xuống chân rơi 17 / 40 px và đi ra hai phía bằng năng lượng một lượt trên đất bằng, sườn ±30° và ±35° (`TestWalkOutOfCrater`); quét sườn: ra được hai phía tới 38° (Rookie) / 39° (Tanko), với 0,5 chỉ tới 34° / 35° (nắp chạm mặt đất sớm hơn, để lại mái). Bom liên hoàn: mỗi quả khoét đúng lòng chảo của vũ khí (bán kính 52 px với `cannon_ball`, `crater: bowl`; trước: tròn 1,4 × = 73 px) và không còn giữ đáy map (`chainFloorMargin` 48 px, `floorY`, `DestroyCircleAbove` bỏ): chuỗi đào bao sâu cũng được. Đào thủng hàng cuối của map thì cột đó không còn đất: đạn sau nổ ở sàn dưới map (`IsSolid` y ≥ Height), người đứng trên cột rơi khỏi map và chết (`out_of_map`, luật hiện tại; `TestBombChainDigsToTheBottom`). Trên grassland_valley (đất legacy, toàn bộ phá được) cấp 4 không bonus: bắn thẳng đứng xuống chân Tanko ở x 450 đào giếng 282 px (~40 px mỗi quả), còn 9 px đất dưới đáy; ở x 200 (điểm spawn) và x 800 thì thủng và Tanko tự rơi khỏi map; ở x 450 từ +10% terrain damage là thủng. Phát cầu vồng 600 px trúng thân địch mọi quả (6 quả có damage là chạm trần 698, quả 7 hết trần): nổ ở tâm thân (~40 px trên chân), lòng chảo 37 px không chạm tới đất nên không đào gì; phát thẳng: quả đầu nổ cạnh chân, mục tiêu rơi 40 px, 5 quả sau trúng thân: cũng chạm trần (698 damage; trước với hố tròn chỉ quả đầu trúng, 133).
- **Bom đào của Tanko** (thiết kế cũ, 028 thay bằng Bom liên hoàn; `single_large_bomb`, hằng số `match.heavyBombBlastScale` 2 và `heavyBombCraterScale` 1,8): nổ 120 px, hố 187 px (rộng ~374 px). Trúng thẳng một Rookie trên đất bằng (`TestDiggingBombDropsTargetIntoFallDamage`): 426 damage nổ, mục tiêu rơi 166 px (vừa quá ngưỡng 150 px) → 60 damage rơi. Phát thường của Tanko (hố 104 px) chỉ làm mục tiêu tụt 82 px, không mất máu rơi. Người bắn / đồng đội đứng trong hố cũng rơi theo. Trên frozen_peak (đất mỏng nhất ~233 px ở đáy thung lũng) một quả nổ ngay mặt đất ở bất kỳ đâu vẫn chừa ≥ 46 px đất dưới mọi cột của hố (`TestDiggingBombKeepsGroundOnThinMaps`; grassland_valley ~234 px → ≥ 47 px, steel_base 300 px → ≥ 113 px), nên một phát không thủng xuống đáy map; nhiều phát chồng lên cùng chỗ thì vẫn có thể.
- **Hố nhỏ theo nhân vật** (`migrations/039_small_craters`, playtest: phát thường khoét hố ~200 px cạnh nhân vật cao 60 px): `match.craterPxPerTerrainDamage` 0,8 → 0,6 và terrain damage `basic_rocket` 25 → 20, `electric_orb` 25 → 20, `seed_bomb` 30 → 24, `cannon_ball` 65 → 30 (cả `config_characters.terrain_damage`; chỉ row còn giá trị cũ). Hố: Rookie = Spark 12 px (bằng hố một quả của Mưa tên lửa trước đó), Flora 14,4 px (1,2 ×), Tanko 18 px (1,5 ×); sâu trên đất bằng 11 / 15 / 11 / 13 px (`TestCraterDepthByCharacter`). Bán kính nổ (damage) không đổi. Mưa tên lửa giữ 12 px mỗi quả (`rainCrater` 0,6 → 1,0); mỗi quả bom liên hoàn khoét hố như phát thường (52 → 18 px, giếng ~14 px mỗi quả); air strike, đất fragile ×2 theo vũ khí; khoan (`drill_bomb`) vẫn xuyên 50 px. `terrainDamageBonus` trong trận cap +50 % (`MaxTerrainDamageBonus`): hố lớn nhất của phát thường / skill là Tanko 27 px.
- **Spark** `shock_field` `damageMultiplier` 0,9 → 1,3 (config); lưới giữ nguyên. **Flora** `match.skillHealAmount` 450 → 650.

Số phát hạ (trúng thẳng, không gió, không crit; `TestIdentityHitsToKillNoGear` trong `match`, `TestCharacterHitsToKillMatrix` trong `room` với DB): hàng = người bắn, cột = người chịu.

| Không đồ | Rookie | Tanko | Spark | Flora |
|---|---|---|---|---|
| Rookie | 4 | 4 | 3 | 5 |
| Tanko | 4 | 5 | 4 | 6 |
| Spark | 3 | 3 | 3 | 4 |
| Flora | 5 | 5 | 4 | 6 |

| Cả hai mặc Dragon +0 (đồ lv40 + vũ khí lv40) | Rookie | Tanko | Spark | Flora |
|---|---|---|---|---|
| Rookie | 4 | 5 | 4 | 5 |
| Tanko | 5 | 5 | 4 | 6 |
| Spark | 4 | 4 | 3 | 4 |
| Flora | 5 | 5 | 4 | 6 |

Spark hạ nhanh nhất, Flora trụ lâu nhất, Rookie ở giữa; không đồ thì mọi cặp 3–6 phát. Tanko bắn chậm hạ hơn nhưng bù bằng hố và bom đào. Bot (`evalShot`) dùng bán kính nổ và damage của vũ khí nên bắn được với mọi nhân vật (`TestBotAimsWithEveryCharacter`).

### Character Stat Bonuses

Khi trận bắt đầu, room đọc `bonus_*` của `player_characters` (nhân vật đang chọn) và nhân với `statMultipliers` trong `game_settings.character_progression` (key thiếu hoặc 0 → mặc định của seed 006). Nhân vật chưa sở hữu (DEV_UNLOCK_ALL) giữ chỉ số gốc. Bot: điểm của cấp nó (`(level − 1) × pointsPerLevel`) chia đều 6 chỉ số (dư cho HP, damage, defense, ... trước) và súng nó được phát, cùng `battlestats.Compute` / `SumEquipment` (xem Bot AI). Equipment cộng thêm sau đó — chỉ số equipment cũng là **điểm**, đổi ra chỉ số bằng cùng `statMultipliers` (VD Iron Cap `stat_hp` 20 = 20 điểm HP = +40 max HP); crit là % không qua multiplier.

| Chỉ số (`bonus_*`) | Key multiplier | Mặc định | Tác dụng trong trận (`BattlePlayerState`) |
|--------------------|----------------|----------|-------------------------------------------|
| `bonus_hp` | `hp` | 2 | `maxHp = character.hp + điểm × 2` |
| `bonus_defense` | `defense` | 1 | `defense = character.defense + điểm × 1` |
| `bonus_damage` | `damage` | 5 | `damageBonus += điểm × 5`, cộng phẳng vào damage vũ khí (cả weapon lẫn skill) |
| `bonus_mobility` | `mobility` | 1 | `moveEnergyBonus += điểm × 1`; đầu mỗi lượt `moveEnergy = maxMoveEnergy = gốc nhân vật + moveEnergyBonus` (1 năng lượng / 2px) |
| `bonus_skill_power` | `skill_power` | 1 | `skillPowerBonus = điểm × 1` (%): damage skill × `(1 + skillPowerBonus/100)` (Mưa tên lửa, Bom liên hoàn, Tia laze); Hoa hồi xuân hồi theo % máu tối đa, không nhân |
| `bonus_terrain_damage` | `terrain_damage` | 1 | `terrainDamageBonus = điểm × 1` (%): bán kính khoét terrain = `craterRadius × (1 + min(terrainDamageBonus, 50)/100)` (trong trận cap +50 %; profile vẫn hiện tổng điểm) (`craterRadius` = terrain damage vũ khí × 0,6 px, xem Nhân vật); vùng sát thương vẫn dùng `explosionRadius`. `radius` của event `explode` là bán kính khoét |

#### Chỉ số hiển thị (`GET /player/characters`)

Room (`getActualStats` → `newBattlePlayer`) và API cùng tính bằng package `internal/game/battlestats` (`Base`, `FromPoints`, `SumEquipment`, `FromEquipment`, `Compute`, `LoadEquipmentPoints`), nên số trong profile là số dùng trong trận. API load game data (`config_characters`/`config_weapons`, fallback `configs/*.yaml`) lúc khởi động như game server — sửa config nhân vật/vũ khí cần restart cả hai.

```json
{
  "characters": [{
    "characterId": "tanko", "level": 3, "statPoints": 2, "bonusHp": 2, "bonusMobility": 1, "...": "...",
    "stats": {
      "maxHp": 1698, "defense": 62, "damageBonus": 55, "moveEnergy": 81,
      "skillPowerBonus": 0, "terrainDamageBonus": 0, "critChance": 2.2, "weaponDamage": 420,
      "base":          {"maxHp": 1650, "defense": 60, "damageBonus": 0,  "moveEnergy": 80,  "skillPowerBonus": 0, "terrainDamageBonus": 0},
      "fromPoints":    {"maxHp": 4,    "defense": 0,  "damageBonus": 0,  "moveEnergy": 1,   "skillPowerBonus": 0, "terrainDamageBonus": 0},
      "fromEquipment": {"maxHp": 44,   "defense": 2,  "damageBonus": 55, "moveEnergy": 0,   "skillPowerBonus": 0, "terrainDamageBonus": 0, "critChance": 2.2},
      "equipmentPoints": {"hp": 22, "damage": 11, "defense": 2, "mobility": 0, "crit": 2.2}
    }
  }],
  "resetStatsCost": {"currency": "coin", "amount": 500},
  "baseStats": {
    "rookie": {"maxHp": 1500, "defense": 50, "damageBonus": 0, "moveEnergy": 100, "skillPowerBonus": 0, "terrainDamageBonus": 0, "weaponDamage": 480},
    "tanko":  {"maxHp": 1650, "defense": 60, "damageBonus": 0, "moveEnergy": 80,  "skillPowerBonus": 0, "terrainDamageBonus": 0, "weaponDamage": 420}
  },
  "statMultipliers": {"hp": 2, "damage": 5, "mobility": 1, "defense": 1, "skillPower": 1, "terrainDamage": 1}
}
```

- `stats` (tổng) = `base + fromPoints + fromEquipment` từng key. `fromEquipment` là chỉ số đã đổi; `equipmentPoints` là số điểm equipment trước khi đổi (để client hiện "Máu +22 điểm (+44)"). Ở tổng và `base`, `moveEnergy` là năng lượng mỗi lượt (gốc nhân vật `config_characters.mobility` + bonus); ở `fromPoints`/`fromEquipment` là phần cộng thêm. `skillPowerBonus`, `terrainDamageBonus` là %; `damageBonus` cộng phẳng vào `weaponDamage` (damage vũ khí của nhân vật).
- `critChance` (%) chỉ có khi khác 0 (chỉ equipment/gem/set cho crit); số thực, client tự làm tròn.
- `baseStats`: mọi nhân vật trong game data (kể cả chưa sở hữu), không điểm, không equipment — cũng là chỉ số của bot cấp 1 (bia Luyện tập luôn vậy).
- `statMultipliers`: giá trị một điểm, đã điền mặc định cho key thiếu/0 — client dùng để xem trước khi cộng điểm.
- Equipment là **điểm**, cùng đơn vị với điểm cộng tay. `SumEquipment` tính `equipmentPoints`: mỗi món `round(stat × (1 + 0.02 × upgrade + milestone))` điểm (milestone cộng dồn ở +6/+10/+14/+16, `game_settings` `equipment.milestone_*_bonus`, mặc định 0.10/0.20/0.40/1.00; crit không làm tròn), gem `hp`/`damage`/`defense` cộng `stat_value` điểm (cắt phần thập phân), `critical` cộng % giữ nguyên, rồi mỗi tier crafted đủ `pieces_required` cộng dồn % của `config_set_bonuses` và nhân một lần (làm tròn) vào tổng điểm HP/damage/defense (crit cộng %). `FromEquipment` đổi điểm sang chỉ số bằng cùng `statMultipliers`: `hp × hp` → maxHp, `damage × damage` → damageBonus, `defense × defense` → defense, `stat_move_energy × mobility` → moveEnergy; crit không có multiplier, là % crit. Room và API dùng chung hàm này nên làm tròn giống hệt. Ví dụ (multiplier mặc định): Iron Cap +0 (hp 20, def 2) = +40 maxHp, +2 defense; ở +5 = 22 điểm HP = +44.

### Terrain System

Map = lưới tile (`config_maps.tiles`: `gridHeight` hàng × `gridWidth` cột, mỗi ô một `brick_type_id` của `config_brick_types`, 0 = trống, ô `cellSize` px) → `NewTerrainWithBricks` tô từng ô bằng polygon `border` của brick (toạ độ 0..16, gốc dưới trái, y lên) vào mask solid + mask destructible (brick `destructible = false` là đất cứng, nổ không phá được). Map không có tile (`tiles = []`: `grassland_valley`, `frozen_peak`) dùng đất legacy (sine) và `terrainLayers`. Người rơi xuống dưới đáy map chết (`out_of_map`), dù client vẽ nước hay vực.

Terrain zones đặc biệt (chỉ map legacy):
- `lava`: 75 damage/turn khi đứng (không qua defense)
- `ice`: chỉ là hình (không trượt): di chuyển ở mọi nơi đều 1 thể lực = `pxPerEnergy` px
- `fragile`: Sập sau khi bị bắn

Methods: `IsSolid(x,y)`, `IsDestructible(x,y)`, `CarveBowl(cx,cy,radius)` (mọi vụ nổ), `Carve(shape,cx,cy,radius)` (`bowl` / `circle` = `DestroyCircle`, chỉ test và fixture client), `GetLandingY(x,maxY)`, `SpawnY(x,y)`, `CanStandAt(x,y)`, `GetTerrainTypeAt(x,y)`

### Maps (migration 036 / 037 / 038)

**Nguồn**: DB (`config_maps`, `config_brick_types`) là nguồn chính; sửa được trong admin map editor (`/maps/edit`, có ô Theme). Client sync qua `GET /maps` (và `GET /maps/{id}` cho preview), terrain của trận vẫn đến trong `MatchStarted`. `configs/maps.yaml` + `configs/bricks.yaml` là bản mirror cho test và YAML fallback (`LoadGameData` đọc cả `bricks.yaml` vào `gamedata.BrickTypes`).

**Generator**: 16 map được vẽ tay bằng primitive trong `internal/game/mapgen` (`Rect`, `Chamfer` = khối bo góc 45°, `Island`, `Band` = dầm 45° dày 1 ô, `Hill` / `Peak`, `Icicle`, `Paint` đổi vật liệu, `Mirror` lật nửa trái sang phải để hai đội cùng địa hình) theo vị trí bục / cầu / đảo / khe của bản đồ tham khảo (chỉ layout, không dùng ảnh của họ). `go run ./cmd/mapgen` ghi `migrations/036_*`, `038_*`, `configs/maps.yaml`, `configs/bricks.yaml` (`-preview DIR`: PNG mask cứng / mềm + spawn; `-check`: báo file lỗi thời; test `TestGeneratedFilesUpToDate` cũng kiểm tra). `037_battle_maps` (INSERT 16 map ở 100 × 56, `ON CONFLICT DO NOTHING`) được đóng băng đúng như đã chạy, generator không ghi nó nữa; `038_wide_maps` UPDATE từng map sang layout hiện tại (width / height / grid / tiles / spawn / description, `updated_at` mới nên `GET /maps` trả ETag mới) chỉ `WHERE map_id = … AND grid_width = 100 AND theme = <theme của generator>`: chạy lại không ghi đè, map admin đã sửa (đổi kích thước / theme) được giữ. Down của 038 không khôi phục gì (layout 100 × 56 chỉ còn trong 037). Sửa layout lần sau → sửa `mapgen`, đóng băng 038 tương tự và cho generator ghi migration mới (chỉ cập nhật row còn đúng bản trước).

**Kích thước**: 16 map mới 150 × 70 ô × 16 px = 2400 × 1120 (~2,14:1, gần map tham khảo ~2,1:1); `grassland_valley` / `frozen_peak` giữ 1600 × 900. Map rộng hơn màn hình: client có camera pan theo đường đạn / người chơi, đọc kích thước từ `terrain.width` / `terrain.height` của `MatchStarted` / `MatchStateSync` (hoặc `width` / `height` của `GET /maps`), không giả định 1600 × 900. Vật lý không đổi (speed power × 9,6, gravity 445, bay tối đa 4,5 s): phát 45° full lực bay ~2070 px trên mặt phẳng, nên 2400 là gần mức rộng nhất hai bên còn bắn tới nhau. Spawn hai đội cách nhau ~1300–1900 px; spawn thấp hơn đồng đội đứng gần giữa hơn để bục phía trên không che đường bắn; mọi spawn đứng trên ≥ 12 ô (192 px) mặt đi được (thực tế 14–35 ô), bục / đảo chính rộng hơn bản 1600. Bot (`bestMove`) không tìm được chỗ nào trong tầm đi có phát gây damage (và không có item / teleport giúp được, xem Bot AI) thì đi về phía địch gần nhất (trên map 2400 px địch có thể ngoài tầm bắn). Độ dày đất tính theo hố lớn nhất (Tanko cannon_ball với bonus ở cap +50 %: r 27 px, mỗi phát sâu ~22 px; lớn hơn mọi hố skill): đứng ở spawn phải chịu ≥ 3 phát cùng chỗ mới rơi khỏi map (thực tế 4–10 phát trên đất mềm; spawn trên đất cứng / lớp mềm trên đá thì không bao giờ rơi, chỉ bị đào hố và vẫn ăn sát thương nổ), và 1 phát ở bất kỳ đâu trên sàn spawn không khoét thủng sàn (`TestGeneratedMapsGroundHoldsTankoCraters`). Đạn nổ trên đất cứng không khoét gì nhưng sát thương nổ vẫn đủ (`TestExplosionOnHardGroundStillSplashes`). Skill bom liên hoàn (2 + rank quả cùng đường bay, mỗi quả một hố như đạn thường) vẫn đào giếng xuyên đất mềm được, theo thiết kế.

**Theme** (`config_maps.theme`, `gamedata.MapThemes`, mặc định `grassland`): `grassland`, `ice`, `beach`, `graveyard`, `desert`, `sky`, `temple`, `steel`, `city_night`, `stone`, `cave`. Mỗi theme = nền trời, vật liệu mềm, vật liệu cứng, đáy nước (`beach`, `ice`, `sky`, `temple`, `city_night`, `steel`) hoặc vực (còn lại). Theme lạ coi là `grassland` (`ThemeOrDefault`).

**Brick types** (036, id cố định, row có sẵn được giữ; sequence đẩy qua 19): 1 Dirt (mềm, khối đầy), 2 Rock (cứng, khối đầy), 3 Ice, 4 Lava, 5 Fragile (mặc định của admin seed); 6..12 Dirt (mềm) và 13..19 Rock (cứng) các hình: Slope Up-Right ◢, Slope Up-Left ◣, Ceiling Right ◥, Ceiling Left ◤, Half Bottom, Half Top, Plank (6 px trên cùng). Dốc 45°: leo được (≤ 1 px mỗi px). Client không cần brick: đất mềm / cứng có trong mask `terrain.destructible`.

| mapId | Tên | Theme | Hạng tối thiểu | Gió | Layout |
|---|---|---|---|---|---|
| `grassland_valley` | Thung lũng cỏ | grassland | bronze | 0–4 | legacy (đồi sine) |
| `frozen_peak` | Đỉnh băng | grassland | silver | 1–4 | legacy (đỉnh nhọn) |
| `ice_bridge` | Cầu băng | ice | bronze | 1,5–4 | 4 nhịp cầu băng mềm 22 ô, nối bằng 3 vòm băng cứng cao 6 ô, tảng băng thấp dưới vòm |
| `coconut_isles` | Đảo dừa | beach | bronze | 0,5–3,5 | 7 đảo cát mềm ~19 ô, khe 3 ô, cây dừa ở đảo ngoài cùng, đá cứng thấp dưới khe |
| `graveyard` | Nghĩa trang | graveyard | bronze | 0–3 | nền đất rộng trên vực, gò, lòng trũng giữa có huyệt mở, bia cứng, bí ngô, khối treo |
| `pyramid` | Kim tự tháp | desert | bronze | 0–3 | kim tự tháp rỗng lớn bằng đá cứng ở giữa (bắn vòng qua đỉnh), cửa ở chân, đụn cát + gờ bên trong, nền cát trên đá |
| `floating_isle` | Đảo nổi | sky | bronze | 2–4 | đảo cỏ rộng (mềm) trên lõi đá cứng, đảo lớn 42 ô ở giữa |
| `frozen_land` | Băng giá | ice | bronze | 1,5–4 | thềm băng rộng, nhũ băng treo (6 cái giữa cứng) |
| `sky_temple` | Đền tế | temple | silver | 0,5–3 | mái ván dài, cột cứng, hai sàn 25 ô (sống cứng), thuyền giữa, hai thuyền thấp hai bên |
| `escape` | Trốn thoát | city_night | gold | 0–3 | hàng thùng thép cứng (4 thùng = 16 ô) lơ lửng (không gì phá được) |
| `iron_bars` | Song sắt | steel | bronze | 0–3 | thanh thép dài + thùng cứng nhiều tầng trên sàn mềm có đáy cứng |
| `barricade` | Rào cản | steel | bronze | 0–3 | lưới chốt thép cứng trên sàn mềm có đáy cứng |
| `iron_city` | Thành phố sắt | city_night | gold | 0–2,5 | dầm thép cứng: dốc từ góc xuống bục 15–17 ô có lớp mạ mềm, zigzag giữa |
| `stone_forest` | Rừng đá | stone | bronze | 0–3 | khối đá cứng lơ lửng, nền cỏ rộng trên đá, 2 hố, gai đá |
| `stone_cave` | Hang đá | cave | silver | 0–2 | bát đá cứng rộng (đá vụn mềm trên mặt), que gỗ |
| `sky_islands` | Đảo bay | sky | silver | 2–4 | nhiều đảo 12–18 ô, dải đảo dài sát nước |
| `iron_bridge` | Cầu sắt | steel | bronze | 0,5–3,5 | 3 nhịp cầu dài 34 ô (tấm mềm trên dầm cứng), giàn cứng ở đầu nhịp, thùng cứng |
| `steel_base` | Căn cứ thép | steel | gold | 0–2,5 | dầm, khối thép cứng, bục 15–16 ô có lớp mạ mềm (thay map legacy cùng id khi row còn chưa có tile) |

Spawn: 4 điểm `[đội 1 a, đội 1 b, đội 2 a, đội 2 b]` (`TeamSpawns`: nửa đầu cho đội 1 bên trái), đội 2 là ảnh gương của đội 1; 1v1 dùng điểm đầu của mỗi đội. Test (`internal/game/match/maps_test.go`): mỗi map hợp lệ với brick types (như admin editor kiểm khi lưu), 4 spawn đúng nửa, đứng được (`CanStandAt`) trên đúng bục, dưới chân ≥ 60 px đất hoặc gặp đất cứng, không có ô lẻ trôi nổi; mỗi spawn bắn trúng được ít nhất một spawn địch và bị bắn trúng từ một spawn địch, hai spawn 1v1 bắn được nhau (bot `bestShot`, không gió); mọi spawn bắn trúng được **mọi** spawn địch (`TestGeneratedMapsEveryEnemyReachable`: `SimulateProjectile` trên terrain thật, góc 5–175°, lực 10–100, không gió, trúng thân trong `PlayerHitRadius`); quanh mỗi spawn ≥ 192 px mặt đi liền (`TestGeneratedMapsSpawnsHaveWideFloor`: `WalkTo` từng px, dừng ở tường hoặc mép rơi); bot vs bot 2v2 chơi hết trận trên mọi map.

**Xoay map**: ranked chọn ngẫu nhiên trong các map có `minRankTier` ≤ hạng của người cao nhất (`randomMapForRating`): bronze có 11 map (grassland_valley + 10 map mới), silver thêm frozen_peak, sky_temple, stone_cave, sky_islands, gold thêm escape, iron_city, steel_base. QuickPlay "Đấu với máy" (`bot: "ai"`) chọn ngẫu nhiên trong mọi map đã load (`quickPlayMapID`); Luyện tập (bot idle) luôn `grassland_valley`. Game server đọc map lúc khởi động: thêm / sửa map cần restart `cmd/game`.

### Items & Status Effects

**Immediate items:**

| Item | Hiệu ứng |
|------|----------|
| `medkit` | +`config_items.value` HP (mặc định 450, cap tại MaxHP) |
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

**Hiệu ứng trên player** (`statusEffects[]`, `durationTurn` N = N lượt tới của người bị; giảm 1 ở cuối lượt của chính họ, gắn lại thì làm mới, không cộng dồn):

| Effect | Tác dụng |
|--------|----------|
| `net` | MoveEnergy tối đa 50 |
| `freeze` | Mất trọn lượt như `stun` (trước đây chỉ không di chuyển / bắn, phải chờ hết giờ) |
| `burn` | Đầu lượt mất 10% máu tối đa (`burnMaxHPPercent`), khiên chịu trước, có thể chết |
| `root` | MoveEnergy 0, Move và teleport bị từ chối; vẫn bắn, dùng item, dùng skill |
| `stun` | Mất trọn lượt: không Move / Shoot / UseItem; TurnStarted `skipped`, server tự kết thúc lượt sau 2 s |
| Khiên (`shield`, không phải effect) | Chịu mọi damage trước máu (nổ, laze, rơi, lava, burn); tối đa 50% máu tối đa (`shieldCapPercent`); hết khi vỡ hoặc hết trận |

`Cleanse` (Hoa hồi xuân) gỡ `burn`, `root`, `stun`, `net`, `freeze`; giữ hiệu ứng tốt của chính người đó (`power_shot`, ...).

### Kỹ năng (skills, migration 028)

Skill theo **nhân vật** (`config_characters.skill_id`), dùng bằng `Shoot {actionMode: "skill"}` khi đủ 100 năng lượng. Năng lượng: +20 cuối mỗi lượt của mình, **+10 khi bị địch đánh trúng** (mất máu hoặc khiên vì hành động của địch; một lần cho mỗi hành động, dù 14 tên lửa trúng; burn, tự gây và người chết không được; tối đa 100). Id giữ nguyên (client/admin dùng), `effect_type` chọn cách giải (`skillResolvers`, mỗi skill một file `internal/game/match/skill_*.go`, số liệu ở đầu file đó; số dùng chung ở `skill_tuning.go`). Migration `028_skill_redesign` (chạy lại mỗi deploy, chỉ đổi dòng còn effect type cũ, nên sửa trên admin được giữ) và `configs/skills.yaml` / admin seed; `030_rain_tune` (số tên lửa, mô tả) và `031_chain_cap` (mô tả bom liên hoàn: trần mỗi mục tiêu) cũng chỉ đổi dòng còn giá trị cũ.

**Cấp súng** (`skillRank`, `WeaponSkillRank`): theo tier của vũ khí đang mặc, không theo +nâng cấp. Không vũ khí 0 (bot theo súng nó được phát), `*_lv10` Sắt 1, `*_lv20` Thép 2, `*_lv30` Mithril 3, `*_lv40` Rồng 4; súng chế tạo (Blaster, mặc được cho mọi nhân vật, skill vẫn theo nhân vật) Bạc 2, Vàng 3, Titan 4, Kim cương 4.

**base** = (damage vũ khí + `damageBonus`) × power_shot 1,5 × `damage_multiplier` (1,0; ≤ 0 tính là 1) × (1 + `skillPowerBonus`/100).

| Skill | Rookie — Mưa tên lửa (`triple_shot`, `missile_rain`) | Tanko — Địa chấn liên hoàn (trước 054: Bom liên hoàn; `heavy_bomb`, `bomb_chain`) | Spark — Tia laze (`shock_field`, `laser`) | Flora — Hoa hồi xuân (`healing_bloom`, `bloom`) |
|---|---|---|---|---|
| Bắn | Pháo hiệu như phát thường (gió, trọng lực); đáp: khói đỏ, không damage / hố; bay khỏi map → mất skill | 2 + cấp bom lớn (playtest 6: bớt 1 quả, mỗi quả giữ sát thương cũ: (1,8 + 0,15 × cấp) / (3 + cấp) × base) cùng đường bay đạn thường (cùng gốc, góc, lực, gió; không lái), cách ≥ 0,1 s; bom sau bay qua hố của bom trước và nổ ở chỗ đường bay đó chạm địa hình / người chơi: mỗi quả khoét lòng chảo của vũ khí; không mục tiêu thì đào một giếng (~40 px mỗi quả với `cannon_ball`); trúng thân thì nổ ở tâm thân, hố không chạm đất dưới chân: cầu vồng (dốc) và bắn thẳng ở 600 px đều trúng mục tiêu tới trần (bắn thẳng: quả đầu nổ cạnh chân, mục tiêu rơi vào hố, quả sau trúng thân) | Nạp 0,3 s rồi tia thẳng từ tâm thân theo góc ngắm tới mép map, xuyên địa hình (không giảm damage); bỏ qua lực | Không ngắm; các hạt cùng bay thẳng lên trên Flora ra khỏi đỉnh map (y −140, 0,55 s), sang trên đầu đồng đội ngoài màn hình (0,2 s) rồi rơi thẳng xuống thân (0,65 s): đáp và hồi lúc 1,4 s, mọi đồng đội còn sống (cả Flora); replay kéo thêm 0,9 s cho hiệu ứng nở hoa |
| Số lượng | 0,5 s sau khi đáp: 12 + 4×cấp tên lửa rơi thẳng (không gió, bắn xuống 1500 px/s `rainDropSpeed`, ~0,37 s tới đất) từ đỉnh map, cách 0,05 s (cấp 4: 28 quả trong ~1,8 s, cả skill ~3,0 s; trước 032 ~3,9 s), rải ± (90 + 15×cấp) px quanh điểm đáp, dày ở giữa (độ lệch = khoảng × \|v\|², nửa số quả trong ± 1/4 khoảng; seed theo trận + lượt); giải theo thứ tự chạm đất. Pháo hiệu trúng mục tiêu: ~1,4 × base (cấp 0) tới mức trần (cấp 4, ~1,8 × base sau defense của Tanko), địch cách 100 px dính ~0,55 × base ở cấp 4 | 3 / 4 / 5 / 6 / 7 | 1 tia | 1 hạt / đồng đội |
| Damage | Mỗi quả 0,25 × base (nổ 0,7 × bán kính vũ khí, hố 1,0 × hố vũ khí = 12 px; 039: hố vũ khí 20 → 12 px nên `rainCrater` 0,6 → 1,0); tổng mỗi mục tiêu ≤ 2 × base (trước defense) | Tổng (1,8 + 0,15×cấp) × base chia đều; tổng mỗi mục tiêu ≤ (1,2 + 0,15×cấp) × base (trước defense; cấp 4: phát cầu vồng trúng 6 / 7 quả là chạm trần); nổ 1,3 ×, hố mọi quả = hố vũ khí (lòng chảo 52 px, như phát thường; trước playtest 5: tròn 1,4 × = 73 px); đào bao sâu cũng được (trước: không khoét 48 px đáy map) | Địch gần nhất (1,5 + 0,1×cấp) × base, mỗi địch sau ×0,8; không hố, không văng, không rơi; không trúng đồng đội | Hồi theo máu tối đa: Flora (40 + 5×cấp)%, đồng đội (30 + 5×cấp)%; không nhân skill power; dư thành khiên (≤ 50% máu tối đa) |
| Hiệu ứng | `burn` 1 lượt lên mọi địch trúng | `root` 1 lượt | `stun` 1 lượt | Gỡ hiệu ứng xấu mọi đồng đội |
| Bot | Ngắm như phát thường tốt nhất (trượt theo hit chance) | Phát dốc nhất (lệch ≤ 30° so với thẳng đứng, bay ≤ `MaxFlightSeconds` − 1 s = 3,5 s) gây ≥ 80% damage của phát tốt nhất, không có thì phát tốt nhất; trong các lực tốt như nhau ở một góc, lực mà đường bay (bỏ qua người chơi) chạm đất gần chân địch nhất (`shaftMiss`: giếng đào ngay dưới mục tiêu); trượt theo hit chance | Góc trúng nhiều địch nhất (ưu tiên 2) | Khi máu mình < 60%, đồng đội < 50% hoặc đồng đội dính hiệu ứng xấu |

Đơn giản hoá: bom liên hoàn và tên lửa được giải tuần tự lúc server nhận phát bắn, vị trí rơi cập nhật ngay; tên lửa có thể trúng mục tiêu khi client còn đang vẽ nó rơi. Bom liên hoàn thì không: mọi cú rơi trong chuỗi chạy nhanh ×3 so với rơi tự do (quãng rơi và damage rơi giữ nguyên) và một quả bom chỉ nổ khi mọi cú rơi trước đã chạm đất + 0,08 s (nếu cần, quả sau phóng trễ hơn 0,1 s; ~9% khoảng cách bị kéo dài khi quét các góc bắn trên grassland_valley, phát cầu vồng của bot thì phần lớn). Đường bay vẫn giới hạn `MaxFlightSeconds` (4,5 s; trước 032: 6 s): bắn cầu vồng quá cao (quả đầu bay từ ~4,0 s, trước 032 ~5,2 s; chỉ người chơi bắn tay; bot chỉ lob khi ≤ 3,5 s) thì bom sâu nhất có thể hết giờ bay và biến mất (khoảng cách 0,1 s không đổi điều này: giới hạn tính theo đường bay của từng quả). Nguồn bắn của bom liên hoàn giữ nguyên dù người bắn rơi vào hố của mình. `projectile_count` / `status_effect_id` trong `config_skills` chỉ để hiển thị.

### Ngũ hành, Tide, Địa chấn liên hoàn (migration 054 / 055)

**Hệ** (`config_elements`, `gamedata/elements.go`): Kim `metal`, Mộc `wood`, Thủy `water`, Hỏa `fire`, Thổ `earth`; vòng khắc Kim → Mộc → Thổ → Thủy → Hỏa → Kim (`beats`). Nhân vật (`config_characters.element`): Rookie Hỏa, Tanko Thổ, Spark Kim, Flora Mộc, Tide Thủy. Boss (`config_bosses.element`): Rô-bốt Thép Kim, Cua Dung Nham Hỏa, Phù Thuỷ Băng Thủy; lính mang hệ của boss. `BattlePlayerState.element` (MatchStarted / MatchStateSync) và event `summon` có `element`.

**Hệ số** (`rules.elements`, admin Game Rules › Ngũ hành, 0..100 %; `match/element.go` `elementFactor`):
- PvP (mọi trận không phải boss, kể cả bot): hệ người đánh khắc hệ mục tiêu × (1 + 10 %) `strong`; hệ mục tiêu khắc hệ người đánh × (1 − 10 %) `weak`; còn lại × 1.
- Boss: nhân vật đánh boss / lính mà hệ nhân vật khắc hệ boss × (1 + 20 %); boss / lính đánh nhân vật mà hệ boss khắc × (1 + 10 %) (cả hai `strong`); không bao giờ giảm.
- Cùng đội, cùng hệ, không hệ: × 1.
- Áp lên mọi sát thương từ đơn vị lên đơn vị: đạn, kỹ năng (mưa tên lửa, bom liên hoàn, tia laze, nổ bông tuyết), tia / mưa / sóng chấn động của boss, tự nổ và nổ khi chết của lính (cả loại % máu tối đa). Nhân vào sát thương thô, cùng chỗ với power_shot / skill power, **trước** phòng thủ, chí mạng, khiên lính của boss và khiên; trần sát thương của kỹ năng (`capOfBase`) cũng nhân theo. Không áp cho rơi, `out_of_map`, bỏng / dung nham đầu lượt, hồi máu. Bot tính cả hệ khi chọn phát bắn.

**Tide** (`tide`, Thủy, `controller` "Khống chế"; 1400 HP, damage 500, mobility 110, defense 45, skill power 70, terrain 16, difficulty 2; gun kind `water_gun`; có như các nhân vật khác: shop `unlock_tide_coin` 5000 coin / `unlock_tide_gem` 300 gem, súng `tide_weapon_lv10..40` Iron .. Dragon Water Gun, bot chọn được). Vũ khí `water_orb` (500, nổ 38 px, terrain 16 → hố 9,6 px, nặng 0,9, gió 1,0, `bounces` 1): nảy một lần rồi nổ (xem Đạn nảy).

**Nổ bông tuyết** (`snow_nova`, effect `nova`, `match/skill_nova.go`, params `gamedata.NovaTuning`): không ngắm (`aimKind` `none`), không đạn, không hố, không rơi. Sau `castTime` 0,35 s nổ tại tâm thân Tide; mọi địch (người, lính, boss, kể cả đang bay) có thân trong `radius` 500 px (`blastDistance`: tới mép thân) nhận (`centreBase` 1,3 + `centrePerRank` 0,1 × cấp) × base ở tâm, giảm tuyến tính còn `edgeFactor` 0,4 × base ở mép, × hệ, rồi phòng thủ / chí mạng / khiên lính boss; lúc vòng băng (`waveSpeed` 1400 px/s) tới (`castTime + d / waveSpeed`): `damage` cause `nova`, còn sống thì `freeze` `freezeTurns` 1 lượt. Tide, đồng đội không bị gì. Bot: dùng khi ≥ 2 địch trong tầm, hoặc 1 địch mà sát thương dự kiến của nova > sát thương phát bắn tốt nhất × tỉ lệ trúng.

**Địa chấn liên hoàn** (Tanko, `heavy_bomb`): đổi tên và mô tả (tảng đá bọc đất, đất nứt, gai đá, đất đá vùi chân), cơ chế và số liệu giữ nguyên (`bomb_chain`, `root`). 054 chỉ đổi khi vẫn là chữ cũ.

### Reward System

Sau mỗi trận:

| Loại | Thắng | Thua |
|------|-------|------|
| EXP cơ bản | 80 (50+30) | 50 |
| Coins | 50 (30+20) | 30 |
| Damage bonus | +1 EXP / 300 damage dealt | +1 EXP / 300 damage |
| Kill bonus | +10/kill | +10/kill |

EXP vào cả profile và nhân vật đang chơi (`player_characters.exp`, cộng dồn). Level nhân vật = level cao nhất của `game_settings.character_levels` có `expRequired` ≤ exp; mỗi level lên được `pointsPerLevel` (10) điểm. Bảng mặc định (migration 006 + 019) có level 2–80: L2–L10 giữ nguyên 200, 400, 700, 1100, 1600, 2200, 3000, 4000, 5200; từ L11 bước lên level sau tăng thêm 25:

`expRequired(L) = 5200 + 1200·n + 25·n(n+1)/2`, `n = L − 10` (bước từ L−1 lên L = 1200 + 25·n)

| Level | 10 | 15 | 20 | 30 | 35 | 40 | 55 | 75 | 80 |
|-------|----|----|----|----|----|----|----|----|----|
| EXP tích luỹ | 5.200 | 11.575 | 18.575 | 34.450 | 43.325 | 52.825 | 85.075 | 136.825 | 151.325 |
| ≈ số trận (≈90 EXP/trận) | 58 | 129 | 206 | 383 | 481 | 587 | 945 | 1.520 | 1.681 |

≈90 EXP/trận là trung bình thắng/thua (50/80) + ~4.500 damage (+15) + ~0,5 kill. Level 80 cao hơn level yêu cầu lớn nhất của trang bị (75). Admin thêm/bớt level ở `/api/config/character-levels`; level tối đa là level cuối bảng.

### Mốc thưởng (milestones, migration 026)

Thưởng một lần cho mỗi tài khoản khi đạt mốc tiến độ; không reset, người chơi tự bấm nhận. Mỗi mốc là một dòng `missions` với `type = 'milestone'` (`track`, `target`, `required_value`, `sort_order`, `description`, `reward_coin`, `reward_gem`, `reward_items`); đã nhận = dòng `mission_progress` có `is_claimed` (`current_value` = tiến độ lúc nhận). `/missions/*` không thấy các dòng này.

| Track | Target | Tiến độ | Mốc |
|-------|--------|---------|-----|
| `character` | `char_level` | Level cao nhất trong các nhân vật của người chơi (`player_characters`) | 5, 10, …, 80 |
| `upgrade` | `upgrade_peak` | Level cao nhất một món từng đạt (counter, hoặc món hiện có nếu cao hơn); bán món hay thất bại tụt cấp không làm giảm | +3 +6 +10 +12 +14 +16 |
| `upgrade` | `weapon_upgrade_peak` | Như trên, chỉ vũ khí; mô tả "Mở khoá hào quang tím/đỏ sau lưng" | +14 +16 |
| `upgrade` | `set_level:N` | Số slot (0–6) nhiều nhất mà một nhân vật đang mặc ở ≥ +N; cần 6; kiểm tra trạng thái hiện tại, nhận rồi là xong | +3 +6 +10 +14 +16 |
| `combat` | `matches` / `wins` | Số trận / số trận thắng được tính | 10/50/100/300/1000 · 5/25/100/500 |
| `combat` | `char_wins` | Số trận thắng được tính nhiều nhất với một nhân vật | 10/50/200 |
| `rank` | `rank_peak` | Tier cao nhất từng đạt (bronze 1 … master 6), riêng với thưởng mùa | Bạc 2, Vàng 3, Bạch kim 4, Kim cương 5, Cao thủ 6 |

Thưởng: `reward_coin`, `reward_gem`, `reward_items` = `[{"kind":"stone","level":L,"qty":Q}]` (`player_stones`), `{"kind":"item","id":...,"qty":Q}` (`inventory_items`, source `milestone_reward`) hoặc `{"kind":"title","id":...}` (`player_titles`, không có qty; đã sở hữu thì bỏ qua, không lỗi). Khi trả `GET /milestones` và `rewards.items` của claim, item title có thêm `name`, `rarity` lấy từ `config_titles` (không lưu trong `reward_items`). Coin/gem ghi `economy_transactions` source `milestone_reward`, `ref_id` = id mốc. Mốc có target lạ, kind chưa hỗ trợ, hoặc item title có id không phải danh hiệu đang bật không được trả về và không nhận được — điểm mở rộng: `supportedRewardKinds` (`milestone/model.go`) + một case trong `Repository.GrantItemTx`. Giá trị seed ở `milestone.Defaults`; admin `SeedMilestones` ghi giống migration 026 (`ON CONFLICT DO NOTHING`).

**Counter** (`player_stat_counters (player_id, counter, value)`): `matches`, `wins`, `char_wins:<characterId>` (+1 lúc kết thúc trận, trong tx thưởng trận), `rank_peak`, `rating_peak` (GREATEST mỗi lần ghi rating, từ `highest_tier` của mùa — dòng rank mới bắt đầu ở silver, nên trận có rating đầu tiên đã đạt mốc Bạc), `upgrade_peak`, `weapon_upgrade_peak` (GREATEST khi cường hoá thành công, trong tx cường hoá). Level nhân vật và set tính lúc đọc.

**Trận được tính** (`Match.CountsForMilestones` + `milestoneCounts`): trận ghép (`ranked_2v2` và `casual_2v2`, kể cả có bot) và QuickPlay đấu với máy AI (`bot: "ai"`); không tính phòng tự tạo (`CreateRoom` pvp_1v1/pvp_2v2) và Luyện tập (bia đứng yên). Với từng người: không rời trận (`Leave` → `Left`), là người thật, và đã thực hiện ≥ 3 lượt hành động (`ShotTurns`: mỗi ShotResolved của họ — bắn, skill, teleport). Thua/hoà vẫn tính trận.

**Claim**: một tx — khoá `player_profiles` (mỗi lần một claim của người chơi), đọc mốc, cờ đã nhận và tiến độ qua tx (không dùng Pool), ghi claim bằng `INSERT … ON CONFLICT DO UPDATE … WHERE is_claimed = FALSE` (0 dòng → `MILESTONE_ALREADY_CLAIMED`), cộng coin/gem, đá, item. Client gặp `MILESTONE_ALREADY_CLAIMED` thì coi như xong và tải lại.

**Backfill (một lần, khi bảng counter được tạo)**: upgrade peaks từ level hiện tại + `equipment_upgrade_log` thành công của món còn giữ; `rank_peak`/`rating_peak` từ mọi dòng `player_ranks`; matches/wins/char_wins từ `match_histories` có `MatchStarted` trong `match_event_logs`: `ranked_2v2` hoặc `pvp_1v1` có bot không phải "Target Bot", và người chơi có ≥ 3 `ShotResolved`; nhân vật lấy từ `MatchStarted`. Rời trận không lưu nên không lọc được; trận không còn event log không được tính. Chạy lại 026 không đếm lại.

### Danh hiệu (titles, migration 027)

Danh hiệu là biển trang trí, không có chỉ số. Cấu hình ở `config_titles` (`title_id`, `name` tiếng Việt, `rarity` `blue`/`orange`/`purple`/`red`, `sort_order`, `description` = cách đạt, không có tiền tố — client hiện "Đạt: " + description, `is_active`). Sở hữu: `player_titles (player_id, title_id, obtained_at)`, chỉ nhận qua mốc thưởng (item `{"kind":"title","id":...}`). Đang đeo: `player_profiles.equipped_title_id` (FK `ON DELETE SET NULL`); chỉ tính khi còn sở hữu và đang bật — nếu không, mọi chỗ đọc (`/player/titles` equipped, profile `equippedTitle`, `title` trong trận) coi như null.

| title_id | Tên | Rarity | Mốc |
|----------|-----|--------|-----|
| `veteran` | Chiến binh kỳ cựu | blue | `ms_char_lv_40` — 1 nhân vật đạt cấp 40 |
| `smith` | Thợ rèn lành nghề | blue | `ms_upgrade_10` — 1 trang bị đạt +10 |
| `sharpshooter` | Xạ thủ bách chiến | orange | `ms_wins_0100` — Thắng 100 trận |
| `old_hand` | Lão làng | orange | `ms_matches_1000` — Chơi 1000 trận |
| `elite_armor` | Chiến giáp tinh anh | purple | `ms_set_14` — 1 bộ trang bị hoàn chỉnh +14 |
| `battle_master` | Bậc thầy chiến trường | purple | `ms_char_lv_80` — 1 nhân vật đạt cấp 80 |
| `legend_smith` | Thợ rèn huyền thoại | red | `ms_upgrade_16` — 1 trang bị đạt +16 |
| `grandmaster` | Cao thủ | red | `ms_rank_master` — Lần đầu đạt hạng Cao thủ (tier 6) |

**Equip**: một tx — khoá `player_profiles` `FOR UPDATE`, kiểm tra sở hữu + đang bật qua tx, `UPDATE equipped_title_id`, kiểm tra `RowsAffected`. **Migration 027** (chạy lại được): seed 8 danh hiệu `ON CONFLICT DO NOTHING` (giữ chỉnh sửa admin), thêm item title vào cuối `reward_items` của mốc tương ứng nếu chưa có (giữ các item khác), backfill: ai đã nhận (`is_claimed`) mốc có item title thì được danh hiệu đó, `obtained_at` = `updated_at` của claim. Giá trị seed ở `title.Defaults` + `milestone.TitleRewards`; admin `SeedTitles` / `SeedMilestones` ghi giống migration; reset-data xoá `player_titles`.

### Nhiệm vụ ngày (daily missions, migration 034)

Màn "Nhiệm vụ" ở sảnh (`internal/api/mission`). Mỗi nhiệm vụ là một dòng `missions` `type = 'daily'` (`title`, `target`, `required_value`, `activity` = điểm năng động, `goto_screen` → JSON `goto`: `none`/`play`/`equipment`/`shop`, `sort_order`); tiến độ là dòng `mission_progress` có `period` = ngày nó tính. Xong thì người chơi bấm nhận: chỉ được điểm năng động, cộng vào năng động hôm nay **và** tuần này (`player_mission_activity (player_id, track daily|weekly, period, points)`). Rương năng động là dòng `missions` `type = 'mission_chest'` (`track` daily/weekly, `required_value` = ngưỡng, `reward_coin`/`reward_gem`/`reward_items` cùng dạng mốc thưởng); đã nhận = dòng `mission_progress` `is_claimed` với `period` của kỳ nhận.

**Kỳ (UTC+7, offset cố định, không dùng tz database)**: ngày `2026-10-21` bắt đầu 00:00 UTC+7 (17:00 UTC hôm trước); tuần ISO `2026-W43` bắt đầu thứ Hai 00:00 UTC+7. `dayResetAt` / `weekResetAt` là lúc kỳ hiện tại hết, RFC3339 UTC. **Reset lười**: dòng của kỳ cũ đọc là 0 / chưa nhận và bị ghi đè ở lần ghi kế tiếp; không cần worker (`daily_reset` giờ không làm gì — bản cũ xoá tiến độ lúc 00:00 UTC = 07:00 giờ VN).

| id | target | cần | năng động | goto |
|----|--------|-----|-----------|------|
| `daily_login` | `login` | 1 | 10 | none |
| `daily_play_3` | `play_match` | 3 | 20 | play |
| `daily_win_1` | `win_match` | 1 | 20 | play |
| `daily_damage_3000` | `damage_dealt` | 3000 | 20 | play |
| `daily_kill_3` | `enemy_killed` | 3 | 20 | play |
| `daily_hit_10` | `shots_hit` | 10 | 10 | play |
| `daily_skill_2` | `skill_used` | 2 | 10 | play |
| `daily_item_3` | `item_used` | 3 | 10 | play |
| `daily_upgrade_1` | `upgrade` | 1 | 10 | equipment |
| `daily_shop_1` | `shop_purchase` | 1 | 10 | shop |

Tổng 140 năng động. Rương ngày: `chest_d20` 300 coin · `chest_d40` 500 coin · `chest_d60` 10 gem · `chest_d80` 500 coin + 2 `medkit` · `chest_d100` 1000 coin + 20 gem. Tuần: `chest_w150` 2000 coin · `chest_w300` 30 gem · `chest_w450` 3 đá cấp 4 · `chest_w600` 3000 coin + 80 gem.

**Tiến độ được ghi ở đâu** (`mission.RecordTx`, upsert một câu lệnh, trong tx của bên gọi): kết thúc trận (`match.ProcessMatchRewards`, cùng tx và cùng điều kiện `Counts` với `milestone.RecordMatchTx`: trận được tính, người thật, không rời trận, ≥ 3 lượt) cộng `play_match` 1, `win_match`, `damage_dealt` (`DamageDealt`), `enemy_killed` (`KillCount`), `shots_hit` (`ShotsHit`), `skill_used` (`SkillUses`, +1 mỗi lần skill bắn đủ năng lượng), `item_used` (số item đã dùng); `upgrade` +1 mỗi lần cường hoá (thành công hay thất bại, `equipment.UpgradeEquipment`); `shop_purchase` +1 mỗi lần mua thành công ở `/shop/purchase`, `/shop/equipment/buy`, `/shop/stones/buy`, `/shop/gems/buy`, `/shop/materials/buy` (replay idempotent không tính; IAP không tính); `login` = `GET /missions/daily` đầu tiên trong ngày (claim cũng ghi). Giá trị lưu không bị cắt; JSON `progress` cắt ở `required`.

**Claim**: một tx — khoá `player_profiles` `FOR UPDATE` (mỗi lần một claim của người chơi), đọc trạng thái qua tx (không dùng Pool), rồi ghi có điều kiện và kiểm tra `RowsAffected`: nhiệm vụ `UPDATE … WHERE period = hôm nay AND is_claimed = FALSE AND current_value >= required_value`, rương `INSERT … ON CONFLICT DO UPDATE … WHERE period <> kỳ này OR is_claimed = FALSE` (0 dòng → `*_ALREADY_CLAIMED`). Coin/gem của rương ghi `economy_transactions` source `mission`, `ref_id` = id rương; item/đá qua `milestone.GrantRewardItemTx` (item source `mission_reward`). Migration 034 chạy lại được (`IF NOT EXISTS`, seed `ON CONFLICT DO NOTHING` — giữ chỉnh sửa của admin).

**Rời trận** (`Leave` / mất kết nối → `Left`, mọi mode): không bao giờ thắng (cả khi team thắng; không tính hoà), 0 EXP, 0 coin, lịch sử ghi `loss`; đồng đội vẫn nhận kết quả bình thường.

**Rating — chỉ `ranked_2v2`** (`casual_2v2` Đấu thường, `pvp_1v1` QuickPlay / phòng tự tạo và `pvp_2v2` không đổi rating, không ghi `player_ranks`):
- Elo `round(K × (actual − expected))`, K = `elo.kFactor` (32), `expected = 1 / (1 + 10^((opp − team)/400))` với rating trung bình mỗi team; bot tính bằng trung bình rating người thật trong trận. `actual`: thắng 1, hoà 0,5, thua 0, **rời trận 0**. Trận có bot: × `botRatingModifier` (0,5).
- Chưa có dòng `player_ranks` → bắt đầu từ `elo.defaultRating` (1000 = silver 3); rating không xuống dưới `elo.ratingFloor` (0); `ratingChange` là thay đổi thật sau khi chặn sàn.
- Ghi vào mùa active (`ranked.EnsureActiveSeason`, tạo "Mùa N" nếu chưa có), cùng tx phần thưởng; mốc `rank_peak` / `rating_peak` theo sau.

**`MatchEnded.rewards[playerId]`** (chỉ người thật): `{playerId, expGained, coinGained, ratingChange, levelUp, newLevel, charLevelUp, charNewLevel, charLevelsGained}`; ranked thêm `oldRating, oldTier, oldDivision, newRating, newTier, newDivision`. Mode khác: `ratingChange` 0 và **không có** các field rating (trước đây gửi 1000/silver/3 giả).

`stats` (mọi mode, mọi người chơi kể cả bot và người đã rời): `{playerId: {damageDealt, kills, shotsHit, shotsFired}}`. `mvpPlayerId`: người (kể cả bot) có `damageDealt` cao nhất; hoà → nhiều kill hơn → thuộc team thắng → `playerId` nhỏ hơn; `""` khi no-contest.

```json
{"winningTeam":1,
 "rewards":{"p1":{"playerId":"p1","expGained":95,"coinGained":50,"ratingChange":18,
   "oldRating":1050,"oldTier":"silver","oldDivision":3,"newRating":1068,"newTier":"silver","newDivision":2,
   "levelUp":false,"newLevel":12,"charLevelUp":true,"charNewLevel":5,"charLevelsGained":1}},
 "stats":{"p1":{"damageDealt":4200,"kills":1,"shotsHit":6,"shotsFired":9},
          "bot_1a2b3c4d":{"damageDealt":1800,"kills":0,"shotsHit":3,"shotsFired":8}},
 "mvpPlayerId":"p1"}
```

`charNewLevel` / `charLevelsGained` là 0 khi nhân vật không lên level; `newLevel` luôn là level tài khoản sau trận.

---

### Game data: DB, snapshot, hot reload (S2, migration 041)

Mọi luật và con số của trận nằm trong DB, sửa ở admin; Go chỉ giữ giá trị mặc định (`gamedata.DefaultRules`, `DefaultPhysics`, `Default*Tuning`, `DefaultStatusEffects`, `ranked.DefaultBands`) khi thiếu dòng hoặc giá trị sai kiểu / ngoài khoảng (bỏ qua và log).

| Ở đâu | Giá trị |
|---|---|
| `game_settings` `physics.*`, `combat.defense_k`, `match.turn_time_seconds`, `match.idle_timeout_minutes`, `move.step_pixels` + `move.energy_cost_per_2px` (đi `step` px tốn `cost` năng lượng: 2 / 1), `fall.damage_threshold` / `fall.damage_per_pixel` (150 / 3,75), `equipment.*` | `gamedata.ScalarSettings` |
| `game_settings` `rules.physics`, `rules.aim`, `rules.aim_aid`, `rules.combat`, `rules.terrain`, `rules.movement`, `rules.timings`, `rules.skill_energy`, `rules.skill_ranks`, `rules.items`, `rules.player`, `rules.equipment`, `rules.economy` (JSON) | `gamedata.RuleGroups` (khoảng hợp lệ từng field) |
| `config_items.value` | medkit hồi máu, wind_stopper số lượt, drill_bomb px xuyên, freeze_bomb số lượt, **power_shot hệ số sát thương**, **spider_net giới hạn năng lượng di chuyển** |
| `config_skills.params` (JSONB) + `description_template` | tuning theo loại hiệu ứng (`gamedata.SkillParamSpecs`) |
| `config_status_effects.bad / skips_turn / blocks_move` | Cleanse, mất lượt (thứ tự `sort_order`: stun trước freeze), chặn đi / dịch chuyển |
| `config_rank_tiers.min_rating / divisions / division_size` | bands hạng (`ranked.SetBands`, đổi nguyên khối, atomic) |

Còn trong Go (không phải số cân bằng hay số client cần): tham số tìm đường bắn của bot AI (`bot*` trong `bot.go`, `skill_*.go`), nhịp replay (đệm 1,2 s sau phát bắn, 0,3 s sau teleport), cấu trúc 2v2 (`matchmaker.TeamSlots`), danh sách theme map (`gamedata.MapThemes`, client có art riêng), terrain zone của map legacy (đất giòn ×2, dung nham trừ máu đầu lượt; vùng băng không còn làm trượt).

**Snapshot.** `gamedata.LoadFromDB` đọc mọi bảng config + game_settings trong một transaction REPEATABLE READ READ ONLY thành một `gamedata.Snapshot` bất biến; `gamedatasync.Reloader` gắn `Version` = `apigamedata.VersionOf(snapshot)` (đúng hàm build của `GET /gamedata`), lưu payload vào `gamedata_versions`, rồi `gamedata.Publish` (atomic pointer). `match.NewMatchWithData(snapshot, …)` (room dùng `NewMatch` = `gamedata.Current()` lúc tạo trận) giữ con trỏ đó suốt trận: vật lý, luật, tuning, item, map đều đọc từ `m.gd` / `Terrain.gd`, không đọc biến toàn cục. Còn toàn cục (đổi atomic): `gamedata.Current()` cho room / lobby / API (chỉ số nhân vật lúc vào trận, giới hạn loadout, luật trang bị của API) và `ranked.Bands()` (hạng là của người chơi, không theo trận).

**Hot reload.** Trigger trên mọi bảng config tăng `gamedata_revision`; middleware của admin thấy revision đổi sau một request POST thì publish `gamedata:changed`. cmd/game, cmd/api, cmd/admin subscribe và đọc lại DB; dự phòng mỗi 10 s so revision. Nội dung không đổi (cùng version) → giữ snapshot cũ; DB lỗi / config sai (vd nhân vật trỏ vũ khí không có) → giữ snapshot đang chạy và báo lỗi ở `gamedata:loaded` (header admin hiện đỏ). Trận mới dùng snapshot mới, trận đang chơi không đổi. `GET /gamedata` build một lần mỗi snapshot.

## 8. Ranked Matchmaking System

### Flow

```
CreateLobby {queue} → UpdateLoadout → (SetLobbyQueue) → StartQueue → Matchmaker ghép hàng đợi của lobby
  (4 slot, 2 team × 2) → MatchFound → (matchStartDelay) MatchStarted → trận ranked_2v2 / casual_2v2
  → MatchEnded (rating chỉ ranked_2v2) → ReturnToLobby
```

### Lobby Room

- 1-2 người cùng team, actor pattern (goroutine + event channel); mọi event trừ Create/Join chạy trên goroutine lobby (giữ thứ tự)
- Character/items lưu trong `player_loadouts` DB table
- Status: `preparing` → `in_queue` → `in_match` → `preparing` (loop; `MatchFound` / `ReturnToLobby` qua `LobbyHub.Notify`)
- Host rời → lobby giải tán, member nhận `LobbyDisbanded`; rời khi `in_queue` → entry rời hàng đợi
- Rating của member: dòng `player_ranks` của mùa active, không có → `elo.defaultRating`; đọc lại mỗi `StartQueue`
- Hàng đợi của lobby: `LobbyState.queue` (`CreateLobby {queue?}`, host đổi bằng `SetLobbyQueue` khi `preparing`), `StartQueue` vào hàng đợi đó
- Party (mời đồng đội, §6 Party): `InviteToLobby` / `RespondInvite`, `KickMember`, `SetReady`; host luôn `ready`, member vào lobby `ready = false` và phải `SetReady` trước khi host `StartQueue`. Presence (ai đang online) và lời mời chỉ trong bộ nhớ của một game node

### Matchmaker Engine

- Goroutine chạy trong game server, tick mỗi `tickInterval` (3s); mỗi tick ghép lần lượt từng hàng đợi (`matchmaker.Queues`: `ranked_2v2`, `casual_2v2`) bằng cùng `planMatches` và cùng config; một leader lock chạy mọi hàng đợi
- Redis leader election (`SETNX` TTL 10s) cho multi-node
- Hot-reload config mỗi 30s từ `game_settings` DB

**Queue (Redis):**
- Sorted set mỗi hàng đợi `matchmaking:queue:<queue>` (`matchmaking:queue:ranked_2v2`, `matchmaking:queue:casual_2v2`; score = rating). Key cũ `matchmaking:queue:2v2` (trước khi có nhiều hàng đợi): leader chuyển entry còn trong đó sang `ranked_2v2` mỗi tick (`Queue.MigrateLegacy`), entry JSON không có `queue` coi là `ranked_2v2`
- Entry detail: `matchmaking:entry:{id}` (JSON string, TTL 120s): solo (1 người) hoặc party (lobby 2 người); `queue`; `rating` = party rating, `playerRatings` = rating riêng
- Player mapping: `matchmaking:player:{id}` (string, TTL 120s)
- Lấy entry cho một trận (`Queue.Claim`): `ZREM` từng entry; entry đã biến mất (vừa bị hủy) → trả lại các entry đã lấy, bỏ trận đó. `CancelByPlayer` cũng `ZREM`; 0 → `ErrAlreadyMatched` (trận đang được tạo, cancel bị bỏ qua). Tạo room lỗi → entry vào lại hàng đợi (giữ `queuedAt`)

**Ghép trận** (`planMatches`, mỗi tick):
1. Phục vụ entry chờ lâu nhất trước; mỗi entry dùng 1 lần mỗi tick
2. Hai entry ghép được khi |rating₁ − rating₂| ≤ min(allowedRange₁, allowedRange₂); mọi cặp entry trong trận đều phải ghép được. `allowedRange = baseRatingRange(100) + (wait/expandInterval(10s)) × expandStep(50)`, tối đa `maxRatingRange` (300)
3. Trận đủ 4 người: party vs party, party vs 2 solo, hoặc 4 solo chia thành cặp có chênh lệch trung bình team nhỏ nhất (thử cả 3 cách chia); nhiều lựa chọn → chọn chênh lệch nhỏ nhất
4. Không đủ người và entry đã chờ ≥ `botFillAfter` (20s): dùng người trong tầm trước (nhiều người nhất), bot lấp chỗ trống:
   - party + 1 solo: party vs solo + bot; solo + party: solo + bot vs party
   - solo + 2 solo: 2 người cùng team vs người thứ 3 + bot (cách chia cân nhất)
   - solo + 1 solo: **đối thủ của nhau**, mỗi người 1 bot (1+bot vs 1+bot) — giữ PvP thay vì 2 người cùng team đánh 2 bot
   - một mình: party vs 2 bot, solo + bot vs 2 bot
5. Bot: nhân vật, cấp, súng và item theo `rules.bots` (Bot AI, §8); độ khó (`bot_difficulty`) theo tier của **trung bình rating người thật**; trong Elo bot = trung bình rating người thật; trận có bot nhân `botRatingModifier`
6. Map: ngẫu nhiên trong các map có `min_rank_tier` ≤ tier của rating người thật cao nhất

**Bắt đầu trận** (`room.Hub.CreateBattleFromMatch`): đọc profile (tên, level, danh hiệu) → dựng roster + `MatchFound` → giữ chỗ item của mọi người thật (`reservePlayerItems` như phòng tự tạo; ai không giữ được item thì chơi không item) → đăng ký room → `MatchFound` tới từng lobby, client vào room → match chạy, chờ `matchStartDelay` (3s) rồi `MatchStarted`. Item đã dùng bị trừ lúc kết thúc, phần còn lại trả về.

**Party rating:** 3 strategies (config từ admin): `max` (default), `average`, `weighted`

**Config `game_settings.matchmaking`**: `tickInterval, baseRatingRange, expandInterval, expandStep, maxRatingRange, botFillAfter (20, migration 035), matchStartDelay (3, migration 035), inviteTimeoutSec (30, migration 042), botRatingModifier, partyRatingStrategy, weightedRatio`; `maxWaitTime` (60) không còn dùng khi ghép. `game_settings.elo`: `kFactor, ratingFloor, defaultRating`.

### Tier & mùa giải

`internal/shared/ranked` là nơi duy nhất: `Tier` / `TierAndDivision` (bronze < 1000, silver < 1200, gold < 1500, platinum < 1800, diamond < 2200, master; division 3 thấp nhất → 1, master luôn 1), `TierIndex` / `CompareTiers`, `LoadEloConfig`, `EnsureActiveSeason`. Matchmaker, room, match rewards, lobby và `/rank` đều dùng nó (không còn `ratingToTier` trùng lặp).

**Mùa giải (phase 1, chưa có vòng đời):** `EnsureActiveSeason` trả mùa `active`; không có thì tạo `season_<n>` tên "Mùa <n>" (n = số mùa + 1), 90 ngày — dùng chung cho match rewards và `/rank/seasons/current`, `/rank/me` (không còn mùa giả). Migration 035 đổi tên `season_1` "Default Season" → "Mùa 1".

### Bot AI

Bot của trận ghép (Đấu hạng / Đấu thường, lấp chỗ trống) và QuickPlay "Đấu với máy" (`bot: "ai"`). Bot Luyện tập (bia đứng yên) giữ nguyên: `rookie`, chỉ số gốc, không item. Không ghi gì cho bot: không `player_*`, không inventory, không reservation (`reservePlayerItems` bỏ qua bot, item bot dùng không vào `usedItems` lúc kết trận).

**Loadout** (`internal/game/room/bots.go`, `botOutfitter`, mỗi trận một cái):

- Nhân vật: ngẫu nhiên trong các nhân vật của game data có vũ khí; các bot trong trận khác nhau, hết thì lặp lại.
- Cấp: trận ghép = trung bình level tài khoản người thật, Đấu máy = level người chơi; ± ngẫu nhiên tới `levelSpread` (3), kẹp 1 .. cấp cao nhất của `character_levels` (80). Gửi trong card `MatchFound`.
- Súng: một dòng thật của `config_equipment_items` (`slot = 'weapon'`, `is_active`), đọc qua pool lúc tạo trận: súng thường của nhân vật đó (hoặc súng chung) có `required_level` cao nhất ≤ cấp, hoặc súng chế tạo như vậy, chọn ngẫu nhiên một trong hai. Dưới cấp 10 không có súng (súng cơ bản như người mới). Cường hoá ngẫu nhiên `weaponUpgradeMin` (0) .. ⌊`required_level` × `weaponUpgradePerLevel` (0,2)⌋ (lv10 +2, lv20 +4, lv40 +8, kim cương +15), tối đa `rules.equipment maxUpgradeLevel`. `weaponItemId` / `weaponUpgradeLevel` như người thật: client vẽ súng và hào quang +14 / +16; `skillRank` theo súng đó.
- Chỉ số: `battlestats.Compute(nhân vật, điểm, statMultipliers, SumEquipment([súng], milestones))` như `getActualStats`; điểm = `(level − 1) × pointsPerLevel` chia đều 6 chỉ số. Độ khó theo hạng không đổi chỉ số. Cấp 1: không điểm, không súng = chỉ số gốc của nhân vật (như trước, chỉ khác nhân vật); cấp 4 (1 + spread): +30 điểm = +10 HP, +25 damage, +5 defense, +5 năng lượng, +5 skill / terrain.
- Item: `defaultItems` (medkit, teleport) + `randomItemCount` (1) món khác nhau rút từ `randomItems` (power_shot, drill_bomb, spider_net, freeze_bomb, air_strike, wind_stopper; trống = mọi item bot biết dùng). Chỉ item có trong game data và bot biết dùng (`match.BotUsableItems`), theo luật của người chơi: tối đa `rules.items maxLoadout`, mỗi món ≤ `maxUsePerMatch`.

**Quyết định** (`internal/game/match/bot.go`, `bot_items.go`; mỗi bước `DecideAction`, tối đa 4 bước một lượt):

1. Medkit (bước đầu lượt): máu ≤ `medkitHpPercent` (35 %) máu tối đa và chưa đầy; luôn dùng, trừ khi quay trúng `medkitForgetChance` của hạng (bronze 0,15, còn lại 0).
2. Skill khi đủ năng lượng (planner của từng skill).
3. `bestShot`: lưới góc / lực, mô phỏng y hệt `processShoot` với gió thật; xếp theo damage − 25 × đồng đội (kể cả mình) trong vùng nổ, rồi điểm nổ gần địch nhất.
4. Không phát nào gây damage (hoặc chỉ có phát hại đồng đội):
   - đi tới chỗ trong tầm (năng lượng × `PxPerEnergy`, như `processMove`) có phát gây damage, nếu quay trúng `movementSmart` (không trúng: bắn luôn);
   - không đi được tới chỗ như vậy: item mở đường bắn (mỗi món quay `useItemChance` của hạng): drill_bomb nếu phát khoan xuyên (item value 50 px) gây damage; air_strike nếu bom rơi từ trên xuống trong ±60 px quanh một địch gây damage (`targetX`, góc / lực `noAim`); wind_stopper nếu tắt gió thì có phát gây damage (bước sau bắn);
   - teleport (không quay xác suất): mô phỏng mọi góc / lực thô y hệt `shootTeleport` (đạn của vũ khí, gió, không khoan), giữ một điểm rơi mỗi 40 px, chỉ điểm an toàn: đứng được (`CanStandAt`, đúng mặt đất, đủ chỗ cho thân), có đất hai bên chân (không mép hố đáy map), không dung nham trong `teleportLavaMarginPx` (40), cách mọi địch theo chiều ngang ≥ `teleportEnemyMarginPx` (120), dịch ≥ 60 px; chọn điểm có phát thô (bước 10° / 10) tốt nhất có damage. Teleport là phát bắn item nên hết lượt (luật hiện tại);
   - không có gì: đi tới chỗ tốt hơn hoặc về phía địch gần nhất (`movementSmart`).
5. Bắn phát tốt nhất. Không phát nào gây damage thì đó là phát rơi gần địch nhất (có gió, tránh đồng đội nếu được); mọi đường bay ra khỏi map thì phát có điểm cuối gần địch nhất (`closestOffMapShot`). Không còn phát cố định 45° / 50.
6. Phát trúng quay `hitChance` như trước (`rollHit` / `nearMiss`); phát trượt cố ý không mang item. Phát trúng mang power_shot, rồi freeze_bomb, rồi spider_net nếu có (quay `useItemChance`).

Không có item khiên (bloom là skill), nên luật "khiên khi máu thấp và hết medkit" không áp dụng. Item lạ (admin thêm vào `randomItems` mà bot không biết dùng) bị bỏ khỏi pool.

**Config**: `rules.bots` (trang Game Rules → Bot; migration 043 seed, thiếu dòng thì dùng mặc định Go): `levelSpread` 3, `weaponUpgradeMin` 0, `weaponUpgradePerLevel` 0,2, `defaultItems` `medkit, teleport`, `randomItems` (6 món trên), `randomItemCount` 1, `medkitHpPercent` 35, `teleportLavaMarginPx` 40, `teleportEnemyMarginPx` 120. `bot_difficulty` (trang Matchmaking) mỗi hạng: `hitChance`, `useItemChance` (item ngẫu nhiên), `movementSmart`, `medkitForgetChance` (043). Không có trong `GET /gamedata` (client không cần).

---

## 8a. Chế độ Boss (migration 045 / 046, v2: 047 / 048 / 049, v3: 050 / 051, sau v3: 052 / 053, hệ: 054)

Đội tối đa 4 người (người thật + bot đồng đội, team 1) đánh một boss và lính của nó (team 2). Mỗi boss là một dòng `config_bosses` (admin trang **Bosses**), có chế độ lobby riêng trong mục Boss của màn Chọn chế độ (`config_game_modes` kind `lobby`, category `boss`, icon `boss`, `party_size` 4, queue `boss:<bossId>`) và dòng match cùng id với queue (`boss:steel_robot` "Rô-bốt Thép": lịch sử / kết quả hiện tên boss). Code: `internal/game/gamedata/boss.go` (config, kiểm tra), `match/boss.go` (trận), `match/reward_boss.go` (thưởng), `matchmaker/plan_boss.go` (ghép), `room/boss.go` (tạo trận), `internal/api/boss` (`GET /boss/runs`), `mapgen/arenas.go` (arena).

### Boss mặc định (thiết kế riêng, tên tiếng Việt)

| bossId | Tên | Lv đề xuất | HP | Def | Damage | Thân | Arena | Lính | Kỹ năng |
|---|---|---|---|---|---|---|---|---|---|
| `steel_robot` | Rô-bốt Thép | 10 | 10.000 (v2 12.000) | 60 | 630 (v2 420) | 3 | `arena_steel_robot` Xưởng thép (steel) | `drone` Drone (spark, 650 HP, 160 dmg, thân 0,7; bomber bay 270 px / lượt, bắn 0,8 ×, đạn `drone_bolt`), đầu trận 2, tối đa 3; triệu hồi 3 | `shell_barrage` Mưa đạn pháo (rain 24 quả ± 840 px quanh vị trí trung bình của đội, 0,3 × dmg, nổ 75, hố 21, trần 1 ×, đạn `steel_shell`), `laser_beam` Tia laze nghiền (beam `sweep`: 5 tia ngang toàn map ở độ cao của đội, 0,9 ×, hồi 2) |
| `lava_crab` | Cua Dung Nham | 25 | 15.000 (v2 18.000) | 90 | 720 (v2 480) | 3,2 | `arena_lava_crab_mound` Gò dung nham (cave) | `crab` Cua con (tanko, 800 HP, 180 dmg, move energy 120; bomber đi bộ 240 px / lượt, bắn 0,8 ×, nổ 35 %, đạn `crab_spit`), đầu 2, tối đa 4; triệu hồi 4 | `fire_bombs` Bom dung nham (rain 10 quả khắp map, 0,455 ×, nổ 80, hố 27, trần 0,91 ×, `burn` 1 (50 %), mỗi chỗ nổ một vũng dung nham r 40 px, 120 HP / lượt, 1 vòng; hồi 1; đạn `magma_bomb`), `claw_slam` Cú kẹp đập (shockwave 0,6 ×, cú đập nổ 120, mỗi bước 150 px / 0,08 s, hố 14, ít nhất 0,4; hồi 1) |
| `ice_witch` | Phù Thuỷ Băng | 40 | 14.000 (052; v3 13.000, v2 30.000) | 80 | 780 (v2 520) | 2,8 | `arena_ice_witch_islands` Đảo băng (ice; v2 `arena_ice_witch_pillars`, v1 `arena_ice_witch`) | `snowman` Người tuyết (flora, 900 HP, 170 dmg; leaper, bắn 1,3 ×, chết thì nổ, nổ 15 % r 140, đạn `snowball`), đầu 3, tối đa 5; triệu hồi 5 | `ice_spikes` Gai băng (rain 14 quả ± 360, 0,325 ×, nổ 65, hố 15, trần 0,91 ×, `freeze` 1 (50 %); hồi 2; đạn `ice_spike`), `frost_beam` Tia băng (beam `radial`: 20 tia toả tròn đều 18°, 1,04 ×, `stun` 1 (50 %); hồi 3), `blizzard` Bão tuyết (rain 40 mảnh khắp map, bay ra từ vòng quanh phù thuỷ, 0,195 ×, nổ 50, hố 12, trần 0,78 ×, `freeze` 1 (50 %); hồi 3; đạn `ice_shard`) |

Rô-bốt Thép, Cua: 15 lượt boss tối đa; Phù thuỷ: 30 lượt, bay lượn. Cả ba: khiên 0,1 (còn lính sống: boss nhận 10 % damage), rơi khỏi map `clamp`, 3 lượt thưởng lớn mỗi ngày, `minionDamageFactor` 0,3, triệu hồi miễn phí hồi 3 vòng từ vòng 2. Cân bằng v3: xem "Boss v3" bên dưới.

### Config `config_bosses` (gamedata.BossConfig)

| Cột | Ý nghĩa |
|---|---|
| `boss_id`, `name`, `description`, `recommended_level`, `enabled`, `sort_order` | Thẻ ở tab Boss; tắt → boss, chế độ của nó không có trong `GET /gamedata`, queue không chạy (`INVALID_QUEUE`) |
| `map_id` | Arena (`config_maps`): spawn 0..3 đội, 4 boss, 5.. lính (`gamedata.BossSpawns`) |
| `hp`, `defense`, `damage` | Chỉ số boss; kỹ năng gây `damage × skill.damage` (rồi qua defense người trúng) |
| `body_size` | × thân người chơi: va chạm trong `physics.player_hit_radius × size` quanh tâm `shotOriginLift × size` trên chân; vụ nổ đo tới mép thân (khoảng cách tới tâm − size × hitRadius, như mọi đơn vị); tia laze / beam trúng trong `halfWidth + hitRadius × size`. Người chơi (size 1) không đổi gì |
| `turn_limit` | Số lượt boss; hết lượt thứ này mà boss còn sống → đội thua `boss_escaped` |
| `shielded_damage_factor` | Còn lính sống: boss nhận damage × hệ số (0..1) |
| `out_of_map` | `clamp` (mặc định): vụ nổ làm boss rơi khỏi đáy map thì boss đứng yên tại chỗ (không có event `fall`); `kill`: boss rơi chết như người chơi (đội thắng). Arena đặt boss trên nền cứng không phá được nên thực tế không xảy ra. Lính rơi khỏi map thì chết như người chơi (không ảnh hưởng trận) |
| `skills` (JSON) | `[{skillId, name, description, kind, weight, cooldown, target, …}]` (mọi kỹ năng có `status`: `statusChance` 0..1, 052, 0 = 0,5: mỗi người trúng một đòn tung một lần, dưới tỉ lệ thì dính hiệu ứng): `kind` `rain` (count ≤ 60, spread, damage, blast, crater, cap, status, statusTurns, lavaRadius, lavaTurns, lavaDamage, projectileKind, launchFrom), `beam` (damage, halfWidth, decay, status, statusTurns, pattern `""` / `radial` / `sweep`, count), `shockwave` (v3: damage, blast, crater, step, interval, minFactor), `summon` (cũ: minionId, count; không bao giờ dùng làm đòn đánh); `target` `random` / `nearest` / `lowest_hp` (rain còn `party`, `map`) |
| `summon` (JSON, v3) | `{minionId, count, cooldownRounds, firstRound}` hoặc NULL: triệu hồi miễn phí đầu lượt boss (NULL: theo kỹ năng summon cũ nếu có) |
| `minions` (JSON) | `[{minionId, name, characterId, hp, defense, damage, moveEnergy, bodySize, hitChance}]`: lính đánh bằng bot AI và súng của `characterId` (damage của lính thay damage súng: `damageBonus = damage − damage súng`), `hitChance` thay hit chance của tier bot (0 = tier) |
| `waves` (JSON) | `{initial: [{minionId, count}], maxAlive}` |
| `rewards` (JSON) | `{minionDamageFactor, bonus: bảng, normal: bảng}`, xem Thưởng |
| `daily_bonus_runs` | Số lượt đầu mỗi ngày dùng bảng `bonus` |

Admin **Bosses** (`/bosses`, `/bosses/edit?id=`, POST `/bosses/save`, `/bosses/delete`): sửa mọi trường (v2: ô `flying`, textarea `hover`, các trường lính mới trong JSON lính), cột JSON là textarea; lưu kiểm tra bằng `gamedata.ValidateBoss` (khoảng số, kind / target, status có lượt, summon / waves trỏ lính có thật, lính trỏ nhân vật có thật, drop trỏ `config_items` có thật, JSON không có field lạ) và arena có trong `config_maps`; sai → hiện lại form với lỗi, không lưu. Lưu bump `gamedata_revision` (trigger) → server reload. Xoá boss mà còn chế độ dùng queue của nó → từ chối. Game data loader bỏ (và log) boss không hợp lệ hoặc arena không load được.

Game Modes (`checkGameModes`): queue `boss:<id>` hợp lệ khi boss có và đang bật; số người 1..4 với queue boss, 1..2 với queue 2v2; `ranked` phải tắt. Cột queue giờ là ô text (không còn select).

### Hàng đợi và lobby

- `matchmaker.AllQueues()` = `ranked_2v2`, `casual_2v2`, rồi `boss:<id>` của mọi boss đang bật (theo game data hiện tại); `KnownQueue` nhận chúng. Sorted set `matchmaking:queue:boss:<id>`.
- Lobby: số người tối đa = `party_size` của chế độ lobby có queue đó (`matchmaker.PartySize`), chặn ở 4 với queue boss, 2 với 2v2 (`MaxPartySize`). `JoinLobby`, `InviteToLobby` (`LOBBY_FULL`), `RespondInvite`, lời mời bị huỷ khi đủ người: đều theo số này. `CreateLobby {queue: "boss:<id>"}` / `SetLobbyQueue {queue: "boss:<id>"}` như mọi queue; đổi lobby đang có 3–4 người sang queue 2v2 → `PARTY_TOO_LARGE` (`model.ErrPartyTooLarge`; cả `CreateLobby` khi lobby được dùng lại quá đông). `LobbyInvite.mode` = `boss:<id>`.
- Ghép (`planBossMatches`, mỗi tick): không xét rating; entry (party 1..4 hoặc solo) xếp theo lúc vào hàng; mỗi trận lấy entry cũ nhất còn lại rồi thêm các entry sau (theo thứ tự) còn vừa 4 người. Đủ 4 → bắt đầu ngay; thiếu → khi entry cũ nhất đã chờ `botFillAfter` (20 s) thì bot đồng đội lấp chỗ trống. Mọi người thật ở team 1; team 2 do room dựng (boss + lính). Map = arena của boss. Độ khó bot đồng đội theo tier của trung bình rating (như Đấu hạng); bot đa dạng theo `rules.bots` ở cấp trung bình level người thật.
- Arena không có trong vòng map PvP (`cmd/game matchMaps`, QuickPlay `quickPlayMapID`).

### Trận

- `MatchFound.mode` / `MatchStarted.mode` / `match_histories.mode` = `boss:<bossId>`. `MatchFound.teams[0]` = đội (người rồi bot), `teams[1]` = boss và lính đầu trận: card thêm `role` (`boss` / `minion`), `bossId`, `minionId`; boss `level` = level đề xuất, `rating` 0.
- Boss và lính là `BattlePlayerState` bình thường (`isBot: true`, team 2) cộng: `role` `"boss"` / `"minion"`, `bossId`, `minionId` (lính), `bodySize`, và ở người chơi `bossDamage` / `minionDamage` (damage gây cho boss / lính; omitempty). Boss: `playerId` `boss_<bossId>`, `characterId` `""`, `displayName` = tên boss, `maxMoveEnergy` 0. Lính: `playerId` `minion_<minionId>_<n>`, `characterId` = nhân vật nền (súng / AI; client vẽ theo `minionId`, không theo `characterId`), `damageBonus` có thể âm. Phe boss không có năng lượng skill.
- `MatchState` (MatchStarted, MatchStateSync) thêm (chỉ chế độ boss): `boss {bossId, playerId, round, turnLimit}` (`round` = số lượt boss đã bắt đầu, 0 trước lượt đầu), `bossShielded` (bool), `lavaZones [{zoneId, position{x,y}, radius, damage, expiresRound}]`.
- Thứ tự lượt: đội và lính xen kẽ (đội[0], lính[0], đội[1], lính[1], …, phần còn lại), boss cuối: mỗi vòng boss đi một lần. Lính mới gọi ra vào thứ tự (trước boss) từ vòng sau; lính chết bị bỏ qua (và bỏ khỏi thứ tự khi xếp lại). `TurnStarted` thêm `round` và `lavaZones` (chỉ chế độ boss).
- Lượt boss: `TurnStarted` (boss; `round` mới) → 1,2 s → `ActionSelected {playerId: boss, actionMode: "skill", skillId}` → 1,0 s → `ShotResolved {shooterId: boss, actionMode: "skill", skillId}`. Chọn kỹ năng theo `weight` trong các kỹ năng hết hồi (dùng ở vòng r thì dùng lại được từ vòng r + cooldown + 1); `summon` chỉ khi lính sống < `maxAlive`; không có gì → kỹ năng không-summon đầu tiên. Boss bị stun / freeze thì mất lượt như người chơi (vẫn tính một vòng).
- Sự kiện trong `ShotResolved` của boss (mọi `t` tính từ lúc bắn, `bossChargeTime` 0,6 s):
  - `rain`: `rain {t: 0.6, position (người bị nhắm), radius (spread), count}`, rồi mỗi quả là một projectile `kind: "missile"`, `skillId`, rơi thẳng từ trên map (y −60, 1500 px/s), cách nhau 0,08 s; mỗi quả `explode` / `damage` (cause `explosion`) / `die` / `fall` như thường; `status` (effect của kỹ năng) một lần mỗi người trúng; kỹ năng có dung nham: mỗi chỗ nổ thêm `lava {t, zoneId, position, radius, amount (HP mỗi lượt), turns (vòng)}`.
  - `beam`: `beam {t: 0.6, playerId: boss, from (tâm thân boss), to (mép map), duration 0.8}` qua tâm thân người bị nhắm, xuyên địa hình; mỗi người trúng `damage` cause `laser` lúc đầu tia tới (12000 px/s), rồi `status`.
  - `summon` (mới): `{t, type: "summon", playerId (minion_<id>_<n>), minionId, position, hp}`, cách nhau 0,3 s từ 0,6 s. State của lính: HP / def / damage / thân theo config (`GET /gamedata bosses[].minions`), team 2; `MatchStateSync` có đầy đủ.
  - `lava` (mới): vũng dung nham; người đội đứng trong vũng (|x − position.x| ≤ radius, |y − position.y| ≤ 60) đầu lượt mất `damage` HP (`PlayerDamaged` type `lava`, không qua defense, khiên chịu trước); hết sau lượt boss thứ `expiresRound`. Phe boss không bị dung nham.
  - Boss chết trong phát bắn: mọi lính còn sống chết theo, `die {playerId, cause: "boss_defeated"}` cùng `t` với `die` của boss.
- Khiên: còn lính sống → mọi damage boss nhận × `shieldedDamageFactor` (`damage` event mang số đã giảm; bot đồng đội cũng tính như vậy khi chọn mục tiêu). Đổi trạng thái (lính cuối chết, lính mới ra) → `BossShieldChanged {bossId, playerId, shielded}` gửi ngay sau `ShotResolved` / `PlayerDamaged` gây ra nó (client áp dụng khi phát lại xong).
- Kết thúc: boss chết → `winningTeam` 1, `result` `boss_defeated` (kể cả khi đội cũng chết cùng lúc). Cả đội chết → 2, `party_defeated`. Hết lượt boss thứ `turnLimit` (sau phát bắn của nó) → 2, `boss_escaped`. Rời trận / mất kết nối như mọi trận.
- `MatchEnded` (chế độ boss) thêm `result` và `boss {bossId, result, round, turnLimit, bossHp, bossMaxHp, bossDamage: {playerId: damage}}` (`bossDamage` gồm cả bot đồng đội: bảng xếp hạng damage); `mvpPlayerId` chỉ xét đội. `stats` như mọi trận (cả boss / lính).

### Thưởng (`processBossRewards`, một tx)

Mỗi người thật không rời trận: damage hiệu dụng = `bossDamage + minionDamageFactor × minionDamage`; bảng = `bonus` cho `daily_bonus_runs` lượt đầu trong ngày **với boss đó**, sau đó `normal`:

```
gold  = min(baseGold + hiệu dụng / 1000 × goldPer1000Damage, maxGold) + winGold (nếu thắng)   → coin
exp   = min(baseExp  + hiệu dụng / 1000 × expPer1000Damage,  maxExp)  + winExp  (nếu thắng)   → EXP tài khoản + nhân vật
rolls = min(floor(hiệu dụng / damagePerDropRoll), maxDropRolls) + winDropRolls (nếu thắng)
mỗi roll: một drop theo weight (hoặc không gì với emptyWeight), số lượng min..max
```

Rô-bốt Thép, bảng bonus: 100 + 60 / 1000 damage (tối đa 1.500) + 400 thắng coin; 40 + 25 / 1000 (tối đa 600) + 120 EXP; 1 roll / 2.000 damage (tối đa 4) + 1 thắng (medkit, power_shot, teleport, drill_bomb, freeze_bomb). Bảng normal ~¼. Cua ×1,33, Phù thuỷ ×1,6.

- Lượt trong ngày: `player_boss_runs (player_id, boss_id, day, runs)`, `day` = ngày UTC+7 như nhiệm vụ ngày (`mission.PeriodsAt`, hết lúc 17:00 UTC). Khoá dòng `FOR UPDATE`, tạo / tăng và kiểm tra `RowsAffected`; lượt thứ N dùng bonus khi N ≤ `daily_bonus_runs`.
- Coin: `economy_transactions` source `boss_reward`, ref = boss id; item: `inventory_items` source `boss_reward` (`milestone.GrantRewardItemTx`); lịch sử `match_histories` mode `boss:<id>`, `rating_change` 0; trận được tính cho mốc thưởng / nhiệm vụ ngày như trận ghép (≥ 3 lượt hành động). Mọi đọc / ghi qua tx, không query Pool.
- Rời trận: không gì, lượt không bị tính (`bonusRunsLeft` = số còn lại hiện tại). Bot / boss / lính: không thưởng, không ghi dòng nào.
- `MatchEnded.rewards[playerId]` thêm `bossDamage`, `minionDamage`, `bonusRun` (bool: lượt này dùng bảng bonus), `bonusRunsLeft` (còn lại hôm nay sau lượt này), `items [{itemId, quantity}]`; `ratingChange` 0, không có field rank.

```json
{"winningTeam":1,"result":"boss_defeated",
 "boss":{"bossId":"steel_robot","result":"boss_defeated","round":9,"turnLimit":15,"bossHp":0,"bossMaxHp":12000,
   "bossDamage":{"p1":9000,"bot_1a2b3c4d":2400}},
 "rewards":{"p1":{"playerId":"p1","expGained":393,"coinGained":1058,"ratingChange":0,"levelUp":false,"newLevel":12,
   "charLevelUp":false,"charNewLevel":0,"charLevelsGained":0,
   "bossDamage":9000,"minionDamage":1000,"bonusRun":true,"bonusRunsLeft":2,"items":[{"itemId":"medkit","quantity":2}]}},
 "stats":{...},"mvpPlayerId":"p1"}
```

### `GET /boss/runs` (Bearer)

`{day, resetAt, bosses: [{bossId, dailyBonusRuns, runsToday, bonusRunsLeft}]}`: ngày UTC+7 hiện tại, lúc hết (RFC3339 UTC), mỗi boss đang bật theo thứ tự hiển thị (`bonusRunsLeft = max(dailyBonusRuns − runsToday, 0)`). `internal/api/boss` (handler → service → repository).

### Arena (`internal/game/mapgen/arenas.go`, migration 045)

150 × 70 ô như map PvP, không lật gương: sàn đội bên trái (đất mềm trên nền cứng, 4 spawn), giữa là chỗ của lính, bên phải bục lớn bằng vật liệu cứng cho boss (cách đội ~1.400–1.800 px). `go run ./cmd/mapgen` ghi `045_boss_arenas` (`INSERT … ON CONFLICT DO NOTHING`: chạy lại giữ chỉnh sửa trong map editor) và thêm arena vào `configs/maps.yaml`. Test: arena hợp lệ, mọi spawn đứng được, nền dưới boss cứng, mỗi spawn đội có phát gây damage cho boss.

### Boss v2: boss bay, lính bomber, arena mới (migration 047 / 048 / 049, `match/boss_v2.go`)

- **Lính** (`minions` JSON, `gamedata.MinionConfig`, số 0 / trống = mặc định, `Normalized`): `behavior` `shooter` (mặc định) / `bomber`; `movement` `walk` (mặc định) / `none` / `fly`; `shotDamageFactor` (0: bomber 0,35, shooter 1: lính bắn `damage × hệ số`); `detonateRange` 70; `walkRange` 80 (px đi tối đa mỗi lượt, kể cả khi move energy cho đi xa hơn); `flyRange` 90; `hoverHeight` 60; `flySpeed` 220 px/s; `explodeOnDeath`; `explodeDamageOf` `target` (mặc định) / `self`; `explodeDamagePct` 50; `explodeRadius` 80; `explodeCrater` 20. `BattlePlayerState` của boss / lính thêm `flying`, `behavior`, `movement` (omitempty).
- **Lượt của lính "mover"** (bomber, hoặc lính bay): `TurnStarted` → 0,6 s → di chuyển về phía người đội gần nhất: đi bộ (`PlayerMoved`, như người chơi; tối đa `walkRange`, `walkChecked` nên không bao giờ đi vào hố xuống đáy map) hoặc bay thẳng (`UnitFlew {playerId, from, to, duration}`, `duration` = quãng / `flySpeed`, tối đa `flyRange`, đậu cách mặt đất bên dưới `hoverHeight` px, giữ trong map; server đã ở `to`) → khi bước đi đã chạy xong + 0,4 s: bomber KẾT THÚC bước đi cách một người đội ≤ `detonateRange` (chân tới chân) thì tự nổ, không thì `PlayerAiming` (0,4 s) rồi bắn phát yếu (bot AI `bestShot`, hit chance của lính). Lính `shooter` + `none` / `walk` vẫn chơi bot AI như cũ (`none`: move energy 0, đứng yên).
- **Tự nổ**: `ShotResolved {shooterId: lính, actionMode: "self_destruct", projectiles: []}`, t = 0,2: `explode {playerId: lính, position (chân lính), radius = hố (explodeCrater, như mọi explode: client khoét theo nó), crater: "bowl", terrainVersion, blastRadius (explodeRadius)}`, `damage {cause: "self_destruct"}` mỗi người đội trúng, `die {lính, cause: "self_destruct"}`, rồi `fall` / `die` như thường.
- **Damage vụ nổ**: người trúng trong `explodeRadius` (khoảng cách tới mép thân: tâm thân − bán kính trúng × thân) mất `explodeDamagePct` % máu tối đa của chính họ (`target`) hoặc của lính (`self`), × (1 − 0,5 × khoảng cách / bán kính): đủ ở tâm, một nửa ở mép; không qua defense, khiên chịu trước.
- **Nổ khi chết** (`explodeOnDeath`): lính chết vì bất cứ gì trừ tự nổ và `boss_defeated` thì nổ tại chỗ, ngay sau `die` của nó trong cùng `ShotResolved` (`explode` + `damage {cause: "death_blast"}`), trúng đội và các lính khác (không trúng boss), nên nổ dây chuyền (`deathBlasts`, gắn ở `broadcastShot`; `terrainVersion` các explode được đánh lại theo thứ tự thời gian). Damage lên lính tính `minionDamage` cho người bắn. Lính chết vì bỏng đầu lượt: một `ShotResolved {shooterId: lính, actionMode: "death_blast"}` riêng.
- **Boss bay** (`flying`, `hover {minX, maxX, minY, maxY, minMove (200), flySpeed (220)}`, cột mới): không bao giờ rơi (`settleFalls` bỏ qua mọi đơn vị bay; hố nổ bên dưới không làm nó rơi). Có `hover`: đầu mỗi lượt `TurnStarted` → 0,6 s → `UnitFlew` tới một điểm ngẫu nhiên trong vùng, cách chỗ cũ ≥ `minMove` → khi bay xong + 0,4 s `ActionSelected` → 1,0 s `ShotResolved` như cũ. Bắt đầu trận ở cột giữa, kẹp trong vùng. Phù Thuỷ Băng: vùng x 400–2000, y 320–430 (049: minY 200 → 320, ở y 200 thân bà lên tới ~y 40, nằm dưới thanh HP boss của client).
- **summon** event mang đủ đơn vị mới: `{type: "summon", playerId, minionId, position, hp, maxHp, displayName, teamId, bodySize, flying, behavior, movement}`. Lính hiện ở chỗ spawn trống gần boss nhất (arena cột băng: cột trống ngẫu nhiên), lính bay ở độ cao `hoverHeight`. Kỹ năng summon chỉ được chọn khi đợt lính trước (đợt đầu là `waves.initial`) đã chết hết và số lính sống < `maxAlive` (3), cộng hồi chiêu 3.
- **Arena** (`mapgen.ArenasV2`, migration 047; Rô-bốt Thép giữ `arena_steel_robot`): `arena_lava_crab_mound` Gò dung nham: nền liền từ mép tới mép (5 hàng đất mềm trên lõi bazan cứng đến đáy: hố nổ không bao giờ thủng xuống đáy), đồi thấp, gò bazan cứng ở giữa (mặt phẳng hàng 40, cột 67–82) cho boss; spawn đội 2 trái + 2 phải (x 200, 2200, 456, 1944), lính trên sườn gò (x 936, 1464, y 764: cua con cách thân cua boss vẽ ra (x 1192, ~158 px mỗi bên) hơn 40 px; 047 để trên mặt gò x 1080 / 1320, lọt trong thân boss, migration 049 dời xuống) và chân gò (872, 1528). Migration 047 đóng băng như đã chạy (`cmd/mapgen` không sinh lại), 049 đưa spawn về `mapgen.ArenasV2`. `arena_ice_witch_pillars` Cột băng: 9 cột băng cứng phủ tuyết (rộng 144 px, cách nhau 112 px, khe hở xuống đáy map), đỉnh ở x 184 + 256 k, hàng 44 / 39 / 46 / 41 / 37 / 41 / 46 / 39 / 44; mọi spawn đội / lính có `"set": "pillar"` (`gamedata.ArenaPillars`): đội đứng trên các cột khác nhau chọn ngẫu nhiên lúc bắt đầu trận (`gamedata.BossPartySpawns`), người tuyết trên cột trống. `IsBossArena` coi mọi map id bắt đầu bằng `arena_` là arena (arena v1 không còn boss nào dùng vẫn không vào vòng PvP). Bản đồ admin giữ `set` khi lưu.
- **Bot đánh thử** (4 bot cấp 1, 12 trận mỗi boss): Rô-bốt Thép thắng 12/12 (vòng ~11), Cua thắng 0/12 (thoát, còn ~7.600 HP), Phù thuỷ thắng 1/12 (vòng ~29, còn ~10.600 / 30.000 HP).

### Boss v3: triệu hồi miễn phí, kỹ năng diện rộng, người tuyết nhảy, đảo băng (migration 050 / 051, `match/boss_v3.go`)

- **Triệu hồi miễn phí** (`config_bosses.summon` `{minionId, count, cooldownRounds, firstRound}`, `gamedata.BossSummon`; cột mới, NULL = không, hoặc theo kỹ năng `summon` cũ đầu tiên: `SummonConfig`). Đầu lượt boss, khi đến đợt (vòng ≥ `firstRound`, đợt lính trước đã chết hết, đã qua hơn `cooldownRounds` vòng từ lần gọi trước, còn dưới `waves.maxAlive`): `TurnStarted` → 0,6 s → `ShotResolved {shooterId: boss, actionMode: "summon", projectiles: [], events: [summon…]}` (summon event như v2, từ t 0,6, cách nhau 0,3 s; không kết thúc lượt; `BossShieldChanged` sau đó) → khi phát xong + 0,4 s → lượt đánh như cũ (boss bay lượn: `UnitFlew`, rồi `ActionSelected` → `ShotResolved` kỹ năng; boss đứng: `ActionSelected` ngay). Boss bị `stun` / `freeze`: vẫn triệu hồi rồi mất đòn đánh (lượt kết thúc sau `skipTurnDelaySec`). Không đến đợt: lượt boss như v2. Kỹ năng `summon` không còn trong danh sách đòn đánh (`AttackSkills`), `GET /gamedata` bỏ chúng khỏi `skills` và thêm `summon`.
- **Kỹ năng mới** (chỉ dùng event có sẵn):
  - `rain` `target: "map"`: rơi khắp map (`rain.position` = tâm map, `radius` = nửa bề rộng map khi `spread` 0); `target: "party"`: quanh vị trí trung bình của đội. Cột rơi chia đều (mỗi quả một khoảng bằng nhau, lệch ngẫu nhiên trong khoảng), nên 40 mảnh bão tuyết phủ mọi cột 120 px.
  - `rain` `launchFrom: "boss"` (bão tuyết): mỗi mảnh rời một vòng bán kính `bodySize × 30` px quanh tâm thân boss (góc 360° / count · k, cả vòng xoay ngẫu nhiên), bay cong lên (đường cong bậc hai, 10 điểm path đầu, 0,4–1,1 s, bay qua địa hình) tới (x cột, y 20), rồi rơi thẳng xuống như rain thường (chỉ phần rơi va chạm). `launchDelay` = lúc rời vòng; `path[10]` là điểm bắt đầu rơi.
  - `projectileKind` (rain): `kind` của projectile trong `ShotResolved`: `steel_shell` (Mưa đạn pháo), `magma_bomb` (Bom dung nham), `ice_spike` (Gai băng), `ice_shard` (Bão tuyết); trống = `missile`. Lính: `minions[].projectileKind` là `kind` phát bắn thường của nó (`drone_bolt`, `crab_spit`, `snowball`; trống = phát bắn thường `""`). Tia (beam) và sóng chấn động không có projectile.
  - `beam` `pattern: "radial"` (Tia băng): `count` tia (20) cùng `t` 0,6 từ tâm thân boss, cách đều 360° / count (18°), cả quạt xoay một góc ngẫu nhiên, mỗi tia `beam {playerId: boss, from: tâm, to: chỗ tia chạm mép map, duration 0,8}`, xuyên địa hình như laze. Người trúng (trong `halfWidth + hitRadius × thân` của bất kỳ tia nào) mất damage MỘT lần (lúc tia đầu tiên tới) rồi `status` (`stun` 1).
  - `beam` `pattern: "sweep"` (Tia laze nghiền): `count` tia (5) ngang toàn map (`from` mép phía boss, `to` mép bên kia, cùng y), ở độ cao tâm thân của từng người đội, các tia còn lại chia đều trong khoảng y của đội ± 90 px; mỗi người trúng một lần, damage nặng (`cause` `laser`).
  - `shockwave` (Cú kẹp đập): `explode {position: chân boss, radius: crater, blastRadius: blast}` lúc 0,6 s, rồi `explode` trên mặt đất (y = mặt đất trên cùng ở x đó; khoảng không thì bỏ) mỗi `step` px (150) về hai phía tới hai mép map (bước cuối ở x 2 / bề rộng − 2), lúc 0,6 + k · `interval` (0,08) s, mỗi cái khoét hố `crater` px (14); các bước được khoét theo thứ tự phát (bước k bên trái, bước k bên phải, rồi k + 1), nên `terrainVersion` của các `explode` tăng dần theo `t` (trước bản sửa này cả bên trái được khoét trước: version bên phải nhảy cóc, client tưởng địa hình lệch, gửi `Reconnect`, `MatchStateSync` bỏ phần còn lại của replay cùng các `damage`, nên sóng "không gây sát thương"; nay `broadcastShot` cũng luôn đánh lại version theo thứ tự thời gian cho mọi phát bắn). Mọi người đội không bay mất `damage × boss damage × max(minFactor, 1 − khoảng cách x / khoảng cách tới mép xa hơn)` (qua defense, `cause` `explosion`) lúc sóng tới x của họ; không hất tung (không `fall` riêng), các cú rơi do hố được tính sau explode cuối.
- **Người tuyết nhảy** (`behavior: "leaper"`, `leapRange` px tối đa (0 = cả map), `leapDetonateNextTurn` (mặc định true), `shootAfterLeap` (mặc định false)). Lượt của nó (lượt "mover": `TurnStarted` → 0,6 s → di chuyển → xong + 0,4 s → đòn):
  1. vừa nhảy ở lượt trước (`leapt`) và còn người đội cách ≤ `detonateRange` (chân tới chân): tự nổ (`ShotResolved` `self_destruct` như bomber, số nổ của nó);
  2. không ai trong `detonateRange`: nhảy tới cạnh người đội gần nhất: `UnitJumped {playerId, from {x,y}, to {x,y}, duration, height}` (`duration` 0,6–1,5 s theo quãng, `height` = đỉnh cung cao hơn đầu cao hơn 80–320 px; server đã ở `to`), chỗ đáp là đất vững (đứng được, có đất 8 px hai bên, cùng đảo: lệch y ≤ 40 px) 30–60 px cạnh người đó và trong `detonateRange` của họ, không bao giờ vào khoảng không; không có chỗ thì bắn. Nhảy xong: hết lượt (với `shootAfterLeap` thì bắn tiếp);
  3. còn lại: bắn như cũ.
  `BattlePlayerState.leapt` (omitempty; cả `MatchStateSync`) = true từ lúc nhảy tới hết lượt sau của nó (kể cả lượt bị đóng băng), để client cảnh báo. Bot đồng đội ở trong `detonateRange + 15` px của người tuyết `leapt` sẽ đi ra xa (đường đi an toàn ngắn nhất trong move energy) trước khi bắn.
- **Lính v3**: drone `flyRange` 270, cua `walkRange` 240 (move energy 120), bomber bắn 0,8 × (mặc định `BomberShotDamageFactor` 0,8), người tuyết 1,3 ×; đợt triệu hồi 3 / 4 / 5 = `maxAlive`. Từ chỗ xuất hiện, lính bomber cần ≥ 2 lượt để tới đội (test).
- **Arena đảo băng** (`mapgen.ArenasV3`, migration 051, `arena_ice_witch_islands` Đảo băng): 12 đảo băng lơ lửng (`mapgen.WitchIslets`: cột c0–c1, hàng đỉnh): {3–12, 40}, {17–26, 50}, {22–33, 33}, {38–51, 44}, {55–64, 56}, {60–69, 34}, {72–85, 47}, {88–97, 36}, {104–117, 45}, {120–129, 52}, {125–134, 33}, {138–147, 42}; mỗi đảo 5 hàng băng cứng, đáy vát 3 hàng, phủ 2 hàng tuyết mềm; đảo 14 ô (đảo lớn) ở cột 38, 72, 104. Không gì nối xuống đáy (140 px dưới cùng là trời trống): rơi = chết `out_of_map`. Spawn: đội 4 (dự phòng: đảo 1, 12, 4, 9), boss trên đảo giữa (x 1256), lính 8 đảo còn lại; mọi spawn trừ boss `"set": "pillar"` (giữ tên tag cũ): đội trên 4 đảo khác nhau ngẫu nhiên, người tuyết trên đảo trống (đảo "đã có người" xét theo khoảng cách 2 chiều < 80 px). Vùng bay của phù thuỷ giữ x 400–2000, y 320–430 (đỉnh đảo cao nhất y 528). `arena_ice_witch_pillars` vẫn còn trong `config_maps`, không boss nào dùng. Xem trước: `go run ./cmd/mapgen -preview DIR` ghi `DIR/arena_ice_witch_islands.png`.
- **Cân bằng** (`TestBossBalanceSim`, `BOSS_SIM=<số trận> go test ./internal/game/match -run TestBossBalanceSim -v`, có `TEST_POSTGRES_DSN` thì bot mang súng của `config_equipment_items`): 4 bot ở level đề xuất (nhân vật khác nhau, điểm chia đều, súng tốt nhất của level, medkit + teleport, tier gold), 30 trận mỗi boss: Rô-bốt Thép thắng 13 / 30 (43 %, vòng ~12), Cua 13 / 30 (43 %, vòng ~13), Phù thuỷ 14 / 30 (47 %, vòng ~20). Để đạt mức này HP giảm (10.000 / 15.000 / 13.000) và trần một số kỹ năng / vụ nổ lính được hạ (cua nổ 35 %, người tuyết nổ 15 %).
- **Sau v3 (migration 052 `052_boss_status_items`)**:
  - **Hiệu ứng theo tỉ lệ**: mọi kỹ năng boss có `status` (Bom dung nham `burn`, Gai băng / Bão tuyết `freeze`, Tia băng `stun`) có `statusChance` (0..1, 0 = `gamedata.DefaultBossStatusChance` 0,5): mỗi người đội bị một đòn trúng tung MỘT lần (mưa nhiều quả trúng cùng người vẫn một lần), dưới tỉ lệ thì dính hiệu ứng (event `status`), không thì chỉ mất máu. Số ngẫu nhiên lấy theo trận và lượt (`turnRNG("status")`: replay ra như nhau); test cố định được bằng `bossRuntime.statusRollFix`. Vũng dung nham vẫn để lại ở mọi chỗ nổ.
  - **Đổi lại damage × 1,3** (cả trần của mưa): `fire_bombs` 0,455 × (trần 0,91), `ice_spikes` 0,325 × (trần 0,91), `frost_beam` 1,04 ×, `blizzard` 0,195 × (trần 0,78); mô tả kỹ năng ghi "có 50 %". HP phù thuỷ 13.000 → 14.000.
  - **Cân bằng 052** (30 trận mỗi boss, bot gold có súng DB): trước (v3, hiệu ứng luôn dính) Rô-bốt Thép 17 / 30 (57 %), Cua 11 / 30 (37 %), Phù thuỷ 15 / 30 (50 %); sau (50 %, × 1,3, phù thuỷ 14.000 HP) Rô-bốt Thép 13 / 30 (43 %, không đổi cấu hình: dao động mẫu 43–57 %), Cua 14 / 30 (47 %), Phù thuỷ 15 / 30 (50 %); với 13.000 HP phù thuỷ thắng 18 / 30 (60 %).
  - **6 vật phẩm trong trận boss**: `config_game_modes.max_items` (INT 0..20, NULL = `rules.items maxLoadout` 3; seed 6 cho các dòng lobby `boss:`; admin Texts → Game Modes cột "Số vật phẩm", trống = NULL). `gamedata.Snapshot.ModeMaxItems(queue)` (dòng lobby có queue đó, rồi dòng mode id đó) = `room.MaxLoadoutItems(mode)`, áp dụng ở: lobby `UpdateLoadout` theo queue của lobby (`room.ValidateLoadout(…, queue, items)`, quá thì `Error LOADOUT_INVALID "You can bring at most N items"`), `SetLobbyQueue` / `StartQueue` (cắt còn N món đầu, không đổi loadout đã lưu), phòng ghép trận (`buildRankedRoster` cắt theo mode), `SelectItems` của phòng tự tạo (theo `RoomState.mode`) và giữ chỗ vật phẩm (`reservePlayerItems`: quá thì `LoadoutError`, trận ghép cho người đó vào không vật phẩm; khoá dòng `FOR UPDATE` như cũ). Bot vẫn theo `maxLoadout`. `GET /gamedata` `modes.lobby[].maxItems`.
- **Migration 050** `050_boss_mode_v3`: thêm cột `summon` (một lần, kèm giá trị cho 3 boss), cập nhật skills / minions / waves / description / damage / hp và arena phù thuỷ, mỗi giá trị chỉ khi còn là giá trị v2 (chạy lại giữ chỉnh sửa admin); down trả về v2 và bỏ cột. Admin **Bosses** thêm ô JSON "Triệu hồi" (`summon`, kiểm tra `ValidateBoss`: lính có thật, count 1..12, cooldownRounds 0..50, firstRound 0..200) và các trường mới trong JSON kỹ năng / lính (`pattern`, `count`, `step`, `interval`, `minFactor`, `projectileKind`, `launchFrom`, `leapRange`, `leapDetonateNextTurn`, `shootAfterLeap`; `projectileKind` chỉ gồm a-z, 0-9, _).

- **Sau 052 (migration 053 `053_snowman_death_blast`)**:
  - **Vụ nổ đo tới mép thân, mọi đơn vị** (`blastDistance`, `match/body.go`): khoảng cách = max(0, |điểm nổ − tâm thân| − `player_hit_radius × bodySize`), đúng vòng tròn đạn va chạm, cho người chơi, lính và boss, cho mọi vụ nổ (đạn, skill, mưa của boss, lính tự nổ / nổ khi chết; bot AI ước lượng cùng cách). Lỗi cũ: đạn đo tới CHÂN (thân ≤ 1) hoặc tới "mép thân trừ một thân người thường" (boss), trong khi đạn nổ trên mặt thân: đạn rơi trúng đầu một người tuyết (bodySize 0,85: bán kính 20,4 px, tâm 17 px trên chân) cách chân người tuyết đứng sát bên ~55 px > bán kính 40 của Basic Rocket, nên chỉ con bị trúng mất máu (hai người chơi đứng sát nhau cũng vậy). Nay hai thân sát nhau cùng ăn nổ, cùng khoảng cách thì cùng damage; người bị trúng trực tiếp vẫn khoảng cách 0 (đủ damage). Hệ quả cân bằng PvP: tầm nổ hiệu dụng xa hơn ~24 px (bán kính thân), nổ trên đất cạnh chân mạnh hơn (đất cách chân 20 px, bán kính 40: 50 % → ~89 %).
  - **Người tuyết nổ khi chết yếu hơn tự nổ**: `minions[].deathBlastDamageFactor` (× `explodeDamagePct` chỉ cho vụ nổ khi chết `death_blast`, không áp dụng cho tự nổ `self_destruct`; 0 / thiếu = 1, `ValidateBoss` 0..2; luôn được lưu, kể cả 0, khi admin lưu). Người tuyết 0,5: tự nổ vẫn 15 % máu tối đa của người trúng, nổ khi chết 7,5 %. `GET /gamedata` `bosses[].minions[].deathBlastDamageFactor` (đã điền mặc định). Migration 053 chỉ đặt 0,5 cho người tuyết của Phù Thuỷ Băng khi trường còn thiếu (chạy lại giữ chỉnh sửa admin); down bỏ trường khi còn là 0,5.
  - **Cân bằng 053** (`TestBossBalanceSim`, 60 trận mỗi boss, bot gold có súng DB, trước → sau): Phù thuỷ 29 / 60 (48 %) → 33 / 60 (55 %, vòng ~17; người đội chết vì nổ khi chết của người tuyết 18 → 8, vì tự nổ 38 → 20), Rô-bốt Thép 35 / 60 (58 %) → 24 / 60 (40 %; mưa đạn pháo của boss cũng đo tới mép thân nên trúng đội nhiều hơn), Cua 33 / 60 (55 %) → 36 / 60 (60 %). Cả ba trong 40–60 %, không chỉnh thêm số.

### Migration 046

`config_bosses` (+ trigger revision, seed 3 boss = `gamedata.DefaultBosses()`, `ON CONFLICT DO NOTHING`), 6 dòng `config_game_modes` (3 lobby `boss_<id>` sort 11–13, 3 match `boss:<id>`), `player_boss_runs`. Chạy lại giữ chỉnh sửa admin (`TestBossMigration`); down xoá các dòng chế độ và hai bảng.

---

## 9. Admin Dashboard

### Web UI Routes

| Method | Path | Mô tả |
|--------|------|-------|
| GET | `/` | Dashboard (active rooms, total players) |
| GET/POST | `/characters/*` | CRUD nhân vật (trang chi tiết: hệ ngũ hành, 054) |
| GET/POST | `/weapons/*` | CRUD vũ khí (`bounces` 0..5, 054) |
| GET/POST | `/skills/*` | CRUD skills |
| GET/POST | `/items/*` | CRUD items |
| GET/POST | `/maps/*` | CRUD maps |
| GET/POST | `/physics` | Mọi dòng game_settings (trừ `rules.*`), kiểm tra kiểu / khoảng |
| GET/POST | `/gamedata`, `/gamedata/reload` | Version game data (admin, từng server game / api, lỗi reload), payload `/gamedata`; "Tải lại cấu hình" tăng revision + báo mọi server |
| GET/POST | `/rules` | Game Rules: các dòng số (physics, lượt, di chuyển, rơi, trang bị) và mọi nhóm `rules.*`, kiểm tra kiểu / khoảng / bắt buộc |
| GET/POST | `/skills/tuning` | Mẫu mô tả + thông số kỹ năng (`config_skills.params`), xem trước số và mô tả theo cấp súng |
| GET/POST | `/texts/{table}` | Sửa tại chỗ: status-effects (tên, marker, banner, xấu / mất lượt / chặn di chuyển), rank-tiers (tên, khoảng điểm, kiểm tra bands), equipment-tiers, equipment-slots, elements (054: tên hệ, hệ bị khắc; không thêm / xoá, kiểm tra không tự khắc), gun-kinds, type-words, game-modes (category, icon, queue `ranked_2v2` / `casual_2v2` / `boss:<id>` của boss đang bật, số người 1..2 / 1..4 với boss, xếp hạng, bật; `checkGameModes`), mode-categories, tips |
| GET/POST | `/bosses`, `/bosses/edit`, `/bosses/save`, `/bosses/delete` | Boss: sửa mọi trường của `config_bosses` (JSON là textarea, kiểm tra `gamedata.ValidateBoss`), xoá khi không chế độ nào dùng (§8a) |
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
| DailyResetJob | 24 giờ | Không làm gì: nhiệm vụ ngày reset lười theo `period` (§7 Nhiệm vụ ngày) |
| AccountDeletionJob | 1 giờ | Anonymize + mark deleted cho accounts quá grace period |

---

## 11. Database Schema

30 tables, 10 domain groups. Chi tiết: `docs/database-erd.html` (interactive) hoặc `docs/database-erd.md`.

### Core tables

| Table | PK | Mô tả |
|-------|-----|-------|
| `accounts` | `account_id` | Tài khoản (guest/google/apple, role, status) |
| `auth_identities` | `identity_id` | Login providers (1 account : N identities) |
| `player_profiles` | `player_id` | Profile (name, level, coin, gem, `equipped_title_id`) — **central entity** |
| `player_loadouts` | `player_id` | Loadout mặc định (character + items) |

### Economy & Inventory

| Table | PK | Mô tả |
|-------|-----|-------|
| `player_characters` | `(player_id, character_id)` | Nhân vật đã unlock |
| `inventory_items` | `(player_id, item_id)` | Items đang có |
| `inventory_reservations` | `reservation_id` | Items reserved cho trận đấu |
| `economy_transactions` | `transaction_id` | Ledger coin/gem |
| `missions` / `mission_progress` | `mission_id` / `(player_id, mission_id)` | Nhiệm vụ; mốc thưởng là `type = 'milestone'`, nhiệm vụ ngày `daily`, rương năng động `mission_chest`; đã nhận = `is_claimed`, `period` = ngày / tuần ISO (UTC+7) của dòng |
| `player_mission_activity` | `(player_id, track)` | Điểm năng động ngày / tuần với `period` của nó |
| `player_stat_counters` | `(player_id, counter)` | Counter cho mốc thưởng (trận, thắng, thắng theo nhân vật, đỉnh cường hoá, đỉnh rank) |
| `config_titles` | `title_id` | Danh hiệu: tên, rarity, thứ tự, mô tả cách đạt, bật/tắt |
| `player_titles` | `(player_id, title_id)` | Danh hiệu người chơi sở hữu, `obtained_at` |
| `player_boss_runs` | `(player_id, boss_id, day)` | 046: số lượt đánh boss xong trong ngày UTC+7 (`runs`); `daily_bonus_runs` lượt đầu dùng bảng thưởng lớn (§8a) |

### Equipment

| Table | PK | Mô tả |
|-------|-----|-------|
| `config_equipment_items` | `item_id` | Catalog trang bị (normal/crafted, level yêu cầu, chỉ số, giá) |
| `config_upgrade_rates` / `config_stones` / `config_gems` / `config_set_bonuses` | | Cường hoá, đá, ngọc, set bonus |
| `config_materials` / `config_crafting_recipes` | `material_id` / `recipe_id` | Nguyên liệu và công thức chế tạo |
| `player_equipment` | `equipment_id` | Trang bị sở hữu; unique (player, `equipped_on`, slot) khi đang mặc |
| `player_stones` / `player_gems` / `player_materials` | | Đá, ngọc, nguyên liệu của người chơi |
| `equipment_upgrade_log` | `log_id` | Lịch sử cường hoá (dùng để hoàn đá khi tách) |
| `equipment_purchases` | `(player_id, idempotency_key)` | Lần mua trang bị có idempotency key → `equipment_id` để replay |

### Ranking

| Table | PK | Mô tả |
|-------|-----|-------|
| `rank_seasons` | `season_id` | Mùa giải (status: upcoming→active→ended→closed); tự tạo "Mùa N" khi chưa có mùa active |
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
| `game_settings` | `key` | Key-value config (matchmaking, elo, bot_difficulty); 041: `rules.*` (một dòng JSON mỗi nhóm luật, `gamedata.RuleGroups`) |
| `config_characters/weapons/skills/items/maps` | `*_id` | Game data (admin editable); `config_maps.theme` (036); 040: `config_characters.role_name / playstyle / gun_kind / sort_order`, `config_items.display_name / sort_order`, `config_skills.description_template` |
| `config_status_effects`, `config_rank_tiers`, `config_equipment_tiers`, `config_game_modes`, `config_tips` | `effect_id` / `tier` / `tier` / `mode_id` / `tip_id` | 040: text tiếng Việt + thứ tự hiển thị cho `GET /gamedata`; 044: `config_game_modes.category / icon / party_size / ranked / enabled` |
| `config_bosses` | `boss_id` | 046: boss (chỉ số, kỹ năng / lính / đợt / thưởng JSON, lượt thưởng lớn mỗi ngày; §8a; admin Bosses) |
| `config_mode_categories` | `category_id` | 044: các mục của màn Chọn chế độ (`name`, `sort_order`; `GET /gamedata` `modes.categories`) |
| `config_brick_types` | `brick_type_id` | Brick của map tile: tên, `destructible`, `border` polygon, `color`; id 1..19 do 036 tạo |
| `config_gun_kinds`, `config_equipment_slots`, `config_item_type_words` | `gun_kind` / `slot` / `word` | 041: tên loại súng, tên ô trang bị, chữ dịch loại trong tên trang bị "<Tier> <Type>" (`GET /gamedata` `gunKinds`, `equipment.slots`, `equipment.typeWords`) |
| `gamedata_revision` | `id` (=1) | 041: revision game data; trigger statement-level trên mọi bảng config_* và game_settings tăng nó mỗi lần ghi |
| `gamedata_versions` | `version` | 041: payload `GET /gamedata` (text JSON) của mỗi version server đã publish (`?version=`), xoá sau 30 ngày ngoài 50 bản mới nhất |

---

## 12. Redis Data Structures

| Key Pattern | Type | TTL | Mô tả |
|-------------|------|-----|-------|
| `rooms:active` | Hash | — | Room registry (field=roomID, value=metadata JSON) |
| `matchmaking:queue:<queue>` | Sorted Set | — | Matchmaking queue mỗi hàng đợi (`ranked_2v2`, `casual_2v2`; score=rating); `matchmaking:queue:2v2` cũ được chuyển sang `ranked_2v2` |
| `matchmaking:entry:{id}` | String (JSON) | 120s | Queue entry detail |
| `matchmaking:player:{id}` | String | 120s | Player → entry mapping |
| `matchmaking:leader` | String | 10s | Leader election lock |
| `leaderboard:current` | Sorted Set | — | Current season leaderboard |
| `session:{token}` | String | — | Refresh token blacklist |
| `feature:{flag}` | String | — | Feature flags |
| `idempotency:{key}` | String | 300s | Request deduplication |
| `gamedata:changed` | Pub/Sub channel | — | Admin lưu cấu hình → cmd/game, cmd/api, cmd/admin tải lại game data (payload = revision) |
| `gamedata:loaded` | Hash | — | `<service>/<node>` → `{service, node, version, revision, source, loadedAt, error?, checkedAt}`: game data mỗi process đang phục vụ (header admin) |

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
| `SHOP_OFFER_INACTIVE` | 404 | Offer không tồn tại, chưa bắt đầu hoặc đã hết hạn |
| `SHOP_ALREADY_OWNED` | 409 | Đã sở hữu nhân vật |
| `IDEMPOTENCY_KEY_REUSED` | 409 | Idempotency key đã dùng cho request khác |
| `IAP_PRODUCT_INACTIVE` | 404 | Gói nạp không tồn tại hoặc đã tắt |
| `IAP_VERIFICATION_FAILED` | 400 | Store từ chối receipt |
| `IAP_TRANSACTION_CONFLICT` | 409 | Receipt đã dùng cho người chơi/gói khác |
| `IAP_STORE_NOT_CONFIGURED` | 503 | Chưa cấu hình xác minh store |
| `PLAYER_UPDATE_FAILED` | 400 | Display name rỗng sau khi trim hoặc dài quá 20 ký tự |
| `CHARACTER_NOT_FOUND` | 404 | Chưa sở hữu nhân vật |
| `CHARACTER_INVALID_STATS` | 400 | Giá trị cộng điểm âm |
| `CHARACTER_NO_STATS_ALLOCATED` | 400 | Tổng điểm cộng bằng 0 |
| `CHARACTER_INSUFFICIENT_STAT_POINTS` | 400 | Không đủ `statPoints` |
| `CHARACTER_NO_STATS_TO_RESET` | 400 | Chưa cộng điểm nào để tẩy |
| `CHARACTER_INSUFFICIENT_BALANCE` | 400 | Không đủ tiền tẩy điểm |
| `EQUIP_NOT_FOUND` | 404 | Item không tồn tại hoặc đã tắt (mua) |
| `EQUIP_NOT_OWNED` | 403 | Không sở hữu trang bị |
| `EQUIP_ALREADY_EQUIPPED` | 409 | Trang bị đang được mặc (tháo trước khi mặc cho nhân vật khác) |
| `EQUIP_NOT_EQUIPPED` | 409 | Tháo món không đang mặc |
| `EQUIP_EQUIPPED` | 409 | Tách món đang mặc — tháo trước |
| `EQUIP_SLOT_OCCUPIED` | 409 | Slot/ô ngọc đã có |
| `EQUIP_LEVEL_REQUIRED` | 400 | Level nhân vật thấp hơn `requiredLevel` |
| `EQUIP_WRONG_CHARACTER` | 400 | Item dành riêng cho nhân vật khác |
| `EQUIP_MODIFIED` | 409 | Trang bị vừa bị request khác thay đổi, thử lại |
| `EQUIP_MAX_UPGRADE` | 400 | Đã +16 |
| `EQUIP_NO_STONES` | 400 | Cường hoá không có đá |
| `EQUIP_INSUFFICIENT_STONES` | 400 | Không đủ đá |
| `EQUIP_INSUFFICIENT_MATERIALS` | 400 | Không đủ nguyên liệu chế tạo |
| `EQUIP_GEM_SLOT_INVALID` | 400 | `slotIndex` sai hoặc vượt `gemSlots` |
| `EQUIP_GEM_NOT_OWNED` | 403 | Không sở hữu ngọc (hoặc ngọc đang khảm) |
| `MERGE_MIN_COUNT` / `MERGE_MAX_COUNT` / `MERGE_MAX_LEVEL` | 400 | Ghép cần 2–4 viên, chưa tới cấp tối đa |
| `CRAFT_RECIPE_NOT_FOUND` | 404 | Công thức không tồn tại hoặc đã tắt |
| `MILESTONE_ALREADY_CLAIMED` | 409 | Mốc thưởng đã nhận |
| `MILESTONE_NOT_REACHED` | 400 | Mốc thưởng chưa đạt |
| `TITLE_NOT_OWNED` | 403 | Đeo danh hiệu chưa sở hữu, không tồn tại hoặc đã tắt |
| `MAP_NOT_FOUND` | 404 | `GET /maps/{mapId}` với map không có |
| `MISSION_NOT_FOUND` / `MISSION_NOT_DONE` / `MISSION_ALREADY_CLAIMED` | 404 / 400 / 409 | Nhận nhiệm vụ ngày không có / chưa xong / đã nhận hôm nay |
| `CHEST_NOT_FOUND` / `CHEST_LOCKED` / `CHEST_ALREADY_CLAIMED` | 404 / 400 / 409 | Rương năng động không có / chưa đủ năng động / đã nhận kỳ này |
| `METHOD_NOT_ALLOWED` | 405 | Sai HTTP method |
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
