package mission

import (
	"context"
	"fmt"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/milestone"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/model"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
	now         func() time.Time
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{repo: repo, economyRepo: economyRepo, db: db, now: time.Now}
}

// Daily answers GET /missions/daily; the first call of the day completes the login mission.
func (s *Service) Daily(ctx context.Context, playerID string) (DailyState, error) {
	p := PeriodsAt(s.now())
	if err := s.repo.RecordLogin(ctx, s.db.Pool, playerID, p.Day); err != nil {
		return DailyState{}, err
	}
	return s.state(ctx, s.db.Pool, playerID, p)
}

func (s *Service) state(ctx context.Context, q querier, playerID string, p Periods) (DailyState, error) {
	ms, err := s.repo.DailyMissions(ctx, q, playerID)
	if err != nil {
		return DailyState{}, err
	}
	cs, err := s.repo.Chests(ctx, q, playerID)
	if err != nil {
		return DailyState{}, err
	}
	act, err := s.repo.Activity(ctx, q, playerID)
	if err != nil {
		return DailyState{}, err
	}
	return BuildState(p, ms, cs, act), nil
}

// ClaimMission claims a done mission for its activity points (today's and this week's), in
// one tx under the player's profile lock: MISSION_NOT_FOUND, MISSION_NOT_DONE,
// MISSION_ALREADY_CLAIMED.
func (s *Service) ClaimMission(ctx context.Context, playerID, missionID string) (*ClaimMissionResult, error) {
	if missionID == "" {
		return nil, model.ErrBadRequest
	}
	p := PeriodsAt(s.now())
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
	if err := s.repo.RecordLogin(ctx, tx, playerID, p.Day); err != nil {
		return nil, err
	}
	ms, err := s.repo.DailyMissions(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	var m *DailyMission
	for i := range ms {
		if ms[i].ID == missionID {
			m = &ms[i]
		}
	}
	if m == nil {
		return nil, model.ErrMissionNotFound
	}
	switch MissionState(*m, p) {
	case StateClaimed:
		return nil, model.ErrMissionAlreadyClaimed
	case StateProgress:
		return nil, model.ErrMissionNotDone
	}
	ok, err := s.repo.ClaimMissionTx(ctx, tx, playerID, missionID, p.Day)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, model.ErrMissionAlreadyClaimed
	}
	if m.Activity > 0 {
		if err := s.repo.AddActivityTx(ctx, tx, playerID, p.Day, p.Week, m.Activity); err != nil {
			return nil, err
		}
	}
	st, err := s.state(ctx, tx, playerID, p)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ClaimMissionResult{Activity: m.Activity, State: st}, nil
}

// ClaimChest claims an activity chest once per period (day or week) and grants its coin / gem
// (economy source "mission") and items, in one tx under the player's profile lock:
// CHEST_NOT_FOUND, CHEST_LOCKED, CHEST_ALREADY_CLAIMED.
func (s *Service) ClaimChest(ctx context.Context, playerID, chestID string) (*ClaimChestResult, error) {
	if chestID == "" {
		return nil, model.ErrBadRequest
	}
	p := PeriodsAt(s.now())
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
	cs, err := s.repo.Chests(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	var c *Chest
	for i := range cs {
		if cs[i].ID == chestID && p.Key(cs[i].Track) != "" {
			c = &cs[i]
		}
	}
	if c == nil {
		return nil, model.ErrChestNotFound
	}
	act, err := s.repo.Activity(ctx, tx, playerID)
	if err != nil {
		return nil, err
	}
	daily, weekly := Activity(act, p)
	switch ChestState(*c, p, daily, weekly) {
	case StateClaimed:
		return nil, model.ErrChestAlreadyClaimed
	case StateLocked:
		return nil, model.ErrChestLocked
	}
	activity := daily
	if c.Track == TrackWeekly {
		activity = weekly
	}
	ok, err := s.repo.ClaimChestTx(ctx, tx, playerID, c.ID, p.Key(c.Track), activity)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, model.ErrChestAlreadyClaimed
	}
	if c.Rewards.Coin > 0 {
		if _, err := s.economyRepo.CreditTx(ctx, tx, playerID, "coin", c.Rewards.Coin, EconomySource, c.ID); err != nil {
			return nil, err
		}
	}
	if c.Rewards.Gem > 0 {
		if _, err := s.economyRepo.CreditTx(ctx, tx, playerID, "gem", c.Rewards.Gem, EconomySource, c.ID); err != nil {
			return nil, err
		}
	}
	for _, it := range c.Rewards.Items {
		if err := milestone.GrantRewardItemTx(ctx, tx, playerID, it, ItemSource); err != nil {
			return nil, fmt.Errorf("chest %s: %w", c.ID, err)
		}
	}
	st, err := s.state(ctx, tx, playerID, p)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ClaimChestResult{Rewards: normalized(c.Rewards), State: st}, nil
}

// GetMissions lists the missions of missionType (GET /missions/achievements).
func (s *Service) GetMissions(ctx context.Context, playerID string, missionType string) ([]MissionProgress, error) {
	return s.repo.GetPlayerMissions(ctx, playerID, missionType)
}
