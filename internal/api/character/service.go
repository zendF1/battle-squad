package character

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"

	"github.com/jackc/pgx/v5"
)

// Service implements business logic for character progression.
type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

// NewService creates a new Service.
func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
	}
}

// GetPlayerCharacters returns all characters for a player with level/exp metadata resolved.
func (s *Service) GetPlayerCharacters(ctx context.Context, playerID string) ([]PlayerCharacter, error) {
	chars, err := s.repo.GetPlayerCharacters(ctx, playerID)
	if err != nil {
		return nil, err
	}

	// Load the level curve from game_settings to compute level metadata.
	levelsRaw, err := s.repo.GetConfigValue(ctx, "character_levels_config")
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load levels config: %w", err)
	}
	levelsCfg := LoadLevelsConfig(levelsRaw)

	for i := range chars {
		lvl, nextExp, isMax := CalcLevelFromExp(chars[i].Exp, levelsCfg.Levels)
		chars[i].Level = lvl
		chars[i].NextLevelExp = nextExp
		chars[i].IsMaxLevel = isMax
	}

	return chars, nil
}

// AllocateStats validates the payload and applies stat point allocation.
func (s *Service) AllocateStats(ctx context.Context, playerID string, payload AllocateStatsPayload) error {
	if payload.CharacterID == "" {
		return model.ErrBadRequest
	}

	// All values must be non-negative.
	if payload.HP < 0 || payload.Damage < 0 || payload.Mobility < 0 ||
		payload.Defense < 0 || payload.SkillPower < 0 || payload.TerrainDamage < 0 {
		return model.AppError{
			Code:    "CHARACTER_INVALID_STATS",
			Message: "Stat allocation values must be non-negative",
			Status:  http.StatusBadRequest,
		}
	}

	total := payload.HP + payload.Damage + payload.Mobility + payload.Defense + payload.SkillPower + payload.TerrainDamage
	if total == 0 {
		return model.AppError{
			Code:    "CHARACTER_NO_STATS_ALLOCATED",
			Message: "Total stat points to allocate must be greater than zero",
			Status:  http.StatusBadRequest,
		}
	}

	err := s.repo.AllocateStats(ctx, playerID, payload.CharacterID,
		payload.HP, payload.Damage, payload.Mobility,
		payload.Defense, payload.SkillPower, payload.TerrainDamage,
	)
	if err != nil {
		// Distinguish insufficient points from other DB errors.
		if err.Error() == "insufficient stat points or character not found" {
			return model.AppError{
				Code:    "CHARACTER_INSUFFICIENT_STAT_POINTS",
				Message: "Not enough stat points to complete this allocation",
				Status:  http.StatusBadRequest,
			}
		}
		return err
	}
	return nil
}

// ResetStats charges the player the reset cost and refunds all allocated bonus points.
func (s *Service) ResetStats(ctx context.Context, playerID, characterID string) error {
	if characterID == "" {
		return model.ErrBadRequest
	}

	// Load progression config to get reset cost.
	cfgRaw, err := s.repo.GetConfigValue(ctx, "character_progression_config")
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load progression config: %w", err)
	}
	cfg := LoadProgressionConfig(cfgRaw)

	// Fetch the character to confirm it exists and has allocated bonuses.
	pc, err := s.repo.GetPlayerCharacter(ctx, playerID, characterID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.AppError{
				Code:    "CHARACTER_NOT_FOUND",
				Message: "Character not found or not owned by this player",
				Status:  http.StatusNotFound,
			}
		}
		return err
	}

	totalAllocated := pc.BonusHP + pc.BonusDamage + pc.BonusMobility +
		pc.BonusDefense + pc.BonusSkillPower + pc.BonusTerrainDmg
	if totalAllocated == 0 {
		return model.AppError{
			Code:    "CHARACTER_NO_STATS_TO_RESET",
			Message: "No allocated stat points to reset",
			Status:  http.StatusBadRequest,
		}
	}

	// Begin transaction: debit currency then reset stats.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Debit the reset cost. allowNegative = false so it fails on insufficient funds.
	_, err = s.economyRepo.DebitTx(ctx, tx, playerID,
		cfg.ResetCostCurrency, cfg.ResetCostAmount,
		"character_stat_reset", characterID,
		false,
	)
	if err != nil {
		if err.Error() == "insufficient coin balance" || err.Error() == "insufficient gem balance" {
			return model.AppError{
				Code:    "CHARACTER_INSUFFICIENT_BALANCE",
				Message: fmt.Sprintf("Insufficient %s to reset stats (cost: %d)", cfg.ResetCostCurrency, cfg.ResetCostAmount),
				Status:  http.StatusBadRequest,
			}
		}
		return fmt.Errorf("debit reset cost: %w", err)
	}

	_, err = s.repo.ResetStats(ctx, playerID, characterID)
	if err != nil {
		return fmt.Errorf("reset stats: %w", err)
	}

	return tx.Commit(ctx)
}
