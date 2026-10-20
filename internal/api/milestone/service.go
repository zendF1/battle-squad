package milestone

import (
	"context"
	"fmt"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

// EconomySource is economy_transactions.source of milestone coin and gem; ref_id is the id.
const EconomySource = "milestone_reward"

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, economyRepo: economyRepo, db: db}
}

// List answers GET /milestones.
func (s *Service) List(ctx context.Context, playerID string) (ListResponse, error) {
	ms, err := s.repo.Milestones(ctx, s.db.Pool, playerID)
	if err != nil {
		return ListResponse{}, err
	}
	p, err := s.repo.Progress(ctx, s.db.Pool, playerID)
	if err != nil {
		return ListResponse{}, err
	}
	return BuildList(ms, p), nil
}

// Summary answers GET /milestones/summary.
func (s *Service) Summary(ctx context.Context, playerID string) (SummaryResponse, error) {
	list, err := s.List(ctx, playerID)
	return SummaryResponse{ClaimableCount: list.ClaimableCount}, err
}

// Claim claims one milestone: NOT_FOUND when unknown (or not servable),
// MILESTONE_ALREADY_CLAIMED, MILESTONE_NOT_REACHED.
func (s *Service) Claim(ctx context.Context, playerID, id string) (*ClaimResult, error) {
	if id == "" {
		return nil, model.ErrBadRequest
	}
	return s.claim(ctx, playerID, id)
}

// ClaimAll claims every claimable milestone; none is an empty claimed list.
func (s *Service) ClaimAll(ctx context.Context, playerID string) (*ClaimResult, error) {
	return s.claim(ctx, playerID, "")
}

// claim runs in one tx: lock the profile (one claim of the player at a time), read the
// milestones, their claim flags and the progress through the tx, record each claim (a claim
// row already claimed is MILESTONE_ALREADY_CLAIMED), grant coin / gem with economy logs and
// the items. only == "" claims every claimable milestone.
func (s *Service) claim(ctx context.Context, playerID, only string) (*ClaimResult, error) {
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
	ms, err := s.repo.Milestones(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	progress, err := s.repo.Progress(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}

	var picked []Milestone
	if only != "" {
		var m *Milestone
		for i := range ms {
			if ms[i].ID == only && Supported(ms[i]) {
				m = &ms[i]
			}
		}
		switch {
		case m == nil:
			return nil, model.ErrNotFound
		case m.Claimed:
			return nil, model.ErrMilestoneAlreadyClaimed
		case Progress(m.Target, progress) < m.Required:
			return nil, model.ErrMilestoneNotReached
		}
		picked = []Milestone{*m}
	} else {
		for _, m := range ms {
			if Supported(m) && State(m, Progress(m.Target, progress)) == StateClaimable {
				picked = append(picked, m)
			}
		}
	}

	result := &ClaimResult{Claimed: []string{}, Rewards: Rewards{Items: []RewardItem{}}}
	for _, m := range picked {
		ok, err := s.repo.InsertClaimTx(ctx, tx, playerID, m.ID, Progress(m.Target, progress))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, model.ErrMilestoneAlreadyClaimed
		}
		if m.Rewards.Coin > 0 {
			if _, err := s.economyRepo.CreditTx(ctx, tx, playerID, "coin", m.Rewards.Coin, EconomySource, m.ID); err != nil {
				return nil, err
			}
		}
		if m.Rewards.Gem > 0 {
			if _, err := s.economyRepo.CreditTx(ctx, tx, playerID, "gem", m.Rewards.Gem, EconomySource, m.ID); err != nil {
				return nil, err
			}
		}
		for _, it := range m.Rewards.Items {
			if err := s.repo.GrantItemTx(ctx, tx, playerID, it); err != nil {
				return nil, fmt.Errorf("milestone %s: %w", m.ID, err)
			}
		}
		result.Claimed = append(result.Claimed, m.ID)
		result.Rewards = SumRewards(result.Rewards, m.Rewards)
	}

	coin, gem, stones, err := s.repo.BalancesTx(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	result.Coin, result.Gem, result.Stones = coin, gem, stones

	claimed := map[string]bool{}
	for _, id := range result.Claimed {
		claimed[id] = true
	}
	for i := range ms {
		if claimed[ms[i].ID] {
			ms[i].Claimed = true
		}
	}
	result.ClaimableCount = BuildList(ms, progress).ClaimableCount

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
