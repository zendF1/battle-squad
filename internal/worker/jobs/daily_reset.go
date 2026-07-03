package jobs

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/worker"
)

const dailyResetRedisKey = "worker:daily_reset:last_run_date"

// NewDailyResetJob returns a Job that resets daily mission progress once per
// UTC day. The job runs every hour but uses Redis to ensure it only executes
// the reset a single time per calendar day.
func NewDailyResetJob(db *database.PostgresDB, redis *database.RedisClient) worker.Job {
	return worker.Job{
		Name:     "daily_reset",
		Interval: 1 * time.Hour,
		Fn: func(ctx context.Context) error {
			log := observability.Log

			// Determine today's UTC date string, e.g. "2026-07-03".
			today := time.Now().UTC().Format("2006-01-02")

			// Check whether we already ran the reset today.
			lastRun, err := redis.Client.Get(ctx, dailyResetRedisKey).Result()
			if err == nil && lastRun == today {
				log.Debug().
					Str("job", "daily_reset").
					Str("today", today).
					Msg("daily reset already ran today, skipping")
				return nil
			}

			// Run the reset.
			result, err := db.Pool.Exec(ctx, `
				UPDATE mission_progress
				SET current_value = 0,
				    is_claimed    = false,
				    updated_at    = CURRENT_TIMESTAMP
				WHERE mission_id IN (
				    SELECT mission_id FROM missions WHERE type = 'daily'
				)
			`)
			if err != nil {
				return err
			}

			log.Info().
				Str("job", "daily_reset").
				Int64("rows_reset", result.RowsAffected()).
				Msg("daily mission progress reset")

			// Record that we've run the reset for today. TTL of 25 hours is
			// generous enough to survive a brief clock skew while still
			// expiring well before the next day's window opens.
			if setErr := redis.Client.Set(ctx, dailyResetRedisKey, today, 25*time.Hour).Err(); setErr != nil {
				log.Warn().Str("job", "daily_reset").Err(setErr).Msg("failed to persist last-run marker in Redis")
			}

			return nil
		},
	}
}
