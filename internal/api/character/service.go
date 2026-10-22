package character

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
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
// The stored level is authoritative (it is what equip requirements check); the
// level curve only supplies next-level progress.
func (s *Service) GetPlayerCharacters(ctx context.Context, playerID string) ([]PlayerCharacter, error) {
	chars, err := s.repo.GetPlayerCharacters(ctx, playerID)
	if err != nil {
		return nil, err
	}

	// Load the level curve from game_settings to compute level metadata.
	levelsRaw, err := s.repo.GetConfigValue(ctx, SettingsKeyLevels)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load levels config: %w", err)
	}
	levelsCfg := LoadLevelsConfig(levelsRaw)

	for i := range chars {
		chars[i].NextLevelExp, chars[i].IsMaxLevel = NextLevelExp(chars[i].Level, levelsCfg.Levels)
	}

	return chars, nil
}

// GetCharacters returns the player's characters with their battle stats, the
// stat reset fee (the same character_progression config ResetStats charges),
// every character's base stats and the per-point stat multipliers. Stats come
// from battlestats, the code the game server builds match players with.
func (s *Service) GetCharacters(ctx context.Context, playerID string) (CharactersResponse, error) {
	chars, err := s.GetPlayerCharacters(ctx, playerID)
	if err != nil {
		return CharactersResponse{}, err
	}
	if chars == nil {
		chars = []PlayerCharacter{}
	}
	cfg, err := s.progressionConfig(ctx)
	if err != nil {
		return CharactersResponse{}, err
	}
	equipment, err := s.repo.GetEquipmentPoints(ctx, playerID)
	if err != nil {
		return CharactersResponse{}, err
	}
	mult := cfg.Multipliers()
	for i := range chars {
		id := chars[i].CharacterID
		chars[i].Stats = newCharacterStats(id, battlestats.Compute(id, chars[i].Points(), mult, equipment[id]))
	}
	return CharactersResponse{
		Characters:      chars,
		ResetStatsCost:  cfg.ResetStatsCost(),
		BaseStats:       allBaseStats(),
		StatMultipliers: mult,
	}, nil
}

// allBaseStats returns the base stats of every character in the game data.
func allBaseStats() map[string]BaseStats {
	out := map[string]BaseStats{}
	for id := range gamedata.Current().Data.Characters {
		out[id] = BaseStats{StatValues: statValues(battlestats.Base(id)), WeaponDamage: battlestats.WeaponDamage(id)}
	}
	return out
}

// progressionConfig loads character_progression from game_settings (defaults when absent).
func (s *Service) progressionConfig(ctx context.Context) (ProgressionConfig, error) {
	raw, err := s.repo.GetConfigValue(ctx, SettingsKeyProgression)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ProgressionConfig{}, fmt.Errorf("load progression config: %w", err)
	}
	return LoadProgressionConfig(raw), nil
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

// ResetStats charges the player the reset cost, clears all allocated bonus points and
// grants the points the character's level earns under the current pointsPerLevel.
func (s *Service) ResetStats(ctx context.Context, playerID, characterID string) error {
	if characterID == "" {
		return model.ErrBadRequest
	}

	cfg, err := s.progressionConfig(ctx)
	if err != nil {
		return err
	}

	// The checks, debit and reset share one transaction: the locked rows stop a
	// concurrent reset from charging twice for one refund.
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := s.repo.LockPlayerTx(ctx, tx, playerID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrCharacterNotOwned
		}
		return fmt.Errorf("lock player: %w", err)
	}

	totalAllocated, err := s.repo.LockAllocatedPointsTx(ctx, tx, playerID, characterID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ErrCharacterNotOwned
		}
		return fmt.Errorf("lock character: %w", err)
	}
	if totalAllocated == 0 {
		return model.AppError{
			Code:    "CHARACTER_NO_STATS_TO_RESET",
			Message: "No allocated stat points to reset",
			Status:  http.StatusBadRequest,
		}
	}

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

	if err := s.repo.ResetStatsTx(ctx, tx, playerID, characterID, cfg.PointsPerLevel); err != nil {
		return fmt.Errorf("reset stats: %w", err)
	}

	return tx.Commit(ctx)
}
