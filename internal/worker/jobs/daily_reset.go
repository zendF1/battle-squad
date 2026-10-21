package jobs

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/worker"
)

// NewDailyResetJob is kept registered but does nothing: daily missions reset lazily. Every
// progress, activity and chest claim row stores the UTC+7 day / ISO week it counts for, a row
// of an older period reads as 0 / unclaimed and is overwritten on the next write
// (internal/api/mission). The old job zeroed mission_progress at 00:00 UTC (07:00 in Vietnam),
// which would wipe a day's progress in the middle of it.
func NewDailyResetJob(db *database.PostgresDB, redis *database.RedisClient) worker.Job {
	return worker.Job{
		Name:     "daily_reset",
		Interval: 24 * time.Hour,
		Fn: func(ctx context.Context) error {
			return nil
		},
	}
}
