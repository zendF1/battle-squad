package room

import (
	"context"
	"fmt"

	"battle-squad/internal/game/gamedata"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MaxLoadoutItems is how many items a player may bring into a match of mode (a queue, a room
// mode; "" quick play): the mode's config_game_modes max_items, else rules.items maxLoadout of
// the current game data (gamedata.Snapshot.ModeMaxItems; 6 for the boss queues since 052).
func MaxLoadoutItems(mode string) int { return gamedata.Current().ModeMaxItems(mode) }

// TrimLoadout is items cut to the first MaxLoadoutItems(mode) (a loadout saved for a mode that
// allows more).
func TrimLoadout(items []string, mode string) []string {
	if limit := MaxLoadoutItems(mode); len(items) > limit {
		return append([]string(nil), items[:max(limit, 0)]...)
	}
	return items
}

// DefaultCharacterID is the character every player owns and plays when none is picked
// (rules.player defaultCharacterId of the current game data).
func DefaultCharacterID() string { return gamedata.Current().Rules.Player.DefaultCharacterID }

// loadoutOwnership reports which characters and items a player owns.
type loadoutOwnership interface {
	ownsCharacter(ctx context.Context, playerID, characterID string) (bool, error)
	// ownedItems returns the subset of items the player holds (quantity > 0, not expired).
	ownedItems(ctx context.Context, playerID string, items []string) (map[string]bool, error)
}

// poolOwnership reads ownership with single pool queries (no transaction).
type poolOwnership struct {
	pool *pgxpool.Pool
}

func (o poolOwnership) ownsCharacter(ctx context.Context, playerID, characterID string) (bool, error) {
	var exists bool
	err := o.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM player_characters WHERE player_id = $1 AND character_id = $2)",
		playerID, characterID).Scan(&exists)
	return exists, err
}

func (o poolOwnership) ownedItems(ctx context.Context, playerID string, items []string) (map[string]bool, error) {
	rows, err := o.pool.Query(ctx, `
		SELECT item_id FROM inventory_items
		WHERE player_id = $1 AND item_id = ANY($2) AND quantity > 0 AND (expires_at IS NULL OR expires_at > NOW())
	`, playerID, items)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	owned := make(map[string]bool, len(items))
	for rows.Next() {
		var itemID string
		if err := rows.Scan(&itemID); err != nil {
			continue
		}
		owned[itemID] = true
	}
	return owned, rows.Err()
}

// validCharacterID reports whether characterID is a playable character (one of the game
// data's characters).
func validCharacterID(characterID string) bool {
	_, ok := gamedata.Current().Data.Characters[characterID]
	return ok
}

// checkCharacterOwned returns a LoadoutError when the player has not unlocked
// characterID. The default character (Rookie) is always available.
func checkCharacterOwned(ctx context.Context, own loadoutOwnership, playerID, characterID string) error {
	if characterID == DefaultCharacterID() {
		return nil
	}
	ok, err := own.ownsCharacter(ctx, playerID, characterID)
	if err != nil {
		return fmt.Errorf("failed to check character %s: %w", characterID, err)
	}
	if !ok {
		return &LoadoutError{Reason: fmt.Sprintf("You do not own character: %s", characterID)}
	}
	return nil
}

// checkItemSelection enforces the item count of mode (MaxLoadoutItems) and the per-match use
// limits.
func checkItemSelection(items []string, mode string) error {
	if limit := MaxLoadoutItems(mode); len(items) > limit {
		return &LoadoutError{Reason: fmt.Sprintf("You can bring at most %d items", limit)}
	}
	if _, err := itemCounts(items); err != nil {
		return &LoadoutError{Reason: err.Error()}
	}
	return nil
}

// checkItemsKnown rejects item ids missing from the game data.
func checkItemsKnown(items []string) error {
	for _, itemID := range items {
		if _, ok := gamedata.Current().Data.Items[itemID]; !ok {
			return &LoadoutError{Reason: fmt.Sprintf("Unknown item: %s", itemID)}
		}
	}
	return nil
}

// checkItemsOwned returns a LoadoutError naming the first selected item the
// player does not hold.
func checkItemsOwned(ctx context.Context, own loadoutOwnership, playerID string, items []string) error {
	if len(items) == 0 {
		return nil
	}
	owned, err := own.ownedItems(ctx, playerID, items)
	if err != nil {
		return fmt.Errorf("failed to query inventory: %w", err)
	}
	for _, itemID := range items {
		if !owned[itemID] {
			return &LoadoutError{Reason: fmt.Sprintf("You do not own item: %s", itemID)}
		}
	}
	return nil
}

// validateQuickPlayLoadout applies the room SelectCharacter / SelectItems
// rules to a QuickPlay loadout. devUnlock skips only the ownership checks.
func validateQuickPlayLoadout(ctx context.Context, own loadoutOwnership, playerID string, p QuickPlayPayload, devUnlock bool) error {
	return validateLoadout(ctx, own, playerID, p.CharacterID, p.Items, "", devUnlock)
}

// validateLoadout checks a loadout for a match of mode: a playable character the player owns,
// at most MaxLoadoutItems(mode) known items within their per-match limits, all held.
func validateLoadout(ctx context.Context, own loadoutOwnership, playerID, characterID string, items []string, mode string, devUnlock bool) error {
	if !validCharacterID(characterID) {
		return &LoadoutError{Reason: fmt.Sprintf("Unknown character: %s", characterID)}
	}
	if err := checkItemsKnown(items); err != nil {
		return err
	}
	if err := checkItemSelection(items, mode); err != nil {
		return err
	}
	if devUnlock {
		return nil
	}
	if err := checkCharacterOwned(ctx, own, playerID, characterID); err != nil {
		return err
	}
	return checkItemsOwned(ctx, own, playerID, items)
}

// ValidateLoadout applies the QuickPlay loadout rules (a playable character the player owns,
// known items within their per-match limits, all held) to a lobby loadout for queue: at most
// MaxLoadoutItems(queue) items (rules.items maxLoadout, 6 for a boss queue). A rule broken is a
// *LoadoutError.
func ValidateLoadout(ctx context.Context, pool *pgxpool.Pool, playerID, characterID, queue string, items []string) error {
	return validateLoadout(ctx, poolOwnership{pool: pool}, playerID, characterID, items, queue, false)
}
