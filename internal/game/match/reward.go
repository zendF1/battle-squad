package match

import (
	"context"
	"fmt"
	"math"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type PlayerStats struct {
	PlayerID  string
	TeamID    int
	Damage    int
	Kills     int
	Accuracy  float64
	IsWinner  bool
	IsDraw    bool
}

type RewardResult struct {
	PlayerID      string
	ExpGained     int
	CoinGained    int
	RatingChange  int
	NewRating     int
	NewTier       string
	NewDivision   int
	LevelUp       bool
	NewLevel      int
}

func ProcessMatchRewards(
	ctx context.Context,
	db *database.PostgresDB,
	economyRepo *economy.Repository,
	matchID string,
	mode string,
	mapID string,
	stats map[string]*PlayerStats,
	playerItems map[string][]string,
) (map[string]RewardResult, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Consume item reservations for all players inside this transaction
	for playerID, items := range playerItems {
		for _, itemID := range items {
			// Get reservation
			var reservationID string
			var quantity int
			querySelect := `
				SELECT reservation_id, quantity
				FROM inventory_reservations
				WHERE player_id = $1 AND match_id = $2 AND item_id = $3 AND status = 'reserved'
				LIMIT 1
			`
			err := tx.QueryRow(ctx, querySelect, playerID, matchID, itemID).Scan(&reservationID, &quantity)
			if err != nil {
				// No reservation found — skip silently (item may not have been reserved)
				continue
			}

			// Deduct from inventory
			_, err = tx.Exec(ctx,
				`UPDATE inventory_items SET quantity = quantity - $1 WHERE player_id = $2 AND item_id = $3`,
				quantity, playerID, itemID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to deduct item %s for player %s: %w", itemID, playerID, err)
			}

			// Mark reservation as consumed
			_, err = tx.Exec(ctx,
				`UPDATE inventory_reservations SET status = 'consumed', updated_at = CURRENT_TIMESTAMP WHERE reservation_id = $1`,
				reservationID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to mark reservation consumed for item %s player %s: %w", itemID, playerID, err)
			}
		}
	}

	results := make(map[string]RewardResult)

	for _, p := range stats {
		// 1. Calculate experience
		// baseExp = 50, winBonusExp = 30
		// damageBonusExp = totalDamage * 0.05
		// killBonusExp = killCount * 10
		expGained := 50
		if p.IsWinner {
			expGained += 30
		}
		expGained += int(math.Round(float64(p.Damage) * 0.05))
		expGained += p.Kills * 10

		// 2. Calculate coins
		// baseCoin = 30, winBonusCoin = 20
		coinGained := 30
		if p.IsWinner {
			coinGained += 20
		}

		// 3. Update player profile: add exp and coins
		levelUp, newLevel, err := updateProfilePostMatch(ctx, tx, economyRepo, p.PlayerID, expGained, coinGained)
		if err != nil {
			return nil, fmt.Errorf("failed to update player profile: %w", err)
		}

		// 4. Update player rank rating (Mainly for PvP 1v1 as spec requests)
		ratingChange := 0
		newRating := 1000
		newTier := "silver"
		newDivision := 3

		if mode == "pvp_1v1" {
			ratingChange = -20
			if p.IsWinner {
				ratingChange = 25
			} else if p.IsDraw {
				ratingChange = 0
			}

			newRating, newTier, newDivision, err = updatePlayerRank(ctx, tx, p.PlayerID, ratingChange, p.IsWinner, p.IsDraw)
			if err != nil {
				return nil, fmt.Errorf("failed to update rank rating: %w", err)
			}
		}

		// 5. Insert match history record
		resStr := "loss"
		if p.IsWinner {
			resStr = "win"
		} else if p.IsDraw {
			resStr = "draw"
		}

		historyQuery := `
			INSERT INTO match_histories (match_id, player_id, mode, map_id, result, damage, kills, accuracy, exp_gained, coin_gained)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`
		_, err = tx.Exec(
			ctx,
			historyQuery,
			matchID,
			p.PlayerID,
			mode,
			mapID,
			resStr,
			p.Damage,
			p.Kills,
			p.Accuracy,
			expGained,
			coinGained,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to write match history: %w", err)
		}

		results[p.PlayerID] = RewardResult{
			PlayerID:     p.PlayerID,
			ExpGained:    expGained,
			CoinGained:   coinGained,
			RatingChange: ratingChange,
			NewRating:    newRating,
			NewTier:      newTier,
			NewDivision:  newDivision,
			LevelUp:      levelUp,
			NewLevel:     newLevel,
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit match rewards transaction: %w", err)
	}

	return results, nil
}

func updateProfilePostMatch(ctx context.Context, tx pgx.Tx, economyRepo *economy.Repository, playerID string, expGained, coinGained int) (bool, int, error) {
	// Query current level, exp
	var level, exp int
	query := "SELECT level, exp FROM player_profiles WHERE player_id = $1 FOR UPDATE"
	err := tx.QueryRow(ctx, query, playerID).Scan(&level, &exp)
	if err != nil {
		return false, 0, err
	}

	// Calculate exp curve: level N requires N * 100 exp to level up
	nextExp := exp + expGained
	levelUp := false
	for nextExp >= level*100 {
		nextExp -= level * 100
		level++
		levelUp = true
	}

	// Update profiles table
	updateQuery := "UPDATE player_profiles SET level = $1, exp = $2 WHERE player_id = $3"
	_, err = tx.Exec(ctx, updateQuery, level, nextExp, playerID)
	if err != nil {
		return false, 0, err
	}

	// Credit Coins using economy transaction ledger
	if coinGained > 0 {
		_, err = economyRepo.CreditTx(ctx, tx, playerID, "coin", coinGained, "match_reward", "")
		if err != nil {
			return false, 0, err
		}
	}

	return levelUp, level, nil
}

func updatePlayerRank(ctx context.Context, tx pgx.Tx, playerID string, ratingChange int, isWin, isDraw bool) (int, string, int, error) {
	// Fetch active rank season (status = 'active')
	var seasonID string
	err := tx.QueryRow(ctx, "SELECT season_id FROM rank_seasons WHERE status = 'active' LIMIT 1").Scan(&seasonID)
	if err != nil {
		// Fallback to default season for local test if none configured
		seasonID = "season_1"
	}

	// Find or initialize player rank config
	var rating, wins, losses, draws, winStreak int
	var highestTier string
	querySelect := `
		SELECT rating, wins, losses, draws, win_streak, highest_tier
		FROM player_ranks
		WHERE player_id = $1 AND season_id = $2
		FOR UPDATE
	`
	err = tx.QueryRow(ctx, querySelect, playerID, seasonID).Scan(&rating, &wins, &losses, &draws, &winStreak, &highestTier)
	
	isNewRank := false
	if err != nil {
		if err == pgx.ErrNoRows {
			rating = 1000
			wins = 0
			losses = 0
			draws = 0
			winStreak = 0
			highestTier = "silver"
			isNewRank = true
		} else {
			return 0, "", 0, err
		}
	}

	// Calculate wins/losses count
	if isWin {
		wins++
		winStreak++
	} else if isDraw {
		draws++
		winStreak = 0
	} else {
		losses++
		winStreak = 0
	}

	// Update rating (never go below 0)
	rating = rating + ratingChange
	if rating < 0 {
		rating = 0
	}

	// Map tier and division
	tier, division := getTierAndDivision(rating)

	// Keep track of highest tier
	if compareTiers(tier, highestTier) > 0 {
		highestTier = tier
	}

	if isNewRank {
		queryInsert := `
			INSERT INTO player_ranks (player_id, season_id, rating, tier, division, wins, losses, draws, win_streak, highest_tier)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`
		_, err = tx.Exec(ctx, queryInsert, playerID, seasonID, rating, tier, division, wins, losses, draws, winStreak, highestTier)
	} else {
		queryUpdate := `
			UPDATE player_ranks
			SET rating = $1, tier = $2, division = $3, wins = $4, losses = $5, draws = $6, win_streak = $7, highest_tier = $8, updated_at = CURRENT_TIMESTAMP
			WHERE player_id = $9 AND season_id = $10
		`
		_, err = tx.Exec(ctx, queryUpdate, rating, tier, division, wins, losses, draws, winStreak, highestTier, playerID, seasonID)
	}

	return rating, tier, division, err
}

func getTierAndDivision(rating int) (string, int) {
	// Bronze: 0-999
	// Silver: 1000-1199
	// Gold: 1200-1499
	// Platinum: 1500-1799
	// Diamond: 1800-2199
	// Master: 2200+
	switch {
	case rating < 1000:
		div := 3 - (rating / 333)
		if div < 1 {
			div = 1
		}
		return "bronze", div
	case rating < 1200:
		div := 3 - ((rating - 1000) / 66)
		if div < 1 {
			div = 1
		}
		return "silver", div
	case rating < 1500:
		div := 3 - ((rating - 1200) / 100)
		if div < 1 {
			div = 1
		}
		return "gold", div
	case rating < 1800:
		div := 3 - ((rating - 1500) / 100)
		if div < 1 {
			div = 1
		}
		return "platinum", div
	case rating < 2200:
		div := 3 - ((rating - 1800) / 133)
		if div < 1 {
			div = 1
		}
		return "diamond", div
	default:
		return "master", 1
	}
}

func compareTiers(t1, t2 string) int {
	order := map[string]int{
		"bronze":   1,
		"silver":   2,
		"gold":     3,
		"platinum": 4,
		"diamond":  5,
		"master":   6,
	}
	return order[t1] - order[t2]
}
