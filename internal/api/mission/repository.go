package mission

import (
	"context"
	"errors"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetPlayerMissions(ctx context.Context, playerID string, missionType string) ([]MissionProgress, error) {
	// Query list of active missions of specified type, joining player progress if exists
	query := `
		SELECT m.mission_id, m.type, m.target, m.required_value, m.reward_coin, m.reward_gem,
		       COALESCE(p.current_value, 0) as current_value, COALESCE(p.is_claimed, FALSE) as is_claimed
		FROM missions m
		LEFT JOIN mission_progress p ON m.mission_id = p.mission_id AND p.player_id = $1
		WHERE m.is_active = TRUE AND m.type = $2
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID, missionType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var progresses []MissionProgress
	for rows.Next() {
		var p MissionProgress
		p.PlayerID = playerID
		err := rows.Scan(
			&p.MissionID,
			&p.Type,
			&p.Target,
			&p.RequiredValue,
			&p.RewardCoin,
			&p.RewardGem,
			&p.CurrentValue,
			&p.IsClaimed,
		)
		if err != nil {
			return nil, err
		}
		progresses = append(progresses, p)
	}

	return progresses, nil
}

func (r *Repository) GetMissionProgress(ctx context.Context, playerID, missionID string) (*MissionProgress, error) {
	query := `
		SELECT m.mission_id, m.type, m.required_value, m.reward_coin, m.reward_gem,
		       COALESCE(p.current_value, 0) as current_value, COALESCE(p.is_claimed, FALSE) as is_claimed
		FROM missions m
		LEFT JOIN mission_progress p ON m.mission_id = p.mission_id AND p.player_id = $1
		WHERE m.mission_id = $2
	`
	var p MissionProgress
	p.PlayerID = playerID
	err := r.db.Pool.QueryRow(ctx, query, playerID, missionID).Scan(
		&p.MissionID,
		&p.Type,
		&p.RequiredValue,
		&p.RewardCoin,
		&p.RewardGem,
		&p.CurrentValue,
		&p.IsClaimed,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) IncrementProgressTx(ctx context.Context, tx pgx.Tx, playerID, target string, value int) error {
	// Increment progress of active missions matching the target event
	query := `
		INSERT INTO mission_progress (player_id, mission_id, current_value)
		SELECT $1, m.mission_id, $3
		FROM missions m
		WHERE m.is_active = TRUE AND m.target = $2
		ON CONFLICT (player_id, mission_id)
		DO UPDATE SET current_value = mission_progress.current_value + EXCLUDED.current_value, updated_at = CURRENT_TIMESTAMP
	`
	_, err := tx.Exec(ctx, query, playerID, target, value)
	return err
}

func (r *Repository) MarkClaimedTx(ctx context.Context, tx pgx.Tx, playerID, missionID string) error {
	query := `
		UPDATE mission_progress
		SET is_claimed = TRUE, updated_at = CURRENT_TIMESTAMP
		WHERE player_id = $1 AND mission_id = $2
	`
	_, err := tx.Exec(ctx, query, playerID, missionID)
	return err
}
