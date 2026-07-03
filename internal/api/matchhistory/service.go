package matchhistory

import "context"

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetPlayerHistory(ctx context.Context, playerID string, limit, offset int) ([]MatchHistoryEntry, error) {
	return s.repo.GetPlayerHistory(ctx, playerID, limit, offset)
}
