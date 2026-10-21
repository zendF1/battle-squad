package mission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// querier is a pool or a tx: reads run on the pool outside a claim and on the tx inside it.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

// RecordLogin completes today's login missions of the player (no-op when already done).
func (r *Repository) RecordLogin(ctx context.Context, q querier, playerID, day string) error {
	return recordLogin(ctx, q, playerID, day)
}

// DailyMissions returns the active daily missions with the player's progress row.
func (r *Repository) DailyMissions(ctx context.Context, q querier, playerID string) ([]DailyMission, error) {
	rows, err := q.Query(ctx, `
		SELECT m.mission_id, m.title, m.target, m.required_value, m.activity, m.goto_screen, m.sort_order,
		       COALESCE(p.current_value, 0), COALESCE(p.is_claimed, FALSE), COALESCE(p.period, '')
		FROM missions m
		LEFT JOIN mission_progress p ON p.mission_id = m.mission_id AND p.player_id = $1
		WHERE m.type = $2 AND m.is_active = TRUE
		ORDER BY m.sort_order, m.mission_id`, playerID, TypeDaily)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyMission
	for rows.Next() {
		var m DailyMission
		if err := rows.Scan(&m.ID, &m.Title, &m.Target, &m.Required, &m.Activity, &m.Goto, &m.SortOrder,
			&m.Value, &m.Claimed, &m.Period); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Chests returns the active activity chests with the player's claim row.
func (r *Repository) Chests(ctx context.Context, q querier, playerID string) ([]Chest, error) {
	rows, err := q.Query(ctx, `
		SELECT m.mission_id, m.track, m.required_value, m.reward_coin, m.reward_gem, m.reward_items,
		       COALESCE(p.is_claimed, FALSE), COALESCE(p.period, '')
		FROM missions m
		LEFT JOIN mission_progress p ON p.mission_id = m.mission_id AND p.player_id = $1
		WHERE m.type = $2 AND m.is_active = TRUE
		ORDER BY m.required_value, m.mission_id`, playerID, TypeChest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chest
	for rows.Next() {
		var c Chest
		var items []byte
		if err := rows.Scan(&c.ID, &c.Track, &c.Threshold, &c.Rewards.Coin, &c.Rewards.Gem, &items,
			&c.Claimed, &c.Period); err != nil {
			return nil, err
		}
		if len(items) > 0 {
			if err := json.Unmarshal(items, &c.Rewards.Items); err != nil {
				return nil, fmt.Errorf("chest %s reward_items: %w", c.ID, err)
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Activity returns the player's activity rows (one per track, with its period).
func (r *Repository) Activity(ctx context.Context, q querier, playerID string) ([]ActivityRow, error) {
	rows, err := q.Query(ctx, `SELECT track, period, points FROM player_mission_activity WHERE player_id = $1`, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityRow
	for rows.Next() {
		var a ActivityRow
		if err := rows.Scan(&a.Track, &a.Period, &a.Points); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// LockProfileTx locks the player's profile row, serializing the player's claims.
func (r *Repository) LockProfileTx(ctx context.Context, tx pgx.Tx, playerID string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// ClaimMissionTx marks the mission claimed for day; false when it is not done that day or
// already claimed.
func (r *Repository) ClaimMissionTx(ctx context.Context, tx pgx.Tx, playerID, missionID, day string) (bool, error) {
	tag, err := tx.Exec(ctx, `
		UPDATE mission_progress p SET is_claimed = TRUE, updated_at = CURRENT_TIMESTAMP
		FROM missions m
		WHERE p.player_id = $1 AND p.mission_id = $2 AND p.period = $3 AND p.is_claimed = FALSE
		  AND m.mission_id = p.mission_id AND m.type = 'daily' AND p.current_value >= m.required_value`,
		playerID, missionID, day)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// AddActivityTx adds points to the player's activity of day and of week; a row of an older
// period restarts from points.
func (r *Repository) AddActivityTx(ctx context.Context, tx pgx.Tx, playerID, day, week string, points int) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO player_mission_activity (player_id, track, period, points)
		VALUES ($1, 'daily', $2, $4), ($1, 'weekly', $3, $4)
		ON CONFLICT (player_id, track) DO UPDATE SET
			points = CASE WHEN player_mission_activity.period = EXCLUDED.period
				THEN player_mission_activity.points + EXCLUDED.points ELSE EXCLUDED.points END,
			period = EXCLUDED.period,
			updated_at = CURRENT_TIMESTAMP`, playerID, day, week, points)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("add activity: %d rows affected", tag.RowsAffected())
	}
	return nil
}

// ClaimChestTx records the chest claimed for period (activity = the activity at the claim);
// false when it was already claimed for that period.
func (r *Repository) ClaimChestTx(ctx context.Context, tx pgx.Tx, playerID, chestID, period string, activity int) (bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed, period)
		VALUES ($1, $2, $3, TRUE, $4)
		ON CONFLICT (player_id, mission_id) DO UPDATE SET
			current_value = EXCLUDED.current_value, is_claimed = TRUE, period = EXCLUDED.period,
			updated_at = CURRENT_TIMESTAMP
		WHERE mission_progress.period <> EXCLUDED.period OR mission_progress.is_claimed = FALSE`,
		playerID, chestID, activity, period)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// GetPlayerMissions lists the active missions of missionType (achievements) with the
// player's progress.
func (r *Repository) GetPlayerMissions(ctx context.Context, playerID string, missionType string) ([]MissionProgress, error) {
	rows, err := r.db.Pool.Query(ctx, `
		SELECT m.mission_id, m.type, m.target, m.required_value, m.reward_coin, m.reward_gem,
		       COALESCE(p.current_value, 0), COALESCE(p.is_claimed, FALSE)
		FROM missions m
		LEFT JOIN mission_progress p ON m.mission_id = p.mission_id AND p.player_id = $1
		WHERE m.is_active = TRUE AND m.type = $2`, playerID, missionType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissionProgress
	for rows.Next() {
		p := MissionProgress{PlayerID: playerID}
		if err := rows.Scan(&p.MissionID, &p.Type, &p.Target, &p.RequiredValue, &p.RewardCoin, &p.RewardGem,
			&p.CurrentValue, &p.IsClaimed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
