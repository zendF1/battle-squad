package mission

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// execer is a pool or a tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// recordSQL adds each target's delta to the day's progress of the active daily missions with
// that target. A row of an older day restarts from the delta, unclaimed. The sum is capped at
// the INT maximum.
const recordSQL = `
	INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed, period)
	SELECT $1, m.mission_id, d.delta, FALSE, $2
	FROM missions m
	JOIN unnest($3::text[], $4::int[]) AS d(target, delta) ON d.target = m.target
	WHERE m.type = 'daily' AND m.is_active = TRUE
	ON CONFLICT (player_id, mission_id) DO UPDATE SET
		current_value = CASE WHEN mission_progress.period = EXCLUDED.period
			THEN LEAST(mission_progress.current_value::bigint + EXCLUDED.current_value, 2147483647)::int
			ELSE EXCLUDED.current_value END,
		is_claimed = mission_progress.is_claimed AND mission_progress.period = EXCLUDED.period,
		period = EXCLUDED.period,
		updated_at = CURRENT_TIMESTAMP`

// RecordTx adds deltas (target → amount; amounts <= 0 are skipped) to the player's daily
// mission progress for the day of now, inside the caller's tx.
func RecordTx(ctx context.Context, tx pgx.Tx, playerID string, deltas map[string]int, now time.Time) error {
	return record(ctx, tx, playerID, deltas, now)
}

func record(ctx context.Context, q execer, playerID string, deltas map[string]int, now time.Time) error {
	targets := make([]string, 0, len(deltas))
	for t, v := range deltas {
		if v > 0 {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	sort.Strings(targets) // a stable row order for the upserts
	values := make([]int32, len(targets))
	for i, t := range targets {
		values[i] = int32(min(deltas[t], 1<<31-1))
	}
	if _, err := q.Exec(ctx, recordSQL, playerID, PeriodsAt(now).Day, targets, values); err != nil {
		return fmt.Errorf("record daily missions: %w", err)
	}
	return nil
}

// RecordMatchTx counts one match for the player's daily missions (play, win, damage, kills,
// hits, skills, items). The match end calls it for the players whose match counts for
// milestones, in the rewards tx.
func RecordMatchTx(ctx context.Context, tx pgx.Tx, playerID string, s MatchStats, now time.Time) error {
	return RecordTx(ctx, tx, playerID, matchDeltas(s), now)
}

// RecordUpgradeTx counts one equipment upgrade attempt (success or fail).
func RecordUpgradeTx(ctx context.Context, tx pgx.Tx, playerID string, now time.Time) error {
	return RecordTx(ctx, tx, playerID, map[string]int{TargetUpgrade: 1}, now)
}

// RecordShopPurchaseTx counts one successful shop purchase (an idempotent replay is not one).
func RecordShopPurchaseTx(ctx context.Context, tx pgx.Tx, playerID string, now time.Time) error {
	return RecordTx(ctx, tx, playerID, map[string]int{TargetShopPurchase: 1}, now)
}

// recordLoginSQL completes the login missions for the day: written only when the row is of
// an older day (or missing); nothing for an unknown player.
const recordLoginSQL = `
	INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed, period)
	SELECT pp.player_id, m.mission_id, m.required_value, FALSE, $2
	FROM missions m
	JOIN player_profiles pp ON pp.player_id = $1
	WHERE m.type = 'daily' AND m.is_active = TRUE AND m.target = 'login'
	ON CONFLICT (player_id, mission_id) DO UPDATE SET
		current_value = EXCLUDED.current_value, is_claimed = FALSE, period = EXCLUDED.period,
		updated_at = CURRENT_TIMESTAMP
	WHERE mission_progress.period <> EXCLUDED.period`

func recordLogin(ctx context.Context, q execer, playerID, day string) error {
	if _, err := q.Exec(ctx, recordLoginSQL, playerID, day); err != nil {
		return fmt.Errorf("record login mission: %w", err)
	}
	return nil
}
