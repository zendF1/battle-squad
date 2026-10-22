package boss

import (
	"context"
	"time"

	"battle-squad/internal/api/mission"
	"battle-squad/internal/game/gamedata"
)

type Service struct {
	repo    *Repository
	current func() *gamedata.Snapshot
	now     func() time.Time
}

// NewService serves the bosses of current() (the published game data snapshot).
func NewService(repo *Repository, current func() *gamedata.Snapshot) *Service {
	return &Service{repo: repo, current: current, now: time.Now}
}

// Runs is the player's RunsState now.
func (s *Service) Runs(ctx context.Context, playerID string) (*RunsState, error) {
	p := mission.PeriodsAt(s.now())
	runs, err := s.repo.RunsOn(ctx, playerID, p.Day)
	if err != nil {
		return nil, err
	}
	out := &RunsState{Day: p.Day, ResetAt: p.DayResetAt, Bosses: []BossRuns{}}
	for _, b := range s.current().Bosses {
		if !b.Enabled {
			continue
		}
		n := runs[b.BossID]
		out.Bosses = append(out.Bosses, BossRuns{BossID: b.BossID, DailyBonusRuns: b.DailyBonusRuns, RunsToday: n,
			BonusRunsLeft: max(b.DailyBonusRuns-n, 0)})
	}
	return out, nil
}
