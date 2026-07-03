package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

func generateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "fallback-uuid"
	}
	return hex.EncodeToString(bytes)
}

func (r *Repository) FindIdentity(ctx context.Context, provider, providerUserID string) (*AuthIdentity, error) {
	query := `
		SELECT identity_id, account_id, provider, provider_user_id, email_hash, created_at, last_used_at
		FROM auth_identities
		WHERE provider = $1 AND provider_user_id = $2
	`
	var ident AuthIdentity
	err := r.db.Pool.QueryRow(ctx, query, provider, providerUserID).Scan(
		&ident.IdentityID,
		&ident.AccountID,
		&ident.Provider,
		&ident.ProviderUserID,
		&ident.EmailHash,
		&ident.CreatedAt,
		&ident.LastUsedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &ident, nil
}

func (r *Repository) FindAccountByID(ctx context.Context, accountID string) (*Account, error) {
	query := `
		SELECT account_id, account_type, status, role, primary_player_id, created_at, last_login_at, deleted_at
		FROM accounts
		WHERE account_id = $1
	`
	var acc Account
	err := r.db.Pool.QueryRow(ctx, query, accountID).Scan(
		&acc.AccountID,
		&acc.AccountType,
		&acc.Status,
		&acc.Role,
		&acc.PrimaryPlayerID,
		&acc.CreatedAt,
		&acc.LastLoginAt,
		&acc.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &acc, nil
}

func (r *Repository) CreateGuestAccount(ctx context.Context, deviceInstallID string) (*Account, *AuthIdentity, string, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	defer tx.Rollback(ctx)

	accountID := generateID()
	playerID := generateID()
	identityID := generateID()

	// 1. Create account
	accQuery := `
		INSERT INTO accounts (account_id, account_type, status, primary_player_id)
		VALUES ($1, 'guest', 'active', $2)
		RETURNING account_id, account_type, status, primary_player_id, created_at, last_login_at
	`
	var acc Account
	err = tx.QueryRow(ctx, accQuery, accountID, playerID).Scan(
		&acc.AccountID,
		&acc.AccountType,
		&acc.Status,
		&acc.PrimaryPlayerID,
		&acc.CreatedAt,
		&acc.LastLoginAt,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create account: %w", err)
	}

	// 2. Create identity
	identQuery := `
		INSERT INTO auth_identities (identity_id, account_id, provider, provider_user_id)
		VALUES ($1, $2, 'guest', $3)
		RETURNING identity_id, account_id, provider, provider_user_id, email_hash, created_at, last_used_at
	`
	var ident AuthIdentity
	err = tx.QueryRow(ctx, identQuery, identityID, accountID, deviceInstallID).Scan(
		&ident.IdentityID,
		&ident.AccountID,
		&ident.Provider,
		&ident.ProviderUserID,
		&ident.EmailHash,
		&ident.CreatedAt,
		&ident.LastUsedAt,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create identity: %w", err)
	}

	// 3. Create player profile
	displayName := "Rookie_" + deviceInstallID
	if len(displayName) > 20 {
		displayName = displayName[:20]
	}
	profileQuery := `
		INSERT INTO player_profiles (player_id, account_id, display_name)
		VALUES ($1, $2, $3)
	`
	_, err = tx.Exec(ctx, profileQuery, playerID, accountID, displayName)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create player profile: %w", err)
	}

	// Insert default unlocked Rookie character
	_, err = tx.Exec(ctx, "INSERT INTO player_characters (player_id, character_id) VALUES ($1, 'rookie')", playerID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to unlock rookie character: %w", err)
	}

	// 4. Create default player items for Rookie characters (item list from MVP spec)
	// Default items: Medkit, Teleport, Power Shot (MVP spec allows 3 items)
	itemsQuery := `
		INSERT INTO inventory_items (player_id, item_id, quantity, source)
		VALUES 
			($1, 'medkit', 5, 'system_gift'),
			($1, 'teleport', 5, 'system_gift'),
			($1, 'power_shot', 5, 'system_gift')
	`
	_, err = tx.Exec(ctx, itemsQuery, playerID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to grant default inventory items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, "", err
	}

	return &acc, &ident, playerID, nil
}

func (r *Repository) CreateProviderAccount(ctx context.Context, provider, providerUserID, email string) (*Account, *AuthIdentity, string, error) {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	defer tx.Rollback(ctx)

	accountID := generateID()
	playerID := generateID()
	identityID := generateID()

	// 1. Create account
	accQuery := `
		INSERT INTO accounts (account_id, account_type, status, primary_player_id)
		VALUES ($1, $2, 'active', $3)
		RETURNING account_id, account_type, status, primary_player_id, created_at, last_login_at
	`
	var acc Account
	err = tx.QueryRow(ctx, accQuery, accountID, provider, playerID).Scan(
		&acc.AccountID,
		&acc.AccountType,
		&acc.Status,
		&acc.PrimaryPlayerID,
		&acc.CreatedAt,
		&acc.LastLoginAt,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create account: %w", err)
	}

	// 2. Create identity
	identQuery := `
		INSERT INTO auth_identities (identity_id, account_id, provider, provider_user_id, email_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING identity_id, account_id, provider, provider_user_id, email_hash, created_at, last_used_at
	`
	var emailHash *string
	if email != "" {
		hash := hex.EncodeToString([]byte(email))
		emailHash = &hash
	}
	var ident AuthIdentity
	err = tx.QueryRow(ctx, identQuery, identityID, accountID, provider, providerUserID, emailHash).Scan(
		&ident.IdentityID,
		&ident.AccountID,
		&ident.Provider,
		&ident.ProviderUserID,
		&ident.EmailHash,
		&ident.CreatedAt,
		&ident.LastUsedAt,
	)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create identity: %w", err)
	}

	// 3. Create player profile
	displayName := "SquadPlayer"
	profileQuery := `
		INSERT INTO player_profiles (player_id, account_id, display_name)
		VALUES ($1, $2, $3)
	`
	_, err = tx.Exec(ctx, profileQuery, playerID, accountID, displayName)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to create player profile: %w", err)
	}

	// Insert default unlocked Rookie character
	_, err = tx.Exec(ctx, "INSERT INTO player_characters (player_id, character_id) VALUES ($1, 'rookie')", playerID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to unlock rookie character: %w", err)
	}

	// 4. Create default player items
	itemsQuery := `
		INSERT INTO inventory_items (player_id, item_id, quantity, source)
		VALUES 
			($1, 'medkit', 5, 'system_gift'),
			($1, 'teleport', 5, 'system_gift'),
			($1, 'power_shot', 5, 'system_gift')
	`
	_, err = tx.Exec(ctx, itemsQuery, playerID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("failed to grant default inventory items: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, "", err
	}

	return &acc, &ident, playerID, nil
}

func (r *Repository) LinkIdentity(ctx context.Context, accountID, provider, providerUserID, email string) error {
	tx, err := r.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	identityID := generateID()
	var emailHash *string
	if email != "" {
		hash := hex.EncodeToString([]byte(email))
		emailHash = &hash
	}

	// 1. Check if identity already linked to another account
	var existingAccountID string
	err = tx.QueryRow(ctx, "SELECT account_id FROM auth_identities WHERE provider = $1 AND provider_user_id = $2", provider, providerUserID).Scan(&existingAccountID)
	if err == nil {
		return errors.New("provider identity already linked to another account")
	}

	// 2. Add identity
	identQuery := `
		INSERT INTO auth_identities (identity_id, account_id, provider, provider_user_id, email_hash)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err = tx.Exec(ctx, identQuery, identityID, accountID, provider, providerUserID, emailHash)
	if err != nil {
		return err
	}

	// 3. Update account type to 'linked'
	_, err = tx.Exec(ctx, "UPDATE accounts SET account_type = 'linked' WHERE account_id = $1", accountID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *Repository) UpdateLastLogin(ctx context.Context, accountID string) error {
	_, err := r.db.Pool.Exec(ctx, "UPDATE accounts SET last_login_at = CURRENT_TIMESTAMP WHERE account_id = $1", accountID)
	return err
}

func (r *Repository) GetPlayerProfileByAccountID(ctx context.Context, accountID string) (string, string, int, error) {
	query := `
		SELECT player_id, display_name, level
		FROM player_profiles
		WHERE account_id = $1
	`
	var playerID, displayName string
	var level int
	err := r.db.Pool.QueryRow(ctx, query, accountID).Scan(&playerID, &displayName, &level)
	if err != nil {
		return "", "", 0, err
	}
	return playerID, displayName, level, nil
}
