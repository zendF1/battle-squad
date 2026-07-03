package rank

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

func (r *Repository) GetActiveSeason(ctx context.Context) (*Season, error) {
	query := `
		SELECT season_id, name, starts_at, ends_at, status
		FROM rank_seasons
		WHERE status = 'active'
		LIMIT 1
	`
	var s Season
	err := r.db.Pool.QueryRow(ctx, query).Scan(
		&s.SeasonID,
		&s.Name,
		&s.StartsAt,
		&s.EndsAt,
		&s.Status,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (r *Repository) GetPlayerRank(ctx context.Context, playerID, seasonID string) (*PlayerRank, error) {
	query := `
		SELECT r.player_id, p.display_name, r.season_id, r.rating, r.tier, r.division, r.wins, r.losses, r.draws, r.win_streak, r.highest_tier, r.updated_at
		FROM player_ranks r
		JOIN player_profiles p ON r.player_id = p.player_id
		WHERE r.player_id = $1 AND r.season_id = $2
	`
	var pr PlayerRank
	err := r.db.Pool.QueryRow(ctx, query, playerID, seasonID).Scan(
		&pr.PlayerID,
		&pr.DisplayName,
		&pr.SeasonID,
		&pr.Rating,
		&pr.Tier,
		&pr.Division,
		&pr.Wins,
		&pr.Losses,
		&pr.Draws,
		&pr.WinStreak,
		&pr.HighestTier,
		&pr.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &pr, nil
}

func (r *Repository) GetLeaderboard(ctx context.Context, seasonID string, limit, offset int) ([]PlayerRank, error) {
	query := `
		SELECT r.player_id, p.display_name, r.season_id, r.rating, r.tier, r.division, r.wins, r.losses, r.draws, r.win_streak, r.highest_tier, r.updated_at,
		       ROW_NUMBER() OVER(ORDER BY r.rating DESC, r.wins DESC) as rank_pos
		FROM player_ranks r
		JOIN player_profiles p ON r.player_id = p.player_id
		WHERE r.season_id = $1
		ORDER BY r.rating DESC, r.wins DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.db.Pool.Query(ctx, query, seasonID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ranks []PlayerRank
	for rows.Next() {
		var pr PlayerRank
		err := rows.Scan(
			&pr.PlayerID,
			&pr.DisplayName,
			&pr.SeasonID,
			&pr.Rating,
			&pr.Tier,
			&pr.Division,
			&pr.Wins,
			&pr.Losses,
			&pr.Draws,
			&pr.WinStreak,
			&pr.HighestTier,
			&pr.UpdatedAt,
			&pr.RankPos,
		)
		if err != nil {
			return nil, err
		}
		ranks = append(ranks, pr)
	}

	return ranks, nil
}

func (r *Repository) HasClaimedReward(ctx context.Context, playerID, seasonID string) (bool, error) {
	query := `
		SELECT EXISTS(
			SELECT 1 FROM season_reward_claims
			WHERE player_id = $1 AND season_id = $2
		)
	`
	var exists bool
	err := r.db.Pool.QueryRow(ctx, query, playerID, seasonID).Scan(&exists)
	return exists, err
}

func (r *Repository) ClaimRewardTx(ctx context.Context, tx pgx.Tx, claimID, playerID, seasonID, tier string, coin, gem int) error {
	query := `
		INSERT INTO season_reward_claims (claim_id, player_id, season_id, tier, reward_coin, reward_gem)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.Exec(ctx, query, claimID, playerID, seasonID, tier, coin, gem)
	return err
}
