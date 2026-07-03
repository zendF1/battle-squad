package moderation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/shared/database"
)

type Service struct {
	repo *Repository
	db   *database.PostgresDB
}

func NewService(repo *Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, db: db}
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}

func (s *Service) CreateReport(ctx context.Context, reporterID, targetID string, matchID *string, category, description *string) error {
	if targetID == "" || category == nil || *category == "" {
		return errors.New("target player ID and category are required")
	}

	report := &PlayerReport{
		ReportID:         generateID(),
		ReporterPlayerID: reporterID,
		TargetPlayerID:   targetID,
		MatchID:          matchID,
		Category:         *category,
		Description:      description,
		Status:           "open",
	}

	return s.repo.CreateReport(ctx, report)
}

func (s *Service) BanPlayer(ctx context.Context, playerID string, reasonCode, reasonText string, durationHours *int) error {
	if playerID == "" || reasonCode == "" {
		return errors.New("player ID and reason code are required")
	}

	// 1. Start PostgreSQL transaction — account lookup and ban write are atomic
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 2. Get and lock Account ID row (FOR UPDATE prevents concurrent profile changes)
	accountID, err := s.repo.GetAccountIDByPlayerIDTx(ctx, tx, playerID)
	if err != nil {
		return fmt.Errorf("player not found: %w", err)
	}

	var endsAt *time.Time
	if durationHours != nil && *durationHours > 0 {
		t := time.Now().Add(time.Duration(*durationHours) * time.Hour)
		endsAt = &t
	}

	ban := &AccountBan{
		BanID:      generateID(),
		AccountID:  accountID,
		PlayerID:   playerID,
		ReasonCode: reasonCode,
		ReasonText: reasonText,
		Source:     "moderator",
		Status:     "active",
		EndsAt:     endsAt,
	}

	err = s.repo.BanAccountTx(ctx, tx, ban)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) RevokeBan(ctx context.Context, playerID string) error {
	if playerID == "" {
		return errors.New("player ID is required")
	}

	accountID, err := s.repo.GetAccountIDByPlayerID(ctx, playerID)
	if err != nil {
		return fmt.Errorf("player not found: %w", err)
	}

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	err = s.repo.RevokeBanTx(ctx, tx, accountID, playerID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}
