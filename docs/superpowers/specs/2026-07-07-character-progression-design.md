# Character Progression System — Design Spec

**Date:** 2026-07-07

---

## 1. Overview

Mỗi character mà player sở hữu có level riêng. Sau mỗi trận đấu, character được dùng sẽ nhận EXP (cùng lượng EXP với player). Khi EXP vượt ngưỡng → lên level → nhận stat points. Stat points dùng để cộng vào 6 chỉ số của character. Chỉ số thực tế = chỉ số gốc (config) + bonus từ stat points × multiplier.

---

## 2. Database Changes

### Alter `player_characters`

```sql
ALTER TABLE player_characters
    ADD COLUMN level                INT NOT NULL DEFAULT 1,
    ADD COLUMN exp                  INT NOT NULL DEFAULT 0,
    ADD COLUMN stat_points          INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_hp             INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_damage         INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_mobility       INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_defense        INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_skill_power    INT NOT NULL DEFAULT 0,
    ADD COLUMN bonus_terrain_damage INT NOT NULL DEFAULT 0;
```

| Column | Mô tả |
|--------|-------|
| `level` | Level hiện tại của character |
| `exp` | EXP tích lũy (cộng dồn, không reset khi lên level) |
| `stat_points` | Điểm chưa dùng |
| `bonus_*` | Số điểm đã cộng vào từng chỉ số |

---

## 3. Level Up System

### Bảng ngưỡng EXP — Admin config

Key `"character_levels"` trong `game_settings`:

```json
{
  "levels": [
    {"level": 2, "expRequired": 200},
    {"level": 3, "expRequired": 400},
    {"level": 4, "expRequired": 700},
    {"level": 5, "expRequired": 1100},
    {"level": 6, "expRequired": 1600}
  ]
}
```

- `expRequired` là ngưỡng tích lũy (cộng dồn), không phải EXP mỗi level
- Admin add đến level nào thì max level là đó
- Bảng phải sorted theo level tăng dần, expRequired tăng dần

### Flow sau trận đấu

```
1. Trận kết thúc → tính expGained cho player (đã có trong reward.go)
2. Xác định character đã dùng trong trận
3. player_characters.exp += expGained
4. So sánh exp mới với bảng ngưỡng:
   - Tìm level cao nhất mà expRequired ≤ exp hiện tại
   - Nếu level mới > level cũ → tính số level tăng
   - stat_points += (level_mới - level_cũ) × pointsPerLevel
5. Update DB: exp, level, stat_points
```

**Ví dụ:**

```
Character exp=0, level=1
  → nhận 150 EXP → exp=150
  → ngưỡng level 2 = 200 → chưa đạt → vẫn level 1
  → UI: 150/200

  → nhận 120 EXP → exp=270
  → ngưỡng level 2 = 200 ✓, level 3 = 400 ✗
  → lên level 2, +10 stat_points
  → UI: 270/400

  → nhận 200 EXP → exp=470
  → ngưỡng level 3 = 400 ✓, level 4 = 700 ✗
  → lên level 3, +10 stat_points
  → UI: 470/700

  → nhận 800 EXP → exp=1270
  → ngưỡng level 4=700 ✓, level 5=1100 ✓, level 6=1600 ✗
  → nhảy 2 level (3→5), +20 stat_points
  → UI: 1270/1600
```

---

## 4. Stat Points — Allocate & Reset

### Allocate

```
Client gửi: POST /player/character/allocate-stats
{
  "characterId": "rookie",
  "hp": 3,
  "damage": 2,
  "mobility": 0,
  "defense": 0,
  "skillPower": 0,
  "terrainDamage": 0
}

Server:
  1. Tính tổng điểm yêu cầu = 3 + 2 + 0 + 0 + 0 + 0 = 5
  2. Validate: tổng ≤ stat_points hiện có
  3. Validate: tất cả giá trị ≥ 0
  4. Update:
     bonus_hp += 3
     bonus_damage += 2
     stat_points -= 5
```

### Reset

```
Client gửi: POST /player/character/reset-stats
{
  "characterId": "rookie"
}

Server:
  1. Tính tổng điểm đã dùng = bonus_hp + bonus_damage + bonus_mobility + bonus_defense + bonus_skill_power + bonus_terrain_damage
  2. Kiểm tra đủ coin/gem để reset (giá config admin)
  3. Trừ coin/gem
  4. Reset tất cả bonus_* = 0
  5. stat_points += tổng điểm đã dùng (trả lại hết)
```

### Không giới hạn

- Không giới hạn max level (admin add bảng EXP đến đâu thì max đến đó)
- Không giới hạn max điểm mỗi chỉ số (muốn dồn hết vào 1 stat cũng được)
- Không giới hạn tổng điểm (cứ lên level cứ cộng)

---

## 5. Actual Stats Calculation

Khi tạo match player hoặc trả về cho client:

```
actualHP      = config_characters.hp      + (bonus_hp      × statMultipliers.hp)
actualDamage  = config_characters.damage  + (bonus_damage  × statMultipliers.damage)
actualMobility     = config_characters.mobility      + (bonus_mobility      × statMultipliers.mobility)
actualDefense      = config_characters.defense       + (bonus_defense       × statMultipliers.defense)
actualSkillPower   = config_characters.skill_power   + (bonus_skill_power   × statMultipliers.skill_power)
actualTerrainDmg   = config_characters.terrain_damage + (bonus_terrain_damage × statMultipliers.terrain_damage)
```

`statMultipliers` config từ admin — mỗi chỉ số có multiplier riêng:

```json
{
  "hp": 50,
  "damage": 5,
  "mobility": 3,
  "defense": 5,
  "skill_power": 5,
  "terrain_damage": 3
}
```

Ví dụ: character "rookie" có base HP = 100, player cộng 3 điểm HP, multiplier = 50
→ actualHP = 100 + (3 × 50) = 250

---

## 6. Admin Config

### game_settings entries

**Key `"character_progression"`:**

```json
{
  "pointsPerLevel": 10,
  "resetCostCurrency": "coin",
  "resetCostAmount": 500,
  "statMultipliers": {
    "hp": 50,
    "damage": 5,
    "mobility": 3,
    "defense": 5,
    "skill_power": 5,
    "terrain_damage": 3
  }
}
```

**Key `"character_levels"`:**

```json
{
  "levels": [
    {"level": 2, "expRequired": 200},
    {"level": 3, "expRequired": 400},
    {"level": 4, "expRequired": 700},
    {"level": 5, "expRequired": 1100},
    {"level": 6, "expRequired": 1600},
    {"level": 7, "expRequired": 2200},
    {"level": 8, "expRequired": 3000},
    {"level": 9, "expRequired": 4000},
    {"level": 10, "expRequired": 5200}
  ]
}
```

### Admin Dashboard

Thêm trang `/character-progression` vào admin dashboard:

**Section 1: Progression Settings**
- `pointsPerLevel`: điểm nhận mỗi lần lên level
- `resetCostCurrency`: loại tiền reset (coin/gem)
- `resetCostAmount`: giá reset
- `statMultipliers`: 6 fields cho 6 chỉ số

**Section 2: Level Table**
- Bảng level với cột: Level, EXP Required
- Nút Add Level, Delete Level
- Admin add/xóa level tùy ý

---

## 7. API Endpoints

### REST API (API Server)

| Method | Path | Mô tả |
|--------|------|-------|
| GET | `/player/characters` | Danh sách characters đã unlock + level, exp, stats, bonus, nextLevelExp |
| POST | `/player/character/allocate-stats` | Cộng điểm vào chỉ số |
| POST | `/player/character/reset-stats` | Reset điểm (tốn coin/gem) |

### GET /player/characters response

```json
{
  "characters": [
    {
      "characterId": "rookie",
      "level": 5,
      "exp": 1270,
      "nextLevelExp": 1600,
      "isMaxLevel": false,
      "statPoints": 15,
      "baseStats": {
        "hp": 100, "damage": 30, "mobility": 50,
        "defense": 50, "skillPower": 20, "terrainDamage": 25
      },
      "bonusStats": {
        "hp": 3, "damage": 2, "mobility": 0,
        "defense": 0, "skillPower": 0, "terrainDamage": 0
      },
      "actualStats": {
        "hp": 250, "damage": 40, "mobility": 50,
        "defense": 50, "skillPower": 20, "terrainDamage": 25
      },
      "unlockedAt": "2026-06-15T10:00:00Z"
    }
  ]
}
```

Max level:
```json
{
  "level": 50,
  "exp": 52000,
  "nextLevelExp": null,
  "isMaxLevel": true
}
```

---

## 8. Match Integration

### EXP cho character sau trận

Trong `ProcessMatchRewards()` (`reward.go`), sau khi tính `expGained` cho player:

```
1. Xác định characterId từ match player state
2. Upsert player_characters: exp += expGained
3. Load bảng character_levels từ config
4. Tính new level từ exp mới
5. Nếu level tăng: stat_points += (newLevel - oldLevel) × pointsPerLevel
6. Update DB
7. Trả về levelUp info trong reward response
```

### Chỉ số thực tế trong match

Khi build `BattlePlayerState`:

```
1. Đọc config_characters (base stats)
2. Đọc player_characters (bonus_* stats)
3. Đọc statMultipliers từ game_settings
4. Tính actual stats = base + (bonus × multiplier)
5. Dùng actual stats cho HP, Defense trong match
```

Áp dụng cho cả room thường (PvP) và ranked match.

---

## 9. Admin Dashboard

Thêm route `/character-progression` + template + API endpoints:

```
GET  /api/config/character-progression  — Lấy progression config
POST /api/config/character-progression  — Cập nhật progression config
GET  /api/config/character-levels       — Lấy bảng level
POST /api/config/character-levels       — Cập nhật bảng level
```

Sidebar: thêm mục "Character Progression" dưới group "Game Config".

---

## 10. Files Affected

```
New:
  migrations/006_character_progression.up.sql   — ALTER player_characters + seed configs
  migrations/006_character_progression.down.sql
  internal/api/character/handler.go             — API handlers
  internal/api/character/service.go             — Business logic
  internal/api/character/repository.go          — DB queries
  internal/api/character/model.go               — Types
  internal/admin/templates/character_progression.html — Admin UI

Modified:
  internal/game/match/reward.go                 — Thêm character EXP sau trận
  internal/game/room/room.go                    — Đọc actual stats khi build match players
  internal/game/room/hub.go                     — Đọc actual stats cho ranked match
  internal/admin/server.go                      — Thêm routes
  internal/admin/handlers_matchmaking.go        — Thêm page handler (hoặc file mới)
  internal/admin/templates/layout.html          — Thêm nav link
  cmd/api/main.go                               — Wire character module
  cmd/migrate/main.go                           — Thêm migration 006
```
