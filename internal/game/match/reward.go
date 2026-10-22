package match

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/milestone"
	"battle-squad/internal/api/mission"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// damagePerBonusExp is the damage dealt worth 1 bonus EXP. HP is 15× the old
// balance (100 → 1500), so a match deals about 15× the damage: 300 keeps the
// old 1 EXP per 20 damage.
const damagePerBonusExp = 300.0

// releaseReservationsSQL returns every still-reserved item of a match to its owner.
const releaseReservationsSQL = `
	UPDATE inventory_reservations SET status = 'released', updated_at = CURRENT_TIMESTAMP
	WHERE match_id = $1 AND status = 'reserved'
`

// ReleaseMatchReservations returns every still-reserved item of a match to its
// owner. Consumed and released rows are left alone, so repeating it is a no-op.
func ReleaseMatchReservations(ctx context.Context, db *database.PostgresDB, matchID string) error {
	_, err := db.Pool.Exec(ctx, releaseReservationsSQL, matchID)
	return err
}

type PlayerStats struct {
	PlayerID    string
	CharacterID string
	TeamID      int
	Damage      int
	Kills       int
	Accuracy    float64
	ShotsHit    int
	SkillUses   int // skills fired
	ItemsUsed   int // items spent
	IsWinner    bool
	IsDraw      bool
	// Left: the player left the match (Leave or disconnect): never a winner, no coin or
	// EXP, and in ranked the full loss of rating (actual score 0) whatever the team did
	Left bool
	// Counts: the match counts for this player's milestone counters and daily missions
	// (Match.milestoneCounts)
	Counts bool
}

// RankChange is a ranked player's rating before and after the match. Embedded
// as a pointer in RewardResult: nil (unranked modes) leaves the fields out of
// the JSON.
type RankChange struct {
	OldRating   int    `json:"oldRating"`
	OldTier     string `json:"oldTier"`
	OldDivision int    `json:"oldDivision"`
	NewRating   int    `json:"newRating"`
	NewTier     string `json:"newTier"`
	NewDivision int    `json:"newDivision"`
}

// RewardResult is one human's MatchEnded rewards entry. Only ranked_2v2 changes
// rating: there RatingChange and the RankChange fields are set; other modes send
// ratingChange 0 and no rating fields.
type RewardResult struct {
	PlayerID     string `json:"playerId"`
	ExpGained    int    `json:"expGained"`
	CoinGained   int    `json:"coinGained"`
	RatingChange int    `json:"ratingChange"`
	*RankChange
	LevelUp          bool `json:"levelUp"`
	NewLevel         int  `json:"newLevel"`
	CharLevelUp      bool `json:"charLevelUp"`
	CharNewLevel     int  `json:"charNewLevel"`
	CharLevelsGained int  `json:"charLevelsGained"`
}

func ProcessMatchRewards(
	ctx context.Context,
	db *database.PostgresDB,
	economyRepo *economy.Repository,
	matchID string,
	mode string,
	mapID string,
	stats map[string]*PlayerStats,
	usedItems map[string][]string, // playerID → items spent during the match
	teamRatings map[int]int,
	eloParams EloParams,
) (map[string]RewardResult, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Consume reservations of items spent during the match, one per use
	for playerID, items := range usedItems {
		for _, itemID := range items {
			// Get reservation
			var reservationID string
			var quantity int
			querySelect := `
				SELECT reservation_id, quantity
				FROM inventory_reservations
				WHERE player_id = $1 AND match_id = $2 AND item_id = $3 AND status = 'reserved'
				LIMIT 1
				FOR UPDATE
			`
			err := tx.QueryRow(ctx, querySelect, playerID, matchID, itemID).Scan(&reservationID, &quantity)
			if err != nil {
				// No reservation found — skip silently (item may not have been reserved)
				continue
			}

			// Deduct from inventory; never below zero
			tag, err := tx.Exec(ctx,
				`UPDATE inventory_items SET quantity = quantity - $1 WHERE player_id = $2 AND item_id = $3 AND quantity >= $1`,
				quantity, playerID, itemID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to deduct item %s for player %s: %w", itemID, playerID, err)
			}
			if tag.RowsAffected() == 0 {
				// Inventory no longer covers the reservation; it is released below instead
				observability.Log.Error().Str("matchId", matchID).Str("playerId", playerID).Str("itemId", itemID).
					Msg("insufficient inventory to consume reservation")
				continue
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

	// Items brought but not used go back to the players
	if _, err := tx.Exec(ctx, releaseReservationsSQL, matchID); err != nil {
		return nil, fmt.Errorf("failed to release unused reservations: %w", err)
	}

	results := make(map[string]RewardResult)

	for _, p := range stats {
		// Skip bot players — they have no player_profiles record
		if len(p.PlayerID) >= 4 && p.PlayerID[:4] == "bot_" {
			continue
		}

		// 1. Calculate experience
		// baseExp = 50, winBonusExp = 30
		// damageBonusExp = totalDamage / damagePerBonusExp
		// killBonusExp = killCount * 10
		// A player who left gets neither
		expGained := 50
		if p.IsWinner {
			expGained += 30
		}
		expGained += int(math.Round(float64(p.Damage) / damagePerBonusExp))
		expGained += p.Kills * 10

		// 2. Calculate coins
		// baseCoin = 30, winBonusCoin = 20
		coinGained := 30
		if p.IsWinner {
			coinGained += 20
		}
		if p.Left {
			expGained, coinGained = 0, 0
		}

		// 3. Update player profile: add exp and coins
		levelUp, newLevel, err := updateProfilePostMatch(ctx, tx, economyRepo, p.PlayerID, expGained, coinGained)
		if err != nil {
			return nil, fmt.Errorf("failed to update player profile: %w", err)
		}

		// 4. Rating: only ranked games change it (Elo of the team averages; a leaver
		// scores 0)
		ratingChange := 0
		var rank *RankChange
		if mode == RankedMode {
			actualScore := 0.0
			if p.IsWinner {
				actualScore = 1.0
			} else if p.IsDraw {
				actualScore = 0.5
			}

			opponentTeamID := 1
			if p.TeamID == 1 {
				opponentTeamID = 2
			}
			ratingChange = CalculateEloChange(teamRatings[p.TeamID], teamRatings[opponentTeamID], actualScore, eloParams)

			rank, err = updatePlayerRank(ctx, tx, p.PlayerID, ratingChange, p.IsWinner, p.IsDraw, eloParams)
			if err != nil {
				return nil, fmt.Errorf("failed to update rank rating: %w", err)
			}
			ratingChange = rank.NewRating - rank.OldRating // the floor may cut a loss
		}

		// 5. Insert match history record
		resStr := "loss"
		if p.IsWinner {
			resStr = "win"
		} else if p.IsDraw {
			resStr = "draw"
		}

		historyQuery := `
			INSERT INTO match_histories (match_id, player_id, mode, map_id, result, damage, kills, accuracy, exp_gained, coin_gained, rating_change)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
			ratingChange,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to write match history: %w", err)
		}

		if p.Counts {
			if err := milestone.RecordMatchTx(ctx, tx, p.PlayerID, p.CharacterID, p.IsWinner); err != nil {
				return nil, fmt.Errorf("failed to count match for milestones: %w", err)
			}
			if err := mission.RecordMatchTx(ctx, tx, p.PlayerID, mission.MatchStats{
				Won: p.IsWinner, Damage: p.Damage, Kills: p.Kills, ShotsHit: p.ShotsHit,
				SkillUses: p.SkillUses, ItemsUsed: p.ItemsUsed,
			}, time.Now()); err != nil {
				return nil, fmt.Errorf("failed to count match for daily missions: %w", err)
			}
		}

		// Update character EXP and level
		charLevelUp := false
		charNewLevel := 0
		charLevelsGained := 0

		if p.CharacterID != "" {
			var charLevelsVal string
			if err := tx.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'character_levels'`).Scan(&charLevelsVal); err == nil {
				type levelEntry struct {
					Level       int `json:"level"`
					ExpRequired int `json:"expRequired"`
				}
				type levelsConfig struct {
					Levels []levelEntry `json:"levels"`
				}
				var levelsCfg levelsConfig
				if json.Unmarshal([]byte(charLevelsVal), &levelsCfg) == nil && len(levelsCfg.Levels) > 0 {
					pointsPerLevel := 10
					var progressionVal string
					if err := tx.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'character_progression'`).Scan(&progressionVal); err == nil {
						type progConfig struct {
							PointsPerLevel int `json:"pointsPerLevel"`
						}
						var progCfg progConfig
						if json.Unmarshal([]byte(progressionVal), &progCfg) == nil && progCfg.PointsPerLevel > 0 {
							pointsPerLevel = progCfg.PointsPerLevel
						}
					}

					// Get or create character row
					var currentExp, currentLevel int
					err := tx.QueryRow(ctx,
						`SELECT exp, level FROM player_characters WHERE player_id = $1 AND character_id = $2 FOR UPDATE`,
						p.PlayerID, p.CharacterID).Scan(&currentExp, &currentLevel)
					if err != nil {
						// Insert default row if not exists
						tx.Exec(ctx,
							`INSERT INTO player_characters (player_id, character_id, level, exp, stat_points) VALUES ($1, $2, 1, 0, 0) ON CONFLICT DO NOTHING`,
							p.PlayerID, p.CharacterID)
						currentExp = 0
						currentLevel = 1
					}

					newExp := currentExp + expGained
					newLevel := 1
					for _, entry := range levelsCfg.Levels {
						if newExp >= entry.ExpRequired {
							newLevel = entry.Level
						}
					}

					levelsGained := 0
					if newLevel > currentLevel {
						levelsGained = newLevel - currentLevel
					}
					pointsToAdd := levelsGained * pointsPerLevel

					tx.Exec(ctx,
						`UPDATE player_characters SET exp = $3, level = $4, stat_points = stat_points + $5 WHERE player_id = $1 AND character_id = $2`,
						p.PlayerID, p.CharacterID, newExp, newLevel, pointsToAdd)

					if levelsGained > 0 {
						charLevelUp = true
						charNewLevel = newLevel
						charLevelsGained = levelsGained
					}
				}
			}
		}

		results[p.PlayerID] = RewardResult{
			PlayerID:         p.PlayerID,
			ExpGained:        expGained,
			CoinGained:       coinGained,
			RatingChange:     ratingChange,
			RankChange:       rank,
			LevelUp:          levelUp,
			NewLevel:         newLevel,
			CharLevelUp:      charLevelUp,
			CharNewLevel:     charNewLevel,
			CharLevelsGained: charLevelsGained,
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

	// Calculate exp curve: level N requires N * PlayerExpPerLevel exp to level up
	nextExp := exp + expGained
	levelUp := false
	for nextExp >= level*PlayerExpPerLevel {
		nextExp -= level * PlayerExpPerLevel
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

// RankedMode is the only mode that changes rating.
const RankedMode = "ranked_2v2"

// PlayerExpPerLevel: a player at level N needs N × PlayerExpPerLevel EXP to reach N + 1.
const PlayerExpPerLevel = 100

// updatePlayerRank applies a ranked result to the player's row of the active
// season (created, as "Mùa N", when no season is active). A player without a
// row starts at the elo config's default rating; the rating never falls below
// its floor.
func updatePlayerRank(ctx context.Context, tx pgx.Tx, playerID string, ratingChange int, isWin, isDraw bool, params EloParams) (*RankChange, error) {
	season, err := ranked.EnsureActiveSeason(ctx, tx)
	if err != nil {
		return nil, err
	}
	seasonID := season.SeasonID

	var rating, wins, losses, draws, winStreak int
	var highestTier string
	err = tx.QueryRow(ctx, `
		SELECT rating, wins, losses, draws, win_streak, highest_tier
		FROM player_ranks
		WHERE player_id = $1 AND season_id = $2
		FOR UPDATE`, playerID, seasonID).Scan(&rating, &wins, &losses, &draws, &winStreak, &highestTier)

	isNewRank := false
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		rating = params.defaultRating()
		highestTier = ranked.Tier(rating)
		isNewRank = true
	}
	change := &RankChange{OldRating: rating}
	change.OldTier, change.OldDivision = ranked.TierAndDivision(rating)

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

	rating += ratingChange
	if rating < params.RatingFloor {
		rating = params.RatingFloor
	}
	if rating < 0 {
		rating = 0
	}
	tier, division := ranked.TierAndDivision(rating)
	if ranked.CompareTiers(tier, highestTier) > 0 {
		highestTier = tier
	}
	change.NewRating, change.NewTier, change.NewDivision = rating, tier, division

	if isNewRank {
		_, err = tx.Exec(ctx, `
			INSERT INTO player_ranks (player_id, season_id, rating, tier, division, wins, losses, draws, win_streak, highest_tier)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			playerID, seasonID, rating, tier, division, wins, losses, draws, winStreak, highestTier)
	} else {
		var tag pgconn.CommandTag
		tag, err = tx.Exec(ctx, `
			UPDATE player_ranks
			SET rating = $1, tier = $2, division = $3, wins = $4, losses = $5, draws = $6, win_streak = $7, highest_tier = $8, updated_at = CURRENT_TIMESTAMP
			WHERE player_id = $9 AND season_id = $10`,
			rating, tier, division, wins, losses, draws, winStreak, highestTier, playerID, seasonID)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("player_ranks row of %s vanished", playerID)
		}
	}
	if err != nil {
		return nil, err
	}

	// Lifetime peaks for the rank milestones (separate from the season's highest_tier)
	if err := milestone.RecordRankTx(ctx, tx, playerID, highestTier, rating); err != nil {
		return nil, err
	}
	return change, nil
}
