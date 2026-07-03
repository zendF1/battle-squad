package inventory

import "context"

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetAvailableInventory(ctx context.Context, playerID string) ([]InventoryItem, error) {
	return s.repo.GetAvailableInventory(ctx, playerID)
}
