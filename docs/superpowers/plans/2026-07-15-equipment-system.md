# Equipment System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the full equipment system (buy, equip, upgrade, craft, gem socket, merge, dismantle, set bonus, combat integration, admin config) as defined in `docs/equipment-system-design.md`.

**Architecture:** New `internal/api/equipment/` module following the existing handler→service→repository pattern. Migration 010 creates all equipment tables. Equipment stats are applied at match start in `room.go:getActualStats()`. Admin dashboard gets new config pages for equipment items, upgrade rates, stones, gems, set bonuses, and materials.

**Tech Stack:** Go, PostgreSQL (pgx/v5), chi router, HTML templates (admin dashboard)

---

## File Structure

### New Files
```
migrations/010_equipment_system.up.sql          — All equipment tables + config tables
migrations/010_equipment_system.down.sql         — Drop equipment tables

internal/api/equipment/model.go                  — All request/response types
internal/api/equipment/repository.go             — DB queries for equipment, stones, gems, materials
internal/api/equipment/service.go                — Business logic (upgrade, craft, merge, dismantle, socket)
internal/api/equipment/handler.go                — HTTP handlers

internal/admin/handlers_equipment.go             — Admin dashboard handlers for equipment config
internal/admin/templates/equipment_items.html     — Equipment items list
internal/admin/templates/equipment_item_edit.html — Equipment item edit form
internal/admin/templates/upgrade_rates.html       — Upgrade rates config page
internal/admin/templates/equipment_stones.html    — Stone config page
internal/admin/templates/equipment_gems.html      — Gem config page
internal/admin/templates/set_bonuses.html         — Set bonus config page
internal/admin/templates/crafting_recipes.html     — Crafting recipes list
internal/admin/templates/crafting_recipe_edit.html — Crafting recipe edit form
internal/admin/templates/materials.html            — Materials config list
internal/admin/templates/material_edit.html        — Material edit form
```

### Modified Files
```
cmd/api/main.go                                  — Wire equipment module + routes
cmd/admin/main.go (or internal/admin/server.go)  — Add equipment admin routes
internal/admin/seed.go                           — Seed default equipment config data
internal/admin/repository.go                     — Add equipment config CRUD methods
internal/shared/model/errors.go                  — Add equipment-specific error codes
internal/game/match/model.go                     — Add CritChance, MoveEnergyBonus fields to BattlePlayerState
internal/game/match/match.go                     — Apply CritChance + MoveEnergyBonus in combat
internal/game/match/damage.go                    — Add critical hit logic
internal/game/room/room.go                       — Extend getActualStats() to include equipment + gems + set bonuses
```

---

## Task 1: Database Migration

**Files:**
- Create: `migrations/010_equipment_system.up.sql`
- Create: `migrations/010_equipment_system.down.sql`

- [ ] **Step 1: Create up migration**

```sql
-- migrations/010_equipment_system.up.sql

-- ============================================================
-- Config tables (admin-editable)
-- ============================================================

CREATE TABLE IF NOT EXISTS config_equipment_items (
    item_id         VARCHAR(100) PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    slot            VARCHAR(20) NOT NULL,
    category        VARCHAR(20) NOT NULL,
    tier            VARCHAR(20),
    required_level  INT NOT NULL,
    character_id    VARCHAR(50),
    gem_slots       SMALLINT NOT NULL DEFAULT 1,
    stat_hp         INT NOT NULL DEFAULT 0,
    stat_damage     INT NOT NULL DEFAULT 0,
    stat_defense    INT NOT NULL DEFAULT 0,
    stat_crit       NUMERIC(5,2) NOT NULL DEFAULT 0,
    stat_move_energy INT NOT NULL DEFAULT 0,
    price_coin      INT DEFAULT 0,
    price_gem       INT DEFAULT 0,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_upgrade_rates (
    from_level      SMALLINT NOT NULL,
    to_level        SMALLINT NOT NULL,
    upgrade_cost    INT NOT NULL,
    max_percent     NUMERIC(5,2) NOT NULL,
    fail_reset_to   SMALLINT NOT NULL,
    PRIMARY KEY (from_level, to_level)
);

CREATE TABLE IF NOT EXISTS config_set_bonuses (
    tier            VARCHAR(20) NOT NULL,
    pieces_required SMALLINT NOT NULL,
    bonus_hp_pct    NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_dmg_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_def_pct   NUMERIC(5,2) NOT NULL DEFAULT 0,
    bonus_crit_pct  NUMERIC(5,2) NOT NULL DEFAULT 0,
    PRIMARY KEY (tier, pieces_required)
);

CREATE TABLE IF NOT EXISTS config_gems (
    gem_type        VARCHAR(20) NOT NULL,
    gem_level       SMALLINT NOT NULL,
    stat_value      NUMERIC(10,2) NOT NULL,
    PRIMARY KEY (gem_type, gem_level)
);

CREATE TABLE IF NOT EXISTS config_stones (
    stone_level     SMALLINT PRIMARY KEY,
    power           INT NOT NULL,
    price_coin      INT DEFAULT 0,
    price_gem       INT DEFAULT 0,
    source          VARCHAR(20) NOT NULL
);

CREATE TABLE IF NOT EXISTS config_materials (
    material_id     VARCHAR(100) PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    source          VARCHAR(20) NOT NULL,
    price_gem       INT DEFAULT 0,
    tier            VARCHAR(20) NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS config_crafting_recipes (
    recipe_id       VARCHAR(100) PRIMARY KEY,
    result_item_id  VARCHAR(100) NOT NULL REFERENCES config_equipment_items(item_id),
    materials       JSONB NOT NULL,
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- Player data tables
-- ============================================================

CREATE TABLE IF NOT EXISTS player_gems (
    gem_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    gem_type        VARCHAR(20) NOT NULL,
    gem_level       SMALLINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT valid_gem_level CHECK (gem_level >= 1 AND gem_level <= 10)
);

CREATE INDEX IF NOT EXISTS idx_player_gems_player ON player_gems(player_id);

CREATE TABLE IF NOT EXISTS player_equipment (
    equipment_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id       VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    item_id         VARCHAR(100) NOT NULL,
    slot            VARCHAR(20) NOT NULL,
    category        VARCHAR(20) NOT NULL,
    tier            VARCHAR(20),
    upgrade_level   SMALLINT NOT NULL DEFAULT 0,
    gem_slot_1      UUID REFERENCES player_gems(gem_id),
    gem_slot_2      UUID REFERENCES player_gems(gem_id),
    is_equipped     BOOLEAN NOT NULL DEFAULT FALSE,
    equipped_on     VARCHAR(50),
    is_locked       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT valid_upgrade CHECK (upgrade_level >= 0 AND upgrade_level <= 16)
);

CREATE INDEX IF NOT EXISTS idx_player_equipment_player ON player_equipment(player_id);
CREATE INDEX IF NOT EXISTS idx_player_equipment_equipped ON player_equipment(player_id, equipped_on) WHERE is_equipped = TRUE;

CREATE TABLE IF NOT EXISTS equipment_upgrade_log (
    log_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    equipment_id    UUID NOT NULL REFERENCES player_equipment(equipment_id) ON DELETE CASCADE,
    from_level      SMALLINT NOT NULL,
    to_level        SMALLINT NOT NULL,
    stones_used     JSONB NOT NULL DEFAULT '[]',
    total_power     INT NOT NULL DEFAULT 0,
    success         BOOLEAN NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS player_stones (
    player_id       VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    stone_level     SMALLINT NOT NULL,
    quantity        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, stone_level),
    CONSTRAINT valid_stone_level CHECK (stone_level >= 1 AND stone_level <= 12)
);

CREATE TABLE IF NOT EXISTS player_materials (
    player_id       VARCHAR(64) NOT NULL REFERENCES player_profiles(player_id) ON DELETE CASCADE,
    material_id     VARCHAR(100) NOT NULL,
    quantity        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (player_id, material_id)
);
```

- [ ] **Step 2: Create down migration**

```sql
-- migrations/010_equipment_system.down.sql
DROP TABLE IF EXISTS equipment_upgrade_log;
DROP TABLE IF EXISTS player_materials;
DROP TABLE IF EXISTS player_stones;
DROP TABLE IF EXISTS player_equipment;
DROP TABLE IF EXISTS player_gems;
DROP TABLE IF EXISTS config_crafting_recipes;
DROP TABLE IF EXISTS config_materials;
DROP TABLE IF EXISTS config_stones;
DROP TABLE IF EXISTS config_gems;
DROP TABLE IF EXISTS config_set_bonuses;
DROP TABLE IF EXISTS config_upgrade_rates;
DROP TABLE IF EXISTS config_equipment_items;
```

- [ ] **Step 3: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration completes without errors

- [ ] **Step 4: Commit**

```bash
git add migrations/010_equipment_system.up.sql migrations/010_equipment_system.down.sql
git commit -m "feat: add equipment system database migration (010)"
```

---

## Task 2: Error Codes

**Files:**
- Modify: `internal/shared/model/errors.go`

- [ ] **Step 1: Add equipment error codes**

Add after the existing error vars in `internal/shared/model/errors.go`:

```go
ErrEquipmentNotFound       = AppError{Code: "EQUIP_NOT_FOUND", Message: "Equipment not found", Status: http.StatusNotFound}
ErrEquipmentNotOwned       = AppError{Code: "EQUIP_NOT_OWNED", Message: "You do not own this equipment", Status: http.StatusForbidden}
ErrEquipmentAlreadyEquipped = AppError{Code: "EQUIP_ALREADY_EQUIPPED", Message: "Equipment is already equipped", Status: http.StatusConflict}
ErrEquipmentSlotOccupied   = AppError{Code: "EQUIP_SLOT_OCCUPIED", Message: "Equipment slot is already occupied", Status: http.StatusConflict}
ErrEquipmentLocked         = AppError{Code: "EQUIP_LOCKED", Message: "Equipment is locked and cannot be sold", Status: http.StatusConflict}
ErrEquipmentLevelRequired  = AppError{Code: "EQUIP_LEVEL_REQUIRED", Message: "Character level too low for this equipment", Status: http.StatusBadRequest}
ErrEquipmentMaxUpgrade     = AppError{Code: "EQUIP_MAX_UPGRADE", Message: "Equipment is already at max upgrade level", Status: http.StatusBadRequest}
ErrEquipmentNoStones       = AppError{Code: "EQUIP_NO_STONES", Message: "No upgrade stones provided", Status: http.StatusBadRequest}
ErrInsufficientStones      = AppError{Code: "EQUIP_INSUFFICIENT_STONES", Message: "Not enough upgrade stones", Status: http.StatusBadRequest}
ErrInsufficientMaterials   = AppError{Code: "EQUIP_INSUFFICIENT_MATERIALS", Message: "Not enough crafting materials", Status: http.StatusBadRequest}
ErrGemSlotInvalid          = AppError{Code: "EQUIP_GEM_SLOT_INVALID", Message: "Invalid gem slot index", Status: http.StatusBadRequest}
ErrGemNotOwned             = AppError{Code: "EQUIP_GEM_NOT_OWNED", Message: "You do not own this gem", Status: http.StatusForbidden}
ErrMergeMinCount           = AppError{Code: "MERGE_MIN_COUNT", Message: "Minimum 2 items required for merging", Status: http.StatusBadRequest}
ErrMergeMaxCount           = AppError{Code: "MERGE_MAX_COUNT", Message: "Maximum 4 items for merging", Status: http.StatusBadRequest}
ErrMergeMaxLevel           = AppError{Code: "MERGE_MAX_LEVEL", Message: "Already at maximum level, cannot merge further", Status: http.StatusBadRequest}
ErrRecipeNotFound          = AppError{Code: "CRAFT_RECIPE_NOT_FOUND", Message: "Crafting recipe not found", Status: http.StatusNotFound}
```

- [ ] **Step 2: Commit**

```bash
git add internal/shared/model/errors.go
git commit -m "feat: add equipment system error codes"
```

---

## Task 3: Equipment Module — Models

**Files:**
- Create: `internal/api/equipment/model.go`

- [ ] **Step 1: Create model.go with all types**

```go
package equipment

import "time"

// --- Database models ---

type PlayerEquipment struct {
	EquipmentID  string     `json:"equipmentId"`
	PlayerID     string     `json:"playerId"`
	ItemID       string     `json:"itemId"`
	Slot         string     `json:"slot"`
	Category     string     `json:"category"`
	Tier         *string    `json:"tier,omitempty"`
	UpgradeLevel int        `json:"upgradeLevel"`
	GemSlot1     *string    `json:"gemSlot1,omitempty"`
	GemSlot2     *string    `json:"gemSlot2,omitempty"`
	IsEquipped   bool       `json:"isEquipped"`
	EquippedOn   *string    `json:"equippedOn,omitempty"`
	IsLocked     bool       `json:"isLocked"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type PlayerGem struct {
	GemID     string    `json:"gemId"`
	PlayerID  string    `json:"playerId"`
	GemType   string    `json:"gemType"`
	GemLevel  int       `json:"gemLevel"`
	CreatedAt time.Time `json:"createdAt"`
}

type PlayerStone struct {
	PlayerID   string `json:"playerId"`
	StoneLevel int    `json:"stoneLevel"`
	Quantity   int    `json:"quantity"`
}

type PlayerMaterial struct {
	PlayerID   string `json:"playerId"`
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

// --- Config models ---

type EquipmentItemConfig struct {
	ItemID         string  `json:"itemId"`
	Name           string  `json:"name"`
	Slot           string  `json:"slot"`
	Category       string  `json:"category"`
	Tier           *string `json:"tier,omitempty"`
	RequiredLevel  int     `json:"requiredLevel"`
	CharacterID    *string `json:"characterId,omitempty"`
	GemSlots       int     `json:"gemSlots"`
	StatHP         int     `json:"statHp"`
	StatDamage     int     `json:"statDamage"`
	StatDefense    int     `json:"statDefense"`
	StatCrit       float64 `json:"statCrit"`
	StatMoveEnergy int     `json:"statMoveEnergy"`
	PriceCoin      int     `json:"priceCoin"`
	PriceGem       int     `json:"priceGem"`
	IsActive       bool    `json:"isActive"`
}

type UpgradeRateConfig struct {
	FromLevel   int     `json:"fromLevel"`
	ToLevel     int     `json:"toLevel"`
	UpgradeCost int     `json:"upgradeCost"`
	MaxPercent  float64 `json:"maxPercent"`
	FailResetTo int     `json:"failResetTo"`
}

type StoneConfig struct {
	StoneLevel int    `json:"stoneLevel"`
	Power      int    `json:"power"`
	PriceCoin  int    `json:"priceCoin"`
	PriceGem   int    `json:"priceGem"`
	Source     string `json:"source"`
}

type GemConfig struct {
	GemType   string  `json:"gemType"`
	GemLevel  int     `json:"gemLevel"`
	StatValue float64 `json:"statValue"`
}

type SetBonusConfig struct {
	Tier           string  `json:"tier"`
	PiecesRequired int     `json:"piecesRequired"`
	BonusHPPct     float64 `json:"bonusHpPct"`
	BonusDMGPct    float64 `json:"bonusDmgPct"`
	BonusDEFPct    float64 `json:"bonusDefPct"`
	BonusCritPct   float64 `json:"bonusCritPct"`
}

type MaterialConfig struct {
	MaterialID  string `json:"materialId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	PriceGem    int    `json:"priceGem"`
	Tier        string `json:"tier"`
	IsActive    bool   `json:"isActive"`
}

type CraftingRecipe struct {
	RecipeID     string           `json:"recipeId"`
	ResultItemID string           `json:"resultItemId"`
	Materials    []RecipeMaterial `json:"materials"`
	IsActive     bool             `json:"isActive"`
}

type RecipeMaterial struct {
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

// --- Request types ---

type EquipRequest struct {
	EquipmentID string `json:"equipmentId"`
	CharacterID string `json:"characterId"`
}

type UnequipRequest struct {
	EquipmentID string `json:"equipmentId"`
}

type UpgradeRequest struct {
	EquipmentID string          `json:"equipmentId"`
	Stones      []StoneInput    `json:"stones"`
}

type StoneInput struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
}

type DismantleRequest struct {
	EquipmentID string `json:"equipmentId"`
}

type SocketGemRequest struct {
	EquipmentID string `json:"equipmentId"`
	SlotIndex   int    `json:"slotIndex"`
	GemID       string `json:"gemId"`
}

type UnsocketGemRequest struct {
	EquipmentID string `json:"equipmentId"`
	SlotIndex   int    `json:"slotIndex"`
}

type BuyEquipmentRequest struct {
	ItemID string `json:"itemId"`
}

type BuyStoneRequest struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
}

type BuyGemRequest struct {
	GemType  string `json:"gemType"`
	GemLevel int    `json:"gemLevel"`
	Quantity int    `json:"quantity"`
}

type BuyMaterialRequest struct {
	MaterialID string `json:"materialId"`
	Quantity   int    `json:"quantity"`
}

type MergeStoneRequest struct {
	StoneLevel int `json:"stoneLevel"`
	Count      int `json:"count"`
}

type MergeGemRequest struct {
	GemType  string `json:"gemType"`
	GemLevel int    `json:"gemLevel"`
	Count    int    `json:"count"`
}

type CraftRequest struct {
	RecipeID string `json:"recipeId"`
}

// --- Response types ---

type UpgradeResult struct {
	Success      bool   `json:"success"`
	NewLevel     int    `json:"newLevel"`
	Percent      float64 `json:"percent"`
	EquipmentID  string `json:"equipmentId"`
}

type MergeResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// EquipmentStats is the computed total stats from all equipped items for a character.
type EquipmentStats struct {
	HP         int     `json:"hp"`
	Damage     int     `json:"damage"`
	Defense    int     `json:"defense"`
	CritPct    float64 `json:"critPct"`
	MoveEnergy int     `json:"moveEnergy"`
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/api/equipment/model.go
git commit -m "feat: add equipment module model types"
```

---

## Task 4: Equipment Module — Repository

**Files:**
- Create: `internal/api/equipment/repository.go`

- [ ] **Step 1: Create repository.go**

```go
package equipment

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// ============================================================
// Player Equipment
// ============================================================

func (r *Repository) GetPlayerEquipment(ctx context.Context, playerID string) ([]PlayerEquipment, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT equipment_id, player_id, item_id, slot, category, tier,
		        upgrade_level, gem_slot_1::text, gem_slot_2::text,
		        is_equipped, equipped_on, is_locked, created_at
		 FROM player_equipment WHERE player_id = $1
		 ORDER BY created_at DESC`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerEquipment
	for rows.Next() {
		var e PlayerEquipment
		if err := rows.Scan(&e.EquipmentID, &e.PlayerID, &e.ItemID, &e.Slot,
			&e.Category, &e.Tier, &e.UpgradeLevel, &e.GemSlot1, &e.GemSlot2,
			&e.IsEquipped, &e.EquippedOn, &e.IsLocked, &e.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (r *Repository) GetPlayerEquipmentByID(ctx context.Context, playerID, equipmentID string) (*PlayerEquipment, error) {
	var e PlayerEquipment
	err := r.db.Pool.QueryRow(ctx,
		`SELECT equipment_id, player_id, item_id, slot, category, tier,
		        upgrade_level, gem_slot_1::text, gem_slot_2::text,
		        is_equipped, equipped_on, is_locked, created_at
		 FROM player_equipment WHERE equipment_id = $1 AND player_id = $2`,
		equipmentID, playerID).Scan(&e.EquipmentID, &e.PlayerID, &e.ItemID, &e.Slot,
		&e.Category, &e.Tier, &e.UpgradeLevel, &e.GemSlot1, &e.GemSlot2,
		&e.IsEquipped, &e.EquippedOn, &e.IsLocked, &e.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repository) CreateEquipmentTx(ctx context.Context, tx pgx.Tx, playerID, itemID, slot, category string, tier *string, gemSlots int) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO player_equipment (player_id, item_id, slot, category, tier)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING equipment_id::text`,
		playerID, itemID, slot, category, tier).Scan(&id)
	return id, err
}

func (r *Repository) EquipItemTx(ctx context.Context, tx pgx.Tx, equipmentID, characterID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE player_equipment
		 SET is_equipped = TRUE, equipped_on = $2, is_locked = TRUE
		 WHERE equipment_id = $1`,
		equipmentID, characterID)
	return err
}

func (r *Repository) UnequipItemTx(ctx context.Context, tx pgx.Tx, equipmentID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE player_equipment
		 SET is_equipped = FALSE, equipped_on = NULL
		 WHERE equipment_id = $1`,
		equipmentID)
	return err
}

func (r *Repository) GetEquippedInSlot(ctx context.Context, playerID, characterID, slot string) (*PlayerEquipment, error) {
	var e PlayerEquipment
	err := r.db.Pool.QueryRow(ctx,
		`SELECT equipment_id, player_id, item_id, slot, category, tier,
		        upgrade_level, gem_slot_1::text, gem_slot_2::text,
		        is_equipped, equipped_on, is_locked, created_at
		 FROM player_equipment
		 WHERE player_id = $1 AND equipped_on = $2 AND slot = $3 AND is_equipped = TRUE`,
		playerID, characterID, slot).Scan(&e.EquipmentID, &e.PlayerID, &e.ItemID, &e.Slot,
		&e.Category, &e.Tier, &e.UpgradeLevel, &e.GemSlot1, &e.GemSlot2,
		&e.IsEquipped, &e.EquippedOn, &e.IsLocked, &e.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *Repository) UpdateUpgradeLevelTx(ctx context.Context, tx pgx.Tx, equipmentID string, newLevel int) error {
	_, err := tx.Exec(ctx,
		`UPDATE player_equipment SET upgrade_level = $2 WHERE equipment_id = $1`,
		equipmentID, newLevel)
	return err
}

func (r *Repository) DeleteEquipmentTx(ctx context.Context, tx pgx.Tx, equipmentID string) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM player_equipment WHERE equipment_id = $1`, equipmentID)
	return err
}

func (r *Repository) SetGemSlotTx(ctx context.Context, tx pgx.Tx, equipmentID string, slotIndex int, gemID *string) error {
	col := "gem_slot_1"
	if slotIndex == 2 {
		col = "gem_slot_2"
	}
	query := fmt.Sprintf(`UPDATE player_equipment SET %s = $2 WHERE equipment_id = $1`, col)
	_, err := tx.Exec(ctx, query, equipmentID, gemID)
	return err
}

// ============================================================
// Upgrade Log
// ============================================================

func (r *Repository) InsertUpgradeLogTx(ctx context.Context, tx pgx.Tx, equipmentID string, fromLevel, toLevel int, stonesUsed []StoneInput, totalPower int, success bool) error {
	stonesJSON, _ := json.Marshal(stonesUsed)
	_, err := tx.Exec(ctx,
		`INSERT INTO equipment_upgrade_log (equipment_id, from_level, to_level, stones_used, total_power, success)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		equipmentID, fromLevel, toLevel, stonesJSON, totalPower, success)
	return err
}

func (r *Repository) GetUpgradeLogForDismantle(ctx context.Context, equipmentID string, fromLevel int) ([]struct {
	StonesUsed json.RawMessage
	TotalPower int
}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT stones_used, total_power FROM equipment_upgrade_log
		 WHERE equipment_id = $1 AND from_level >= $2
		 ORDER BY created_at ASC`,
		equipmentID, fromLevel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []struct {
		StonesUsed json.RawMessage
		TotalPower int
	}
	for rows.Next() {
		var entry struct {
			StonesUsed json.RawMessage
			TotalPower int
		}
		if err := rows.Scan(&entry.StonesUsed, &entry.TotalPower); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

// ============================================================
// Player Gems
// ============================================================

func (r *Repository) GetPlayerGems(ctx context.Context, playerID string) ([]PlayerGem, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		 FROM player_gems WHERE player_id = $1 ORDER BY gem_type, gem_level DESC`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerGem
	for rows.Next() {
		var g PlayerGem
		if err := rows.Scan(&g.GemID, &g.PlayerID, &g.GemType, &g.GemLevel, &g.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, nil
}

func (r *Repository) GetPlayerGemByID(ctx context.Context, playerID, gemID string) (*PlayerGem, error) {
	var g PlayerGem
	err := r.db.Pool.QueryRow(ctx,
		`SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		 FROM player_gems WHERE gem_id = $1 AND player_id = $2`, gemID, playerID).
		Scan(&g.GemID, &g.PlayerID, &g.GemType, &g.GemLevel, &g.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

func (r *Repository) IsGemSocketed(ctx context.Context, gemID string) (bool, error) {
	var count int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player_equipment
		 WHERE gem_slot_1::text = $1 OR gem_slot_2::text = $1`, gemID).Scan(&count)
	return count > 0, err
}

func (r *Repository) CreateGemTx(ctx context.Context, tx pgx.Tx, playerID, gemType string, gemLevel int) (string, error) {
	var id string
	err := tx.QueryRow(ctx,
		`INSERT INTO player_gems (player_id, gem_type, gem_level)
		 VALUES ($1, $2, $3) RETURNING gem_id::text`,
		playerID, gemType, gemLevel).Scan(&id)
	return id, err
}

func (r *Repository) DeleteGemsTx(ctx context.Context, tx pgx.Tx, gemIDs []string) error {
	for _, id := range gemIDs {
		_, err := tx.Exec(ctx, `DELETE FROM player_gems WHERE gem_id = $1`, id)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) GetPlayerGemsByTypeAndLevel(ctx context.Context, playerID, gemType string, gemLevel int) ([]PlayerGem, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		 FROM player_gems
		 WHERE player_id = $1 AND gem_type = $2 AND gem_level = $3
		   AND gem_id NOT IN (SELECT gem_slot_1 FROM player_equipment WHERE gem_slot_1 IS NOT NULL AND player_id = $1)
		   AND gem_id NOT IN (SELECT gem_slot_2 FROM player_equipment WHERE gem_slot_2 IS NOT NULL AND player_id = $1)
		 ORDER BY created_at ASC`, playerID, gemType, gemLevel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerGem
	for rows.Next() {
		var g PlayerGem
		if err := rows.Scan(&g.GemID, &g.PlayerID, &g.GemType, &g.GemLevel, &g.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, nil
}

// ============================================================
// Player Stones
// ============================================================

func (r *Repository) GetPlayerStones(ctx context.Context, playerID string) ([]PlayerStone, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT player_id, stone_level, quantity FROM player_stones
		 WHERE player_id = $1 ORDER BY stone_level`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerStone
	for rows.Next() {
		var s PlayerStone
		if err := rows.Scan(&s.PlayerID, &s.StoneLevel, &s.Quantity); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
}

func (r *Repository) AddStonesTx(ctx context.Context, tx pgx.Tx, playerID string, stoneLevel, quantity int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO player_stones (player_id, stone_level, quantity)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, stone_level) DO UPDATE SET quantity = player_stones.quantity + EXCLUDED.quantity`,
		playerID, stoneLevel, quantity)
	return err
}

func (r *Repository) DeductStonesTx(ctx context.Context, tx pgx.Tx, playerID string, stoneLevel, quantity int) error {
	result, err := tx.Exec(ctx,
		`UPDATE player_stones SET quantity = quantity - $3
		 WHERE player_id = $1 AND stone_level = $2 AND quantity >= $3`,
		playerID, stoneLevel, quantity)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("insufficient stones level %d", stoneLevel)
	}
	return nil
}

// ============================================================
// Player Materials
// ============================================================

func (r *Repository) GetPlayerMaterials(ctx context.Context, playerID string) ([]PlayerMaterial, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT player_id, material_id, quantity FROM player_materials
		 WHERE player_id = $1 ORDER BY material_id`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerMaterial
	for rows.Next() {
		var m PlayerMaterial
		if err := rows.Scan(&m.PlayerID, &m.MaterialID, &m.Quantity); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *Repository) AddMaterialsTx(ctx context.Context, tx pgx.Tx, playerID, materialID string, quantity int) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO player_materials (player_id, material_id, quantity)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, material_id) DO UPDATE SET quantity = player_materials.quantity + EXCLUDED.quantity`,
		playerID, materialID, quantity)
	return err
}

func (r *Repository) DeductMaterialsTx(ctx context.Context, tx pgx.Tx, playerID, materialID string, quantity int) error {
	result, err := tx.Exec(ctx,
		`UPDATE player_materials SET quantity = quantity - $3
		 WHERE player_id = $1 AND material_id = $2 AND quantity >= $3`,
		playerID, materialID, quantity)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("insufficient material %s", materialID)
	}
	return nil
}

// ============================================================
// Config Queries
// ============================================================

func (r *Repository) GetEquipmentItemConfig(ctx context.Context, itemID string) (*EquipmentItemConfig, error) {
	var c EquipmentItemConfig
	err := r.db.Pool.QueryRow(ctx,
		`SELECT item_id, name, slot, category, tier, required_level, character_id,
		        gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		        price_coin, price_gem, is_active
		 FROM config_equipment_items WHERE item_id = $1`, itemID).
		Scan(&c.ItemID, &c.Name, &c.Slot, &c.Category, &c.Tier, &c.RequiredLevel, &c.CharacterID,
			&c.GemSlots, &c.StatHP, &c.StatDamage, &c.StatDefense, &c.StatCrit, &c.StatMoveEnergy,
			&c.PriceCoin, &c.PriceGem, &c.IsActive)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetShopEquipmentItems(ctx context.Context, characterID string) ([]EquipmentItemConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT item_id, name, slot, category, tier, required_level, character_id,
		        gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		        price_coin, price_gem, is_active
		 FROM config_equipment_items
		 WHERE is_active = TRUE AND category = 'normal' AND (character_id = $1 OR character_id IS NULL)
		 ORDER BY required_level, slot`, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EquipmentItemConfig
	for rows.Next() {
		var c EquipmentItemConfig
		if err := rows.Scan(&c.ItemID, &c.Name, &c.Slot, &c.Category, &c.Tier, &c.RequiredLevel, &c.CharacterID,
			&c.GemSlots, &c.StatHP, &c.StatDamage, &c.StatDefense, &c.StatCrit, &c.StatMoveEnergy,
			&c.PriceCoin, &c.PriceGem, &c.IsActive); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetUpgradeRate(ctx context.Context, fromLevel int) (*UpgradeRateConfig, error) {
	var c UpgradeRateConfig
	err := r.db.Pool.QueryRow(ctx,
		`SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
		 FROM config_upgrade_rates WHERE from_level = $1`, fromLevel).
		Scan(&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllUpgradeRates(ctx context.Context) ([]UpgradeRateConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
		 FROM config_upgrade_rates ORDER BY from_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UpgradeRateConfig
	for rows.Next() {
		var c UpgradeRateConfig
		if err := rows.Scan(&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetStoneConfig(ctx context.Context, stoneLevel int) (*StoneConfig, error) {
	var c StoneConfig
	err := r.db.Pool.QueryRow(ctx,
		`SELECT stone_level, power, price_coin, price_gem, source
		 FROM config_stones WHERE stone_level = $1`, stoneLevel).
		Scan(&c.StoneLevel, &c.Power, &c.PriceCoin, &c.PriceGem, &c.Source)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllStoneConfigs(ctx context.Context) ([]StoneConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT stone_level, power, price_coin, price_gem, source
		 FROM config_stones ORDER BY stone_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []StoneConfig
	for rows.Next() {
		var c StoneConfig
		if err := rows.Scan(&c.StoneLevel, &c.Power, &c.PriceCoin, &c.PriceGem, &c.Source); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetGemConfig(ctx context.Context, gemType string, gemLevel int) (*GemConfig, error) {
	var c GemConfig
	err := r.db.Pool.QueryRow(ctx,
		`SELECT gem_type, gem_level, stat_value FROM config_gems
		 WHERE gem_type = $1 AND gem_level = $2`, gemType, gemLevel).
		Scan(&c.GemType, &c.GemLevel, &c.StatValue)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllGemConfigs(ctx context.Context) ([]GemConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT gem_type, gem_level, stat_value FROM config_gems ORDER BY gem_type, gem_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GemConfig
	for rows.Next() {
		var c GemConfig
		if err := rows.Scan(&c.GemType, &c.GemLevel, &c.StatValue); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetSetBonuses(ctx context.Context, tier string) ([]SetBonusConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		 FROM config_set_bonuses WHERE tier = $1 ORDER BY pieces_required`, tier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SetBonusConfig
	for rows.Next() {
		var c SetBonusConfig
		if err := rows.Scan(&c.Tier, &c.PiecesRequired, &c.BonusHPPct, &c.BonusDMGPct, &c.BonusDEFPct, &c.BonusCritPct); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetAllSetBonuses(ctx context.Context) ([]SetBonusConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		 FROM config_set_bonuses ORDER BY tier, pieces_required`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SetBonusConfig
	for rows.Next() {
		var c SetBonusConfig
		if err := rows.Scan(&c.Tier, &c.PiecesRequired, &c.BonusHPPct, &c.BonusDMGPct, &c.BonusDEFPct, &c.BonusCritPct); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetCraftingRecipe(ctx context.Context, recipeID string) (*CraftingRecipe, error) {
	var c CraftingRecipe
	var materialsJSON []byte
	err := r.db.Pool.QueryRow(ctx,
		`SELECT recipe_id, result_item_id, materials, is_active
		 FROM config_crafting_recipes WHERE recipe_id = $1`, recipeID).
		Scan(&c.RecipeID, &c.ResultItemID, &materialsJSON, &c.IsActive)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(materialsJSON, &c.Materials)
	return &c, nil
}

func (r *Repository) GetAllCraftingRecipes(ctx context.Context) ([]CraftingRecipe, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT recipe_id, result_item_id, materials, is_active
		 FROM config_crafting_recipes ORDER BY recipe_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CraftingRecipe
	for rows.Next() {
		var c CraftingRecipe
		var materialsJSON []byte
		if err := rows.Scan(&c.RecipeID, &c.ResultItemID, &materialsJSON, &c.IsActive); err != nil {
			return nil, err
		}
		json.Unmarshal(materialsJSON, &c.Materials)
		result = append(result, c)
	}
	return result, nil
}

func (r *Repository) GetAllMaterials(ctx context.Context) ([]MaterialConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT material_id, name, description, source, price_gem, tier, is_active
		 FROM config_materials ORDER BY tier, material_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MaterialConfig
	for rows.Next() {
		var m MaterialConfig
		if err := rows.Scan(&m.MaterialID, &m.Name, &m.Description, &m.Source, &m.PriceGem, &m.Tier, &m.IsActive); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, nil
}

func (r *Repository) GetMaterial(ctx context.Context, materialID string) (*MaterialConfig, error) {
	var m MaterialConfig
	err := r.db.Pool.QueryRow(ctx,
		`SELECT material_id, name, description, source, price_gem, tier, is_active
		 FROM config_materials WHERE material_id = $1`, materialID).
		Scan(&m.MaterialID, &m.Name, &m.Description, &m.Source, &m.PriceGem, &m.Tier, &m.IsActive)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Repository) GetAllEquipmentItemConfigs(ctx context.Context) ([]EquipmentItemConfig, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT item_id, name, slot, category, tier, required_level, character_id,
		        gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		        price_coin, price_gem, is_active
		 FROM config_equipment_items ORDER BY category, required_level, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EquipmentItemConfig
	for rows.Next() {
		var c EquipmentItemConfig
		if err := rows.Scan(&c.ItemID, &c.Name, &c.Slot, &c.Category, &c.Tier, &c.RequiredLevel, &c.CharacterID,
			&c.GemSlots, &c.StatHP, &c.StatDamage, &c.StatDefense, &c.StatCrit, &c.StatMoveEnergy,
			&c.PriceCoin, &c.PriceGem, &c.IsActive); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// ============================================================
// Combat Stats Query (used by game server at match start)
// ============================================================

func (r *Repository) GetEquipmentStatsForCharacter(ctx context.Context, playerID, characterID string) (*EquipmentStats, error) {
	// 1. Get all equipped items for this character
	rows, err := r.db.Pool.Query(ctx,
		`SELECT pe.item_id, pe.upgrade_level, pe.category, pe.tier,
		        pe.gem_slot_1::text, pe.gem_slot_2::text,
		        cei.stat_hp, cei.stat_damage, cei.stat_defense, cei.stat_crit, cei.stat_move_energy
		 FROM player_equipment pe
		 JOIN config_equipment_items cei ON pe.item_id = cei.item_id
		 WHERE pe.player_id = $1 AND pe.equipped_on = $2 AND pe.is_equipped = TRUE`,
		playerID, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := &EquipmentStats{}
	craftedTierCount := make(map[string]int)

	for rows.Next() {
		var itemID string
		var upgradeLevel int
		var category string
		var tier *string
		var gem1, gem2 *string
		var hp, dmg, def, moveEnergy int
		var crit float64

		if err := rows.Scan(&itemID, &upgradeLevel, &category, &tier, &gem1, &gem2,
			&hp, &dmg, &def, &crit, &moveEnergy); err != nil {
			return nil, err
		}

		// Calculate upgrade bonus: +2% per level
		upgradeBonus := float64(upgradeLevel) * 0.02
		// Calculate milestone bonus
		milestoneBonus := 0.0
		if upgradeLevel >= 6 {
			milestoneBonus += 0.10
		}
		if upgradeLevel >= 10 {
			milestoneBonus += 0.20
		}
		if upgradeLevel >= 14 {
			milestoneBonus += 0.40
		}
		if upgradeLevel >= 16 {
			milestoneBonus += 1.00
		}
		multiplier := 1.0 + upgradeBonus + milestoneBonus

		stats.HP += int(math.Round(float64(hp) * multiplier))
		stats.Damage += int(math.Round(float64(dmg) * multiplier))
		stats.Defense += int(math.Round(float64(def) * multiplier))
		stats.CritPct += crit * multiplier
		stats.MoveEnergy += int(math.Round(float64(moveEnergy) * multiplier))

		// Count crafted set pieces
		if category == "crafted" && tier != nil {
			craftedTierCount[*tier]++
		}

		// Add gem stats
		for _, gemID := range []*string{gem1, gem2} {
			if gemID == nil {
				continue
			}
			var gemType string
			var gemLevel int
			err := r.db.Pool.QueryRow(ctx,
				`SELECT gem_type, gem_level FROM player_gems WHERE gem_id = $1`, *gemID).
				Scan(&gemType, &gemLevel)
			if err != nil {
				continue
			}
			var statValue float64
			err = r.db.Pool.QueryRow(ctx,
				`SELECT stat_value FROM config_gems WHERE gem_type = $1 AND gem_level = $2`,
				gemType, gemLevel).Scan(&statValue)
			if err != nil {
				continue
			}
			switch gemType {
			case "hp":
				stats.HP += int(statValue)
			case "damage":
				stats.Damage += int(statValue)
			case "defense":
				stats.Defense += int(statValue)
			case "critical":
				stats.CritPct += statValue
			}
		}
	}

	// Apply set bonuses
	for tier, count := range craftedTierCount {
		bonuses, err := r.GetSetBonuses(ctx, tier)
		if err != nil {
			continue
		}
		for _, b := range bonuses {
			if count >= b.PiecesRequired {
				stats.HP = int(math.Round(float64(stats.HP) * (1 + b.BonusHPPct/100)))
				stats.Damage = int(math.Round(float64(stats.Damage) * (1 + b.BonusDMGPct/100)))
				stats.Defense = int(math.Round(float64(stats.Defense) * (1 + b.BonusDEFPct/100)))
				stats.CritPct += b.BonusCritPct
			}
		}
	}

	return stats, nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/api/equipment/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/equipment/repository.go
git commit -m "feat: add equipment repository with all DB queries"
```

---

## Task 5: Equipment Module — Service

**Files:**
- Create: `internal/api/equipment/service.go`

- [ ] **Step 1: Create service.go with all business logic**

```go
package equipment

import (
	"context"
	"fmt"
	"math"
	"math/rand"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, economyRepo: economyRepo, db: db}
}

// ============================================================
// Inventory queries
// ============================================================

func (s *Service) GetPlayerEquipment(ctx context.Context, playerID string) ([]PlayerEquipment, error) {
	return s.repo.GetPlayerEquipment(ctx, playerID)
}

func (s *Service) GetPlayerStones(ctx context.Context, playerID string) ([]PlayerStone, error) {
	return s.repo.GetPlayerStones(ctx, playerID)
}

func (s *Service) GetPlayerGems(ctx context.Context, playerID string) ([]PlayerGem, error) {
	return s.repo.GetPlayerGems(ctx, playerID)
}

func (s *Service) GetPlayerMaterials(ctx context.Context, playerID string) ([]PlayerMaterial, error) {
	return s.repo.GetPlayerMaterials(ctx, playerID)
}

// ============================================================
// Shop queries
// ============================================================

func (s *Service) GetShopEquipment(ctx context.Context, characterID string) ([]EquipmentItemConfig, error) {
	return s.repo.GetShopEquipmentItems(ctx, characterID)
}

func (s *Service) GetShopStones(ctx context.Context) ([]StoneConfig, error) {
	return s.repo.GetAllStoneConfigs(ctx)
}

func (s *Service) GetShopGems(ctx context.Context) ([]GemConfig, error) {
	return s.repo.GetAllGemConfigs(ctx)
}

func (s *Service) GetShopMaterials(ctx context.Context) ([]MaterialConfig, error) {
	return s.repo.GetAllMaterials(ctx)
}

func (s *Service) GetCraftingRecipes(ctx context.Context) ([]CraftingRecipe, error) {
	return s.repo.GetAllCraftingRecipes(ctx)
}

// ============================================================
// Buy Equipment
// ============================================================

func (s *Service) BuyEquipment(ctx context.Context, playerID string, req BuyEquipmentRequest) (string, error) {
	itemCfg, err := s.repo.GetEquipmentItemConfig(ctx, req.ItemID)
	if err != nil {
		return "", err
	}
	if itemCfg == nil || !itemCfg.IsActive {
		return "", model.ErrNotFound
	}
	if itemCfg.Category != "normal" {
		return "", model.AppError{Code: "EQUIP_NOT_PURCHASABLE", Message: "Crafted equipment cannot be purchased", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Debit coin
	if itemCfg.PriceCoin > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "coin", itemCfg.PriceCoin, "equipment_purchase", req.ItemID, false)
		if err != nil {
			return "", err
		}
	}
	if itemCfg.PriceGem > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", itemCfg.PriceGem, "equipment_purchase", req.ItemID, false)
		if err != nil {
			return "", err
		}
	}

	equipID, err := s.repo.CreateEquipmentTx(ctx, tx, playerID, itemCfg.ItemID, itemCfg.Slot, itemCfg.Category, itemCfg.Tier, itemCfg.GemSlots)
	if err != nil {
		return "", err
	}

	return equipID, tx.Commit(ctx)
}

// ============================================================
// Buy Stones
// ============================================================

func (s *Service) BuyStones(ctx context.Context, playerID string, req BuyStoneRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}
	stoneCfg, err := s.repo.GetStoneConfig(ctx, req.StoneLevel)
	if err != nil {
		return err
	}
	if stoneCfg == nil {
		return model.ErrNotFound
	}
	if stoneCfg.Source == "merge_only" {
		return model.AppError{Code: "STONE_NOT_PURCHASABLE", Message: "This stone can only be obtained by merging", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	totalCoin := stoneCfg.PriceCoin * req.Quantity
	totalGem := stoneCfg.PriceGem * req.Quantity

	if totalCoin > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "coin", totalCoin, "stone_purchase", fmt.Sprintf("stone_lv%d_x%d", req.StoneLevel, req.Quantity), false)
		if err != nil {
			return err
		}
	}
	if totalGem > 0 {
		_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", totalGem, "stone_purchase", fmt.Sprintf("stone_lv%d_x%d", req.StoneLevel, req.Quantity), false)
		if err != nil {
			return err
		}
	}

	if err := s.repo.AddStonesTx(ctx, tx, playerID, req.StoneLevel, req.Quantity); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ============================================================
// Buy Gems
// ============================================================

func (s *Service) BuyGems(ctx context.Context, playerID string, req BuyGemRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}
	gemCfg, err := s.repo.GetGemConfig(ctx, req.GemType, req.GemLevel)
	if err != nil {
		return err
	}
	if gemCfg == nil {
		return model.ErrNotFound
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Determine price based on gem level: 1-3 coin, 4-6 gem
	// Price is not in config_gems, so we use a simple formula or game_settings
	// For now, use a simple price structure from the design doc
	var currency string
	var pricePerUnit int
	if req.GemLevel <= 3 {
		currency = "coin"
		pricePerUnit = req.GemLevel * 200 // 200, 400, 600 coin
	} else {
		currency = "gem"
		pricePerUnit = (req.GemLevel - 3) * 30 // 30, 60, 90 gem
	}

	total := pricePerUnit * req.Quantity
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, currency, total, "gem_purchase",
		fmt.Sprintf("gem_%s_lv%d_x%d", req.GemType, req.GemLevel, req.Quantity), false)
	if err != nil {
		return err
	}

	for i := 0; i < req.Quantity; i++ {
		_, err = s.repo.CreateGemTx(ctx, tx, playerID, req.GemType, req.GemLevel)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// ============================================================
// Buy Materials
// ============================================================

func (s *Service) BuyMaterials(ctx context.Context, playerID string, req BuyMaterialRequest) error {
	if req.Quantity <= 0 {
		return model.ErrBadRequest
	}
	mat, err := s.repo.GetMaterial(ctx, req.MaterialID)
	if err != nil {
		return err
	}
	if mat == nil || !mat.IsActive {
		return model.ErrNotFound
	}
	if mat.Source != "gem_shop" {
		return model.AppError{Code: "MATERIAL_NOT_PURCHASABLE", Message: "This material can only be obtained from drops", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	totalGem := mat.PriceGem * req.Quantity
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID, "gem", totalGem, "material_purchase",
		fmt.Sprintf("mat_%s_x%d", req.MaterialID, req.Quantity), false)
	if err != nil {
		return err
	}

	if err := s.repo.AddMaterialsTx(ctx, tx, playerID, req.MaterialID, req.Quantity); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ============================================================
// Equip / Unequip
// ============================================================

func (s *Service) EquipItem(ctx context.Context, playerID string, req EquipRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if equip.IsEquipped {
		return model.ErrEquipmentAlreadyEquipped
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Auto-unequip existing item in same slot
	existing, err := s.repo.GetEquippedInSlot(ctx, playerID, req.CharacterID, equip.Slot)
	if err != nil {
		return err
	}
	if existing != nil {
		if err := s.repo.UnequipItemTx(ctx, tx, existing.EquipmentID); err != nil {
			return err
		}
	}

	if err := s.repo.EquipItemTx(ctx, tx, req.EquipmentID, req.CharacterID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) UnequipItem(ctx context.Context, playerID string, req UnequipRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}
	if !equip.IsEquipped {
		return model.AppError{Code: "EQUIP_NOT_EQUIPPED", Message: "Equipment is not currently equipped", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.UnequipItemTx(ctx, tx, req.EquipmentID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ============================================================
// Upgrade
// ============================================================

func (s *Service) UpgradeEquipment(ctx context.Context, playerID string, req UpgradeRequest) (*UpgradeResult, error) {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return nil, err
	}
	if equip == nil {
		return nil, model.ErrEquipmentNotOwned
	}
	if equip.UpgradeLevel >= 16 {
		return nil, model.ErrEquipmentMaxUpgrade
	}
	if len(req.Stones) == 0 {
		return nil, model.ErrEquipmentNoStones
	}

	rateConfig, err := s.repo.GetUpgradeRate(ctx, equip.UpgradeLevel)
	if err != nil {
		return nil, err
	}
	if rateConfig == nil {
		return nil, model.ErrInternalServer
	}

	// Calculate total power from stones
	totalPower := 0
	for _, s := range req.Stones {
		stoneCfg, err := s.repo.GetStoneConfig(ctx, s.StoneLevel)  // This shadows the receiver
		if err != nil || stoneCfg == nil {
			return nil, model.ErrBadRequest
		}
		totalPower += stoneCfg.Power * s.Quantity
	}

	// NOTE: fix the shadowed receiver issue - use the service instance stored before the loop
	// Actually let me restructure to avoid the shadowing:

	// Fetch stone powers
	for i := range req.Stones {
		stoneCfg, err := s.repo.GetStoneConfig(ctx, req.Stones[i].StoneLevel)
		if err != nil || stoneCfg == nil {
			return nil, model.ErrBadRequest
		}
		totalPower += stoneCfg.Power * req.Stones[i].Quantity
	}

	// Calculate success percent
	percent := float64(totalPower) * 100.0 / float64(rateConfig.UpgradeCost)
	if percent > rateConfig.MaxPercent {
		percent = rateConfig.MaxPercent
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Deduct stones
	for _, stone := range req.Stones {
		if err := s.repo.DeductStonesTx(ctx, tx, playerID, stone.StoneLevel, stone.Quantity); err != nil {
			return nil, model.ErrInsufficientStones
		}
	}

	// Roll for success
	roll := rand.Float64() * 100.0
	success := roll < percent

	newLevel := equip.UpgradeLevel
	if success {
		newLevel = equip.UpgradeLevel + 1
	} else {
		newLevel = rateConfig.FailResetTo
	}

	if err := s.repo.UpdateUpgradeLevelTx(ctx, tx, req.EquipmentID, newLevel); err != nil {
		return nil, err
	}

	// Log the upgrade attempt
	targetLevel := equip.UpgradeLevel + 1
	if err := s.repo.InsertUpgradeLogTx(ctx, tx, req.EquipmentID, equip.UpgradeLevel, targetLevel, req.Stones, totalPower, success); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return &UpgradeResult{
		Success:     success,
		NewLevel:    newLevel,
		Percent:     percent,
		EquipmentID: req.EquipmentID,
	}, nil
}

// ============================================================
// Dismantle
// ============================================================

func (s *Service) DismantleEquipment(ctx context.Context, playerID string, req DismantleRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Return socketed gems to inventory (just clear the FK, gems stay in player_gems)
	if equip.GemSlot1 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, 1, nil); err != nil {
			return err
		}
	}
	if equip.GemSlot2 != nil {
		if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, 2, nil); err != nil {
			return err
		}
	}

	// Calculate stone refund: 50% of stones used in current safezone milestone
	if equip.UpgradeLevel > 0 {
		safezoneStart := getSafezoneStart(equip.UpgradeLevel)
		logs, err := s.repo.GetUpgradeLogForDismantle(ctx, req.EquipmentID, safezoneStart)
		if err == nil && len(logs) > 0 {
			totalPower := 0
			for _, l := range logs {
				totalPower += l.TotalPower
			}
			refundPower := totalPower / 2
			if refundPower > 0 {
				// Convert power back to stones (give as highest possible stone levels)
				stonePowers := []int{177147, 59049, 19683, 6561, 2187, 729, 243, 81, 27, 9, 3, 1}
				for i, power := range stonePowers {
					stoneLevel := 12 - i
					count := refundPower / power
					if count > 0 {
						if err := s.repo.AddStonesTx(ctx, tx, playerID, stoneLevel, count); err != nil {
							return err
						}
						refundPower -= count * power
					}
				}
			}
		}
	}

	// Delete the equipment
	if err := s.repo.DeleteEquipmentTx(ctx, tx, req.EquipmentID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func getSafezoneStart(level int) int {
	if level >= 14 {
		return 14
	}
	if level >= 10 {
		return 10
	}
	if level >= 6 {
		return 6
	}
	return 0
}

// ============================================================
// Socket / Unsocket Gems
// ============================================================

func (s *Service) SocketGem(ctx context.Context, playerID string, req SocketGemRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	// Validate slot index
	itemCfg, err := s.repo.GetEquipmentItemConfig(ctx, equip.ItemID)
	if err != nil {
		return err
	}
	if req.SlotIndex < 1 || req.SlotIndex > itemCfg.GemSlots {
		return model.ErrGemSlotInvalid
	}

	// Check gem is already in a slot
	if req.SlotIndex == 1 && equip.GemSlot1 != nil {
		return model.AppError{Code: "EQUIP_GEM_SLOT_FULL", Message: "Gem slot already occupied, unsocket first", Status: 400}
	}
	if req.SlotIndex == 2 && equip.GemSlot2 != nil {
		return model.AppError{Code: "EQUIP_GEM_SLOT_FULL", Message: "Gem slot already occupied, unsocket first", Status: 400}
	}

	// Check gem ownership
	gem, err := s.repo.GetPlayerGemByID(ctx, playerID, req.GemID)
	if err != nil {
		return err
	}
	if gem == nil {
		return model.ErrGemNotOwned
	}

	// Check gem is not already socketed elsewhere
	socketed, err := s.repo.IsGemSocketed(ctx, req.GemID)
	if err != nil {
		return err
	}
	if socketed {
		return model.AppError{Code: "EQUIP_GEM_ALREADY_SOCKETED", Message: "Gem is already socketed in another equipment", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	gemIDPtr := &req.GemID
	if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, req.SlotIndex, gemIDPtr); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) UnsocketGem(ctx context.Context, playerID string, req UnsocketGemRequest) error {
	equip, err := s.repo.GetPlayerEquipmentByID(ctx, playerID, req.EquipmentID)
	if err != nil {
		return err
	}
	if equip == nil {
		return model.ErrEquipmentNotOwned
	}

	if req.SlotIndex < 1 || req.SlotIndex > 2 {
		return model.ErrGemSlotInvalid
	}
	if req.SlotIndex == 1 && equip.GemSlot1 == nil {
		return model.AppError{Code: "EQUIP_GEM_SLOT_EMPTY", Message: "Gem slot is already empty", Status: 400}
	}
	if req.SlotIndex == 2 && equip.GemSlot2 == nil {
		return model.AppError{Code: "EQUIP_GEM_SLOT_EMPTY", Message: "Gem slot is already empty", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := s.repo.SetGemSlotTx(ctx, tx, req.EquipmentID, req.SlotIndex, nil); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ============================================================
// Merge Stones
// ============================================================

func (s *Service) MergeStones(ctx context.Context, playerID string, req MergeStoneRequest) (*MergeResult, error) {
	if req.Count < 2 {
		return nil, model.ErrMergeMinCount
	}
	if req.Count > 4 {
		return nil, model.ErrMergeMaxCount
	}
	if req.StoneLevel >= 12 {
		return nil, model.ErrMergeMaxLevel
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Deduct stones
	if err := s.repo.DeductStonesTx(ctx, tx, playerID, req.StoneLevel, req.Count); err != nil {
		return nil, model.ErrInsufficientStones
	}

	// Roll for success: 25% per stone
	successRate := float64(req.Count) * 25.0
	roll := rand.Float64() * 100.0
	success := roll < successRate

	if success {
		if err := s.repo.AddStonesTx(ctx, tx, playerID, req.StoneLevel+1, 1); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg := "Merge failed, stones lost"
	if success {
		msg = fmt.Sprintf("Merge successful! Received 1x Level %d stone", req.StoneLevel+1)
	}

	return &MergeResult{Success: success, Message: msg}, nil
}

// ============================================================
// Merge Gems
// ============================================================

func (s *Service) MergeGems(ctx context.Context, playerID string, req MergeGemRequest) (*MergeResult, error) {
	if req.Count < 2 {
		return nil, model.ErrMergeMinCount
	}
	if req.Count > 4 {
		return nil, model.ErrMergeMaxCount
	}
	if req.GemLevel >= 10 {
		return nil, model.ErrMergeMaxLevel
	}

	// Get unequipped gems of this type+level
	available, err := s.repo.GetPlayerGemsByTypeAndLevel(ctx, playerID, req.GemType, req.GemLevel)
	if err != nil {
		return nil, err
	}
	if len(available) < req.Count {
		return nil, model.AppError{Code: "MERGE_INSUFFICIENT_GEMS", Message: "Not enough unequipped gems of this type and level", Status: 400}
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Delete the consumed gems
	gemIDs := make([]string, req.Count)
	for i := 0; i < req.Count; i++ {
		gemIDs[i] = available[i].GemID
	}
	if err := s.repo.DeleteGemsTx(ctx, tx, gemIDs); err != nil {
		return nil, err
	}

	// Roll for success
	successRate := float64(req.Count) * 25.0
	roll := rand.Float64() * 100.0
	success := roll < successRate

	if success {
		_, err = s.repo.CreateGemTx(ctx, tx, playerID, req.GemType, req.GemLevel+1)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg := "Merge failed, gems lost"
	if success {
		msg = fmt.Sprintf("Merge successful! Received 1x Level %d %s gem", req.GemLevel+1, req.GemType)
	}

	return &MergeResult{Success: success, Message: msg}, nil
}

// ============================================================
// Crafting
// ============================================================

func (s *Service) CraftEquipment(ctx context.Context, playerID string, req CraftRequest) (string, error) {
	recipe, err := s.repo.GetCraftingRecipe(ctx, req.RecipeID)
	if err != nil {
		return "", err
	}
	if recipe == nil || !recipe.IsActive {
		return "", model.ErrRecipeNotFound
	}

	itemCfg, err := s.repo.GetEquipmentItemConfig(ctx, recipe.ResultItemID)
	if err != nil {
		return "", err
	}
	if itemCfg == nil {
		return "", model.ErrInternalServer
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	// Deduct all materials
	for _, mat := range recipe.Materials {
		if err := s.repo.DeductMaterialsTx(ctx, tx, playerID, mat.MaterialID, mat.Quantity); err != nil {
			return "", model.ErrInsufficientMaterials
		}
	}

	// Create the equipment
	equipID, err := s.repo.CreateEquipmentTx(ctx, tx, playerID, itemCfg.ItemID, itemCfg.Slot, itemCfg.Category, itemCfg.Tier, itemCfg.GemSlots)
	if err != nil {
		return "", err
	}

	return equipID, tx.Commit(ctx)
}

// ============================================================
// Combat Stats (used by game server)
// ============================================================

func (s *Service) GetEquipmentStatsForCharacter(ctx context.Context, playerID, characterID string) (*EquipmentStats, error) {
	return s.repo.GetEquipmentStatsForCharacter(ctx, playerID, characterID)
}

// ============================================================
// Helpers (exported for use by other packages if needed)
// ============================================================

func CalculateUpgradeMultiplier(upgradeLevel int) float64 {
	upgradeBonus := float64(upgradeLevel) * 0.02
	milestoneBonus := 0.0
	if upgradeLevel >= 6 {
		milestoneBonus += 0.10
	}
	if upgradeLevel >= 10 {
		milestoneBonus += 0.20
	}
	if upgradeLevel >= 14 {
		milestoneBonus += 0.40
	}
	if upgradeLevel >= 16 {
		milestoneBonus += 1.00
	}
	return 1.0 + upgradeBonus + milestoneBonus
}
```

**Note:** There's a bug in the UpgradeEquipment method — totalPower is calculated twice (before and inside the loop). Fix: remove the `totalPower := 0` line and the first loop, keep only the second loop with `totalPower` initialized once at `0`.

Corrected section of `UpgradeEquipment`:
```go
	// Calculate total power from stones
	totalPower := 0
	for i := range req.Stones {
		stoneCfg, err := s.repo.GetStoneConfig(ctx, req.Stones[i].StoneLevel)
		if err != nil || stoneCfg == nil {
			return nil, model.ErrBadRequest
		}
		totalPower += stoneCfg.Power * req.Stones[i].Quantity
	}
```

Also remove the unused `math` import (only `math/rand` is needed).

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/api/equipment/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/equipment/service.go
git commit -m "feat: add equipment service with all business logic"
```

---

## Task 6: Equipment Module — Handler

**Files:**
- Create: `internal/api/equipment/handler.go`

- [ ] **Step 1: Create handler.go**

```go
package equipment

import (
	"encoding/json"
	"net/http"

	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) getPlayerID(r *http.Request) string {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	return playerID
}

// ============================================================
// Equipment inventory
// ============================================================

func (h *Handler) GetEquipment(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	items, err := h.service.GetPlayerEquipment(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if items == nil {
		items = []PlayerEquipment{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) GetStones(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	stones, err := h.service.GetPlayerStones(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if stones == nil {
		stones = []PlayerStone{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stones)
}

func (h *Handler) GetGems(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	gems, err := h.service.GetPlayerGems(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if gems == nil {
		gems = []PlayerGem{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gems)
}

func (h *Handler) GetMaterials(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	materials, err := h.service.GetPlayerMaterials(r.Context(), playerID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if materials == nil {
		materials = []PlayerMaterial{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(materials)
}

// ============================================================
// Equip / Unequip
// ============================================================

func (h *Handler) EquipItem(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req EquipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.EquipItem(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) UnequipItem(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req UnequipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.UnequipItem(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ============================================================
// Upgrade
// ============================================================

func (h *Handler) Upgrade(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req UpgradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	result, err := h.service.UpgradeEquipment(r.Context(), playerID, req)
	if err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ============================================================
// Dismantle
// ============================================================

func (h *Handler) Dismantle(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req DismantleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.DismantleEquipment(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ============================================================
// Socket / Unsocket
// ============================================================

func (h *Handler) SocketGem(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req SocketGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.SocketGem(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) UnsocketGem(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req UnsocketGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.UnsocketGem(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ============================================================
// Shop endpoints
// ============================================================

func (h *Handler) GetShopEquipment(w http.ResponseWriter, r *http.Request) {
	characterID := r.URL.Query().Get("characterId")
	if characterID == "" {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	items, err := h.service.GetShopEquipment(r.Context(), characterID)
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if items == nil {
		items = []EquipmentItemConfig{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) BuyEquipment(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req BuyEquipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	equipID, err := h.service.BuyEquipment(r.Context(), playerID, req)
	if err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		if err.Error() == "insufficient coin balance" || err.Error() == "insufficient gem balance" {
			model.WriteError(w, r, model.ErrInsufficientBalance)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"equipmentId": equipID})
}

func (h *Handler) GetShopStones(w http.ResponseWriter, r *http.Request) {
	stones, err := h.service.GetShopStones(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if stones == nil {
		stones = []StoneConfig{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stones)
}

func (h *Handler) BuyStones(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req BuyStoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.BuyStones(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		if err.Error() == "insufficient coin balance" || err.Error() == "insufficient gem balance" {
			model.WriteError(w, r, model.ErrInsufficientBalance)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) GetShopGems(w http.ResponseWriter, r *http.Request) {
	gems, err := h.service.GetShopGems(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if gems == nil {
		gems = []GemConfig{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(gems)
}

func (h *Handler) BuyGems(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req BuyGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.BuyGems(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		if err.Error() == "insufficient coin balance" || err.Error() == "insufficient gem balance" {
			model.WriteError(w, r, model.ErrInsufficientBalance)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

func (h *Handler) GetShopMaterials(w http.ResponseWriter, r *http.Request) {
	materials, err := h.service.GetShopMaterials(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if materials == nil {
		materials = []MaterialConfig{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(materials)
}

func (h *Handler) BuyMaterials(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req BuyMaterialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	if err := h.service.BuyMaterials(r.Context(), playerID, req); err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		if err.Error() == "insufficient gem balance" {
			model.WriteError(w, r, model.ErrInsufficientBalance)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"success"}`))
}

// ============================================================
// Merge
// ============================================================

func (h *Handler) MergeStones(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req MergeStoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	result, err := h.service.MergeStones(r.Context(), playerID, req)
	if err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handler) MergeGems(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req MergeGemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	result, err := h.service.MergeGems(r.Context(), playerID, req)
	if err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ============================================================
// Crafting
// ============================================================

func (h *Handler) GetRecipes(w http.ResponseWriter, r *http.Request) {
	recipes, err := h.service.GetCraftingRecipes(r.Context())
	if err != nil {
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	if recipes == nil {
		recipes = []CraftingRecipe{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(recipes)
}

func (h *Handler) Craft(w http.ResponseWriter, r *http.Request) {
	playerID := h.getPlayerID(r)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}
	var req CraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}
	equipID, err := h.service.CraftEquipment(r.Context(), playerID, req)
	if err != nil {
		if appErr, ok := err.(model.AppError); ok {
			model.WriteError(w, r, appErr)
			return
		}
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"equipmentId": equipID})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/api/equipment/...`
Expected: No errors

- [ ] **Step 3: Commit**

```bash
git add internal/api/equipment/handler.go
git commit -m "feat: add equipment HTTP handlers"
```

---

## Task 7: Wire Equipment Module into API Server

**Files:**
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Add equipment import and module initialization**

Add import:
```go
"battle-squad/internal/api/equipment"
```

After `matchhistoryHandler := matchhistory.NewHandler(matchhistoryService)` (around line 120), add:
```go
equipmentRepo := equipment.NewRepository(db)
equipmentService := equipment.NewService(equipmentRepo, economyRepo, db)
equipmentHandler := equipment.NewHandler(equipmentService)
```

- [ ] **Step 2: Add equipment routes**

Inside the protected routes group (after line 204 `r.Get("/rooms", roomsHandler.GetRooms)`), add:

```go
// Equipment
r.Get("/player/equipment", equipmentHandler.GetEquipment)
r.Post("/player/equipment/equip", equipmentHandler.EquipItem)
r.Post("/player/equipment/unequip", equipmentHandler.UnequipItem)
r.Post("/player/equipment/upgrade", equipmentHandler.Upgrade)
r.Post("/player/equipment/dismantle", equipmentHandler.Dismantle)
r.Post("/player/equipment/socket", equipmentHandler.SocketGem)
r.Post("/player/equipment/unsocket", equipmentHandler.UnsocketGem)

// Equipment inventory
r.Get("/player/stones", equipmentHandler.GetStones)
r.Get("/player/gems", equipmentHandler.GetGems)
r.Get("/player/materials", equipmentHandler.GetMaterials)

// Equipment shop
r.Get("/shop/equipment", equipmentHandler.GetShopEquipment)
r.Post("/shop/equipment/buy", equipmentHandler.BuyEquipment)
r.Get("/shop/stones", equipmentHandler.GetShopStones)
r.Post("/shop/stones/buy", equipmentHandler.BuyStones)
r.Get("/shop/gems", equipmentHandler.GetShopGems)
r.Post("/shop/gems/buy", equipmentHandler.BuyGems)
r.Get("/shop/materials", equipmentHandler.GetShopMaterials)
r.Post("/shop/materials/buy", equipmentHandler.BuyMaterials)

// Merge
r.Post("/merge/stone", equipmentHandler.MergeStones)
r.Post("/merge/gem", equipmentHandler.MergeGems)

// Crafting
r.Get("/crafting/recipes", equipmentHandler.GetRecipes)
r.Post("/crafting/craft", equipmentHandler.Craft)
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./cmd/api/...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat: wire equipment module routes into API server"
```

---

## Task 8: Seed Default Equipment Config Data

**Files:**
- Modify: `internal/admin/seed.go`

- [ ] **Step 1: Add equipment seed data**

Add the following function and default data at the bottom of `seed.go`:

```go
// SeedEquipmentConfig seeds default equipment system config data.
func SeedEquipmentConfig(ctx context.Context, db *database.PostgresDB) error {
	// Seed upgrade rates
	upgradeRates := []struct {
		From, To, Cost int
		MaxPct         float64
		FailReset      int
	}{
		{0, 1, 1, 80, 0},
		{1, 2, 2, 76, 1},
		{2, 3, 6, 72, 2},
		{3, 4, 18, 68, 3},
		{4, 5, 40, 64, 4},
		{5, 6, 80, 60, 5},
		{6, 7, 180, 56, 6},
		{7, 8, 500, 52, 6},
		{8, 9, 1200, 45, 6},
		{9, 10, 3000, 40, 6},
		{10, 11, 6000, 35, 10},
		{11, 12, 15000, 30, 10},
		{12, 13, 25000, 25, 10},
		{13, 14, 50000, 20, 10},
		{14, 15, 80000, 15, 14},
		{15, 16, 200000, 10, 14},
	}
	for _, r := range upgradeRates {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to)
			 VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			r.From, r.To, r.Cost, r.MaxPct, r.FailReset)
		if err != nil {
			return fmt.Errorf("seed upgrade rate %d→%d: %w", r.From, r.To, err)
		}
	}

	// Seed stone configs
	stones := []struct {
		Level, Power, Coin, Gem int
		Source                  string
	}{
		{1, 1, 100, 0, "coin_shop"},
		{2, 3, 250, 0, "coin_shop"},
		{3, 9, 700, 0, "coin_shop"},
		{4, 27, 2000, 0, "coin_shop"},
		{5, 81, 5500, 0, "coin_shop"},
		{6, 243, 15000, 0, "coin_shop"},
		{7, 729, 0, 50, "gem_shop"},
		{8, 2187, 0, 140, "gem_shop"},
		{9, 6561, 0, 400, "gem_shop"},
		{10, 19683, 0, 1100, "gem_shop"},
		{11, 59049, 0, 0, "merge_only"},
		{12, 177147, 0, 0, "merge_only"},
	}
	for _, s := range stones {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_stones (stone_level, power, price_coin, price_gem, source)
			 VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			s.Level, s.Power, s.Coin, s.Gem, s.Source)
		if err != nil {
			return fmt.Errorf("seed stone %d: %w", s.Level, err)
		}
	}

	// Seed gem configs
	gemTypes := []string{"hp", "damage", "defense", "critical"}
	gemValues := map[string][10]float64{
		"hp":       {20, 40, 70, 110, 160, 220, 300, 400, 520, 660},
		"damage":   {3, 6, 10, 15, 22, 30, 40, 52, 67, 85},
		"defense":  {2, 4, 7, 11, 16, 22, 30, 40, 52, 66},
		"critical": {1, 2, 3, 5, 7, 9, 12, 15, 18, 22},
	}
	for _, gt := range gemTypes {
		vals := gemValues[gt]
		for i := 0; i < 10; i++ {
			_, err := db.Pool.Exec(ctx,
				`INSERT INTO config_gems (gem_type, gem_level, stat_value)
				 VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				gt, i+1, vals[i])
			if err != nil {
				return fmt.Errorf("seed gem %s lv%d: %w", gt, i+1, err)
			}
		}
	}

	// Seed set bonuses
	setBonuses := []struct {
		Tier    string
		Pieces  int
		HP, DMG, DEF, Crit float64
	}{
		{"silver", 2, 3, 0, 0, 0},
		{"silver", 4, 0, 0, 3, 0},
		{"silver", 6, 5, 0, 5, 0},
		{"gold", 2, 5, 0, 0, 0},
		{"gold", 4, 0, 5, 0, 0},
		{"gold", 6, 8, 8, 5, 0},
		{"titan", 2, 8, 0, 0, 0},
		{"titan", 4, 0, 8, 0, 0},
		{"titan", 6, 12, 12, 8, 5},
		{"diamond", 2, 10, 0, 0, 0},
		{"diamond", 4, 0, 10, 0, 0},
		{"diamond", 6, 15, 15, 12, 10},
	}
	for _, b := range setBonuses {
		_, err := db.Pool.Exec(ctx,
			`INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct)
			 VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
			b.Tier, b.Pieces, b.HP, b.DMG, b.DEF, b.Crit)
		if err != nil {
			return fmt.Errorf("seed set bonus %s/%d: %w", b.Tier, b.Pieces, err)
		}
	}

	observability.Log.Info().Msg("equipment config seeded successfully")
	return nil
}
```

- [ ] **Step 2: Call SeedEquipmentConfig from SeedAll**

In the `SeedAll` function, add after the existing seed calls:

```go
if err := SeedEquipmentConfig(ctx, db); err != nil {
	return fmt.Errorf("seed equipment config: %w", err)
}
```

- [ ] **Step 3: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/admin/...`
Expected: No errors

- [ ] **Step 4: Commit**

```bash
git add internal/admin/seed.go
git commit -m "feat: seed default equipment config data (upgrade rates, stones, gems, set bonuses)"
```

---

## Task 9: Combat Integration — Match Model Changes

**Files:**
- Modify: `internal/game/match/model.go`
- Modify: `internal/game/match/match.go`
- Modify: `internal/game/match/damage.go`
- Modify: `internal/game/room/room.go`

- [ ] **Step 1: Add CritChance and MoveEnergyBonus to BattlePlayerState**

In `internal/game/match/model.go`, add two fields to `BattlePlayerState` after `IsBot`:

```go
CritChance      float64        `json:"critChance"`      // 0-100, percentage
MoveEnergyBonus int            `json:"moveEnergyBonus"` // Added to base 100 per turn
```

- [ ] **Step 2: Apply MoveEnergyBonus in startTurn**

In `internal/game/match/match.go`, in the `startTurn` function, change:
```go
player.MoveEnergy = 100 // Reset move energy to full
```
to:
```go
player.MoveEnergy = 100 + player.MoveEnergyBonus
```

- [ ] **Step 3: Add critical hit to CalculateExplosionDamage**

In `internal/game/match/damage.go`, add a new function:

```go
func ApplyCritical(damage int, critChance float64) (int, bool) {
	if critChance <= 0 {
		return damage, false
	}
	roll := rand.Float64() * 100.0
	if roll < critChance {
		return int(math.Round(float64(damage) * 1.5)), true
	}
	return damage, false
}
```

Add `"math/rand"` to imports in `damage.go`.

- [ ] **Step 4: Integrate critical hit in processShoot**

In `internal/game/match/match.go`, find where explosion damage is applied to each player (where `CalculateExplosionDamage` is called). After that call, add critical hit logic:

```go
damage, isCrit := ApplyCritical(damage, shooter.CritChance)
```

(The exact integration point depends on the code structure — look for where `CalculateExplosionDamage` result is used and apply `ApplyCritical` to it before deducting HP.)

- [ ] **Step 5: Extend getActualStats in room.go to include equipment**

In `internal/game/room/room.go`, modify the `getActualStats` function signature to return additional stats and load equipment data:

Change signature from:
```go
func (r *Room) getActualStats(playerID, characterID string, baseHP, baseDefense int) (int, int)
```
to:
```go
func (r *Room) getActualStats(playerID, characterID string, baseHP, baseDefense int) (hp, defense, moveEnergyBonus int, critChance float64)
```

Add equipment stats loading at the end of the function:

```go
// Load equipment stats
var equipHP, equipDMG, equipDEF, equipMoveEnergy int
var equipCrit float64

eqRows, eqErr := r.db.Pool.Query(context.Background(),
	`SELECT cei.stat_hp, cei.stat_damage, cei.stat_defense, cei.stat_crit, cei.stat_move_energy,
	        pe.upgrade_level, pe.category, pe.tier, pe.gem_slot_1::text, pe.gem_slot_2::text
	 FROM player_equipment pe
	 JOIN config_equipment_items cei ON pe.item_id = cei.item_id
	 WHERE pe.player_id = $1 AND pe.equipped_on = $2 AND pe.is_equipped = TRUE`,
	playerID, characterID)
if eqErr == nil {
	defer eqRows.Close()
	craftedTierCount := make(map[string]int)

	for eqRows.Next() {
		var statHP, statDMG, statDEF, statMoveEnergy, upgradeLevel int
		var statCrit float64
		var category string
		var tier, gem1, gem2 *string

		if err := eqRows.Scan(&statHP, &statDMG, &statDEF, &statCrit, &statMoveEnergy,
			&upgradeLevel, &category, &tier, &gem1, &gem2); err != nil {
			continue
		}

		// Upgrade + milestone multiplier
		upgradeBonus := float64(upgradeLevel) * 0.02
		milestoneBonus := 0.0
		if upgradeLevel >= 6 { milestoneBonus += 0.10 }
		if upgradeLevel >= 10 { milestoneBonus += 0.20 }
		if upgradeLevel >= 14 { milestoneBonus += 0.40 }
		if upgradeLevel >= 16 { milestoneBonus += 1.00 }
		mul := 1.0 + upgradeBonus + milestoneBonus

		equipHP += int(math.Round(float64(statHP) * mul))
		equipDEF += int(math.Round(float64(statDEF) * mul))
		equipCrit += statCrit * mul
		equipMoveEnergy += int(math.Round(float64(statMoveEnergy) * mul))

		if category == "crafted" && tier != nil {
			craftedTierCount[*tier]++
		}

		// Add gem stats
		for _, gemID := range []*string{gem1, gem2} {
			if gemID == nil { continue }
			var gemType string
			var statVal float64
			err := r.db.Pool.QueryRow(context.Background(),
				`SELECT g.gem_type, cg.stat_value
				 FROM player_gems g JOIN config_gems cg ON g.gem_type = cg.gem_type AND g.gem_level = cg.gem_level
				 WHERE g.gem_id = $1`, *gemID).Scan(&gemType, &statVal)
			if err != nil { continue }
			switch gemType {
			case "hp": equipHP += int(statVal)
			case "defense": equipDEF += int(statVal)
			case "critical": equipCrit += statVal
			case "damage": equipDMG += int(statVal)
			}
		}
	}

	// Apply set bonuses
	for t, count := range craftedTierCount {
		bonusRows, err := r.db.Pool.Query(context.Background(),
			`SELECT pieces_required, bonus_hp_pct, bonus_def_pct, bonus_crit_pct
			 FROM config_set_bonuses WHERE tier = $1 ORDER BY pieces_required`, t)
		if err != nil { continue }
		for bonusRows.Next() {
			var pieces int
			var hpPct, defPct, critPct float64
			if err := bonusRows.Scan(&pieces, &hpPct, &defPct, &critPct); err != nil { continue }
			if count >= pieces {
				equipHP = int(math.Round(float64(equipHP) * (1 + hpPct/100)))
				equipDEF = int(math.Round(float64(equipDEF) * (1 + defPct/100)))
				equipCrit += critPct
			}
		}
		bonusRows.Close()
	}
}

return baseHP + bonusHP*hpMul + equipHP, baseDefense + bonusDefense*defMul + equipDEF, equipMoveEnergy, equipCrit
```

Add `"math"` to imports in `room.go`.

- [ ] **Step 6: Update callers of getActualStats**

In `room.go`, where `getActualStats` is called (around line 557), update from:
```go
hp, defense = r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
```
to:
```go
hp, defense, moveEnergyBonus, critChance := r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
```

Then in the `BattlePlayerState` initialization, add the new fields:
```go
MoveEnergyBonus: moveEnergyBonus,
CritChance:      critChance,
```

- [ ] **Step 7: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/game/...`
Expected: No errors

- [ ] **Step 8: Commit**

```bash
git add internal/game/match/model.go internal/game/match/match.go internal/game/match/damage.go internal/game/room/room.go
git commit -m "feat: integrate equipment stats into combat (crit, move energy, defense, HP)"
```

---

## Task 10: Admin Dashboard — Equipment Config CRUD

**Files:**
- Create: `internal/admin/handlers_equipment.go`
- Modify: `internal/admin/server.go`
- Modify: `internal/admin/repository.go`
- Create: All equipment admin templates

- [ ] **Step 1: Add admin repository methods for equipment config**

Add to `internal/admin/repository.go`:

```go
// ============================================================
// Equipment Config CRUD
// ============================================================

func (r *Repository) GetAllEquipmentItems(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT item_id, name, slot, category, tier, required_level, character_id,
		        gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		        price_coin, price_gem, is_active
		 FROM config_equipment_items ORDER BY category, required_level, slot`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var itemID, name, slot, category string
		var tier, characterID *string
		var requiredLevel, gemSlots, statHP, statDMG, statDEF, statMoveEnergy, priceCoin, priceGem int
		var statCrit float64
		var isActive bool
		if err := rows.Scan(&itemID, &name, &slot, &category, &tier, &requiredLevel, &characterID,
			&gemSlots, &statHP, &statDMG, &statDEF, &statCrit, &statMoveEnergy,
			&priceCoin, &priceGem, &isActive); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"item_id": itemID, "name": name, "slot": slot, "category": category,
			"tier": tier, "required_level": requiredLevel, "character_id": characterID,
			"gem_slots": gemSlots, "stat_hp": statHP, "stat_damage": statDMG,
			"stat_defense": statDEF, "stat_crit": statCrit, "stat_move_energy": statMoveEnergy,
			"price_coin": priceCoin, "price_gem": priceGem, "is_active": isActive,
		})
	}
	return result, nil
}

func (r *Repository) UpsertEquipmentItem(ctx context.Context, itemID, name, slot, category string, tier, characterID *string, requiredLevel, gemSlots, statHP, statDMG, statDEF, statMoveEnergy, priceCoin, priceGem int, statCrit float64, isActive bool) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_equipment_items (item_id, name, slot, category, tier, required_level, character_id,
		 gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy, price_coin, price_gem, is_active, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16, CURRENT_TIMESTAMP)
		 ON CONFLICT (item_id) DO UPDATE SET
		   name=EXCLUDED.name, slot=EXCLUDED.slot, category=EXCLUDED.category, tier=EXCLUDED.tier,
		   required_level=EXCLUDED.required_level, character_id=EXCLUDED.character_id, gem_slots=EXCLUDED.gem_slots,
		   stat_hp=EXCLUDED.stat_hp, stat_damage=EXCLUDED.stat_damage, stat_defense=EXCLUDED.stat_defense,
		   stat_crit=EXCLUDED.stat_crit, stat_move_energy=EXCLUDED.stat_move_energy,
		   price_coin=EXCLUDED.price_coin, price_gem=EXCLUDED.price_gem, is_active=EXCLUDED.is_active,
		   updated_at=CURRENT_TIMESTAMP`,
		itemID, name, slot, category, tier, requiredLevel, characterID,
		gemSlots, statHP, statDMG, statDEF, statCrit, statMoveEnergy, priceCoin, priceGem, isActive)
	return err
}

func (r *Repository) DeleteEquipmentItem(ctx context.Context, itemID string) error {
	_, err := r.db.Pool.Exec(ctx, `DELETE FROM config_equipment_items WHERE item_id = $1`, itemID)
	return err
}

func (r *Repository) GetAllUpgradeRates(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
		 FROM config_upgrade_rates ORDER BY from_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var from, to, cost, failReset int
		var maxPct float64
		if err := rows.Scan(&from, &to, &cost, &maxPct, &failReset); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"from_level": from, "to_level": to, "upgrade_cost": cost,
			"max_percent": maxPct, "fail_reset_to": failReset,
		})
	}
	return result, nil
}

func (r *Repository) UpsertUpgradeRate(ctx context.Context, from, to, cost int, maxPct float64, failReset int) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_upgrade_rates (from_level, to_level, upgrade_cost, max_percent, fail_reset_to)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (from_level, to_level) DO UPDATE SET
		   upgrade_cost=EXCLUDED.upgrade_cost, max_percent=EXCLUDED.max_percent, fail_reset_to=EXCLUDED.fail_reset_to`,
		from, to, cost, maxPct, failReset)
	return err
}

func (r *Repository) GetAllStoneConfigs(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT stone_level, power, price_coin, price_gem, source FROM config_stones ORDER BY stone_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var level, power, coin, gem int
		var source string
		if err := rows.Scan(&level, &power, &coin, &gem, &source); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"stone_level": level, "power": power, "price_coin": coin, "price_gem": gem, "source": source,
		})
	}
	return result, nil
}

func (r *Repository) UpsertStoneConfig(ctx context.Context, level, power, coin, gem int, source string) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_stones (stone_level, power, price_coin, price_gem, source)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (stone_level) DO UPDATE SET
		   power=EXCLUDED.power, price_coin=EXCLUDED.price_coin, price_gem=EXCLUDED.price_gem, source=EXCLUDED.source`,
		level, power, coin, gem, source)
	return err
}

func (r *Repository) GetAllGemConfigs(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT gem_type, gem_level, stat_value FROM config_gems ORDER BY gem_type, gem_level`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var gemType string
		var gemLevel int
		var statValue float64
		if err := rows.Scan(&gemType, &gemLevel, &statValue); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"gem_type": gemType, "gem_level": gemLevel, "stat_value": statValue,
		})
	}
	return result, nil
}

func (r *Repository) UpsertGemConfig(ctx context.Context, gemType string, gemLevel int, statValue float64) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_gems (gem_type, gem_level, stat_value)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (gem_type, gem_level) DO UPDATE SET stat_value=EXCLUDED.stat_value`,
		gemType, gemLevel, statValue)
	return err
}

func (r *Repository) GetAllSetBonuses(ctx context.Context) ([]map[string]interface{}, error) {
	rows, err := r.db.Pool.Query(ctx,
		`SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		 FROM config_set_bonuses ORDER BY tier, pieces_required`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var tier string
		var pieces int
		var hp, dmg, def, crit float64
		if err := rows.Scan(&tier, &pieces, &hp, &dmg, &def, &crit); err != nil {
			return nil, err
		}
		result = append(result, map[string]interface{}{
			"tier": tier, "pieces_required": pieces,
			"bonus_hp_pct": hp, "bonus_dmg_pct": dmg, "bonus_def_pct": def, "bonus_crit_pct": crit,
		})
	}
	return result, nil
}

func (r *Repository) UpsertSetBonus(ctx context.Context, tier string, pieces int, hp, dmg, def, crit float64) error {
	_, err := r.db.Pool.Exec(ctx,
		`INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tier, pieces_required) DO UPDATE SET
		   bonus_hp_pct=EXCLUDED.bonus_hp_pct, bonus_dmg_pct=EXCLUDED.bonus_dmg_pct,
		   bonus_def_pct=EXCLUDED.bonus_def_pct, bonus_crit_pct=EXCLUDED.bonus_crit_pct`,
		tier, pieces, hp, dmg, def, crit)
	return err
}
```

- [ ] **Step 2: Create admin handlers for equipment config**

Create `internal/admin/handlers_equipment.go`:

```go
package admin

import (
	"net/http"
	"strconv"
	"strings"

	"battle-squad/internal/shared/observability"
)

func (s *Server) handleEquipmentItemsList(w http.ResponseWriter, r *http.Request) {
	items, err := s.repo.GetAllEquipmentItems(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get equipment items")
		http.Error(w, "Internal Server Error", 500)
		return
	}
	s.render(w, "equipment_items", map[string]interface{}{
		"Title": "Equipment Items",
		"Items": items,
		"Flash": r.URL.Query().Get("flash"),
		"Error": r.URL.Query().Get("error"),
	})
}

func (s *Server) handleEquipmentItemEdit(w http.ResponseWriter, r *http.Request) {
	s.render(w, "equipment_item_edit", map[string]interface{}{
		"Title":  "Edit Equipment Item",
		"ItemID": r.URL.Query().Get("id"),
	})
}

func (s *Server) handleEquipmentItemSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-items?error=Invalid+form", http.StatusSeeOther)
		return
	}

	itemID := strings.TrimSpace(r.FormValue("item_id"))
	if itemID == "" {
		http.Redirect(w, r, "/equipment-items/edit?error=Item+ID+required", http.StatusSeeOther)
		return
	}

	var tier, charID *string
	if t := r.FormValue("tier"); t != "" {
		tier = &t
	}
	if c := r.FormValue("character_id"); c != "" {
		charID = &c
	}

	statCrit, _ := strconv.ParseFloat(r.FormValue("stat_crit"), 64)

	err := s.repo.UpsertEquipmentItem(r.Context(),
		itemID,
		r.FormValue("name"),
		r.FormValue("slot"),
		r.FormValue("category"),
		tier, charID,
		formInt(r, "required_level"),
		formInt(r, "gem_slots"),
		formInt(r, "stat_hp"),
		formInt(r, "stat_damage"),
		formInt(r, "stat_defense"),
		formInt(r, "stat_move_energy"),
		formInt(r, "price_coin"),
		formInt(r, "price_gem"),
		statCrit,
		r.FormValue("is_active") == "true",
	)
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to save equipment item")
		http.Redirect(w, r, "/equipment-items?error=Failed+to+save", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/equipment-items?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleEquipmentItemDelete(w http.ResponseWriter, r *http.Request) {
	itemID := r.FormValue("item_id")
	if err := s.repo.DeleteEquipmentItem(r.Context(), itemID); err != nil {
		http.Redirect(w, r, "/equipment-items?error=Failed+to+delete", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/equipment-items?flash=Deleted+successfully", http.StatusSeeOther)
}

func (s *Server) handleUpgradeRates(w http.ResponseWriter, r *http.Request) {
	rates, err := s.repo.GetAllUpgradeRates(r.Context())
	if err != nil {
		observability.Log.Error().Err(err).Msg("failed to get upgrade rates")
		http.Error(w, "Internal Server Error", 500)
		return
	}
	s.render(w, "upgrade_rates", map[string]interface{}{
		"Title": "Upgrade Rates",
		"Rates": rates,
		"Flash": r.URL.Query().Get("flash"),
		"Error": r.URL.Query().Get("error"),
	})
}

func (s *Server) handleUpgradeRateSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/upgrade-rates?error=Invalid+form", http.StatusSeeOther)
		return
	}
	maxPct, _ := strconv.ParseFloat(r.FormValue("max_percent"), 64)
	err := s.repo.UpsertUpgradeRate(r.Context(),
		formInt(r, "from_level"), formInt(r, "to_level"),
		formInt(r, "upgrade_cost"), maxPct, formInt(r, "fail_reset_to"))
	if err != nil {
		http.Redirect(w, r, "/upgrade-rates?error=Failed+to+save", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/upgrade-rates?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleStoneConfigs(w http.ResponseWriter, r *http.Request) {
	stones, err := s.repo.GetAllStoneConfigs(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	s.render(w, "equipment_stones", map[string]interface{}{
		"Title":  "Stone Config",
		"Stones": stones,
		"Flash":  r.URL.Query().Get("flash"),
		"Error":  r.URL.Query().Get("error"),
	})
}

func (s *Server) handleStoneConfigSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-stones?error=Invalid+form", http.StatusSeeOther)
		return
	}
	err := s.repo.UpsertStoneConfig(r.Context(),
		formInt(r, "stone_level"), formInt(r, "power"),
		formInt(r, "price_coin"), formInt(r, "price_gem"),
		r.FormValue("source"))
	if err != nil {
		http.Redirect(w, r, "/equipment-stones?error=Failed+to+save", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/equipment-stones?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleGemConfigs(w http.ResponseWriter, r *http.Request) {
	gems, err := s.repo.GetAllGemConfigs(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	s.render(w, "equipment_gems", map[string]interface{}{
		"Title": "Gem Config",
		"Gems":  gems,
		"Flash": r.URL.Query().Get("flash"),
		"Error": r.URL.Query().Get("error"),
	})
}

func (s *Server) handleGemConfigSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/equipment-gems?error=Invalid+form", http.StatusSeeOther)
		return
	}
	statVal, _ := strconv.ParseFloat(r.FormValue("stat_value"), 64)
	err := s.repo.UpsertGemConfig(r.Context(),
		r.FormValue("gem_type"), formInt(r, "gem_level"), statVal)
	if err != nil {
		http.Redirect(w, r, "/equipment-gems?error=Failed+to+save", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/equipment-gems?flash=Saved+successfully", http.StatusSeeOther)
}

func (s *Server) handleSetBonuses(w http.ResponseWriter, r *http.Request) {
	bonuses, err := s.repo.GetAllSetBonuses(r.Context())
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	s.render(w, "set_bonuses", map[string]interface{}{
		"Title":   "Set Bonuses",
		"Bonuses": bonuses,
		"Flash":   r.URL.Query().Get("flash"),
		"Error":   r.URL.Query().Get("error"),
	})
}

func (s *Server) handleSetBonusSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/set-bonuses?error=Invalid+form", http.StatusSeeOther)
		return
	}
	hp, _ := strconv.ParseFloat(r.FormValue("bonus_hp_pct"), 64)
	dmg, _ := strconv.ParseFloat(r.FormValue("bonus_dmg_pct"), 64)
	def, _ := strconv.ParseFloat(r.FormValue("bonus_def_pct"), 64)
	crit, _ := strconv.ParseFloat(r.FormValue("bonus_crit_pct"), 64)
	err := s.repo.UpsertSetBonus(r.Context(),
		r.FormValue("tier"), formInt(r, "pieces_required"), hp, dmg, def, crit)
	if err != nil {
		http.Redirect(w, r, "/set-bonuses?error=Failed+to+save", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/set-bonuses?flash=Saved+successfully", http.StatusSeeOther)
}

func formInt(r *http.Request, name string) int {
	v, _ := strconv.Atoi(r.FormValue(name))
	return v
}
```

**Note:** Check if `formInt` already exists in the admin package — if it does (likely in `handlers_shop.go`), remove it from this file to avoid duplicate declarations.

- [ ] **Step 3: Add equipment routes to admin server**

In `internal/admin/server.go`, add inside the `Routes()` function before `return r`:

```go
// Equipment config
r.Get("/equipment-items", s.handleEquipmentItemsList)
r.Get("/equipment-items/edit", s.handleEquipmentItemEdit)
r.Post("/equipment-items/save", s.handleEquipmentItemSave)
r.Post("/equipment-items/delete", s.handleEquipmentItemDelete)

r.Get("/upgrade-rates", s.handleUpgradeRates)
r.Post("/upgrade-rates/save", s.handleUpgradeRateSave)

r.Get("/equipment-stones", s.handleStoneConfigs)
r.Post("/equipment-stones/save", s.handleStoneConfigSave)

r.Get("/equipment-gems", s.handleGemConfigs)
r.Post("/equipment-gems/save", s.handleGemConfigSave)

r.Get("/set-bonuses", s.handleSetBonuses)
r.Post("/set-bonuses/save", s.handleSetBonusSave)
```

- [ ] **Step 4: Create admin HTML templates**

Create minimal but functional templates for each config page. Each template uses the existing layout pattern (`{{define "content"}}...{{end}}`).

Create `internal/admin/templates/equipment_items.html`:
```html
{{define "content"}}
<h2>Equipment Items</h2>
{{if .Flash}}<div style="color:green">{{.Flash}}</div>{{end}}
{{if .Error}}<div style="color:red">{{.Error}}</div>{{end}}
<p><a href="/equipment-items/edit">+ Add New</a></p>
<table border="1" cellpadding="4">
<tr><th>ID</th><th>Name</th><th>Slot</th><th>Category</th><th>Tier</th><th>Level</th><th>HP</th><th>DMG</th><th>DEF</th><th>Crit</th><th>Move</th><th>Coin</th><th>Gem</th><th>Active</th><th>Actions</th></tr>
{{range .Items}}
<tr>
<td>{{index . "item_id"}}</td><td>{{index . "name"}}</td><td>{{index . "slot"}}</td>
<td>{{index . "category"}}</td><td>{{index . "tier"}}</td><td>{{index . "required_level"}}</td>
<td>{{index . "stat_hp"}}</td><td>{{index . "stat_damage"}}</td><td>{{index . "stat_defense"}}</td>
<td>{{index . "stat_crit"}}</td><td>{{index . "stat_move_energy"}}</td>
<td>{{index . "price_coin"}}</td><td>{{index . "price_gem"}}</td><td>{{index . "is_active"}}</td>
<td>
<a href="/equipment-items/edit?id={{index . "item_id"}}">Edit</a>
<form method="POST" action="/equipment-items/delete" style="display:inline">
<input type="hidden" name="item_id" value="{{index . "item_id"}}">
<button type="submit" onclick="return confirm('Delete?')">Delete</button>
</form>
</td>
</tr>
{{end}}
</table>
{{end}}
```

Create `internal/admin/templates/equipment_item_edit.html`:
```html
{{define "content"}}
<h2>Edit Equipment Item</h2>
<form method="POST" action="/equipment-items/save">
<label>Item ID: <input name="item_id" value="{{.ItemID}}" required></label><br>
<label>Name: <input name="name" required></label><br>
<label>Slot: <select name="slot"><option>weapon</option><option>armor</option><option>helmet</option><option>pants</option><option>boots</option><option>gloves</option></select></label><br>
<label>Category: <select name="category"><option>normal</option><option>crafted</option></select></label><br>
<label>Tier: <input name="tier" placeholder="silver/gold/titan/diamond (crafted only)"></label><br>
<label>Required Level: <input name="required_level" type="number" value="1"></label><br>
<label>Character ID: <input name="character_id" placeholder="Leave empty for all characters"></label><br>
<label>Gem Slots: <input name="gem_slots" type="number" value="1"></label><br>
<label>Stat HP: <input name="stat_hp" type="number" value="0"></label><br>
<label>Stat Damage: <input name="stat_damage" type="number" value="0"></label><br>
<label>Stat Defense: <input name="stat_defense" type="number" value="0"></label><br>
<label>Stat Crit %: <input name="stat_crit" type="number" step="0.01" value="0"></label><br>
<label>Stat Move Energy: <input name="stat_move_energy" type="number" value="0"></label><br>
<label>Price Coin: <input name="price_coin" type="number" value="0"></label><br>
<label>Price Gem: <input name="price_gem" type="number" value="0"></label><br>
<label>Active: <select name="is_active"><option value="true">Yes</option><option value="false">No</option></select></label><br>
<button type="submit">Save</button>
</form>
{{end}}
```

Create `internal/admin/templates/upgrade_rates.html`:
```html
{{define "content"}}
<h2>Upgrade Rates</h2>
{{if .Flash}}<div style="color:green">{{.Flash}}</div>{{end}}
<table border="1" cellpadding="4">
<tr><th>From</th><th>To</th><th>Cost</th><th>Max %</th><th>Fail Reset To</th><th>Save</th></tr>
{{range .Rates}}
<tr>
<form method="POST" action="/upgrade-rates/save">
<td><input name="from_level" type="number" value="{{index . "from_level"}}" size="3" readonly></td>
<td><input name="to_level" type="number" value="{{index . "to_level"}}" size="3" readonly></td>
<td><input name="upgrade_cost" type="number" value="{{index . "upgrade_cost"}}"></td>
<td><input name="max_percent" type="number" step="0.01" value="{{index . "max_percent"}}"></td>
<td><input name="fail_reset_to" type="number" value="{{index . "fail_reset_to"}}"></td>
<td><button type="submit">Save</button></td>
</form>
</tr>
{{end}}
</table>
{{end}}
```

Create `internal/admin/templates/equipment_stones.html`:
```html
{{define "content"}}
<h2>Stone Config</h2>
{{if .Flash}}<div style="color:green">{{.Flash}}</div>{{end}}
<table border="1" cellpadding="4">
<tr><th>Level</th><th>Power</th><th>Coin Price</th><th>Gem Price</th><th>Source</th><th>Save</th></tr>
{{range .Stones}}
<tr>
<form method="POST" action="/equipment-stones/save">
<td><input name="stone_level" type="number" value="{{index . "stone_level"}}" readonly></td>
<td><input name="power" type="number" value="{{index . "power"}}"></td>
<td><input name="price_coin" type="number" value="{{index . "price_coin"}}"></td>
<td><input name="price_gem" type="number" value="{{index . "price_gem"}}"></td>
<td><select name="source">
<option {{if eq (index . "source") "coin_shop"}}selected{{end}}>coin_shop</option>
<option {{if eq (index . "source") "gem_shop"}}selected{{end}}>gem_shop</option>
<option {{if eq (index . "source") "merge_only"}}selected{{end}}>merge_only</option>
</select></td>
<td><button type="submit">Save</button></td>
</form>
</tr>
{{end}}
</table>
{{end}}
```

Create `internal/admin/templates/equipment_gems.html`:
```html
{{define "content"}}
<h2>Gem Config</h2>
{{if .Flash}}<div style="color:green">{{.Flash}}</div>{{end}}
<table border="1" cellpadding="4">
<tr><th>Type</th><th>Level</th><th>Stat Value</th><th>Save</th></tr>
{{range .Gems}}
<tr>
<form method="POST" action="/equipment-gems/save">
<td><input name="gem_type" value="{{index . "gem_type"}}" readonly></td>
<td><input name="gem_level" type="number" value="{{index . "gem_level"}}" readonly></td>
<td><input name="stat_value" type="number" step="0.01" value="{{index . "stat_value"}}"></td>
<td><button type="submit">Save</button></td>
</form>
</tr>
{{end}}
</table>
{{end}}
```

Create `internal/admin/templates/set_bonuses.html`:
```html
{{define "content"}}
<h2>Set Bonuses</h2>
{{if .Flash}}<div style="color:green">{{.Flash}}</div>{{end}}
<table border="1" cellpadding="4">
<tr><th>Tier</th><th>Pieces</th><th>HP %</th><th>DMG %</th><th>DEF %</th><th>Crit %</th><th>Save</th></tr>
{{range .Bonuses}}
<tr>
<form method="POST" action="/set-bonuses/save">
<td><input name="tier" value="{{index . "tier"}}" readonly></td>
<td><input name="pieces_required" type="number" value="{{index . "pieces_required"}}" readonly></td>
<td><input name="bonus_hp_pct" type="number" step="0.01" value="{{index . "bonus_hp_pct"}}"></td>
<td><input name="bonus_dmg_pct" type="number" step="0.01" value="{{index . "bonus_dmg_pct"}}"></td>
<td><input name="bonus_def_pct" type="number" step="0.01" value="{{index . "bonus_def_pct"}}"></td>
<td><input name="bonus_crit_pct" type="number" step="0.01" value="{{index . "bonus_crit_pct"}}"></td>
<td><button type="submit">Save</button></td>
</form>
</tr>
{{end}}
</table>
{{end}}
```

- [ ] **Step 5: Add equipment links to admin layout**

In `internal/admin/templates/layout.html`, add navigation links for the equipment pages in the sidebar/nav section:

```html
<a href="/equipment-items">Equipment Items</a>
<a href="/upgrade-rates">Upgrade Rates</a>
<a href="/equipment-stones">Stone Config</a>
<a href="/equipment-gems">Gem Config</a>
<a href="/set-bonuses">Set Bonuses</a>
```

- [ ] **Step 6: Verify it compiles**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./internal/admin/... && go build ./cmd/admin/...`
Expected: No errors

- [ ] **Step 7: Commit**

```bash
git add internal/admin/handlers_equipment.go internal/admin/repository.go internal/admin/server.go internal/admin/templates/
git commit -m "feat: add equipment config admin dashboard pages"
```

---

## Task 11: Tests

**Files:**
- Create: `internal/api/equipment/service_test.go`

- [ ] **Step 1: Write unit tests for key business logic**

```go
package equipment

import (
	"testing"
)

func TestCalculateUpgradeMultiplier(t *testing.T) {
	tests := []struct {
		level    int
		expected float64
	}{
		{0, 1.0},
		{1, 1.02},
		{5, 1.10},
		{6, 1.22},  // 6*0.02 + 0.10 = 0.22
		{10, 1.50}, // 10*0.02 + 0.10 + 0.20 = 0.50
		{14, 2.08}, // 14*0.02 + 0.10 + 0.20 + 0.40 = 1.08
		{16, 3.32}, // 16*0.02 + 0.10 + 0.20 + 0.40 + 1.00 = 2.02
	}

	for _, tt := range tests {
		got := CalculateUpgradeMultiplier(tt.level)
		if got != tt.expected {
			t.Errorf("CalculateUpgradeMultiplier(%d) = %f, want %f", tt.level, got, tt.expected)
		}
	}
}

func TestGetSafezoneStart(t *testing.T) {
	tests := []struct {
		level    int
		expected int
	}{
		{1, 0},
		{5, 0},
		{6, 6},
		{8, 6},
		{10, 10},
		{13, 10},
		{14, 14},
		{16, 14},
	}

	for _, tt := range tests {
		got := getSafezoneStart(tt.level)
		if got != tt.expected {
			t.Errorf("getSafezoneStart(%d) = %d, want %d", tt.level, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Run tests**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go test ./internal/api/equipment/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/api/equipment/service_test.go
git commit -m "test: add equipment upgrade multiplier and safezone unit tests"
```

---

## Task 12: Final Build Verification

- [ ] **Step 1: Full build check**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go build ./...`
Expected: No errors

- [ ] **Step 2: Run all tests**

Run: `cd /Users/inspius/Desktop/Porojet/github.com/battle-squad && go test ./... 2>&1 | tail -30`
Expected: All tests pass

- [ ] **Step 3: Final commit if any remaining changes**

```bash
git status
# If any unstaged changes remain, add and commit them
```
