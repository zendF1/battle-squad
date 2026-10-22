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
    │     └── return false → không phải lobby event
    │
    └── RoomHandler.HandleMessage() → xử lý room/match events
          ├── CreateRoom, JoinRoom, QuickPlay
          └── Forward to Room → Forward to Match
```

### Lobby Events (Ranked Matchmaking)

Mọi lobby event trừ `CreateLobby` / `JoinLobby` chạy trên goroutine của lobby theo đúng thứ tự client gửi, nên client gửi liền `CreateLobby` → `UpdateLoadout` → `StartQueue` được (`CreateLobby` gán `client.LobbyID` ngay).

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateLobby` | `{}` | Tạo lobby (host). Đã là host của lobby đang `preparing` → trả lại lobby đó (`LobbyUpdated`); lobby solo còn `in_match` mà mình đã rời trận → mở lại (`preparing`) và trả lại; lobby không còn → tạo mới; còn lại `ALREADY_IN_LOBBY` |
| `JoinLobby` | `{lobbyId}` | Join lobby theo ID (tối đa 2 người) |
| `LeaveLobby` | `{}` | Rời lobby (đang `in_queue` thì rời hàng đợi trước). Host rời → giải tán |
| `UpdateLoadout` | `{characterId?, items?}` | Đổi character/items khi `preparing`; cùng luật QuickPlay (`room.ValidateLoadout`: nhân vật chơi được và sở hữu, ≤ 3 item đã biết trong giới hạn mỗi trận, đang có) → lưu `player_loadouts`. Sai → `LobbyError` `LOADOUT_INVALID` |
| `StartQueue` | `{}` | Host bấm tìm trận (lobby `preparing`); đọc lại rating của các thành viên rồi vào hàng đợi |
| `CancelQueue` | `{}` | Hủy tìm trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `LobbyUpdated` | `LobbyState` `{lobbyId, hostPlayerId, members:[{playerId, displayName, characterId, items, rating, tier, division}], status}` | State thay đổi; status `preparing` / `in_queue` / `in_match` |
| `LobbyError` | `{error: {code, message, correlationId}}` | Lỗi (mọi lỗi lobby, cả lỗi trong lobby room — trước đây event `Error`) |
| `LobbyDisbanded` | `{reason}` | Host rời / idle 30 phút → lobby giải tán |
| `QueueStarted` | `{estimatedWait}` | Đã vào hàng đợi; `estimatedWait` = `botFillAfter` (giây, 20) — chậm nhất lúc đó bot được điền |
| `QueueCancelled` | `{reason}` | `cancelled` (CancelQueue) / `teammate disconnected` |
| `MatchFound` | xem dưới | Ghép xong, gửi cho mọi người thật trong trận (cùng payload) |
| `ReturnToLobby` | `{lobbyId}` | Trận xong; lobby về `preparing` (kèm `LobbyUpdated`) |

**`MatchFound`** (`room.MatchFound`):

```json
{"roomId":"...","matchId":"...","mapId":"ice_bridge","mapName":"Cầu băng","mapTheme":"ice","mode":"ranked_2v2",
 "teams":[{"teamId":1,"players":[{"playerId":"p1","displayName":"Smoke","characterId":"spark",
   "isBot":false,"level":40,"rating":1050,"tier":"silver","division":2,"titleId":""}, ...]},
  {"teamId":2,"players":[...]}]}
```

Mỗi team đủ 2 người: người thật trước (theo thứ tự entry), rồi bot. `level` = level tài khoản (`player_profiles.level`), `titleId` = danh hiệu đang đeo (`""` = không). Bot: `playerId` `bot_xxxxxxxx`, `displayName` `Bot xxxx`, `characterId` `rookie`, `level` = trung bình level người thật, `rating` = trung bình rating người thật (tier/division theo rating đó), `titleId` `""`.

**Luồng client sau `MatchFound`** (không cần gửi gì để vào trận):

1. `MatchFound` → ngay sau đó `LobbyUpdated` (status `in_match`). Server đã đưa client vào battle room (`client.RoomID`).
2. `matchStartDelay` giây sau (config `matchmaking`, mặc định 3 s) → `MatchStarted` (snapshot như mọi trận, mode `ranked_2v2`), rồi `TurnStarted` và các match event bình thường. Client buffer match event nếu màn trận chưa sẵn sàng.
3. Kết thúc → `MatchEnded {winningTeam, rewards}` (rewards có rating, xem §7 Reward System), rồi `ReturnToLobby {lobbyId}` + `LobbyUpdated` (`preparing`). Lobby vẫn còn: `StartQueue` lại được, hoặc `LeaveLobby` (lobby solo → giải tán); `CreateLobby` lần sau trả lại đúng lobby đó.
4. `Leave` (hoặc mất kết nối) trong trận = rời trận: thua toàn bộ rating, không coin/EXP; client về Home rồi nên gửi `LeaveLobby` (hoặc `CreateLobby` lại sau khi trận đã kết thúc / lobby solo).
5. `CancelQueue` thua race: nếu matchmaker đã lấy entry, server không gửi `QueueCancelled` mà `MatchFound` đến ngay sau đó → trận vẫn diễn ra.

### Room Events

**Client → Server:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `CreateRoom` | `{mode, mapId, password?}` | Tạo phòng (pvp_1v1, pvp_2v2) |
| `JoinRoom` | `{roomId, password?}` | Vào phòng |
| `QuickPlay` | `{bot?: "ai", characterId?: string, items?: string[]}` | Chơi tutorial: mặc định bot idle (đứng yên, map `grassland_valley`), `bot: "ai"` cho bot AI di chuyển và bắn (map ngẫu nhiên trong mọi map). `characterId` mặc định `rookie`, `items` mặc định `[]`. Loadout được kiểm tra như `SelectCharacter`/`SelectItems` (nhân vật hợp lệ và đã sở hữu, tối đa 3 item, item có trong game data, mỗi item ≤ `maxUsePerMatch`, có trong `inventory_items`) trước khi tạo phòng; item được reserve như `StartMatch` của phòng custom và bị trừ khi dùng trong trận. Sai loadout → `RoomError` code `LOADOUT_INVALID` và không tạo phòng; nếu reserve thất bại (thiếu số lượng, item bị trận khác giữ) cũng trả `LOADOUT_INVALID`, lỗi reserve khác trả `MATCH_START_FAILED`, và phòng đã tạo bị huỷ. `DEV_UNLOCK_ALL` bỏ qua kiểm tra sở hữu và không reserve |
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
| `PlayerMoved` | `{playerId, position, moveEnergy, seq?}` | Player di chuyển; có `seq` khi trả lời `Move` có `seq` |
| `PlayerAiming` | `{playerId, angle, durationMs}` | Player đang ngắm trước khi bắn. `angle` cùng quy ước với `Shoot.angle` và là đúng góc sẽ bắn (phát cố tình trượt thì là góc trượt); `ShotResolved` của player đó tới sau khoảng `durationMs`. Không gửi power. Hiện chỉ gửi cho bot; nếu lượt đổi (timeout) trong lúc ngắm thì không có phát bắn |
| `ActionSelected` | `{playerId, actionMode, skillId?, itemId?}` | Người tới lượt đang chọn gì (gửi mọi player): `skill` (+ `skillId`), `item` (+ `itemId`), `weapon` (bỏ chọn). Client hiện badge trên đầu player đó và giữ tới khi `weapon`, lượt đổi (`TurnStarted`) hoặc hết replay của phát bắn. Chỉ gửi khi đổi so với lần gửi trước trong lượt, tối đa 1 lần / 100 ms / player (`selectBroadcastInterval`; đổi trong cửa sổ thì gửi lựa chọn mới nhất lúc hết cửa sổ). Server reset lựa chọn ở đầu lượt và khi phát bắn resolve, không gửi gì thêm. Bot: gửi ngay trước `PlayerAiming` khi bot sắp dùng skill hoặc item bắn |
| `ShotResolved` | `{shooterId, actionMode, skillId?, itemId?, skillEnergy, skillEnergies, projectiles[], events[], duration}` | Toàn bộ kết quả một lượt bắn (weapon, skill, teleport). `skillEnergy` = năng lượng skill của người bắn sau phát bắn; `skillEnergies` = `{playerId: energy}` của mọi player sau phát bắn (người bị địch đánh trúng +10, xem Kỹ năng) |
| `ItemUsed` | `{playerId, itemId, players, wind}` | Item bị tiêu hao |
| `PlayerDamaged` | `[{playerId, damage, hp, shield, isAlive, type}]` | Damage đầu lượt của người tới lượt, ngoài phát bắn: `type` `lava` (75) hoặc `burn` (10% máu tối đa). Không qua defense, khiên chịu trước (`shield` = khiên còn lại). Gửi trước `TurnStarted`; nếu chết thì không có `TurnStarted` cho họ: server kiểm tra thắng thua rồi chuyển lượt |
| `MatchEnded` | `{winningTeam, rewards, stats, mvpPlayerId}` (no-contest: `{winningTeam: 0, result: "no_contest", message, stats, mvpPlayerId: ""}`) | Trận kết thúc, xem §7 Reward System |
| `MatchStateSync` | `MatchState` + `terrain` + `gamedataVersion` + `lastMoveSeq` + `selectedAction?` | Full sync trả lời `Reconnect` (`gamedataVersion` như `MatchStarted`). `selectedAction` (`ActionSelected` của người tới lượt, cả khi broadcast còn đang gộp) chỉ có khi họ đang chọn skill / item, để client vào lại vẫn hiện badge. `lastMoveSeq`: `seq` của `Move` cuối cùng của người gửi `Reconnect` mà match loop đã nhận trong lượt hiện tại, kể cả `Move` bị bỏ (rate limit, đang resolve, không hợp lệ); `0` nếu chưa có hoặc không phải lượt của họ; reset khi lượt mới bắt đầu. Snapshot đã phản ánh mọi `Move` có `seq <= lastMoveSeq` |

**`ShotResolved`:** server tính xong toàn bộ phát bắn và state đã cập nhật trước khi gửi; client chỉ phát lại. `projectiles[]` là đường bay (`path[]` với `time`) của từng viên đạn; `time` luôn tính từ lúc bắn, nên viên phóng trễ (bom liên hoàn, tên lửa) có `path[0].time = launchDelay > 0` và client chỉ hiện nó từ lúc đó. Mỗi projectile: `{projectileId, ownerPlayerId, skillId?, kind?, launchDelay?, path[], hitPlayerId?, explosionPoint?, explosionRadius, craterRadius}`; `kind` (chỉ với skill): `flare` (pháo hiệu, đáp không nổ), `missile` (tên lửa rơi từ đỉnh map), `bomb` (bom liên hoàn), `seed` (hạt của Hoa hồi xuân, đáp lên đồng đội `hitPlayerId`, không nổ); thiếu = đạn thường. `events[]` sắp theo `t` (giây kể từ lúc bắn):

| `type` | Trường | Ghi chú |
|--------|--------|---------|
| `explode` | `projectileId, position, radius, crater, terrainVersion` | Khoét terrain tại `t` (cuối đường bay). `crater`: hình hố, luôn `bowl` (lòng chảo, `Terrain.CarveBowl`; mọi vụ nổ, cả bom liên hoàn từ playtest 5); `circle` (hình tròn, `Terrain.DestroyCircle`) không còn được gửi, client vẫn khoét được; thiếu = `bowl`. Không còn `floor` (trước playtest 5 bom liên hoàn giữ 48 px đáy map) |
| `damage` | `playerId, amount, hp, cause, projectileId?, critical?, shield?` | `cause`: `explosion`, `laser`, `fall`, `out_of_map`. `amount` = máu + khiên mất; `shield` (khiên còn lại) chỉ có khi player có khiên trước cú đánh |
| `heal` | `playerId, amount, hp` | `amount` = máu thực hồi (có thể 0 khi đầy máu) |
| `shield` | `playerId, amount, shield` | Khiên nhận thêm / khiên sau đó (tối đa 50% máu tối đa) |
| `status` | `playerId, effectId, turns` | Hiệu ứng được gắn (`burn`, `root`, `stun`, `net`, `freeze`) |
| `cleanse` | `playerId, effectIds[]` | Hiệu ứng xấu bị gỡ |
| `fall` | `playerId, from, to, duration` | Rơi từ `t` đến `t + duration`; damage rơi nằm ở mốc chạm đất. Một player rơi nhiều lần trong cùng phát bắn thì lần sau bắt đầu khi lần trước chạm đất |
| `die` | `playerId, cause` | |
| `teleport` | `playerId, from, to` | |
| `flare` | `projectileId, position` | Pháo hiệu đáp (Mưa tên lửa): khói đỏ, không damage, không hố |
| `rain` | `position, radius, count` | Tên lửa bắt đầu rơi: quanh `position.x` ± `radius`, `count` quả |
| `beam` | `playerId, from, to, duration` | Tia laze từ `from` đến mép map `to`, hiện `duration` giây; xuyên địa hình, không khoét. `t` = thời gian nạp ở nòng (0,3 s); đầu tia quét ra 12000 px/s, mỗi địch trúng (`damage` cause `laser`) lúc đầu tia tới thân nó |

Client thiếu field mới (hoặc server cũ) vẫn chạy: field vắng = mặc định (`kind` rỗng = đạn thường, `launchDelay` 0, `shield` không đổi, `floor` 0 = khoét cả hình tròn).

`duration` là lúc animation cuối cùng kết thúc; server kết thúc lượt sau `duration + 1.2s`.

**Player trong `players`** (`BattlePlayerState`, cùng shape trong `MatchStarted`, `MatchStateSync`, `ItemUsed`): `{playerId, displayName, teamId, characterId, weaponItemId, weaponUpgradeLevel, title?, hp, maxHp, shield, defense, position{x,y}, moveEnergy, maxMoveEnergy, items[], statusEffects[], isAlive, isBot, damageBonus, critChance, moveEnergyBonus, skillPowerBonus, terrainDamageBonus, skillEnergy, skillRank, damageDealt, killCount, shotsFired, shotsHit}`. `shield` là khiên hiện có (chịu damage trước máu, tối đa 50% `maxHp`, mất khi vỡ hoặc hết trận). `skillRank` là cấp súng dùng cho skill (`WeaponSkillRank(weaponItemId)`, 0..4, xem Kỹ năng). `weaponItemId` là `item_id` của vũ khí nhân vật đang mặc lúc vào trận (VD `rookie_weapon_lv30`, `crafted_weapon_gold`, `normal_weapon_lv10`), `""` khi không mặc vũ khí và với bot. Room đọc nó cùng câu query equipment khi tính chỉ số (`battlestats.LoadLoadouts`), không tốn thêm gì mỗi phát bắn. Chỉ dùng để client vẽ súng đang cầm; không ảnh hưởng gameplay. `weaponUpgradeLevel` là cấp nâng cấp (`player_equipment.upgrade_level`, 0..16) của vũ khí đó, đọc cùng câu query; `0` khi không mặc vũ khí và với bot. Chỉ để client vẽ hào quang sau lưng (vũ khí +14: tím, +16: đỏ); client cũ bỏ qua, client mới thiếu field coi là 0. `title` là danh hiệu người chơi đang đeo lúc vào trận, `{id, name, rarity}` (rarity `blue`/`orange`/`purple`/`red`), chỉ khi danh hiệu còn sở hữu và đang bật; không có field khi không đeo và với bot. Room đọc nó lúc tính chỉ số (`battlestats.LoadEquippedTitle`, cả khi chơi nhân vật chưa sở hữu); chỉ để client vẽ biển danh hiệu, không ảnh hưởng gameplay. `maxMoveEnergy` là năng lượng di chuyển nạp lại đầu mỗi lượt của player: năng lượng gốc của nhân vật (`config_characters.mobility`) + `moveEnergyBonus` (điểm, trang bị); client dùng nó làm mức đầy của thanh năng lượng.

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
     - Medkit / Move: xử lý ngay, botStep tiếp sau botFollowUpDelay 1.5s
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
  distance = dist(explosionCenter, playerPosition)
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

Khi trận bắt đầu, room đọc `bonus_*` của `player_characters` (nhân vật đang chọn) và nhân với `statMultipliers` trong `game_settings.character_progression` (key thiếu hoặc 0 → mặc định của seed 006). Bot giữ chỉ số gốc của nhân vật; nhân vật chưa sở hữu (DEV_UNLOCK_ALL) cũng vậy. Equipment cộng thêm sau đó — chỉ số equipment cũng là **điểm**, đổi ra chỉ số bằng cùng `statMultipliers` (VD Iron Cap `stat_hp` 20 = 20 điểm HP = +40 max HP); crit là % không qua multiplier.

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
- `baseStats`: mọi nhân vật trong game data (kể cả chưa sở hữu), không điểm, không equipment — cũng là chỉ số của bot.
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

**Kích thước**: 16 map mới 150 × 70 ô × 16 px = 2400 × 1120 (~2,14:1, gần map tham khảo ~2,1:1); `grassland_valley` / `frozen_peak` giữ 1600 × 900. Map rộng hơn màn hình: client có camera pan theo đường đạn / người chơi, đọc kích thước từ `terrain.width` / `terrain.height` của `MatchStarted` / `MatchStateSync` (hoặc `width` / `height` của `GET /maps`), không giả định 1600 × 900. Vật lý không đổi (speed power × 9,6, gravity 445, bay tối đa 4,5 s): phát 45° full lực bay ~2070 px trên mặt phẳng, nên 2400 là gần mức rộng nhất hai bên còn bắn tới nhau. Spawn hai đội cách nhau ~1300–1900 px; spawn thấp hơn đồng đội đứng gần giữa hơn để bục phía trên không che đường bắn; mọi spawn đứng trên ≥ 12 ô (192 px) mặt đi được (thực tế 14–35 ô), bục / đảo chính rộng hơn bản 1600. Bot (`bestMove`) không tìm được chỗ nào trong tầm đi có phát gây damage thì đi về phía địch gần nhất (trên map 2400 px địch có thể ngoài tầm bắn). Độ dày đất tính theo hố lớn nhất (Tanko cannon_ball với bonus ở cap +50 %: r 27 px, mỗi phát sâu ~22 px; lớn hơn mọi hố skill): đứng ở spawn phải chịu ≥ 3 phát cùng chỗ mới rơi khỏi map (thực tế 4–10 phát trên đất mềm; spawn trên đất cứng / lớp mềm trên đá thì không bao giờ rơi, chỉ bị đào hố và vẫn ăn sát thương nổ), và 1 phát ở bất kỳ đâu trên sàn spawn không khoét thủng sàn (`TestGeneratedMapsGroundHoldsTankoCraters`). Đạn nổ trên đất cứng không khoét gì nhưng sát thương nổ vẫn đủ (`TestExplosionOnHardGroundStillSplashes`). Skill bom liên hoàn (2 + rank quả cùng đường bay, mỗi quả một hố như đạn thường) vẫn đào giếng xuyên đất mềm được, theo thiết kế.

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

**Cấp súng** (`skillRank`, `WeaponSkillRank`): theo tier của vũ khí đang mặc, không theo +nâng cấp. Không vũ khí / bot 0, `*_lv10` Sắt 1, `*_lv20` Thép 2, `*_lv30` Mithril 3, `*_lv40` Rồng 4; súng chế tạo (Blaster, mặc được cho mọi nhân vật, skill vẫn theo nhân vật) Bạc 2, Vàng 3, Titan 4, Kim cương 4.

**base** = (damage vũ khí + `damageBonus`) × power_shot 1,5 × `damage_multiplier` (1,0; ≤ 0 tính là 1) × (1 + `skillPowerBonus`/100).

| Skill | Rookie — Mưa tên lửa (`triple_shot`, `missile_rain`) | Tanko — Bom liên hoàn (`heavy_bomb`, `bomb_chain`) | Spark — Tia laze (`shock_field`, `laser`) | Flora — Hoa hồi xuân (`healing_bloom`, `bloom`) |
|---|---|---|---|---|
| Bắn | Pháo hiệu như phát thường (gió, trọng lực); đáp: khói đỏ, không damage / hố; bay khỏi map → mất skill | 2 + cấp bom lớn (playtest 6: bớt 1 quả, mỗi quả giữ sát thương cũ: (1,8 + 0,15 × cấp) / (3 + cấp) × base) cùng đường bay đạn thường (cùng gốc, góc, lực, gió; không lái), cách ≥ 0,1 s; bom sau bay qua hố của bom trước và nổ ở chỗ đường bay đó chạm địa hình / người chơi: mỗi quả khoét lòng chảo của vũ khí; không mục tiêu thì đào một giếng (~40 px mỗi quả với `cannon_ball`); trúng thân thì nổ ở tâm thân, hố không chạm đất dưới chân: cầu vồng (dốc) và bắn thẳng ở 600 px đều trúng mục tiêu tới trần (bắn thẳng: quả đầu nổ cạnh chân, mục tiêu rơi vào hố, quả sau trúng thân) | Nạp 0,3 s rồi tia thẳng từ tâm thân theo góc ngắm tới mép map, xuyên địa hình (không giảm damage); bỏ qua lực | Không ngắm; các hạt cùng bay thẳng lên trên Flora ra khỏi đỉnh map (y −140, 0,55 s), sang trên đầu đồng đội ngoài màn hình (0,2 s) rồi rơi thẳng xuống thân (0,65 s): đáp và hồi lúc 1,4 s, mọi đồng đội còn sống (cả Flora); replay kéo thêm 0,9 s cho hiệu ứng nở hoa |
| Số lượng | 0,5 s sau khi đáp: 12 + 4×cấp tên lửa rơi thẳng (không gió, bắn xuống 1500 px/s `rainDropSpeed`, ~0,37 s tới đất) từ đỉnh map, cách 0,05 s (cấp 4: 28 quả trong ~1,8 s, cả skill ~3,0 s; trước 032 ~3,9 s), rải ± (90 + 15×cấp) px quanh điểm đáp, dày ở giữa (độ lệch = khoảng × \|v\|², nửa số quả trong ± 1/4 khoảng; seed theo trận + lượt); giải theo thứ tự chạm đất. Pháo hiệu trúng mục tiêu: ~1,4 × base (cấp 0) tới mức trần (cấp 4, ~1,8 × base sau defense của Tanko), địch cách 100 px dính ~0,55 × base ở cấp 4 | 3 / 4 / 5 / 6 / 7 | 1 tia | 1 hạt / đồng đội |
| Damage | Mỗi quả 0,25 × base (nổ 0,7 × bán kính vũ khí, hố 1,0 × hố vũ khí = 12 px; 039: hố vũ khí 20 → 12 px nên `rainCrater` 0,6 → 1,0); tổng mỗi mục tiêu ≤ 2 × base (trước defense) | Tổng (1,8 + 0,15×cấp) × base chia đều; tổng mỗi mục tiêu ≤ (1,2 + 0,15×cấp) × base (trước defense; cấp 4: phát cầu vồng trúng 6 / 7 quả là chạm trần); nổ 1,3 ×, hố mọi quả = hố vũ khí (lòng chảo 52 px, như phát thường; trước playtest 5: tròn 1,4 × = 73 px); đào bao sâu cũng được (trước: không khoét 48 px đáy map) | Địch gần nhất (1,5 + 0,1×cấp) × base, mỗi địch sau ×0,8; không hố, không văng, không rơi; không trúng đồng đội | Hồi theo máu tối đa: Flora (40 + 5×cấp)%, đồng đội (30 + 5×cấp)%; không nhân skill power; dư thành khiên (≤ 50% máu tối đa) |
| Hiệu ứng | `burn` 1 lượt lên mọi địch trúng | `root` 1 lượt | `stun` 1 lượt | Gỡ hiệu ứng xấu mọi đồng đội |
| Bot | Ngắm như phát thường tốt nhất (trượt theo hit chance) | Phát dốc nhất (lệch ≤ 30° so với thẳng đứng, bay ≤ `MaxFlightSeconds` − 1 s = 3,5 s) gây ≥ 80% damage của phát tốt nhất, không có thì phát tốt nhất; trong các lực tốt như nhau ở một góc, lực mà đường bay (bỏ qua người chơi) chạm đất gần chân địch nhất (`shaftMiss`: giếng đào ngay dưới mục tiêu); trượt theo hit chance | Góc trúng nhiều địch nhất (ưu tiên 2) | Khi máu mình < 60%, đồng đội < 50% hoặc đồng đội dính hiệu ứng xấu |

Đơn giản hoá: bom liên hoàn và tên lửa được giải tuần tự lúc server nhận phát bắn, vị trí rơi cập nhật ngay; tên lửa có thể trúng mục tiêu khi client còn đang vẽ nó rơi. Bom liên hoàn thì không: mọi cú rơi trong chuỗi chạy nhanh ×3 so với rơi tự do (quãng rơi và damage rơi giữ nguyên) và một quả bom chỉ nổ khi mọi cú rơi trước đã chạm đất + 0,08 s (nếu cần, quả sau phóng trễ hơn 0,1 s; ~9% khoảng cách bị kéo dài khi quét các góc bắn trên grassland_valley, phát cầu vồng của bot thì phần lớn). Đường bay vẫn giới hạn `MaxFlightSeconds` (4,5 s; trước 032: 6 s): bắn cầu vồng quá cao (quả đầu bay từ ~4,0 s, trước 032 ~5,2 s; chỉ người chơi bắn tay; bot chỉ lob khi ≤ 3,5 s) thì bom sâu nhất có thể hết giờ bay và biến mất (khoảng cách 0,1 s không đổi điều này: giới hạn tính theo đường bay của từng quả). Nguồn bắn của bom liên hoàn giữ nguyên dù người bắn rơi vào hố của mình. `projectile_count` / `status_effect_id` trong `config_skills` chỉ để hiển thị.

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

**Trận được tính** (`Match.CountsForMilestones` + `milestoneCounts`): trận ghép (`ranked_2v2`, kể cả có bot) và QuickPlay đấu với máy AI (`bot: "ai"`); không tính phòng tự tạo (`CreateRoom` pvp_1v1/pvp_2v2) và Luyện tập (bia đứng yên). Với từng người: không rời trận (`Leave` → `Left`), là người thật, và đã thực hiện ≥ 3 lượt hành động (`ShotTurns`: mỗi ShotResolved của họ — bắn, skill, teleport). Thua/hoà vẫn tính trận.

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

**Rating — chỉ `ranked_2v2`** (`pvp_1v1` QuickPlay / phòng tự tạo và `pvp_2v2` không đổi rating, không ghi `player_ranks`):
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
CreateLobby → UpdateLoadout → StartQueue → Matchmaker ghép (4 slot, 2 team × 2) → MatchFound
  → (matchStartDelay) MatchStarted → trận ranked_2v2 → MatchEnded (rating) → ReturnToLobby
```

### Lobby Room

- 1-2 người cùng team, actor pattern (goroutine + event channel); mọi event trừ Create/Join chạy trên goroutine lobby (giữ thứ tự)
- Character/items lưu trong `player_loadouts` DB table
- Status: `preparing` → `in_queue` → `in_match` → `preparing` (loop; `MatchFound` / `ReturnToLobby` qua `LobbyHub.Notify`)
- Host rời → lobby giải tán, member nhận `LobbyDisbanded`; rời khi `in_queue` → entry rời hàng đợi
- Rating của member: dòng `player_ranks` của mùa active, không có → `elo.defaultRating`; đọc lại mỗi `StartQueue`

### Matchmaker Engine

- Goroutine chạy trong game server, tick mỗi `tickInterval` (3s)
- Redis leader election (`SETNX` TTL 10s) cho multi-node
- Hot-reload config mỗi 30s từ `game_settings` DB

**Queue (Redis):**
- Sorted set `matchmaking:queue:2v2` (score = rating)
- Entry detail: `matchmaking:entry:{id}` (JSON string, TTL 120s): solo (1 người) hoặc party (lobby 2 người); `rating` = party rating, `playerRatings` = rating riêng
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
5. Bot: `rookie`, chỉ số gốc; độ khó (`bot_difficulty`) theo tier của **trung bình rating người thật**; trong Elo bot = trung bình rating người thật; trận có bot nhân `botRatingModifier`
6. Map: ngẫu nhiên trong các map có `min_rank_tier` ≤ tier của rating người thật cao nhất

**Bắt đầu trận** (`room.Hub.CreateBattleFromMatch`): đọc profile (tên, level, danh hiệu) → dựng roster + `MatchFound` → giữ chỗ item của mọi người thật (`reservePlayerItems` như phòng tự tạo; ai không giữ được item thì chơi không item) → đăng ký room → `MatchFound` tới từng lobby, client vào room → match chạy, chờ `matchStartDelay` (3s) rồi `MatchStarted`. Item đã dùng bị trừ lúc kết thúc, phần còn lại trả về.

**Party rating:** 3 strategies (config từ admin): `max` (default), `average`, `weighted`

**Config `game_settings.matchmaking`**: `tickInterval, baseRatingRange, expandInterval, expandStep, maxRatingRange, botFillAfter (20, migration 035), matchStartDelay (3, migration 035), botRatingModifier, partyRatingStrategy, weightedRatio`; `maxWaitTime` (60) không còn dùng khi ghép. `game_settings.elo`: `kFactor, ratingFloor, defaultRating`.

### Tier & mùa giải

`internal/shared/ranked` là nơi duy nhất: `Tier` / `TierAndDivision` (bronze < 1000, silver < 1200, gold < 1500, platinum < 1800, diamond < 2200, master; division 3 thấp nhất → 1, master luôn 1), `TierIndex` / `CompareTiers`, `LoadEloConfig`, `EnsureActiveSeason`. Matchmaker, room, match rewards, lobby và `/rank` đều dùng nó (không còn `ratingToTier` trùng lặp).

**Mùa giải (phase 1, chưa có vòng đời):** `EnsureActiveSeason` trả mùa `active`; không có thì tạo `season_<n>` tên "Mùa <n>" (n = số mùa + 1), 90 ngày — dùng chung cho match rewards và `/rank/seasons/current`, `/rank/me` (không còn mùa giả). Migration 035 đổi tên `season_1` "Default Season" → "Mùa 1".

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
| GET/POST | `/physics` | Mọi dòng game_settings (trừ `rules.*`), kiểm tra kiểu / khoảng |
| GET/POST | `/gamedata`, `/gamedata/reload` | Version game data (admin, từng server game / api, lỗi reload), payload `/gamedata`; "Tải lại cấu hình" tăng revision + báo mọi server |
| GET/POST | `/rules` | Game Rules: các dòng số (physics, lượt, di chuyển, rơi, trang bị) và mọi nhóm `rules.*`, kiểm tra kiểu / khoảng / bắt buộc |
| GET/POST | `/skills/tuning` | Mẫu mô tả + thông số kỹ năng (`config_skills.params`), xem trước số và mô tả theo cấp súng |
| GET/POST | `/texts/{table}` | Sửa tại chỗ: status-effects (tên, marker, banner, xấu / mất lượt / chặn di chuyển), rank-tiers (tên, khoảng điểm, kiểm tra bands), equipment-tiers, equipment-slots, gun-kinds, type-words, game-modes, tips |
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
| `config_status_effects`, `config_rank_tiers`, `config_equipment_tiers`, `config_game_modes`, `config_tips` | `effect_id` / `tier` / `tier` / `mode_id` / `tip_id` | 040: text tiếng Việt + thứ tự hiển thị cho `GET /gamedata` |
| `config_brick_types` | `brick_type_id` | Brick của map tile: tên, `destructible`, `border` polygon, `color`; id 1..19 do 036 tạo |
| `config_gun_kinds`, `config_equipment_slots`, `config_item_type_words` | `gun_kind` / `slot` / `word` | 041: tên loại súng, tên ô trang bị, chữ dịch loại trong tên trang bị "<Tier> <Type>" (`GET /gamedata` `gunKinds`, `equipment.slots`, `equipment.typeWords`) |
| `gamedata_revision` | `id` (=1) | 041: revision game data; trigger statement-level trên mọi bảng config_* và game_settings tăng nó mỗi lần ghi |
| `gamedata_versions` | `version` | 041: payload `GET /gamedata` (text JSON) của mỗi version server đã publish (`?version=`), xoá sau 30 ngày ngoài 50 bản mới nhất |

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
