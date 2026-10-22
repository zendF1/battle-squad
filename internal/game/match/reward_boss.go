package match

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"time"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/milestone"
	"battle-squad/internal/api/mission"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

// Boss rewards (processBossRewards). Per human of the party who did not leave: the effective
// damage = damage to the boss + rewards.minionDamageFactor × damage to minions picks gold
// (coin), EXP and drop rolls from a reward table (gamedata.BossRewardTable), plus the table's
// win bonus when the boss died. A player's first daily_bonus_runs runs of the day against
// this boss use the bonus table, later ones the normal table: the runs are counted per
// (player, boss, UTC+7 day as the daily missions count days, mission.PeriodsAt) in
// player_boss_runs. A player who left gets nothing and the run does not count. Bots get
// nothing and write nothing. Items go to inventory_items (source boss_reward), coins to the
// economy ledger (source boss_reward, ref the boss id).

// BossRewardSource is the economy / inventory source of boss rewards.
const BossRewardSource = "boss_reward"

// BossRewardItem is an item drop of MatchEnded.rewards (config_items id, quantity).
type BossRewardItem struct {
	ItemID   string `json:"itemId"`
	Quantity int    `json:"quantity"`
}

// BossReward is the boss part of a MatchEnded.rewards entry: the player's damage to the boss
// and to minions, whether this run used the bonus table, the bonus runs left today for this
// boss (after this run) and the items dropped.
type BossReward struct {
	BossDamage    int              `json:"bossDamage"`
	MinionDamage  int              `json:"minionDamage"`
	BonusRun      bool             `json:"bonusRun"`
	BonusRunsLeft int              `json:"bonusRunsLeft"`
	Items         []BossRewardItem `json:"items"`
}

// BossEffectiveDamage is the damage the rewards count: boss damage + factor × minion damage.
func BossEffectiveDamage(bossDamage, minionDamage int, factor float64) float64 {
	return float64(bossDamage) + factor*float64(minionDamage)
}

// BossRewardAmounts is what table t gives for effective damage (win: the boss died): gold, EXP
// and the item drops (merged by item, in table order), rolled with rng.
func BossRewardAmounts(t gamedata.BossRewardTable, effective float64, win bool, rng *rand.Rand) (gold, exp int, items []BossRewardItem) {
	effective = math.Max(effective, 0)
	gold = min(t.BaseGold+int(math.Round(effective/1000*t.GoldPer1000Damage)), t.MaxGold)
	exp = min(t.BaseExp+int(math.Round(effective/1000*t.ExpPer1000Damage)), t.MaxExp)
	rolls := 0
	if t.DamagePerDropRoll > 0 {
		rolls = min(int(effective)/t.DamagePerDropRoll, t.MaxDropRolls)
	}
	if win {
		gold += t.WinGold
		exp += t.WinExp
		rolls += t.WinDropRolls
	}
	total := t.EmptyWeight
	for _, d := range t.Drops {
		total += d.Weight
	}
	got := map[string]int{}
	for r := 0; r < rolls && total > 0; r++ {
		roll := rng.Float64() * total
		for _, d := range t.Drops {
			if roll < d.Weight {
				got[d.ItemID] += d.Min + rng.Intn(d.Max-d.Min+1)
				break
			}
			roll -= d.Weight
		}
	}
	items = []BossRewardItem{}
	for _, d := range t.Drops {
		if q := got[d.ItemID]; q > 0 {
			items = append(items, BossRewardItem{ItemID: d.ItemID, Quantity: q})
			delete(got, d.ItemID)
		}
	}
	return gold, exp, items
}

// countBossRunTx counts one more run of the player against bossID on day (the row locked FOR
// UPDATE, created on the first run) and returns the runs of that day so far, this one included.
func countBossRunTx(ctx context.Context, tx pgx.Tx, playerID, bossID, day string) (int, error) {
	var runs int
	err := tx.QueryRow(ctx, `SELECT runs FROM player_boss_runs WHERE player_id = $1 AND boss_id = $2 AND day = $3 FOR UPDATE`,
		playerID, bossID, day).Scan(&runs)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		tag, err := tx.Exec(ctx, `INSERT INTO player_boss_runs (player_id, boss_id, day, runs) VALUES ($1, $2, $3, 1)
			ON CONFLICT (player_id, boss_id, day) DO NOTHING`, playerID, bossID, day)
		if err != nil {
			return 0, err
		}
		if tag.RowsAffected() != 1 {
			return 0, fmt.Errorf("boss run row of %s / %s / %s appeared meanwhile", playerID, bossID, day)
		}
		return 1, nil
	case err != nil:
		return 0, err
	}
	tag, err := tx.Exec(ctx, `UPDATE player_boss_runs SET runs = runs + 1, updated_at = CURRENT_TIMESTAMP
		WHERE player_id = $1 AND boss_id = $2 AND day = $3`, playerID, bossID, day)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() != 1 {
		return 0, fmt.Errorf("boss run row of %s / %s / %s vanished", playerID, bossID, day)
	}
	return runs + 1, nil
}

// bossRunsTx is the player's runs against bossID on day (0 without a row).
func bossRunsTx(ctx context.Context, tx pgx.Tx, playerID, bossID, day string) (int, error) {
	var runs int
	err := tx.QueryRow(ctx, `SELECT runs FROM player_boss_runs WHERE player_id = $1 AND boss_id = $2 AND day = $3`,
		playerID, bossID, day).Scan(&runs)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return runs, err
}

// processBossRewards settles a boss match (see the top of this file) in one transaction: the
// spent items, each human's run, gold, EXP (account and character), drops, match history
// (mode boss:<bossId>, rating change 0) and, for a counted match, milestones and daily missions.
func processBossRewards(
	ctx context.Context,
	db *database.PostgresDB,
	economyRepo *economy.Repository,
	matchID string,
	boss gamedata.BossConfig,
	mapID string,
	stats map[string]*PlayerStats,
	usedItems map[string][]string,
	expPerLevel int,
	now time.Time,
	rng *rand.Rand,
) (map[string]RewardResult, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if err := consumeUsedItemsTx(ctx, tx, matchID, usedItems); err != nil {
		return nil, err
	}

	day := mission.PeriodsAt(now).Day
	mode := boss.Queue()
	// Stable order: the drop rolls of a seeded rng do not depend on map iteration
	ids := make([]string, 0, len(stats))
	for id := range stats {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	results := make(map[string]RewardResult)
	for _, id := range ids {
		p := stats[id]
		if p.IsBot || isBotPlayerID(p.PlayerID) {
			continue
		}
		reward := &BossReward{BossDamage: p.BossDamage, MinionDamage: p.MinionDamage, Items: []BossRewardItem{}}
		gold, exp := 0, 0
		if p.Left {
			runs, err := bossRunsTx(ctx, tx, p.PlayerID, boss.BossID, day)
			if err != nil {
				return nil, fmt.Errorf("failed to read boss runs: %w", err)
			}
			reward.BonusRunsLeft = max(boss.DailyBonusRuns-runs, 0)
		} else {
			runs, err := countBossRunTx(ctx, tx, p.PlayerID, boss.BossID, day)
			if err != nil {
				return nil, fmt.Errorf("failed to count boss run: %w", err)
			}
			reward.BonusRun = runs <= boss.DailyBonusRuns
			reward.BonusRunsLeft = max(boss.DailyBonusRuns-runs, 0)
			table := boss.Rewards.Normal
			if reward.BonusRun {
				table = boss.Rewards.Bonus
			}
			effective := BossEffectiveDamage(p.BossDamage, p.MinionDamage, boss.Rewards.MinionDamageFactor)
			gold, exp, reward.Items = BossRewardAmounts(table, effective, p.IsWinner, rng)
		}

		levelUp, newLevel, err := updateProfilePostMatch(ctx, tx, economyRepo, p.PlayerID, exp, gold, expPerLevel, BossRewardSource, boss.BossID)
		if err != nil {
			return nil, fmt.Errorf("failed to update player profile: %w", err)
		}
		for _, it := range reward.Items {
			if err := milestone.GrantRewardItemTx(ctx, tx, p.PlayerID, milestone.RewardItem{Kind: milestone.RewardKindItem, ID: it.ItemID, Qty: it.Quantity},
				BossRewardSource); err != nil {
				return nil, fmt.Errorf("failed to grant boss drop: %w", err)
			}
		}
		result := "loss"
		if p.IsWinner {
			result = "win"
		}
		if err := insertHistoryTx(ctx, tx, matchID, mode, mapID, result, p, exp, gold, 0); err != nil {
			return nil, err
		}
		if err := recordCountsTx(ctx, tx, p); err != nil {
			return nil, err
		}
		charLevelUp, charNewLevel, charLevelsGained := characterExpTx(ctx, tx, p.PlayerID, p.CharacterID, exp)
		results[p.PlayerID] = RewardResult{
			PlayerID: p.PlayerID, ExpGained: exp, CoinGained: gold, LevelUp: levelUp, NewLevel: newLevel,
			CharLevelUp: charLevelUp, CharNewLevel: charNewLevel, CharLevelsGained: charLevelsGained, BossReward: reward,
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit boss rewards transaction: %w", err)
	}
	return results, nil
}

func isBotPlayerID(id string) bool { return len(id) >= 4 && id[:4] == "bot_" }
