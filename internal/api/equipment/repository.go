package equipment

import (
	"context"
	"encoding/json"
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

// ---------------------------------------------------------------------------
// Player Equipment
// ---------------------------------------------------------------------------

func (r *Repository) GetPlayerEquipment(ctx context.Context, playerID string) ([]PlayerEquipment, error) {
	query := `
		SELECT equipment_id::text, player_id, item_id, slot, category, tier, upgrade_level,
		       gem_slot_1::text, gem_slot_2::text, is_equipped, equipped_on, is_locked, created_at
		FROM player_equipment
		WHERE player_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerEquipment
	for rows.Next() {
		e, err := scanPlayerEquipment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

func (r *Repository) GetPlayerEquipmentByID(ctx context.Context, playerID, equipmentID string) (*PlayerEquipment, error) {
	query := `
		SELECT equipment_id::text, player_id, item_id, slot, category, tier, upgrade_level,
		       gem_slot_1::text, gem_slot_2::text, is_equipped, equipped_on, is_locked, created_at
		FROM player_equipment
		WHERE player_id = $1 AND equipment_id = $2
	`
	row := r.db.Pool.QueryRow(ctx, query, playerID, equipmentID)
	e, err := scanPlayerEquipment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// GetPlayerEquipmentForUpdateTx reads an equipment row and locks it until the
// transaction ends, so concurrent mutations of the same item are serialized.
func (r *Repository) GetPlayerEquipmentForUpdateTx(ctx context.Context, tx pgx.Tx, playerID, equipmentID string) (*PlayerEquipment, error) {
	query := `
		SELECT equipment_id::text, player_id, item_id, slot, category, tier, upgrade_level,
		       gem_slot_1::text, gem_slot_2::text, is_equipped, equipped_on, is_locked, created_at
		FROM player_equipment
		WHERE player_id = $1 AND equipment_id = $2
		FOR UPDATE
	`
	row := tx.QueryRow(ctx, query, playerID, equipmentID)
	e, err := scanPlayerEquipment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

// LockEquipSlotTx serializes equip operations on one (player, character, slot)
// until the transaction ends. A row lock cannot cover an empty slot, so this
// uses a transaction-scoped advisory lock.
func (r *Repository) LockEquipSlotTx(ctx context.Context, tx pgx.Tx, playerID, characterID, slot string) error {
	key := fmt.Sprintf("equip_slot:%s:%s:%s", playerID, characterID, slot)
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key)
	return err
}

func (r *Repository) CreateEquipmentTx(ctx context.Context, tx pgx.Tx, playerID, itemID, slot, category string, tier *string, gemSlots int) (string, error) {
	query := `
		INSERT INTO player_equipment (player_id, item_id, slot, category, tier)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING equipment_id::text
	`
	var id string
	err := tx.QueryRow(ctx, query, playerID, itemID, slot, category, tier).Scan(&id)
	return id, err
}

func (r *Repository) EquipItemTx(ctx context.Context, tx pgx.Tx, equipmentID, characterID string) (int64, error) {
	query := `
		UPDATE player_equipment
		SET is_equipped = TRUE, equipped_on = $2, is_locked = TRUE
		WHERE equipment_id = $1 AND is_equipped = FALSE
	`
	tag, err := tx.Exec(ctx, query, equipmentID, characterID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// LockPlayerTx locks the player's profile until the transaction ends, so
// equipment buys (and their idempotency lookups) run one at a time per player.
// Returns false when the profile does not exist.
func (r *Repository) LockPlayerTx(ctx context.Context, tx pgx.Tx, playerID string) (bool, error) {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// FindEquipmentPurchaseTx returns the buy committed under this key, or nil.
func (r *Repository) FindEquipmentPurchaseTx(ctx context.Context, tx pgx.Tx, playerID, idempotencyKey string) (*equipmentPurchase, error) {
	var p equipmentPurchase
	err := tx.QueryRow(ctx,
		`SELECT item_id, equipment_id::text FROM equipment_purchases WHERE player_id = $1 AND idempotency_key = $2`,
		playerID, idempotencyKey).Scan(&p.ItemID, &p.EquipmentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) CreateEquipmentPurchaseTx(ctx context.Context, tx pgx.Tx, playerID, idempotencyKey, itemID, equipmentID, currency string, amount int) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO equipment_purchases (player_id, idempotency_key, item_id, equipment_id, price_currency, price_amount)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		playerID, idempotencyKey, itemID, equipmentID, currency, amount)
	return err
}

// LockWalletTx locks the player's profile until the transaction ends and
// returns its balances, so stone buys (and their idempotency lookups) run one
// at a time per player. found is false when the profile does not exist.
func (r *Repository) LockWalletTx(ctx context.Context, tx pgx.Tx, playerID string) (coin, gem int, found bool, err error) {
	err = tx.QueryRow(ctx, `SELECT coin, gem FROM player_profiles WHERE player_id = $1 FOR UPDATE`, playerID).Scan(&coin, &gem)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	return coin, gem, err == nil, err
}

// FindStonePurchaseTx returns the stone buy committed under this key, or nil.
func (r *Repository) FindStonePurchaseTx(ctx context.Context, tx pgx.Tx, playerID, idempotencyKey string) (*stonePurchase, error) {
	var p stonePurchase
	err := tx.QueryRow(ctx,
		`SELECT stone_level, quantity, price_currency, price_amount FROM stone_purchases WHERE player_id = $1 AND idempotency_key = $2`,
		playerID, idempotencyKey).Scan(&p.StoneLevel, &p.Quantity, &p.Currency, &p.Amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) CreateStonePurchaseTx(ctx context.Context, tx pgx.Tx, playerID, idempotencyKey string, p stonePurchase) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO stone_purchases (player_id, idempotency_key, stone_level, quantity, price_currency, price_amount)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		playerID, idempotencyKey, p.StoneLevel, p.Quantity, p.Currency, p.Amount)
	return err
}

// StoneQuantityTx is how many stones of a level the player owns (0 for none).
func (r *Repository) StoneQuantityTx(ctx context.Context, tx pgx.Tx, playerID string, stoneLevel int) (int, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT quantity FROM player_stones WHERE player_id = $1 AND stone_level = $2`, playerID, stoneLevel).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

func (r *Repository) UnequipItemTx(ctx context.Context, tx pgx.Tx, equipmentID string) (int64, error) {
	query := `
		UPDATE player_equipment
		SET is_equipped = FALSE, equipped_on = NULL
		WHERE equipment_id = $1 AND is_equipped = TRUE
	`
	tag, err := tx.Exec(ctx, query, equipmentID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) GetEquippedInSlotTx(ctx context.Context, tx pgx.Tx, playerID, characterID, slot string) (*PlayerEquipment, error) {
	query := `
		SELECT equipment_id::text, player_id, item_id, slot, category, tier, upgrade_level,
		       gem_slot_1::text, gem_slot_2::text, is_equipped, equipped_on, is_locked, created_at
		FROM player_equipment
		WHERE player_id = $1 AND equipped_on = $2 AND slot = $3 AND is_equipped = TRUE
		LIMIT 1
		FOR UPDATE
	`
	row := tx.QueryRow(ctx, query, playerID, characterID, slot)
	e, err := scanPlayerEquipment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}

func (r *Repository) UpdateUpgradeLevelTx(ctx context.Context, tx pgx.Tx, equipmentID string, newLevel int) error {
	query := `UPDATE player_equipment SET upgrade_level = $2 WHERE equipment_id = $1`
	_, err := tx.Exec(ctx, query, equipmentID, newLevel)
	return err
}

func (r *Repository) DeleteEquipmentTx(ctx context.Context, tx pgx.Tx, equipmentID string) (int64, error) {
	query := `DELETE FROM player_equipment WHERE equipment_id = $1`
	tag, err := tx.Exec(ctx, query, equipmentID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) SetGemSlotTx(ctx context.Context, tx pgx.Tx, equipmentID string, slotIndex int, gemID *string) error {
	var col string
	switch slotIndex {
	case 1:
		col = "gem_slot_1"
	case 2:
		col = "gem_slot_2"
	default:
		return fmt.Errorf("invalid gem slot index: %d", slotIndex)
	}
	query := fmt.Sprintf(`UPDATE player_equipment SET %s = $2 WHERE equipment_id = $1`, col)
	_, err := tx.Exec(ctx, query, equipmentID, gemID)
	return err
}

// ---------------------------------------------------------------------------
// Upgrade Log
// ---------------------------------------------------------------------------

func (r *Repository) InsertUpgradeLogTx(ctx context.Context, tx pgx.Tx, equipmentID string, fromLevel, toLevel int, stonesUsed []StoneInput, totalPower int, success bool) error {
	stonesJSON, err := json.Marshal(stonesUsed)
	if err != nil {
		return err
	}
	query := `
		INSERT INTO equipment_upgrade_log (equipment_id, from_level, to_level, stones_used, total_power, success)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = tx.Exec(ctx, query, equipmentID, fromLevel, toLevel, stonesJSON, totalPower, success)
	return err
}

type upgradeLogRow struct {
	StonesUsed json.RawMessage
	TotalPower int
}

func (r *Repository) GetUpgradeLogForDismantleTx(ctx context.Context, tx pgx.Tx, equipmentID string, fromLevel int) ([]upgradeLogRow, error) {
	query := `
		SELECT stones_used, total_power
		FROM equipment_upgrade_log
		WHERE equipment_id = $1 AND from_level >= $2
		ORDER BY from_level ASC
	`
	rows, err := tx.Query(ctx, query, equipmentID, fromLevel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []upgradeLogRow
	for rows.Next() {
		var row upgradeLogRow
		if err := rows.Scan(&row.StonesUsed, &row.TotalPower); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Player Gems
// ---------------------------------------------------------------------------

func (r *Repository) GetPlayerGems(ctx context.Context, playerID string) ([]PlayerGem, error) {
	query := `
		SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		FROM player_gems
		WHERE player_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerGem
	for rows.Next() {
		var g PlayerGem
		if err := rows.Scan(&g.GemID, &g.PlayerID, &g.GemType, &g.GemLevel, &g.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

// LockPlayerGemTx locks a gem row until the transaction ends. Returns false if
// the gem does not exist or belongs to another player.
func (r *Repository) LockPlayerGemTx(ctx context.Context, tx pgx.Tx, playerID, gemID string) (bool, error) {
	var id string
	err := tx.QueryRow(ctx,
		`SELECT gem_id::text FROM player_gems WHERE player_id = $1 AND gem_id = $2 FOR UPDATE`,
		playerID, gemID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AnyGemSocketedTx reports whether any of the given gems is set in an
// equipment gem slot. Run it after locking the gems so it sees committed sockets.
func (r *Repository) AnyGemSocketedTx(ctx context.Context, tx pgx.Tx, gemIDs []string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM player_equipment
			WHERE gem_slot_1 = ANY($1::uuid[]) OR gem_slot_2 = ANY($1::uuid[])
		)
	`
	var exists bool
	err := tx.QueryRow(ctx, query, gemIDs).Scan(&exists)
	return exists, err
}

func (r *Repository) CreateGemTx(ctx context.Context, tx pgx.Tx, playerID, gemType string, gemLevel int) (string, error) {
	query := `
		INSERT INTO player_gems (player_id, gem_type, gem_level)
		VALUES ($1, $2, $3)
		RETURNING gem_id::text
	`
	var id string
	err := tx.QueryRow(ctx, query, playerID, gemType, gemLevel).Scan(&id)
	return id, err
}

func (r *Repository) DeleteGemsTx(ctx context.Context, tx pgx.Tx, gemIDs []string) (int64, error) {
	query := `DELETE FROM player_gems WHERE gem_id = ANY($1::uuid[])`
	tag, err := tx.Exec(ctx, query, gemIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// LockUnsocketedGemsTx locks up to limit unsocketed gems of a type+level.
// Gems deleted by a concurrent transaction are skipped once their lock is
// released, so the result may be shorter than limit.
func (r *Repository) LockUnsocketedGemsTx(ctx context.Context, tx pgx.Tx, playerID, gemType string, gemLevel, limit int) ([]string, error) {
	query := `
		SELECT g.gem_id::text
		FROM player_gems g
		WHERE g.player_id = $1
		  AND g.gem_type = $2
		  AND g.gem_level = $3
		  AND NOT EXISTS (
		      SELECT 1 FROM player_equipment pe
		      WHERE pe.gem_slot_1 = g.gem_id OR pe.gem_slot_2 = g.gem_id
		  )
		ORDER BY g.created_at ASC
		LIMIT $4
		FOR UPDATE OF g
	`
	rows, err := tx.Query(ctx, query, playerID, gemType, gemLevel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ---------------------------------------------------------------------------
// Player Stones
// ---------------------------------------------------------------------------

func (r *Repository) GetPlayerStones(ctx context.Context, playerID string) ([]PlayerStone, error) {
	query := `
		SELECT player_id, stone_level, quantity
		FROM player_stones
		WHERE player_id = $1
		ORDER BY stone_level ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerStone
	for rows.Next() {
		var s PlayerStone
		if err := rows.Scan(&s.PlayerID, &s.StoneLevel, &s.Quantity); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *Repository) AddStonesTx(ctx context.Context, tx pgx.Tx, playerID string, stoneLevel, quantity int) error {
	query := `
		INSERT INTO player_stones (player_id, stone_level, quantity)
		VALUES ($1, $2, $3)
		ON CONFLICT (player_id, stone_level) DO UPDATE
		SET quantity = player_stones.quantity + EXCLUDED.quantity
	`
	_, err := tx.Exec(ctx, query, playerID, stoneLevel, quantity)
	return err
}

func (r *Repository) DeductStonesTx(ctx context.Context, tx pgx.Tx, playerID string, stoneLevel, quantity int) error {
	query := `
		UPDATE player_stones
		SET quantity = quantity - $3
		WHERE player_id = $1 AND stone_level = $2 AND quantity >= $3
	`
	tag, err := tx.Exec(ctx, query, playerID, stoneLevel, quantity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insufficient stones at level %d", stoneLevel)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Player Materials
// ---------------------------------------------------------------------------

func (r *Repository) GetPlayerMaterials(ctx context.Context, playerID string) ([]PlayerMaterial, error) {
	query := `
		SELECT player_id, material_id, quantity
		FROM player_materials
		WHERE player_id = $1
		ORDER BY material_id ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlayerMaterial
	for rows.Next() {
		var m PlayerMaterial
		if err := rows.Scan(&m.PlayerID, &m.MaterialID, &m.Quantity); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (r *Repository) AddMaterialsTx(ctx context.Context, tx pgx.Tx, playerID, materialID string, quantity int) error {
	query := `
		INSERT INTO player_materials (player_id, material_id, quantity)
		VALUES ($1, $2, $3)
		ON CONFLICT (player_id, material_id) DO UPDATE
		SET quantity = player_materials.quantity + EXCLUDED.quantity
	`
	_, err := tx.Exec(ctx, query, playerID, materialID, quantity)
	return err
}

func (r *Repository) DeductMaterialsTx(ctx context.Context, tx pgx.Tx, playerID, materialID string, quantity int) error {
	query := `
		UPDATE player_materials
		SET quantity = quantity - $3
		WHERE player_id = $1 AND material_id = $2 AND quantity >= $3
	`
	tag, err := tx.Exec(ctx, query, playerID, materialID, quantity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("insufficient material %s", materialID)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Config Queries
// ---------------------------------------------------------------------------

func (r *Repository) GetEquipmentItemConfig(ctx context.Context, itemID string) (*EquipmentItemConfig, error) {
	return r.getEquipmentItemConfig(ctx, r.db.Pool, itemID)
}

// GetEquipmentItemConfigTx reads an item config through the transaction.
func (r *Repository) GetEquipmentItemConfigTx(ctx context.Context, tx pgx.Tx, itemID string) (*EquipmentItemConfig, error) {
	return r.getEquipmentItemConfig(ctx, tx, itemID)
}

func (r *Repository) getEquipmentItemConfig(ctx context.Context, q rowQuerier, itemID string) (*EquipmentItemConfig, error) {
	query := `
		SELECT item_id, name, slot, category, tier, required_level, character_id,
		       gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		       price_coin, price_gem, is_active
		FROM config_equipment_items
		WHERE item_id = $1
	`
	var c EquipmentItemConfig
	err := q.QueryRow(ctx, query, itemID).Scan(
		&c.ItemID, &c.Name, &c.Slot, &c.Category, &c.Tier, &c.RequiredLevel, &c.CharacterID,
		&c.GemSlots, &c.StatHP, &c.StatDamage, &c.StatDefense, &c.StatCrit, &c.StatMoveEnergy,
		&c.PriceCoin, &c.PriceGem, &c.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// GetAllEquipmentItemConfigs returns every item, normal and crafted, active or
// not (an inactive item is no longer sold but pieces players own still use it).
func (r *Repository) GetAllEquipmentItemConfigs(ctx context.Context) ([]EquipmentItemConfig, error) {
	query := `
		SELECT item_id, name, slot, category, tier, required_level, character_id,
		       gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		       price_coin, price_gem, is_active
		FROM config_equipment_items
		ORDER BY category ASC, slot ASC, required_level ASC, item_id ASC
	`
	return r.queryEquipmentItemConfigs(ctx, query)
}

func (r *Repository) GetShopEquipmentItems(ctx context.Context, characterID string) ([]EquipmentItemConfig, error) {
	query := `
		SELECT item_id, name, slot, category, tier, required_level, character_id,
		       gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		       price_coin, price_gem, is_active
		FROM config_equipment_items
		WHERE is_active = TRUE
		  AND category = 'normal'
		  AND (character_id = $1 OR character_id IS NULL)
		ORDER BY item_id ASC
	`
	return r.queryEquipmentItemConfigs(ctx, query, characterID)
}

func (r *Repository) queryEquipmentItemConfigs(ctx context.Context, query string, args ...interface{}) ([]EquipmentItemConfig, error) {
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []EquipmentItemConfig
	for rows.Next() {
		var c EquipmentItemConfig
		if err := rows.Scan(
			&c.ItemID, &c.Name, &c.Slot, &c.Category, &c.Tier, &c.RequiredLevel, &c.CharacterID,
			&c.GemSlots, &c.StatHP, &c.StatDamage, &c.StatDefense, &c.StatCrit, &c.StatMoveEnergy,
			&c.PriceCoin, &c.PriceGem, &c.IsActive,
		); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) GetUpgradeRate(ctx context.Context, fromLevel int) (*UpgradeRateConfig, error) {
	query := `
		SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to, careful_gem_cost, careful_bonus_percent
		FROM config_upgrade_rates
		WHERE from_level = $1
	`
	var c UpgradeRateConfig
	err := r.db.Pool.QueryRow(ctx, query, fromLevel).Scan(
		&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo, &c.CarefulGemCost, &c.CarefulBonusPercent,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllUpgradeRates(ctx context.Context) ([]UpgradeRateConfig, error) {
	query := `
		SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to, careful_gem_cost, careful_bonus_percent
		FROM config_upgrade_rates
		ORDER BY from_level ASC
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UpgradeRateConfig
	for rows.Next() {
		var c UpgradeRateConfig
		if err := rows.Scan(&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo, &c.CarefulGemCost, &c.CarefulBonusPercent); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) GetStoneConfig(ctx context.Context, stoneLevel int) (*StoneConfig, error) {
	query := `
		SELECT stone_level, power, price_coin, price_gem, source
		FROM config_stones
		WHERE stone_level = $1
	`
	var c StoneConfig
	err := r.db.Pool.QueryRow(ctx, query, stoneLevel).Scan(
		&c.StoneLevel, &c.Power, &c.PriceCoin, &c.PriceGem, &c.Source,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllStoneConfigs(ctx context.Context) ([]StoneConfig, error) {
	query := `
		SELECT stone_level, power, price_coin, price_gem, source
		FROM config_stones
		ORDER BY stone_level ASC
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []StoneConfig
	for rows.Next() {
		var c StoneConfig
		if err := rows.Scan(&c.StoneLevel, &c.Power, &c.PriceCoin, &c.PriceGem, &c.Source); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) GetGemConfig(ctx context.Context, gemType string, gemLevel int) (*GemConfig, error) {
	query := `
		SELECT gem_type, gem_level, stat_value
		FROM config_gems
		WHERE gem_type = $1 AND gem_level = $2
	`
	var c GemConfig
	err := r.db.Pool.QueryRow(ctx, query, gemType, gemLevel).Scan(
		&c.GemType, &c.GemLevel, &c.StatValue,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) GetAllGemConfigs(ctx context.Context) ([]GemConfig, error) {
	query := `
		SELECT gem_type, gem_level, stat_value
		FROM config_gems
		ORDER BY gem_type ASC, gem_level ASC
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []GemConfig
	for rows.Next() {
		var c GemConfig
		if err := rows.Scan(&c.GemType, &c.GemLevel, &c.StatValue); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// GetCharacterLevel returns the character's level and whether the player owns it.
func (r *Repository) GetCharacterLevel(ctx context.Context, playerID, characterID string) (int, bool, error) {
	var level int
	err := r.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(level, 1) FROM player_characters WHERE player_id = $1 AND character_id = $2`,
		playerID, characterID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return level, true, nil
}

func (r *Repository) GetSetBonuses(ctx context.Context, tier string) ([]SetBonusConfig, error) {
	query := `
		SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		FROM config_set_bonuses
		WHERE tier = $1
		ORDER BY pieces_required ASC
	`
	return r.querySetBonuses(ctx, query, tier)
}

func (r *Repository) GetAllSetBonuses(ctx context.Context) ([]SetBonusConfig, error) {
	query := `
		SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		FROM config_set_bonuses
		ORDER BY tier ASC, pieces_required ASC
	`
	return r.querySetBonuses(ctx, query)
}

func (r *Repository) querySetBonuses(ctx context.Context, query string, args ...interface{}) ([]SetBonusConfig, error) {
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []SetBonusConfig
	for rows.Next() {
		var c SetBonusConfig
		if err := rows.Scan(&c.Tier, &c.PiecesRequired, &c.BonusHPPct, &c.BonusDMGPct, &c.BonusDEFPct, &c.BonusCritPct); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) GetCraftingRecipe(ctx context.Context, recipeID string) (*CraftingRecipe, error) {
	query := `
		SELECT recipe_id, result_item_id, materials, is_active
		FROM config_crafting_recipes
		WHERE recipe_id = $1
	`
	var c CraftingRecipe
	var materialsJSON []byte
	err := r.db.Pool.QueryRow(ctx, query, recipeID).Scan(
		&c.RecipeID, &c.ResultItemID, &materialsJSON, &c.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(materialsJSON, &c.Materials); err != nil {
		return nil, fmt.Errorf("unmarshal recipe materials: %w", err)
	}
	return &c, nil
}

func (r *Repository) GetAllCraftingRecipes(ctx context.Context) ([]CraftingRecipe, error) {
	query := `
		SELECT recipe_id, result_item_id, materials, is_active
		FROM config_crafting_recipes
		ORDER BY recipe_id ASC
	`
	rows, err := r.db.Pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []CraftingRecipe
	for rows.Next() {
		var c CraftingRecipe
		var materialsJSON []byte
		if err := rows.Scan(&c.RecipeID, &c.ResultItemID, &materialsJSON, &c.IsActive); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(materialsJSON, &c.Materials); err != nil {
			return nil, fmt.Errorf("unmarshal recipe materials: %w", err)
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) GetActiveMaterials(ctx context.Context) ([]MaterialConfig, error) {
	query := `
		SELECT material_id, name, description, source, price_gem, COALESCE(tier, ''), is_active
		FROM config_materials
		WHERE is_active = TRUE
		ORDER BY material_id ASC
	`
	return r.queryMaterials(ctx, query)
}

func (r *Repository) GetMaterial(ctx context.Context, materialID string) (*MaterialConfig, error) {
	query := `
		SELECT material_id, name, description, source, price_gem, COALESCE(tier, ''), is_active
		FROM config_materials
		WHERE material_id = $1
	`
	var c MaterialConfig
	err := r.db.Pool.QueryRow(ctx, query, materialID).Scan(
		&c.MaterialID, &c.Name, &c.Description, &c.Source, &c.PriceGem, &c.Tier, &c.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *Repository) queryMaterials(ctx context.Context, query string, args ...interface{}) ([]MaterialConfig, error) {
	rows, err := r.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MaterialConfig
	for rows.Next() {
		var c MaterialConfig
		if err := rows.Scan(
			&c.MaterialID, &c.Name, &c.Description, &c.Source, &c.PriceGem, &c.Tier, &c.IsActive,
		); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

// rowQuerier is a pool or a transaction.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type scannable interface {
	Scan(dest ...interface{}) error
}

func scanPlayerEquipment(row scannable) (PlayerEquipment, error) {
	var e PlayerEquipment
	err := row.Scan(
		&e.EquipmentID,
		&e.PlayerID,
		&e.ItemID,
		&e.Slot,
		&e.Category,
		&e.Tier,
		&e.UpgradeLevel,
		&e.GemSlot1,
		&e.GemSlot2,
		&e.IsEquipped,
		&e.EquippedOn,
		&e.IsLocked,
		&e.CreatedAt,
	)
	return e, err
}
