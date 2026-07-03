package moderation

import (
	"context"
	"fmt"

	"battle-squad/internal/shared/database"

	"github.com/jackc/pgx/v5"
)

type Repository struct {
	db *database.PostgresDB
}

func NewRepository(db *database.PostgresDB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateReport(ctx context.Context, report *PlayerReport) error {
	query := `
		INSERT INTO player_reports (report_id, reporter_player_id, target_player_id, match_id, category, description, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.db.Pool.Exec(
		ctx,
		query,
		report.ReportID,
		report.ReporterPlayerID,
		report.TargetPlayerID,
		report.MatchID,
		report.Category,
		report.Description,
		report.Status,
	)
	return err
}

func (r *Repository) GetAccountIDByPlayerID(ctx context.Context, playerID string) (string, error) {
	query := "SELECT account_id FROM player_profiles WHERE player_id = $1"
	var accID string
	err := r.db.Pool.QueryRow(ctx, query, playerID).Scan(&accID)
	return accID, err
}

func (r *Repository) GetAccountIDByPlayerIDTx(ctx context.Context, tx pgx.Tx, playerID string) (string, error) {
	query := "SELECT account_id FROM player_profiles WHERE player_id = $1 FOR UPDATE"
	var accID string
	err := tx.QueryRow(ctx, query, playerID).Scan(&accID)
	return accID, err
}

func (r *Repository) BanAccountTx(ctx context.Context, tx pgx.Tx, ban *AccountBan) error {
	// 1. Insert ban record
	queryBan := `
		INSERT INTO account_bans (ban_id, account_id, player_id, reason_code, reason_text, source, ends_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := tx.Exec(
		ctx,
		queryBan,
		ban.BanID,
		ban.AccountID,
		ban.PlayerID,
		ban.ReasonCode,
		ban.ReasonText,
		ban.Source,
		ban.EndsAt,
		ban.Status,
	)
	if err != nil {
		return fmt.Errorf("failed to record ban: %w", err)
	}

	// 2. Set account status to 'banned'
	queryAcc := "UPDATE accounts SET status = 'banned' WHERE account_id = $1"
	_, err = tx.Exec(ctx, queryAcc, ban.AccountID)
	return err
}

func (r *Repository) RevokeBanTx(ctx context.Context, tx pgx.Tx, accountID, playerID string) error {
	// 1. Expire/revoke ban records
	queryBan := `
		UPDATE account_bans
		SET status = 'revoked'
		WHERE player_id = $1 AND status = 'active'
	`
	_, err := tx.Exec(ctx, queryBan, playerID)
	if err != nil {
		return fmt.Errorf("failed to revoke active bans: %w", err)
	}

	// 2. Set account status back to 'active'
	queryAcc := "UPDATE accounts SET status = 'active' WHERE account_id = $1"
	_, err = tx.Exec(ctx, queryAcc, accountID)
	return err
}
