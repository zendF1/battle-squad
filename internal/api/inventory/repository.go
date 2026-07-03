package inventory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

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

func (r *Repository) GetAvailableInventory(ctx context.Context, playerID string) ([]InventoryItem, error) {
	query := `
		SELECT i.item_id, i.quantity - COALESCE(SUM(r.quantity), 0) AS available_quantity, i.source, i.acquired_at, i.expires_at
		FROM inventory_items i
		LEFT JOIN inventory_reservations r ON i.player_id = r.player_id AND i.item_id = r.item_id AND r.status = 'reserved'
		WHERE i.player_id = $1 AND (i.expires_at IS NULL OR i.expires_at > CURRENT_TIMESTAMP)
		GROUP BY i.item_id, i.quantity, i.source, i.acquired_at, i.expires_at
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []InventoryItem
	for rows.Next() {
		var item InventoryItem
		item.PlayerID = playerID
		var qty int
		err := rows.Scan(
			&item.ItemID,
			&qty,
			&item.Source,
			&item.AcquiredAt,
			&item.ExpiresAt,
		)
		if err != nil {
			return nil, err
		}
		item.Quantity = qty
		// Only append if available quantity is > 0
		if item.Quantity > 0 {
			items = append(items, item)
		}
	}

	return items, nil
}

func (r *Repository) AddItemsTx(ctx context.Context, tx pgx.Tx, playerID, itemID string, quantity int, source string, expiresAt *time.Time) error {
	if quantity <= 0 {
		return fmt.Errorf("add quantity must be positive")
	}

	query := `
		INSERT INTO inventory_items (player_id, item_id, quantity, source, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (player_id, item_id) 
		DO UPDATE SET quantity = inventory_items.quantity + EXCLUDED.quantity, source = EXCLUDED.source, expires_at = EXCLUDED.expires_at
	`
	_, err := tx.Exec(ctx, query, playerID, itemID, quantity, source, expiresAt)
	return err
}

func (r *Repository) ReserveItemsTx(ctx context.Context, tx pgx.Tx, playerID, matchID, itemID string, quantity int) error {
	// Check available quantity first
	var availableQty int
	queryCheck := `
		SELECT i.quantity - COALESCE(SUM(r.quantity), 0) AS available_quantity
		FROM inventory_items i
		LEFT JOIN inventory_reservations r ON i.player_id = r.player_id AND i.item_id = r.item_id AND r.status = 'reserved'
		WHERE i.player_id = $1 AND i.item_id = $2
		GROUP BY i.quantity
	`
	err := tx.QueryRow(ctx, queryCheck, playerID, itemID).Scan(&availableQty)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("item %s not found in inventory", itemID)
		}
		return err
	}

	if availableQty < quantity {
		return fmt.Errorf("insufficient available quantity for item %s", itemID)
	}

	reservationID := generateID()
	queryInsert := `
		INSERT INTO inventory_reservations (reservation_id, player_id, match_id, item_id, quantity, status)
		VALUES ($1, $2, $3, $4, $5, 'reserved')
	`
	_, err = tx.Exec(ctx, queryInsert, reservationID, playerID, matchID, itemID, quantity)
	return err
}

func (r *Repository) ConsumeReservationTx(ctx context.Context, tx pgx.Tx, playerID, matchID, itemID string) error {
	// 1. Get reservation quantity and verify status is 'reserved'
	var quantity int
	var resID string
	querySelect := `
		SELECT reservation_id, quantity 
		FROM inventory_reservations 
		WHERE player_id = $1 AND match_id = $2 AND item_id = $3 AND status = 'reserved'
	`
	err := tx.QueryRow(ctx, querySelect, playerID, matchID, itemID).Scan(&resID, &quantity)
	if err != nil {
		return fmt.Errorf("no active reservation found: %w", err)
	}

	// 2. Consume from inventory
	queryUpdateInv := `
		UPDATE inventory_items 
		SET quantity = quantity - $1 
		WHERE player_id = $2 AND item_id = $3
	`
	_, err = tx.Exec(ctx, queryUpdateInv, quantity, playerID, itemID)
	if err != nil {
		return fmt.Errorf("failed to deduct item from inventory: %w", err)
	}

	// 3. Mark reservation as consumed
	queryUpdateRes := `
		UPDATE inventory_reservations 
		SET status = 'consumed', updated_at = CURRENT_TIMESTAMP 
		WHERE reservation_id = $1
	`
	_, err = tx.Exec(ctx, queryUpdateRes, resID)
	return err
}

func (r *Repository) ReleaseReservationTx(ctx context.Context, tx pgx.Tx, playerID, matchID, itemID string) error {
	queryUpdateRes := `
		UPDATE inventory_reservations 
		SET status = 'released', updated_at = CURRENT_TIMESTAMP 
		WHERE player_id = $1 AND match_id = $2 AND item_id = $3 AND status = 'reserved'
	`
	_, err := tx.Exec(ctx, queryUpdateRes, playerID, matchID, itemID)
	return err
}
