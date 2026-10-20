package milestone

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AddCounterTx adds delta to the player's counter inside tx.
func AddCounterTx(ctx context.Context, tx pgx.Tx, playerID, counter string, delta int) error {
	tag, err := tx.Exec(ctx, `
		INSERT INTO player_stat_counters (player_id, counter, value) VALUES ($1, $2, $3)
		ON CONFLICT (player_id, counter) DO UPDATE
		SET value = player_stat_counters.value + EXCLUDED.value, updated_at = CURRENT_TIMESTAMP`,
		playerID, counter, delta)
	if err != nil {
		return fmt.Errorf("add counter %s: %w", counter, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("add counter %s: %d rows affected", counter, tag.RowsAffected())
	}
	return nil
}

// RaiseCounterTx keeps the player's counter at least value inside tx (peaks never go down).
func RaiseCounterTx(ctx context.Context, tx pgx.Tx, playerID, counter string, value int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO player_stat_counters (player_id, counter, value) VALUES ($1, $2, $3)
		ON CONFLICT (player_id, counter) DO UPDATE
		SET value = GREATEST(player_stat_counters.value, EXCLUDED.value), updated_at = CURRENT_TIMESTAMP
		WHERE player_stat_counters.value < EXCLUDED.value`,
		playerID, counter, value)
	if err != nil {
		return fmt.Errorf("raise counter %s: %w", counter, err)
	}
	return nil
}

// RecordMatchTx counts one match for the player, and a win for them and their character.
// The match end calls it only for a match that counts (see the match package).
func RecordMatchTx(ctx context.Context, tx pgx.Tx, playerID, characterID string, won bool) error {
	if err := AddCounterTx(ctx, tx, playerID, CounterMatches, 1); err != nil {
		return err
	}
	if !won {
		return nil
	}
	if err := AddCounterTx(ctx, tx, playerID, CounterWins, 1); err != nil {
		return err
	}
	if characterID != "" {
		return AddCounterTx(ctx, tx, playerID, CounterCharWinsPrefix+characterID, 1)
	}
	return nil
}

// RecordRankTx raises the player's rank peak (tier order) and rating peak.
func RecordRankTx(ctx context.Context, tx pgx.Tx, playerID, tier string, rating int) error {
	if order := TierOrder(tier); order > 0 {
		if err := RaiseCounterTx(ctx, tx, playerID, CounterRankPeak, order); err != nil {
			return err
		}
	}
	return RaiseCounterTx(ctx, tx, playerID, CounterRatingPeak, rating)
}

// RecordUpgradeTx raises the upgrade peaks after a successful upgrade to level.
func RecordUpgradeTx(ctx context.Context, tx pgx.Tx, playerID, slot string, level int) error {
	if err := RaiseCounterTx(ctx, tx, playerID, CounterUpgradePeak, level); err != nil {
		return err
	}
	if slot == "weapon" {
		return RaiseCounterTx(ctx, tx, playerID, CounterWeaponPeak, level)
	}
	return nil
}
