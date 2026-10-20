package title

import (
	"context"
	"fmt"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

type Service struct {
	repo *Repository
	db   *database.PostgresDB
}

func NewService(repo *Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, db: db}
}

// List answers GET /player/titles.
func (s *Service) List(ctx context.Context, playerID string) (ListResponse, error) {
	titles, err := s.repo.Titles(ctx, s.db.Pool, playerID)
	if err != nil {
		return ListResponse{}, err
	}
	equipped, err := s.repo.Equipped(ctx, s.db.Pool, playerID)
	if err != nil {
		return ListResponse{}, err
	}
	return ListResponse{Titles: titles, Equipped: equipped}, nil
}

// Equip wears titleID ("" unequips) in one tx: lock the profile, check ownership of an
// active title through the tx (else TITLE_NOT_OWNED, also for an unknown id), write it.
func (s *Service) Equip(ctx context.Context, playerID, titleID string) (*Ref, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	found, err := s.repo.LockProfileTx(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, model.ErrNotFound
	}
	var ref *Ref
	if titleID != "" {
		if ref, err = s.repo.OwnedTx(ctx, tx, playerID, titleID); err != nil {
			return nil, err
		}
		if ref == nil {
			return nil, model.ErrTitleNotOwned
		}
	}
	ok, err := s.repo.SetEquippedTx(ctx, tx, playerID, titleID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("equip title %q: profile %s not updated", titleID, playerID)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return ref, nil
}
