package jobs

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/worker"
)

// NewBanExpireJob returns a Job that expires overdue bans and reactivates
// accounts that have no remaining active bans. Runs every 5 minutes.
func NewBanExpireJob(db *database.PostgresDB) worker.Job {
	return worker.Job{
		Name:     "ban_expire",
		Interval: 5 * time.Minute,
		Fn: func(ctx context.Context) error {
			log := observability.Log

			// Step 1: expire bans whose ends_at has passed.
			expireResult, err := db.Pool.Exec(ctx, `
				UPDATE account_bans
				SET status = 'expired'
				WHERE status = 'active'
				  AND ends_at IS NOT NULL
				  AND ends_at <= CURRENT_TIMESTAMP
			`)
			if err != nil {
				return err
			}
			log.Info().
				Str("job", "ban_expire").
				Int64("bans_expired", expireResult.RowsAffected()).
				Msg("expired overdue bans")

			// Step 2: reactivate accounts that have no remaining active bans.
			reactivateResult, err := db.Pool.Exec(ctx, `
				UPDATE accounts
				SET status = 'active'
				WHERE status = 'banned'
				  AND account_id NOT IN (
				      SELECT DISTINCT account_id FROM account_bans WHERE status = 'active'
				  )
			`)
			if err != nil {
				return err
			}
			log.Info().
				Str("job", "ban_expire").
				Int64("accounts_reactivated", reactivateResult.RowsAffected()).
				Msg("reactivated accounts with no active bans")

			return nil
		},
	}
}
