# Character Progression System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add character leveling system — characters earn EXP per match, level up via admin-configured thresholds, gain stat points to allocate into 6 stats with configurable multipliers, and support paid stat reset.

**Architecture:** Alter `player_characters` table to add level/exp/stat columns. New `internal/api/character/` module (handler→service→repo) for stat allocation/reset APIs. Modify `reward.go` to grant character EXP post-match. Modify `room.go` to calculate actual stats (base + bonus × multiplier) when building match players. Admin dashboard page for progression config + level table.

**Tech Stack:** Go, PostgreSQL (pgx/v5), chi router, HTML templates

**Spec:** `docs/superpowers/specs/2026-07-07-character-progression-design.md`

---

## File Structure

```
New:
  migrations/006_character_progression.up.sql
  migrations/006_character_progression.down.sql
  internal/api/character/model.go
  internal/api/character/repository.go
  internal/api/character/service.go
  internal/api/character/handler.go
  internal/admin/templates/character_progression.html

Modified:
  internal/game/match/reward.go         — grant character EXP after match
  internal/game/room/room.go            — use actual stats in processStartMatch + startRankedMatch
  internal/admin/server.go              — add routes
  internal/admin/handlers_matchmaking.go — add page handler
  internal/admin/templates/layout.html  — add nav link
  cmd/api/main.go                       — wire character module
  cmd/migrate/main.go                   — add migration 006
```

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/006_character_progression.up.sql`
- Create: `migrations/006_character_progression.down.sql`
- Modify: `cmd/migrate/main.go`

- [ ] **Step 1: Create up migration**

```sql
-- migrations/006_character_progression.up.sql

ALTER TABLE player_characters
    ADD COLUMN IF NOT EXISTS level                INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS exp                  INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS stat_points          INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_hp             INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_damage         INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_mobility       INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_defense        INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_skill_power    INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bonus_terrain_damage INT NOT NULL DEFAULT 0;

-- Seed character progression config
INSERT INTO game_settings (key, value, value_type, description, category) VALUES
    ('character_progression', '{"pointsPerLevel":10,"resetCostCurrency":"coin","resetCostAmount":500,"statMultipliers":{"hp":50,"damage":5,"mobility":3,"defense":5,"skill_power":5,"terrain_damage":3}}', 'json', 'Character stat progression config', 'character'),
    ('character_levels', '{"levels":[{"level":2,"expRequired":200},{"level":3,"expRequired":400},{"level":4,"expRequired":700},{"level":5,"expRequired":1100},{"level":6,"expRequired":1600},{"level":7,"expRequired":2200},{"level":8,"expRequired":3000},{"level":9,"expRequired":4000},{"level":10,"expRequired":5200}]}', 'json', 'Character level thresholds', 'character')
ON CONFLICT (key) DO NOTHING;
```

- [ ] **Step 2: Create down migration**

```sql
-- migrations/006_character_progression.down.sql
ALTER TABLE player_characters
    DROP COLUMN IF EXISTS level,
    DROP COLUMN IF EXISTS exp,
    DROP COLUMN IF EXISTS stat_points,
    DROP COLUMN IF EXISTS bonus_hp,
    DROP COLUMN IF EXISTS bonus_damage,
    DROP COLUMN IF EXISTS bonus_mobility,
    DROP COLUMN IF EXISTS bonus_defense,
    DROP COLUMN IF EXISTS bonus_skill_power,
    DROP COLUMN IF EXISTS bonus_terrain_damage;

DELETE FROM game_settings WHERE key IN ('character_progression', 'character_levels');
```

- [ ] **Step 3: Add migration 006 to migrate tool**

In `cmd/migrate/main.go`, add to the migrations slice:

```go
filepath.Join("migrations", "006_character_progression.up.sql"),
```

- [ ] **Step 4: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: All migrations apply successfully

- [ ] **Step 5: Commit**

```bash
git add migrations/006_character_progression.up.sql migrations/006_character_progression.down.sql cmd/migrate/main.go
git commit -m "feat: add character progression migration (level, exp, stat points)"
```

---

### Task 2: Character Module — Model & Repository

**Files:**
- Create: `internal/api/character/model.go`
- Create: `internal/api/character/repository.go`

- [ ] **Step 1: Create model.go**

```go
package character

import (
	"encoding/json"
	"time"
)

type PlayerCharacter struct {
	PlayerID         string    `json:"playerId"`
	CharacterID      string    `json:"characterId"`
	Level            int       `json:"level"`
	Exp              int       `json:"exp"`
	NextLevelExp     *int      `json:"nextLevelExp"`
	IsMaxLevel       bool      `json:"isMaxLevel"`
	StatPoints       int       `json:"statPoints"`
	BonusHP          int       `json:"bonusHp"`
	BonusDamage      int       `json:"bonusDamage"`
	BonusMobility    int       `json:"bonusMobility"`
	BonusDefense     int       `json:"bonusDefense"`
	BonusSkillPower  int       `json:"bonusSkillPower"`
	BonusTerrainDmg  int       `json:"bonusTerrainDamage"`
	UnlockedAt       time.Time `json:"unlockedAt"`
}

type AllocateStatsPayload struct {
	CharacterID    string `json:"characterId"`
	HP             int    `json:"hp"`
	Damage         int    `json:"damage"`
	Mobility       int    `json:"mobility"`
	Defense        int    `json:"defense"`
	SkillPower     int    `json:"skillPower"`
	TerrainDamage  int    `json:"terrainDamage"`
}

type ResetStatsPayload struct {
	CharacterID string `json:"characterId"`
}

type ProgressionConfig struct {
	PointsPerLevel    int               `json:"pointsPerLevel"`
	ResetCostCurrency string            `json:"resetCostCurrency"`
	ResetCostAmount   int               `json:"resetCostAmount"`
	StatMultipliers   map[string]int    `json:"statMultipliers"`
}

type LevelEntry struct {
	Level       int `json:"level"`
	ExpRequired int `json:"expRequired"`
}

type CharacterLevelsConfig struct {
	Levels []LevelEntry `json:"levels"`
}

func LoadProgressionConfig(value string) ProgressionConfig {
	var cfg ProgressionConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return ProgressionConfig{
			PointsPerLevel:    10,
			ResetCostCurrency: "coin",
			ResetCostAmount:   500,
			StatMultipliers:   map[string]int{"hp": 50, "damage": 5, "mobility": 3, "defense": 5, "skill_power": 5, "terrain_damage": 3},
		}
	}
	return cfg
}

func LoadLevelsConfig(value string) CharacterLevelsConfig {
	var cfg CharacterLevelsConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		return CharacterLevelsConfig{}
	}
	return cfg
}

// CalcLevelFromExp returns the level and next level exp threshold for a given total exp.
func CalcLevelFromExp(exp int, levels []LevelEntry) (level int, nextLevelExp *int, isMaxLevel bool) {
	level = 1
	for _, entry := range levels {
		if exp >= entry.ExpRequired {
			level = entry.Level
		} else {
			next := entry.ExpRequired
			return level, &next, false
		}
	}
	// Past all levels = max level
	return level, nil, true
}
```

- [ ] **Step 2: Create repository.go**

```go
package character

import (
	"context"
	"fmt"

	"battle-squad/internal/shared/database"
	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetPlayerCharacters(ctx context.Context, playerID string) ([]PlayerCharacter, error) {
	query := `
		SELECT player_id, character_id, level, exp, stat_points,
		       bonus_hp, bonus_damage, bonus_mobility, bonus_defense,
		       bonus_skill_power, bonus_terrain_damage, unlocked_at
		FROM player_characters
		WHERE player_id = $1
		ORDER BY unlocked_at
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, fmt.Errorf("query characters: %w", err)
	}
	defer rows.Close()

	var chars []PlayerCharacter
	for rows.Next() {
		var c PlayerCharacter
		if err := rows.Scan(
			&c.PlayerID, &c.CharacterID, &c.Level, &c.Exp, &c.StatPoints,
			&c.BonusHP, &c.BonusDamage, &c.BonusMobility, &c.BonusDefense,
			&c.BonusSkillPower, &c.BonusTerrainDmg, &c.UnlockedAt,
		); err != nil {
			return nil, fmt.Errorf("scan character: %w", err)
		}
		chars = append(chars, c)
	}
	return chars, rows.Err()
}

func (r *Repository) GetPlayerCharacter(ctx context.Context, playerID, characterID string) (*PlayerCharacter, error) {
	query := `
		SELECT player_id, character_id, level, exp, stat_points,
		       bonus_hp, bonus_damage, bonus_mobility, bonus_defense,
		       bonus_skill_power, bonus_terrain_damage, unlocked_at
		FROM player_characters
		WHERE player_id = $1 AND character_id = $2
	`
	var c PlayerCharacter
	err := r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(
		&c.PlayerID, &c.CharacterID, &c.Level, &c.Exp, &c.StatPoints,
		&c.BonusHP, &c.BonusDamage, &c.BonusMobility, &c.BonusDefense,
		&c.BonusSkillPower, &c.BonusTerrainDmg, &c.UnlockedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("character not found")
		}
		return nil, fmt.Errorf("get character: %w", err)
	}
	return &c, nil
}

func (r *Repository) AllocateStats(ctx context.Context, playerID, characterID string, hp, damage, mobility, defense, skillPower, terrainDmg int) error {
	total := hp + damage + mobility + defense + skillPower + terrainDmg
	query := `
		UPDATE player_characters
		SET bonus_hp = bonus_hp + $3,
		    bonus_damage = bonus_damage + $4,
		    bonus_mobility = bonus_mobility + $5,
		    bonus_defense = bonus_defense + $6,
		    bonus_skill_power = bonus_skill_power + $7,
		    bonus_terrain_damage = bonus_terrain_damage + $8,
		    stat_points = stat_points - $9
		WHERE player_id = $1 AND character_id = $2 AND stat_points >= $9
	`
	result, err := r.db.Pool.Exec(ctx, query, playerID, characterID, hp, damage, mobility, defense, skillPower, terrainDmg, total)
	if err != nil {
		return fmt.Errorf("allocate stats: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("not enough stat points or character not found")
	}
	return nil
}

func (r *Repository) ResetStats(ctx context.Context, playerID, characterID string) (int, error) {
	// Returns total points refunded
	query := `
		UPDATE player_characters
		SET stat_points = stat_points + bonus_hp + bonus_damage + bonus_mobility + bonus_defense + bonus_skill_power + bonus_terrain_damage,
		    bonus_hp = 0, bonus_damage = 0, bonus_mobility = 0, bonus_defense = 0, bonus_skill_power = 0, bonus_terrain_damage = 0
		WHERE player_id = $1 AND character_id = $2
		RETURNING stat_points
	`
	var newPoints int
	err := r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(&newPoints)
	if err != nil {
		return 0, fmt.Errorf("reset stats: %w", err)
	}
	return newPoints, nil
}

func (r *Repository) GetConfigValue(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("get config %s: %w", key, err)
	}
	return value, nil
}

// GetCharacterBonuses returns bonus stats for a player's character (used by match engine).
func (r *Repository) GetCharacterBonuses(ctx context.Context, playerID, characterID string) (bonusHP, bonusDamage, bonusMobility, bonusDefense, bonusSkillPower, bonusTerrainDmg int, err error) {
	query := `
		SELECT bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage
		FROM player_characters
		WHERE player_id = $1 AND character_id = $2
	`
	err = r.db.Pool.QueryRow(ctx, query, playerID, characterID).Scan(
		&bonusHP, &bonusDamage, &bonusMobility, &bonusDefense, &bonusSkillPower, &bonusTerrainDmg,
	)
	if err == pgx.ErrNoRows {
		return 0, 0, 0, 0, 0, 0, nil // no bonus, character not owned or is "rookie"
	}
	return
}

// AddExpAndLevel adds EXP to a character and levels up if thresholds are met.
// Called within a DB transaction from reward processing.
func AddExpAndLevelTx(ctx context.Context, tx pgx.Tx, playerID, characterID string, expGained int, levels []LevelEntry, pointsPerLevel int) (newLevel int, levelsGained int, err error) {
	// Lock row and get current state
	var currentExp, currentLevel int
	err = tx.QueryRow(ctx,
		`SELECT exp, level FROM player_characters WHERE player_id = $1 AND character_id = $2 FOR UPDATE`,
		playerID, characterID).Scan(&currentExp, &currentLevel)
	if err == pgx.ErrNoRows {
		// Character not owned (e.g., "rookie" default not in table) — insert it
		_, err = tx.Exec(ctx,
			`INSERT INTO player_characters (player_id, character_id, level, exp, stat_points) VALUES ($1, $2, 1, $3, 0) ON CONFLICT DO NOTHING`,
			playerID, characterID, expGained)
		if err != nil {
			return 1, 0, err
		}
		currentExp = expGained
		currentLevel = 1
	} else if err != nil {
		return 0, 0, err
	} else {
		currentExp += expGained
	}

	// Calculate new level
	newLevel = 1
	for _, entry := range levels {
		if currentExp >= entry.ExpRequired {
			newLevel = entry.Level
		}
	}

	levelsGained = 0
	if newLevel > currentLevel {
		levelsGained = newLevel - currentLevel
	}

	pointsToAdd := levelsGained * pointsPerLevel

	_, err = tx.Exec(ctx,
		`UPDATE player_characters SET exp = $3, level = $4, stat_points = stat_points + $5 WHERE player_id = $1 AND character_id = $2`,
		playerID, characterID, currentExp, newLevel, pointsToAdd)

	return newLevel, levelsGained, err
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/character/...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add internal/api/character/model.go internal/api/character/repository.go
git commit -m "feat: add character progression model and repository"
```

---

### Task 3: Character Module — Service & Handler

**Files:**
- Create: `internal/api/character/service.go`
- Create: `internal/api/character/handler.go`

- [ ] **Step 1: Create service.go**

```go
package character

import (
	"context"
	"fmt"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, economyRepo: economyRepo, db: db}
}

func (s *Service) GetPlayerCharacters(ctx context.Context, playerID string) ([]PlayerCharacter, error) {
	chars, err := s.repo.GetPlayerCharacters(ctx, playerID)
	if err != nil {
		return nil, err
	}

	// Load level config to compute nextLevelExp
	levelsVal, err := s.repo.GetConfigValue(ctx, "character_levels")
	if err != nil {
		// Return chars without level info if config missing
		return chars, nil
	}
	levelsCfg := LoadLevelsConfig(levelsVal)

	for i := range chars {
		_, nextExp, isMax := CalcLevelFromExp(chars[i].Exp, levelsCfg.Levels)
		chars[i].NextLevelExp = nextExp
		chars[i].IsMaxLevel = isMax
	}

	return chars, nil
}

func (s *Service) AllocateStats(ctx context.Context, playerID string, payload AllocateStatsPayload) error {
	// Validate all values >= 0
	if payload.HP < 0 || payload.Damage < 0 || payload.Mobility < 0 || payload.Defense < 0 || payload.SkillPower < 0 || payload.TerrainDamage < 0 {
		return fmt.Errorf("stat values must be non-negative")
	}

	total := payload.HP + payload.Damage + payload.Mobility + payload.Defense + payload.SkillPower + payload.TerrainDamage
	if total == 0 {
		return fmt.Errorf("must allocate at least 1 point")
	}

	return s.repo.AllocateStats(ctx, playerID, payload.CharacterID,
		payload.HP, payload.Damage, payload.Mobility, payload.Defense, payload.SkillPower, payload.TerrainDamage)
}

func (s *Service) ResetStats(ctx context.Context, playerID string, characterID string) error {
	// Load cost config
	cfgVal, err := s.repo.GetConfigValue(ctx, "character_progression")
	if err != nil {
		return fmt.Errorf("failed to load progression config: %w", err)
	}
	cfg := LoadProgressionConfig(cfgVal)

	// Check if character has any allocated points
	char, err := s.repo.GetPlayerCharacter(ctx, playerID, characterID)
	if err != nil {
		return err
	}

	totalBonuses := char.BonusHP + char.BonusDamage + char.BonusMobility + char.BonusDefense + char.BonusSkillPower + char.BonusTerrainDmg
	if totalBonuses == 0 {
		return fmt.Errorf("no stats to reset")
	}

	// Deduct cost in a transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	err = s.economyRepo.DebitTx(ctx, tx, playerID, cfg.ResetCostCurrency, cfg.ResetCostAmount, "stat_reset", "reset_"+characterID)
	if err != nil {
		return fmt.Errorf("insufficient %s for reset (cost: %d)", cfg.ResetCostCurrency, cfg.ResetCostAmount)
	}

	// Reset stats
	_, err = s.repo.ResetStats(ctx, playerID, characterID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
```

- [ ] **Step 2: Create handler.go**

```go
package character

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

func (h *Handler) GetCharacters(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	chars, err := h.service.GetPlayerCharacters(r.Context(), playerID)
	if err != nil {
		observability.FromContext(r.Context()).Error().Err(err).Msg("failed to get characters")
		model.WriteError(w, r, model.ErrInternalServer)
		return
	}

	if chars == nil {
		chars = []PlayerCharacter{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"characters": chars,
	})
}

func (h *Handler) AllocateStats(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var payload AllocateStatsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if payload.CharacterID == "" {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.AllocateStats(r.Context(), playerID, payload); err != nil {
		observability.FromContext(r.Context()).Warn().Err(err).Msg("allocate stats failed")
		appErr := model.AppError{Code: "STAT_ALLOCATE_FAILED", Message: err.Error(), Status: 400}
		model.WriteError(w, r, appErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) ResetStats(w http.ResponseWriter, r *http.Request) {
	playerID, _ := r.Context().Value(observability.PlayerIDKey).(string)
	if playerID == "" {
		model.WriteError(w, r, model.ErrUnauthorized)
		return
	}

	var payload ResetStatsPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if payload.CharacterID == "" {
		model.WriteError(w, r, model.ErrBadRequest)
		return
	}

	if err := h.service.ResetStats(r.Context(), playerID, payload.CharacterID); err != nil {
		observability.FromContext(r.Context()).Warn().Err(err).Msg("reset stats failed")
		appErr := model.AppError{Code: "STAT_RESET_FAILED", Message: err.Error(), Status: 400}
		model.WriteError(w, r, appErr)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/api/character/...`
Expected: Build succeeds (note: `economy.Repository.DebitTx` must exist — verify, adapt method name if different)

- [ ] **Step 4: Commit**

```bash
git add internal/api/character/service.go internal/api/character/handler.go
git commit -m "feat: add character stat allocation and reset API"
```

---

### Task 4: Wire Character Module in API Server

**Files:**
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Add character module wiring**

Read `cmd/api/main.go`. In the module initialization section (after inventoryHandler), add:

```go
	characterRepo := character.NewRepository(db)
	characterService := character.NewService(characterRepo, economyRepo, db)
	characterHandler := character.NewHandler(characterService)
```

Add import: `"battle-squad/internal/api/character"`

- [ ] **Step 2: Add routes**

In the protected routes group (after inventory route), add:

```go
		r.Get("/player/characters", characterHandler.GetCharacters)
		r.Post("/player/character/allocate-stats", characterHandler.AllocateStats)
		r.Post("/player/character/reset-stats", characterHandler.ResetStats)
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./cmd/api/...`
Expected: Build succeeds

- [ ] **Step 4: Commit**

```bash
git add cmd/api/main.go
git commit -m "feat: wire character progression module in API server"
```

---

### Task 5: Grant Character EXP After Match

**Files:**
- Modify: `internal/game/match/reward.go`

- [ ] **Step 1: Add character EXP processing in ProcessMatchRewards**

Read `internal/game/match/reward.go`. Find the loop that iterates over `stats` (the `for _, p := range stats {` block). After the match history insert and before building `RewardResult`, add character EXP logic:

```go
		// 6. Update character EXP and level
		charLevelUp := false
		charNewLevel := 0
		charLevelsGained := 0

		// Load character levels config
		var charLevelsVal string
		if err := tx.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'character_levels'`).Scan(&charLevelsVal); err == nil {
			var levelsCfg struct {
				Levels []struct {
					Level       int `json:"level"`
					ExpRequired int `json:"expRequired"`
				} `json:"levels"`
			}
			if json.Unmarshal([]byte(charLevelsVal), &levelsCfg) == nil && len(levelsCfg.Levels) > 0 {
				var progressionVal string
				pointsPerLevel := 10
				if err := tx.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'character_progression'`).Scan(&progressionVal); err == nil {
					var progCfg struct {
						PointsPerLevel int `json:"pointsPerLevel"`
					}
					if json.Unmarshal([]byte(progressionVal), &progCfg) == nil && progCfg.PointsPerLevel > 0 {
						pointsPerLevel = progCfg.PointsPerLevel
					}
				}

				// Build level entries for CalcLevelFromExp equivalent
				type levelEntry struct {
					Level       int
					ExpRequired int
				}
				entries := make([]levelEntry, len(levelsCfg.Levels))
				for i, l := range levelsCfg.Levels {
					entries[i] = levelEntry{Level: l.Level, ExpRequired: l.ExpRequired}
				}

				// Get current character state (or insert if rookie default)
				var currentExp, currentLevel int
				err := tx.QueryRow(ctx,
					`SELECT exp, level FROM player_characters WHERE player_id = $1 AND character_id = $2 FOR UPDATE`,
					p.PlayerID, matchCharacterIDs[p.PlayerID]).Scan(&currentExp, &currentLevel)
				if err != nil {
					// Character row doesn't exist — insert it
					_, _ = tx.Exec(ctx,
						`INSERT INTO player_characters (player_id, character_id, level, exp, stat_points) VALUES ($1, $2, 1, 0, 0) ON CONFLICT DO NOTHING`,
						p.PlayerID, matchCharacterIDs[p.PlayerID])
					currentExp = 0
					currentLevel = 1
				}

				newExp := currentExp + expGained
				newLevel := 1
				for _, entry := range entries {
					if newExp >= entry.ExpRequired {
						newLevel = entry.Level
					}
				}

				levelsGained := 0
				if newLevel > currentLevel {
					levelsGained = newLevel - currentLevel
				}
				pointsToAdd := levelsGained * pointsPerLevel

				_, _ = tx.Exec(ctx,
					`UPDATE player_characters SET exp = $3, level = $4, stat_points = stat_points + $5 WHERE player_id = $1 AND character_id = $2`,
					p.PlayerID, matchCharacterIDs[p.PlayerID], newExp, newLevel, pointsToAdd)

				if levelsGained > 0 {
					charLevelUp = true
					charNewLevel = newLevel
					charLevelsGained = levelsGained
				}
			}
		}
```

- [ ] **Step 2: Build matchCharacterIDs map**

Before the reward loop, build a map of playerID → characterID from the match players. Add this before `for _, p := range stats`:

```go
	// Build playerID → characterID mapping for character EXP
	matchCharacterIDs := make(map[string]string)
	for _, p := range stats {
		// Character ID is stored in match player state — need to pass it
		matchCharacterIDs[p.PlayerID] = p.CharacterID
	}
```

This requires adding `CharacterID` to `PlayerStats`. In `match/model.go`, add to `PlayerStats`:

```go
type PlayerStats struct {
	PlayerID    string
	CharacterID string   // ADD THIS
	TeamID      int
	Damage      int
	Kills       int
	Accuracy    float64
	IsWinner    bool
	IsDraw      bool
}
```

Then in `checkWinCondition` where `PlayerStats` is built, set `CharacterID` from `BattlePlayerState`:

```go
stats[p.PlayerID] = &PlayerStats{
	PlayerID:    p.PlayerID,
	CharacterID: p.CharacterID,  // ADD THIS
	TeamID:      p.TeamID,
	// ... rest unchanged
}
```

- [ ] **Step 3: Add character level info to RewardResult**

In `RewardResult` struct (in reward.go), add:

```go
type RewardResult struct {
	// ... existing fields
	CharLevelUp      bool   `json:"charLevelUp"`
	CharNewLevel     int    `json:"charNewLevel"`
	CharLevelsGained int    `json:"charLevelsGained"`
}
```

Set these in the result:

```go
results[p.PlayerID] = RewardResult{
	// ... existing fields
	CharLevelUp:      charLevelUp,
	CharNewLevel:     charNewLevel,
	CharLevelsGained: charLevelsGained,
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/game/match/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/game/match/
git commit -m "feat: grant character EXP and level up after match"
```

---

### Task 6: Use Actual Stats in Match

**Files:**
- Modify: `internal/game/room/room.go`

- [ ] **Step 1: Create helper function to load actual stats**

Add this helper function to `room.go`:

```go
func (r *Room) getActualStats(playerID, characterID string, baseHP, baseDefense int) (int, int) {
	// Load bonus stats from player_characters
	var bonusHP, bonusDamage, bonusMobility, bonusDefense, bonusSkillPower, bonusTerrainDmg int
	err := r.db.Pool.QueryRow(context.Background(),
		`SELECT COALESCE(bonus_hp,0), COALESCE(bonus_damage,0), COALESCE(bonus_mobility,0), COALESCE(bonus_defense,0), COALESCE(bonus_skill_power,0), COALESCE(bonus_terrain_damage,0)
		 FROM player_characters WHERE player_id = $1 AND character_id = $2`,
		playerID, characterID).Scan(&bonusHP, &bonusDamage, &bonusMobility, &bonusDefense, &bonusSkillPower, &bonusTerrainDmg)
	if err != nil {
		return baseHP, baseDefense
	}

	// Load multipliers from game_settings
	var cfgVal string
	err = r.db.Pool.QueryRow(context.Background(),
		`SELECT value FROM game_settings WHERE key = 'character_progression'`).Scan(&cfgVal)
	if err != nil {
		return baseHP, baseDefense
	}

	type progConfig struct {
		StatMultipliers map[string]int `json:"statMultipliers"`
	}
	var cfg progConfig
	if json.Unmarshal([]byte(cfgVal), &cfg) != nil || cfg.StatMultipliers == nil {
		return baseHP, baseDefense
	}

	actualHP := baseHP + bonusHP*cfg.StatMultipliers["hp"]
	actualDefense := baseDefense + bonusDefense*cfg.StatMultipliers["defense"]

	return actualHP, actualDefense
}
```

- [ ] **Step 2: Update processStartMatch**

Find the section in `processStartMatch` where `hp` and `defense` are set from `charData`:

```go
		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp := 100
		defense := 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}
```

Add after it:

```go
		if !p.IsHost || true { // apply for all real players
			hp, defense = r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
		}
```

Wait — `IsHost` is not the right check. We want to apply for all non-bot players. But in `processStartMatch`, all players are real. So just add:

```go
		hp, defense = r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
```

- [ ] **Step 3: Update startRankedMatch**

Find the same pattern in `startRankedMatch` where `hp` and `defense` are set. After:

```go
		charData, ok := gamedata.Data.Characters[p.CharacterID]
		hp, defense := 100, 50
		if ok {
			hp = charData.HP
			defense = charData.Defense
		}

		isBot := len(p.PlayerID) >= 4 && p.PlayerID[:4] == "bot_"
```

Add after `isBot` check:

```go
		if !isBot {
			hp, defense = r.getActualStats(p.PlayerID, p.CharacterID, hp, defense)
		}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./internal/game/room/...`
Expected: Build succeeds

- [ ] **Step 5: Commit**

```bash
git add internal/game/room/room.go
git commit -m "feat: use actual character stats (base + bonus) in matches"
```

---

### Task 7: Admin Dashboard — Character Progression Config

**Files:**
- Create: `internal/admin/templates/character_progression.html`
- Modify: `internal/admin/templates/layout.html`
- Modify: `internal/admin/server.go`
- Modify: `internal/admin/handlers_matchmaking.go`

- [ ] **Step 1: Add nav link in layout.html**

In `layout.html`, after the "Matchmaking Config" link, add:

```html
    <a href="/character-progression" class="{{if eq .ActivePage "character-progression"}}active{{end}}">Character Progression</a>
```

- [ ] **Step 2: Add page handler**

In `handlers_matchmaking.go`, add:

```go
func (s *Server) handleCharacterProgressionPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, "character_progression", map[string]interface{}{
		"ActivePage": "character-progression",
	})
}
```

- [ ] **Step 3: Add routes in server.go**

Add in `Routes()`:

```go
	r.Get("/character-progression", s.handleCharacterProgressionPage)
	r.Get("/api/config/character-progression", s.handleMatchmakingConfigGet("character_progression"))
	r.Post("/api/config/character-progression", s.handleMatchmakingConfigSave("character_progression"))
	r.Get("/api/config/character-levels", s.handleMatchmakingConfigGet("character_levels"))
	r.Post("/api/config/character-levels", s.handleMatchmakingConfigSave("character_levels"))
```

- [ ] **Step 4: Create character_progression.html template**

```html
{{define "content"}}
<h1>Character Progression</h1>

<div id="app">

<div class="card">
<h2 style="font-size:18px;margin-bottom:16px;color:#1a1a2e;">Progression Settings</h2>
<form id="form-progression">
<div style="display:grid;grid-template-columns:1fr 1fr 1fr;gap:16px;">
    <div class="form-group">
        <label>pointsPerLevel</label>
        <input type="number" name="pointsPerLevel" min="1" max="100" step="1">
        <div class="desc">Số điểm stat nhận được mỗi khi lên level</div>
    </div>
    <div class="form-group">
        <label>resetCostCurrency</label>
        <select name="resetCostCurrency">
            <option value="coin">coin</option>
            <option value="gem">gem</option>
        </select>
        <div class="desc">Loại tiền tệ dùng để reset điểm stat</div>
    </div>
    <div class="form-group">
        <label>resetCostAmount</label>
        <input type="number" name="resetCostAmount" min="0" step="50">
        <div class="desc">Giá reset điểm stat — đặt 0 thì reset miễn phí</div>
    </div>
</div>
<h3 style="font-size:15px;margin:16px 0 12px;color:#555;">statMultipliers</h3>
<p style="font-size:12px;color:#888;margin-bottom:12px;">Mỗi điểm cộng vào chỉ số sẽ tăng thêm bao nhiêu giá trị thực tế. Ví dụ hp = 50 nghĩa là cộng 1 điểm HP thì HP tăng thêm 50.</p>
<div style="display:grid;grid-template-columns:repeat(6,1fr);gap:12px;">
    <div class="form-group"><label>hp</label><input type="number" name="mul_hp" min="1" step="1"></div>
    <div class="form-group"><label>damage</label><input type="number" name="mul_damage" min="1" step="1"></div>
    <div class="form-group"><label>mobility</label><input type="number" name="mul_mobility" min="1" step="1"></div>
    <div class="form-group"><label>defense</label><input type="number" name="mul_defense" min="1" step="1"></div>
    <div class="form-group"><label>skill_power</label><input type="number" name="mul_skill_power" min="1" step="1"></div>
    <div class="form-group"><label>terrain_damage</label><input type="number" name="mul_terrain_damage" min="1" step="1"></div>
</div>
<button type="submit" class="btn btn-primary">Save Progression Settings</button>
</form>
</div>

<div class="card">
<div class="top-bar">
    <h2 style="font-size:18px;color:#1a1a2e;">Level Table</h2>
    <button class="btn btn-success btn-sm" onclick="addLevel()">+ Add Level</button>
</div>
<p style="font-size:12px;color:#888;margin-bottom:12px;">Bảng ngưỡng EXP để lên level. expRequired là EXP tích lũy (cộng dồn). Admin add đến level nào thì max level là đó.</p>
<form id="form-levels">
<table>
<thead><tr><th>Level</th><th>expRequired</th><th>Actions</th></tr></thead>
<tbody id="levels-tbody"></tbody>
</table>
<div style="margin-top:12px;">
<button type="submit" class="btn btn-primary">Save Level Table</button>
</div>
</form>
</div>

</div>

<script>
function showFlash(msg, isError) {
    const existing = document.querySelectorAll('.flash');
    existing.forEach(el => el.remove());
    const div = document.createElement('div');
    div.className = 'flash ' + (isError ? 'flash-error' : 'flash-success');
    div.textContent = msg;
    document.querySelector('#app').prepend(div);
    if (!isError) setTimeout(() => div.remove(), 3000);
}

// ── Progression Config ──
async function loadProgression() {
    try {
        const res = await fetch('/api/config/character-progression');
        const data = await res.json();
        const form = document.getElementById('form-progression');
        form.querySelector('[name="pointsPerLevel"]').value = data.pointsPerLevel || 10;
        form.querySelector('[name="resetCostCurrency"]').value = data.resetCostCurrency || 'coin';
        form.querySelector('[name="resetCostAmount"]').value = data.resetCostAmount || 500;
        const mul = data.statMultipliers || {};
        form.querySelector('[name="mul_hp"]').value = mul.hp || 50;
        form.querySelector('[name="mul_damage"]').value = mul.damage || 5;
        form.querySelector('[name="mul_mobility"]').value = mul.mobility || 3;
        form.querySelector('[name="mul_defense"]').value = mul.defense || 5;
        form.querySelector('[name="mul_skill_power"]').value = mul.skill_power || 5;
        form.querySelector('[name="mul_terrain_damage"]').value = mul.terrain_damage || 3;
    } catch(e) { console.error(e); }
}

document.getElementById('form-progression').addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    const data = {
        pointsPerLevel: Number(fd.get('pointsPerLevel')),
        resetCostCurrency: fd.get('resetCostCurrency'),
        resetCostAmount: Number(fd.get('resetCostAmount')),
        statMultipliers: {
            hp: Number(fd.get('mul_hp')),
            damage: Number(fd.get('mul_damage')),
            mobility: Number(fd.get('mul_mobility')),
            defense: Number(fd.get('mul_defense')),
            skill_power: Number(fd.get('mul_skill_power')),
            terrain_damage: Number(fd.get('mul_terrain_damage')),
        }
    };
    try {
        const res = await fetch('/api/config/character-progression', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(data)});
        if (res.ok) showFlash('Progression settings saved!');
        else showFlash('Failed to save', true);
    } catch(e) { showFlash('Error: '+e.message, true); }
});

// ── Level Table ──
let levelsData = [];

async function loadLevels() {
    try {
        const res = await fetch('/api/config/character-levels');
        const data = await res.json();
        levelsData = data.levels || [];
        renderLevels();
    } catch(e) { console.error(e); }
}

function renderLevels() {
    const tbody = document.getElementById('levels-tbody');
    tbody.innerHTML = '';
    levelsData.forEach((l, idx) => {
        tbody.innerHTML += `<tr>
            <td><strong>${l.level}</strong></td>
            <td><input type="number" name="exp_${idx}" value="${l.expRequired}" min="1" step="100" style="width:120px;padding:4px 8px;border:1px solid #ccc;border-radius:4px;"></td>
            <td><button type="button" class="btn btn-danger btn-sm" onclick="removeLevel(${idx})">Delete</button></td>
        </tr>`;
    });
}

function addLevel() {
    const nextLevel = levelsData.length > 0 ? levelsData[levelsData.length-1].level + 1 : 2;
    const lastExp = levelsData.length > 0 ? levelsData[levelsData.length-1].expRequired : 0;
    levelsData.push({level: nextLevel, expRequired: lastExp + 500});
    renderLevels();
}

function removeLevel(idx) {
    levelsData.splice(idx, 1);
    // Renumber levels starting from 2
    levelsData.forEach((l, i) => { l.level = i + 2; });
    renderLevels();
}

document.getElementById('form-levels').addEventListener('submit', async (e) => {
    e.preventDefault();
    const fd = new FormData(e.target);
    levelsData.forEach((l, idx) => {
        l.expRequired = Number(fd.get(`exp_${idx}`));
    });
    // Validate ascending
    for (let i = 1; i < levelsData.length; i++) {
        if (levelsData[i].expRequired <= levelsData[i-1].expRequired) {
            showFlash('expRequired must be ascending for each level', true);
            return;
        }
    }
    try {
        const res = await fetch('/api/config/character-levels', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({levels:levelsData})});
        if (res.ok) showFlash('Level table saved!');
        else showFlash('Failed to save', true);
    } catch(e) { showFlash('Error: '+e.message, true); }
});

loadProgression();
loadLevels();
</script>
{{end}}
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./cmd/admin/...`
Expected: Build succeeds

- [ ] **Step 6: Commit**

```bash
git add internal/admin/
git commit -m "feat: add character progression config page to admin dashboard"
```

---

### Task 8: Integration Build & Test

**Files:** None (verification only)

- [ ] **Step 1: Run migration**

Run: `go run cmd/migrate/main.go`
Expected: Migration 006 applies

- [ ] **Step 2: Full build**

Run: `go build ./...`
Expected: All packages compile

- [ ] **Step 3: Run all tests**

Run: `go test ./... -count=1`
Expected: All tests pass

- [ ] **Step 4: Fix any breakages**

If `PlayerStats.CharacterID` causes test failures, update test code to include the new field.

- [ ] **Step 5: Commit if fixes needed**

```bash
git add -A
git commit -m "fix: resolve compilation issues for character progression"
```

---

## Task Dependency Order

```
Task 1 (migration) → Task 2 (model + repo) → Task 3 (service + handler) → Task 4 (wire API)
Task 5 (character EXP in rewards) — depends on Task 1
Task 6 (actual stats in match) — depends on Task 1
Task 7 (admin dashboard) — depends on Task 1
Task 8 (integration) — depends on all
```

Parallelizable: Tasks 5, 6, 7 can run in parallel after Task 1.
