package economy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}

func (r *Repository) CreditTx(ctx context.Context, tx pgx.Tx, playerID, currency string, amount int, source, refID string) (int, error) {
	if amount <= 0 {
		return 0, errors.New("credit amount must be positive")
	}

	// 1. Get current balance with lock
	var coin, gem int
	querySelect := `SELECT coin, gem FROM player_profiles WHERE player_id = $1 FOR UPDATE`
	err := tx.QueryRow(ctx, querySelect, playerID).Scan(&coin, &gem)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch profile for locking: %w", err)
	}

	var newBalance int
	var queryUpdate string

	if currency == "coin" {
		newBalance = coin + amount
		queryUpdate = `UPDATE player_profiles SET coin = $1 WHERE player_id = $2`
	} else if currency == "gem" {
		newBalance = gem + amount
		queryUpdate = `UPDATE player_profiles SET gem = $1 WHERE player_id = $2`
	} else {
		return 0, fmt.Errorf("unsupported currency: %s", currency)
	}

	// 2. Update balance
	_, err = tx.Exec(ctx, queryUpdate, newBalance, playerID)
	if err != nil {
		return 0, fmt.Errorf("failed to update player balance: %w", err)
	}

	// 3. Record transaction log
	txID := generateID()
	queryLog := `
		INSERT INTO economy_transactions (transaction_id, player_id, currency, amount, balance_after, source, ref_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.Exec(ctx, queryLog, txID, playerID, currency, amount, newBalance, source, refID)
	if err != nil {
		return 0, fmt.Errorf("failed to record economy log: %w", err)
	}

	return newBalance, nil
}

func (r *Repository) DebitTx(ctx context.Context, tx pgx.Tx, playerID, currency string, amount int, source, refID string, allowNegative bool) (int, error) {
	if amount <= 0 {
		return 0, errors.New("debit amount must be positive")
	}

	// 1. Get current balance with lock
	var coin, gem int
	querySelect := `SELECT coin, gem FROM player_profiles WHERE player_id = $1 FOR UPDATE`
	err := tx.QueryRow(ctx, querySelect, playerID).Scan(&coin, &gem)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch profile for locking: %w", err)
	}

	var newBalance int
	var queryUpdate string

	if currency == "coin" {
		newBalance = coin - amount
		if newBalance < 0 && !allowNegative {
			return coin, errors.New("insufficient coin balance")
		}
		queryUpdate = `UPDATE player_profiles SET coin = $1 WHERE player_id = $2`
	} else if currency == "gem" {
		newBalance = gem - amount
		if newBalance < 0 && !allowNegative {
			return gem, errors.New("insufficient gem balance")
		}
		queryUpdate = `UPDATE player_profiles SET gem = $1 WHERE player_id = $2`
	} else {
		return 0, fmt.Errorf("unsupported currency: %s", currency)
	}

	// 2. Update balance
	_, err = tx.Exec(ctx, queryUpdate, newBalance, playerID)
	if err != nil {
		return 0, fmt.Errorf("failed to update player balance: %w", err)
	}

	// 3. Record transaction log
	txID := generateID()
	queryLog := `
		INSERT INTO economy_transactions (transaction_id, player_id, currency, amount, balance_after, source, ref_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.Exec(ctx, queryLog, txID, playerID, currency, -amount, newBalance, source, refID)
	if err != nil {
		return 0, fmt.Errorf("failed to record economy log: %w", err)
	}

	return newBalance, nil
}
