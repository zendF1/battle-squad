package economy

import (
	"context"
	"fmt"

	"battle-squad/internal/shared/database"
)

type Service struct {
	repo *Repository
	db   *database.PostgresDB
}

func NewService(repo *Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, db: db}
}

func (s *Service) Credit(ctx context.Context, playerID, currency string, amount int, source, refID string) (int, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	newBal, err := s.repo.CreditTx(ctx, tx, playerID, currency, amount, source, refID)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit economy transaction: %w", err)
	}

	return newBal, nil
}

func (s *Service) Debit(ctx context.Context, playerID, currency string, amount int, source, refID string, allowNegative bool) (int, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	newBal, err := s.repo.DebitTx(ctx, tx, playerID, currency, amount, source, refID, allowNegative)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("failed to commit economy transaction: %w", err)
	}

	return newBal, nil
}
