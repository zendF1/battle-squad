package rank

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
)

type Service struct {
	repo        *Repository
	economyRepo *economy.Repository
	db          *database.PostgresDB
}

func NewService(repo *Repository, economyRepo *economy.Repository, db *database.PostgresDB) *Service {
	return &Service{
		repo:        repo,
		economyRepo: economyRepo,
		db:          db,
	}
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}

func (s *Service) GetCurrentSeason(ctx context.Context) (*Season, error) {
	season, err := s.repo.GetActiveSeason(ctx)
	if err != nil {
		return nil, err
	}
	if season == nil {
		// Mock season for testing if none active in DB
		return &Season{
			SeasonID: "season_1",
			Name:     "Pre-Season Alpha",
			StartsAt: time.Now().Add(-24 * time.Hour),
			EndsAt:   time.Now().Add(30 * 24 * time.Hour),
			Status:   "active",
		}, nil
	}
	return season, nil
}

func (s *Service) GetPlayerRank(ctx context.Context, playerID string) (*PlayerRank, error) {
	season, err := s.GetCurrentSeason(ctx)
	if err != nil {
		return nil, err
	}

	pr, err := s.repo.GetPlayerRank(ctx, playerID, season.SeasonID)
	if err != nil {
		return nil, err
	}
	if pr == nil {
		// Return default unranked/new-rank values
		var displayName string
		query := "SELECT display_name FROM player_profiles WHERE player_id = $1"
		err := s.db.Pool.QueryRow(ctx, query, playerID).Scan(&displayName)
		if err != nil {
			displayName = "Player_" + playerID[:6]
		}
		return &PlayerRank{
			PlayerID:    playerID,
			DisplayName: displayName,
			SeasonID:    season.SeasonID,
			Rating:      1000,
			Tier:        "silver",
			Division:    3,
			HighestTier: "silver",
			UpdatedAt:   time.Now(),
		}, nil
	}
	return pr, nil
}

func (s *Service) GetLeaderboard(ctx context.Context, page, limit int) (*LeaderboardResponse, error) {
	season, err := s.GetCurrentSeason(ctx)
	if err != nil {
		return nil, err
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	leaders, err := s.repo.GetLeaderboard(ctx, season.SeasonID, limit, offset)
	if err != nil {
		return nil, err
	}

	if leaders == nil {
		leaders = []PlayerRank{}
	}

	return &LeaderboardResponse{
		SeasonID: season.SeasonID,
		Leader:   leaders,
		Page:     page,
		Limit:    limit,
	}, nil
}

func (s *Service) ClaimSeasonReward(ctx context.Context, playerID, seasonID string) (int, int, error) {
	// 1. Check if already claimed
	claimed, err := s.repo.HasClaimedReward(ctx, playerID, seasonID)
	if err != nil {
		return 0, 0, err
	}
	if claimed {
		return 0, 0, errors.New("reward already claimed for this season")
	}

	// 2. Fetch season status
	var status string
	querySeason := "SELECT status FROM rank_seasons WHERE season_id = $1"
	err = s.db.Pool.QueryRow(ctx, querySeason, seasonID).Scan(&status)
	if err != nil {
		return 0, 0, errors.New("season not found")
	}
	// Reward claimable if season status is 'ended' or 'reward_granting'
	if status != "ended" && status != "reward_granting" && status != "closed" {
		return 0, 0, errors.New("season has not ended yet")
	}

	// 3. Fetch player's season ranking
	pr, err := s.repo.GetPlayerRank(ctx, playerID, seasonID)
	if err != nil {
		return 0, 0, err
	}
	if pr == nil {
		return 0, 0, errors.New("no rank record found for this season")
	}

	// 4. Calculate reward amounts based on highest tier
	coinReward, gemReward := calculateReward(pr.HighestTier)

	// 5. Open database transaction
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// Credit Coins
	if coinReward > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "coin", coinReward, "season_reward", seasonID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to credit coins: %w", err)
		}
	}

	// Credit Gems
	if gemReward > 0 {
		_, err = s.economyRepo.CreditTx(ctx, tx, playerID, "gem", gemReward, "season_reward", seasonID)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to credit gems: %w", err)
		}
	}

	// Add claim log
	claimID := generateID()
	err = s.repo.ClaimRewardTx(ctx, tx, claimID, playerID, seasonID, pr.HighestTier, coinReward, gemReward)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to record claim log: %w", err)
	}

	// Commit
	err = tx.Commit(ctx)
	return coinReward, gemReward, err
}

func calculateReward(tier string) (int, int) {
	// Bronze = 100 Coin
	// Silver = 300 Coin + 10 Gem
	// Gold = 500 Coin + 30 Gem
	// Platinum = 1000 Coin + 50 Gem
	// Diamond = 2000 Coin + 100 Gem
	// Master = 5000 Coin + 200 Gem
	switch tier {
	case "bronze":
		return 100, 0
	case "silver":
		return 300, 10
	case "gold":
		return 500, 30
	case "platinum":
		return 1000, 50
	case "diamond":
		return 2000, 100
	case "master":
		return 5000, 200
	default:
		return 100, 0
	}
}
