package migrate

import (
	"context"
	"errors"
	"fmt"

	"battle-squad/internal/shared/auth"
)

// AdminBootstrap is the outcome of BootstrapAdmin.
type AdminBootstrap string

const (
	AdminCreated AdminBootstrap = "created" // the first admin was inserted
	AdminExists  AdminBootstrap = "exists"  // an admin was already there: nothing changed
	AdminMissing AdminBootstrap = "missing" // no admin and no ADMIN_BOOTSTRAP_* credentials
)

// BootstrapAdmin creates the first admin dashboard login (admin_users) from email and password
// when the table is empty. An existing admin is never changed (a rerun, or credentials left in
// the environment, cannot reset a password). There is no default password: without both
// values nothing is created.
func BootstrapAdmin(ctx context.Context, q Execer, email, password string) (AdminBootstrap, error) {
	var count int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count); err != nil {
		return "", fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		return AdminExists, nil
	}
	email = auth.NormalizeAdminEmail(email)
	if email == "" && password == "" {
		return AdminMissing, nil
	}
	if email == "" || password == "" {
		return "", errors.New("set both ADMIN_BOOTSTRAP_EMAIL and ADMIN_BOOTSTRAP_PASSWORD")
	}
	hash, err := auth.HashAdminPassword(password)
	if err != nil {
		return "", err
	}
	// The NOT EXISTS keeps it to one admin even if two runs raced past the count
	tag, err := q.Exec(ctx, `INSERT INTO admin_users (email, password_hash)
		SELECT $1, $2 WHERE NOT EXISTS (SELECT 1 FROM admin_users)
		ON CONFLICT (email) DO NOTHING`, email, hash)
	if err != nil {
		return "", fmt.Errorf("insert admin: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return AdminExists, nil
	}
	return AdminCreated, nil
}
