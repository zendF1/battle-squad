package jobs

import (
	"context"
	"time"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/worker"
)

// NewAccountDeletionJob returns a Job that anonymizes and marks deleted any
// account whose deletion grace period has elapsed. Runs every 1 hour.
func NewAccountDeletionJob(db *database.PostgresDB) worker.Job {
	return worker.Job{
		Name:     "account_deletion",
		Interval: 1 * time.Hour,
		Fn: func(ctx context.Context) error {
			log := observability.Log

			// Step 1: anonymize player profiles for accounts pending deletion.
			anonymizeResult, err := db.Pool.Exec(ctx, `
				UPDATE player_profiles
				SET display_name = 'Deleted User'
				WHERE account_id IN (
				    SELECT account_id FROM accounts
				    WHERE status = 'pending_deletion'
				      AND deleted_at <= CURRENT_TIMESTAMP
				)
			`)
			if err != nil {
				return err
			}
			log.Info().
				Str("job", "account_deletion").
				Int64("profiles_anonymized", anonymizeResult.RowsAffected()).
				Msg("anonymized player profiles for pending-deletion accounts")

			// Step 2: mark accounts as deleted.
			deleteResult, err := db.Pool.Exec(ctx, `
				UPDATE accounts
				SET status = 'deleted'
				WHERE status = 'pending_deletion'
				  AND deleted_at <= CURRENT_TIMESTAMP
			`)
			if err != nil {
				return err
			}
			log.Info().
				Str("job", "account_deletion").
				Int64("accounts_deleted", deleteResult.RowsAffected()).
				Msg("marked accounts as deleted")

			return nil
		},
	}
}
