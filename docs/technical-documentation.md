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
│   ├── battlestats/      Chỉ số vào trận (gốc + điểm + equipment), dùng chung cho room và GET /player/characters
│   └── gamedata/         Load game configs (DB, fallback YAML) — game server và API đều load lúc khởi động
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
migrations/               SQL migration files (001-019)
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
| POST | `/player/character/reset-stats` | ResetStats | Tẩy điểm `{characterId}`: trả mọi điểm đã cộng về `statPoints`, phí `resetCostAmount` `resetCostCurrency` (seed: 500 coin). Khoá profile rồi row nhân vật `FOR UPDATE`, kiểm tra, trừ tiền và reset trong một transaction — reset đồng thời chỉ bị trừ tiền một lần |

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

Slot: `weapon`, `armor`, `helmet`, `pants`, `boots`, `gloves`. Mỗi món sở hữu (`player_equipment`) có `upgradeLevel` 0–16, tối đa 2 ô ngọc, và được mặc cho **một** nhân vật (`equippedOn`); mỗi (nhân vật, slot) chỉ một món. Chỉ số equipment vào `stats.fromEquipment` của `GET /player/characters` và vào trận (xem [Chỉ số hiển thị](#chỉ-số-hiển-thị-get-playercharacters)).

| Method | Path | Handler | Mô tả |
|--------|------|---------|-------|
| GET | `/equipment/catalog` | GetCatalog | Toàn bộ catalog: `{items, upgradeRates, setBonuses, stones}` (items gồm cả `normal` và `crafted`, **cả item đã tắt** với `isActive: false` — món người chơi đã sở hữu vẫn cần tên/chỉ số; client lọc `isActive` cho shop). Client cache được, chỉ đổi khi admin sửa config |
| GET | `/player/equipment` | GetEquipment | Trang bị đang có (mới nhất trước). Tên/chỉ số lấy từ catalog theo `itemId` |
| POST | `/player/equipment/equip` | EquipItem | `{equipmentId, characterId}` → `{"success":true}`. Slot của nhân vật đã có món khác thì món cũ tự tháo. Món đang mặc (cho bất kỳ nhân vật nào) → 409 `EQUIP_ALREADY_EQUIPPED`: tháo trước rồi mặc lại |
| POST | `/player/equipment/unequip` | UnequipItem | `{equipmentId}` → `{"success":true}`; món không đang mặc → 409 `EQUIP_NOT_EQUIPPED` |
| POST | `/player/equipment/upgrade` | Upgrade | `{equipmentId, stones: [{stoneLevel, quantity}]}` → `{success, newLevel, percent, equipmentId}` |
| POST | `/player/equipment/dismantle` | Dismantle | `{equipmentId}` → `{"success":true}`; hoàn 50% power đá đã dùng từ safe zone gần nhất. Món đang mặc → 409 `EQUIP_EQUIPPED` |
| POST | `/player/equipment/socket` | SocketGem | `{equipmentId, slotIndex (1\|2), gemId}` |
| POST | `/player/equipment/unsocket` | UnsocketGem | `{equipmentId, slotIndex}` |
| GET | `/player/stones` · `/player/gems` · `/player/materials` | GetStones · GetGems · GetMaterials | `[{stoneLevel, quantity}]` · `[{gemId, gemType, gemLevel}]` · `[{materialId, quantity}]` (kèm `playerId`) |
| GET | `/shop/equipment?characterId=` | GetShopEquipment | Item `normal` active dùng được cho nhân vật đó: đồ dùng chung (`characterId` null: áo, mũ, quần, giày, găng) + vũ khí của nhân vật đó (`<character>_weapon_lvN`). Không có `characterId` → chỉ đồ dùng chung |
| POST | `/shop/equipment/buy` | BuyEquipment | `{itemId, idempotencyKey}` → `{equipmentId}`. Trả `priceCoin` (hoặc `priceGem` nếu coin = 0). Key ≤ 64, tuỳ chọn: cùng key + item trả lại `equipmentId` của lần mua đầu, không trừ tiền lần nữa; key đã dùng cho item khác → 409 `IDEMPOTENCY_KEY_REUSED`; lần mua lỗi không lưu key nên retry chạy lại từ đầu. Không gửi key → mỗi request mua một món (như cũ). Không đủ tiền → 400 `SHOP_INSUFFICIENT_BALANCE`; item không có/đã tắt → 404 `EQUIP_NOT_FOUND`; item crafted, thiếu `itemId` hoặc key > 64 → 400 `BAD_REQUEST`. Khoá profile `FOR UPDATE` nên các lần mua của một người chạy tuần tự |
| GET/POST | `/shop/stones`, `/shop/stones/buy` | GetShopStones, BuyStones | `{stoneLevel, quantity}`; đá `merge_only` không bán |
| GET/POST | `/shop/gems`, `/shop/gems/buy` | GetShopGems, BuyGems | `{gemType, gemLevel (1–6), quantity}`; lv1–3 = level×200 coin, lv4–6 = (level−3)×30 gem |
| GET/POST | `/shop/materials`, `/shop/materials/buy` | GetShopMaterials, BuyMaterials | `{materialId, quantity}`, giá `priceGem` |
| POST | `/merge/stone` · `/merge/gem` | MergeStones · MergeGems | `{stoneLevel, count}` · `{gemType, gemLevel, count}`, count 2–4 → `{success, message}` |
| GET | `/crafting/recipes` | GetRecipes | `[{recipeId, resultItemId, materials: [{materialId, quantity}], isActive}]` |
| POST | `/crafting/craft` | Craft | `{recipeId}` → `{equipmentId}` |

`GET /equipment/catalog` (`tier`, `characterId` bị bỏ khi null):

```json
{
  "items": [
    {"itemId": "rookie_weapon_lv10", "name": "Rookie Sword", "slot": "weapon", "category": "normal", "requiredLevel": 10,
     "characterId": "rookie", "gemSlots": 1, "statHp": 0, "statDamage": 8, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 500, "priceGem": 0, "isActive": true},
    {"itemId": "normal_weapon_lv10", "name": "Iron Sword", "slot": "weapon", "category": "normal", "requiredLevel": 10,
     "gemSlots": 1, "statHp": 0, "statDamage": 8, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 500, "priceGem": 0, "isActive": false},
    {"itemId": "crafted_weapon_silver", "name": "Silver Sword", "slot": "weapon", "category": "crafted", "tier": "silver",
     "requiredLevel": 15, "gemSlots": 2, "statHp": 0, "statDamage": 10, "statDefense": 0, "statCrit": 0, "statMoveEnergy": 0,
     "priceCoin": 0, "priceGem": 0, "isActive": true}
  ],
  "upgradeRates": [{"fromLevel": 0, "toLevel": 1, "upgradeCost": 1, "maxPercent": 80, "failResetTo": 0}],
  "setBonuses": [{"tier": "silver", "piecesRequired": 2, "bonusHpPct": 3, "bonusDmgPct": 0, "bonusDefPct": 0, "bonusCritPct": 0}],
  "stones": [{"stoneLevel": 1, "power": 1, "priceCoin": 100, "priceGem": 0, "source": "coin_shop"}]
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

Đồ thường dùng chung cho mọi nhân vật, **trừ vũ khí**: mỗi nhân vật có 4 vũ khí riêng (`characterId` = nhân vật, tên "Rookie Sword" / "Tanko Cannon" / "Spark Staff" / "Flora Slingshot", chỉ số và giá bằng tier vũ khí chung). Đồ crafted dùng chung. 4 vũ khí chung `normal_weapon_lv10..40` (Iron/Steel/Mithril/Dragon Sword) được `migrations/019_equipment_design_levels.up.sql` tắt (`is_active = false`): không bán nữa, món đã mua vẫn mặc được cho mọi nhân vật và vẫn có trong catalog.

Migration 019 (chạy lại mỗi lần deploy, mọi bước có điều kiện nên chạy lại không đổi gì và không ghi đè chỉnh sửa admin):
1. Trả level yêu cầu về thiết kế cho DB còn giữ bảng sai 1/4/7/10 (normal) và 2/5/8/10 (crafted) của bản 016 cũ: mỗi slot chỉ được sửa khi **cả 4** dòng của slot (normal lv10..lv40, hoặc crafted silver..diamond) vẫn đúng giá trị đã remap; slot admin đã sửa một dòng thì giữ nguyên cả slot. 016 không còn bước remap.
2. Mở rộng `character_levels` tới level 80 (xem [Reward System](#reward-system)), chỉ khi giá trị còn đúng nguyên văn bảng 10 level của migration 006.
3. Tắt 4 vũ khí chung nếu còn đúng như seed, và chỉ khi chưa có vũ khí riêng nào (nên chỉ chạy một lần: admin bật lại thì lần sau không tắt nữa); rồi thêm 16 vũ khí riêng (`ON CONFLICT DO NOTHING`).

Down của 019 xoá vũ khí riêng còn đúng seed và không ai sở hữu, bật lại vũ khí chung còn đúng seed, trả bảng 10 level nếu còn đúng bảng 80 level; không đổi level yêu cầu. Chạy down 019 trước down 016.

Giá normal (coin) — weapon (mỗi nhân vật) 500/1500/4000/10000, armor 400/1200/3500/8000, helmet 300/900/2500/6000, pants 350/1000/3000/7000, boots 300/800/2200/5500, gloves 250/750/2000/5000. Ngoài ra: 16 mức cường hoá (`config_upgrade_rates`), 12 cấp đá (1–6 coin, 7–10 gem, 11–12 chỉ ghép), ngọc `hp`/`damage`/`defense`/`critical` lv1–10, set bonus 2/4/6 món cho mỗi tier crafted, 20 nguyên liệu (5 mỗi tier) và 24 công thức (5 nguyên liệu của tier × 10/20/30/40, weapon × 15/30/45/60). JSON nguyên liệu của công thức dùng key `materialId`; công thức cũ của admin dùng `material_id` vẫn đọc được và được migration đổi key (giữ các key khác; `quantity` dạng số/chuỗi số được làm tròn thành số nguyên, giá trị khác giữ nguyên). Down migration chỉ xoá item/nguyên liệu/công thức còn đúng như seed và không ai sở hữu; không đụng `config_upgrade_rates`, `config_stones`, `config_gems`, `config_set_bonuses`.

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
| `QuickPlay` | `{bot?: "ai", characterId?: string, items?: string[]}` | Chơi tutorial: mặc định bot idle (đứng yên), `bot: "ai"` cho bot AI di chuyển và bắn. `characterId` mặc định `rookie`, `items` mặc định `[]`. Loadout được kiểm tra như `SelectCharacter`/`SelectItems` (nhân vật hợp lệ và đã sở hữu, tối đa 3 item, item có trong game data, mỗi item ≤ `maxUsePerMatch`, có trong `inventory_items`) trước khi tạo phòng; item được reserve như `StartMatch` của phòng custom và bị trừ khi dùng trong trận. Sai loadout → `RoomError` code `LOADOUT_INVALID` và không tạo phòng; nếu reserve thất bại (thiếu số lượng, item bị trận khác giữ) cũng trả `LOADOUT_INVALID`, lỗi reserve khác trả `MATCH_START_FAILED`, và phòng đã tạo bị huỷ. `DEV_UNLOCK_ALL` bỏ qua kiểm tra sở hữu và không reserve |
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
| `Reconnect` | `{}` | Kết nối lại sau disconnect |
| `Leave` | `{}` | Rời trận |

**Server → Client:**

| Event | Payload | Mô tả |
|-------|---------|-------|
| `MatchStarted` | `{matchId, players, turnOrder, wind, mapId}` | Trận bắt đầu |
| `TurnStarted` | `{currentPlayerId, turnIndex, wind, timeLeft}` | Lượt mới |
| `TurnTimerTick` | `{timeLeft}` | Đếm ngược (mỗi 1s) |
| `PlayerMoved` | `{playerId, position, moveEnergy, seq?}` | Player di chuyển; có `seq` khi trả lời `Move` có `seq` |
| `ShotResolved` | `{shooterId, actionMode, skillId?, itemId?, skillEnergy, projectiles[], events[], duration}` | Toàn bộ kết quả một lượt bắn (weapon, skill, teleport) |
| `ItemUsed` | `{playerId, itemId, players, wind}` | Item bị tiêu hao |
| `PlayerDamaged` | `[{playerId, damage, hp, isAlive, type}]` | Damage ngoài phát bắn (lava đầu lượt) |
| `MatchEnded` | `{winningTeam, rewards}` | Trận kết thúc |
| `MatchStateSync` | `MatchState` + `terrain` + `lastMoveSeq` | Full sync trả lời `Reconnect`. `lastMoveSeq`: `seq` của `Move` cuối cùng của người gửi `Reconnect` mà match loop đã nhận trong lượt hiện tại, kể cả `Move` bị bỏ (rate limit, đang resolve, không hợp lệ); `0` nếu chưa có hoặc không phải lượt của họ; reset khi lượt mới bắt đầu. Snapshot đã phản ánh mọi `Move` có `seq <= lastMoveSeq` |

**`ShotResolved`:** server tính xong toàn bộ phát bắn và state đã cập nhật trước khi gửi; client chỉ phát lại. `projectiles[]` là đường bay (`path[]` với `time`) của từng viên đạn. `events[]` sắp theo `t` (giây kể từ lúc bắn):

| `type` | Trường | Ghi chú |
|--------|--------|---------|
| `explode` | `projectileId, position, radius, terrainVersion` | Khoét terrain tại `t` (cuối đường bay) |
| `damage` | `playerId, amount, hp, cause, projectileId?, critical?` | `cause`: `explosion`, `fall`, `out_of_map` |
| `heal` | `playerId, amount, hp` | |
| `status` | `playerId, effectId, turns` | |
| `fall` | `playerId, from, to, duration` | Rơi từ `t` đến `t + duration`; damage rơi nằm ở mốc chạm đất |
| `die` | `playerId, cause` | |
| `teleport` | `playerId, from, to` | |

`duration` là lúc animation cuối cùng kết thúc; server kết thúc lượt sau `duration + 1.2s`.

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
  rawDamage = (weaponDamage + shooter.damageBonus) * itemFactor * multiplier
    // itemFactor: power_shot ×1.5; skill: × skill.damageMultiplier × (1 + skillPowerBonus/100)
  finalDamage = rawDamage * (100 / (100 + playerDefense))
  crit: ×1.5 với xác suất critChance%

Fall damage:
  threshold = 150px
  if fallDistance > threshold:
    damage = (fallDistance - 150) * 0.25
```

### Character Stat Bonuses

Khi trận bắt đầu, room đọc `bonus_*` của `player_characters` (nhân vật đang chọn) và nhân với `statMultipliers` trong `game_settings.character_progression` (key thiếu hoặc 0 → mặc định của seed 006). Bot giữ chỉ số gốc của nhân vật; nhân vật chưa sở hữu (DEV_UNLOCK_ALL) cũng vậy. Equipment cộng thêm sau đó.

| Chỉ số (`bonus_*`) | Key multiplier | Mặc định | Tác dụng trong trận (`BattlePlayerState`) |
|--------------------|----------------|----------|-------------------------------------------|
| `bonus_hp` | `hp` | 50 | `maxHp = character.hp + điểm × 50` |
| `bonus_defense` | `defense` | 5 | `defense = character.defense + điểm × 5` |
| `bonus_damage` | `damage` | 5 | `damageBonus += điểm × 5`, cộng phẳng vào damage vũ khí (cả weapon lẫn skill) |
| `bonus_mobility` | `mobility` | 3 | `moveEnergyBonus += điểm × 3`; đầu mỗi lượt `moveEnergy = 100 + moveEnergyBonus` (1 năng lượng / 2px) |
| `bonus_skill_power` | `skill_power` | 5 | `skillPowerBonus = điểm × 5` (%): damage skill × `(1 + skillPowerBonus/100)`, heal của `healing_bloom` = `round(30 × (1 + skillPowerBonus/100))` |
| `bonus_terrain_damage` | `terrain_damage` | 3 | `terrainDamageBonus = điểm × 3` (%): bán kính khoét terrain = `explosionRadius × (1 + terrainDamageBonus/100)`; vùng sát thương vẫn dùng `explosionRadius`. `radius` của event `explode` là bán kính khoét |

#### Chỉ số hiển thị (`GET /player/characters`)

Room (`getActualStats` → `newBattlePlayer`) và API cùng tính bằng package `internal/game/battlestats` (`Base`, `FromPoints`, `FromEquipment`, `Compute`, `LoadEquipmentStats`), nên số trong profile là số dùng trong trận. API load game data (`config_characters`/`config_weapons`, fallback `configs/*.yaml`) lúc khởi động như game server — sửa config nhân vật/vũ khí cần restart cả hai.

```json
{
  "characters": [{
    "characterId": "tanko", "level": 3, "statPoints": 2, "bonusHp": 2, "bonusMobility": 1, "...": "...",
    "stats": {
      "maxHp": 268, "defense": 86, "damageBonus": 6, "moveEnergy": 105,
      "skillPowerBonus": 0, "terrainDamageBonus": 0, "critChance": 2.2, "weaponDamage": 75,
      "base":          {"maxHp": 130, "defense": 80, "damageBonus": 0, "moveEnergy": 100, "skillPowerBonus": 0, "terrainDamageBonus": 0},
      "fromPoints":    {"maxHp": 100, "defense": 0,  "damageBonus": 0, "moveEnergy": 3,   "skillPowerBonus": 0, "terrainDamageBonus": 0},
      "fromEquipment": {"maxHp": 38,  "defense": 6,  "damageBonus": 6, "moveEnergy": 2,   "skillPowerBonus": 0, "terrainDamageBonus": 0, "critChance": 2.2}
    }
  }],
  "resetStatsCost": {"currency": "coin", "amount": 500},
  "baseStats": {
    "rookie": {"maxHp": 100, "defense": 50, "damageBonus": 0, "moveEnergy": 100, "skillPowerBonus": 0, "terrainDamageBonus": 0, "weaponDamage": 60},
    "tanko":  {"maxHp": 130, "defense": 80, "damageBonus": 0, "moveEnergy": 100, "skillPowerBonus": 0, "terrainDamageBonus": 0, "weaponDamage": 75}
  },
  "statMultipliers": {"hp": 50, "damage": 5, "mobility": 3, "defense": 5, "skillPower": 5, "terrainDamage": 3}
}
```

- `stats` (tổng) = `base + fromPoints + fromEquipment` từng key. Ở tổng và `base`, `moveEnergy` là năng lượng mỗi lượt (`100 + bonus`); ở `fromPoints`/`fromEquipment` là phần cộng thêm. `skillPowerBonus`, `terrainDamageBonus` là %; `damageBonus` cộng phẳng vào `weaponDamage` (damage vũ khí của nhân vật).
- `critChance` (%) chỉ có khi khác 0 (chỉ equipment/gem/set cho crit); số thực, client tự làm tròn.
- `baseStats`: mọi nhân vật trong game data (kể cả chưa sở hữu), không điểm, không equipment — cũng là chỉ số của bot.
- `statMultipliers`: giá trị một điểm, đã điền mặc định cho key thiếu/0 — client dùng để xem trước khi cộng điểm.
- Equipment (`FromEquipment`): mỗi món `round(stat × (1 + 0.02 × upgrade + milestone))` (milestone cộng dồn ở +6/+10/+14/+16, `game_settings` `equipment.milestone_*_bonus`, mặc định 0.10/0.20/0.40/1.00; crit không làm tròn), gem cộng phẳng (`hp`/`damage`/`defense` cắt phần thập phân, `critical` giữ nguyên), rồi mỗi tier crafted đủ `pieces_required` cộng dồn % của `config_set_bonuses` và nhân một lần vào tổng HP/damage/defense của equipment (crit cộng %).

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

EXP vào cả profile và nhân vật đang chơi (`player_characters.exp`, cộng dồn). Level nhân vật = level cao nhất của `game_settings.character_levels` có `expRequired` ≤ exp; mỗi level lên được `pointsPerLevel` (10) điểm. Bảng mặc định (migration 006 + 019) có level 2–80: L2–L10 giữ nguyên 200, 400, 700, 1100, 1600, 2200, 3000, 4000, 5200; từ L11 bước lên level sau tăng thêm 25:

`expRequired(L) = 5200 + 1200·n + 25·n(n+1)/2`, `n = L − 10` (bước từ L−1 lên L = 1200 + 25·n)

| Level | 10 | 15 | 20 | 30 | 35 | 40 | 55 | 75 | 80 |
|-------|----|----|----|----|----|----|----|----|----|
| EXP tích luỹ | 5.200 | 11.575 | 18.575 | 34.450 | 43.325 | 52.825 | 85.075 | 136.825 | 151.325 |
| ≈ số trận (≈90 EXP/trận) | 58 | 129 | 206 | 383 | 481 | 587 | 945 | 1.520 | 1.681 |

≈90 EXP/trận là trung bình thắng/thua (50/80) + ~300 damage (+15) + ~0,5 kill. Level 80 cao hơn level yêu cầu lớn nhất của trang bị (75). Admin thêm/bớt level ở `/api/config/character-levels`; level tối đa là level cuối bảng.

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
