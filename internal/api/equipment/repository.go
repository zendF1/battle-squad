package equipment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"

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
	return result, nil
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

func (r *Repository) EquipItemTx(ctx context.Context, tx pgx.Tx, equipmentID, characterID string) error {
	query := `
		UPDATE player_equipment
		SET is_equipped = TRUE, equipped_on = $2, is_locked = TRUE
		WHERE equipment_id = $1
	`
	_, err := tx.Exec(ctx, query, equipmentID, characterID)
	return err
}

func (r *Repository) UnequipItemTx(ctx context.Context, tx pgx.Tx, equipmentID string) error {
	query := `
		UPDATE player_equipment
		SET is_equipped = FALSE, equipped_on = NULL
		WHERE equipment_id = $1
	`
	_, err := tx.Exec(ctx, query, equipmentID)
	return err
}

func (r *Repository) GetEquippedInSlot(ctx context.Context, playerID, characterID, slot string) (*PlayerEquipment, error) {
	query := `
		SELECT equipment_id::text, player_id, item_id, slot, category, tier, upgrade_level,
		       gem_slot_1::text, gem_slot_2::text, is_equipped, equipped_on, is_locked, created_at
		FROM player_equipment
		WHERE player_id = $1 AND equipped_on = $2 AND slot = $3 AND is_equipped = TRUE
		LIMIT 1
	`
	row := r.db.Pool.QueryRow(ctx, query, playerID, characterID, slot)
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

func (r *Repository) DeleteEquipmentTx(ctx context.Context, tx pgx.Tx, equipmentID string) error {
	query := `DELETE FROM player_equipment WHERE equipment_id = $1`
	_, err := tx.Exec(ctx, query, equipmentID)
	return err
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

func (r *Repository) GetUpgradeLogForDismantle(ctx context.Context, equipmentID string, fromLevel int) ([]upgradeLogRow, error) {
	query := `
		SELECT stones_used, total_power
		FROM equipment_upgrade_log
		WHERE equipment_id = $1 AND from_level >= $2 AND success = TRUE
		ORDER BY from_level ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, equipmentID, fromLevel)
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
	return result, nil
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
	return result, nil
}

func (r *Repository) GetPlayerGemByID(ctx context.Context, playerID, gemID string) (*PlayerGem, error) {
	query := `
		SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		FROM player_gems
		WHERE player_id = $1 AND gem_id = $2
	`
	var g PlayerGem
	err := r.db.Pool.QueryRow(ctx, query, playerID, gemID).Scan(
		&g.GemID, &g.PlayerID, &g.GemType, &g.GemLevel, &g.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &g, nil
}

func (r *Repository) IsGemSocketed(ctx context.Context, gemID string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM player_equipment
			WHERE gem_slot_1 = $1 OR gem_slot_2 = $1
		)
	`
	var exists bool
	err := r.db.Pool.QueryRow(ctx, query, gemID).Scan(&exists)
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

func (r *Repository) DeleteGemsTx(ctx context.Context, tx pgx.Tx, gemIDs []string) error {
	query := `DELETE FROM player_gems WHERE gem_id = ANY($1::uuid[])`
	_, err := tx.Exec(ctx, query, gemIDs)
	return err
}

func (r *Repository) GetPlayerGemsByTypeAndLevel(ctx context.Context, playerID, gemType string, gemLevel int) ([]PlayerGem, error) {
	query := `
		SELECT gem_id::text, player_id, gem_type, gem_level, created_at
		FROM player_gems g
		WHERE g.player_id = $1
		  AND g.gem_type = $2
		  AND g.gem_level = $3
		  AND NOT EXISTS (
		      SELECT 1 FROM player_equipment pe
		      WHERE pe.gem_slot_1 = g.gem_id OR pe.gem_slot_2 = g.gem_id
		  )
		ORDER BY g.created_at ASC
	`
	rows, err := r.db.Pool.Query(ctx, query, playerID, gemType, gemLevel)
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
	return result, nil
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
	return result, nil
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
	return result, nil
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
	query := `
		SELECT item_id, name, slot, category, tier, required_level, character_id,
		       gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		       price_coin, price_gem, is_active
		FROM config_equipment_items
		WHERE item_id = $1
	`
	var c EquipmentItemConfig
	err := r.db.Pool.QueryRow(ctx, query, itemID).Scan(
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

func (r *Repository) GetAllEquipmentItemConfigs(ctx context.Context) ([]EquipmentItemConfig, error) {
	query := `
		SELECT item_id, name, slot, category, tier, required_level, character_id,
		       gem_slots, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy,
		       price_coin, price_gem, is_active
		FROM config_equipment_items
		ORDER BY item_id ASC
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
	return result, nil
}

func (r *Repository) GetUpgradeRate(ctx context.Context, fromLevel int) (*UpgradeRateConfig, error) {
	query := `
		SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
		FROM config_upgrade_rates
		WHERE from_level = $1
	`
	var c UpgradeRateConfig
	err := r.db.Pool.QueryRow(ctx, query, fromLevel).Scan(
		&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo,
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
		SELECT from_level, to_level, upgrade_cost, max_percent, fail_reset_to
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
		if err := rows.Scan(&c.FromLevel, &c.ToLevel, &c.UpgradeCost, &c.MaxPercent, &c.FailResetTo); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
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
	return result, nil
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
	return result, nil
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
	return result, nil
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
	return result, nil
}

func (r *Repository) GetAllMaterials(ctx context.Context) ([]MaterialConfig, error) {
	query := `
		SELECT material_id, name, description, source, price_gem, COALESCE(tier, ''), is_active
		FROM config_materials
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
	return result, nil
}

// ---------------------------------------------------------------------------
// Combat Stats Query
// ---------------------------------------------------------------------------

// GetEquipmentStatsForCharacter computes total stats for all equipped items on a character.
// Upgrade multiplier: base * (1 + level*0.02 + milestoneBonuses)
// Milestones: level>=6 +0.10, level>=10 +0.20, level>=14 +0.40, level>=16 +1.00
// Gem stats are looked up from config_gems and applied on top of the item base stats.
// Set bonuses are applied when a tier reaches enough pieces_required.
func (r *Repository) GetEquipmentStatsForCharacter(ctx context.Context, playerID, characterID string) (*EquipmentStats, error) {
	// Step 1: fetch all equipped items for this character with their base stats
	itemQuery := `
		SELECT
			pe.equipment_id::text,
			pe.item_id,
			pe.tier,
			pe.upgrade_level,
			pe.gem_slot_1::text,
			pe.gem_slot_2::text,
			ci.stat_hp,
			ci.stat_damage,
			ci.stat_defense,
			ci.stat_crit,
			ci.stat_move_energy
		FROM player_equipment pe
		JOIN config_equipment_items ci ON ci.item_id = pe.item_id
		WHERE pe.player_id = $1
		  AND pe.equipped_on = $2
		  AND pe.is_equipped = TRUE
	`
	rows, err := r.db.Pool.Query(ctx, itemQuery, playerID, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type equippedRow struct {
		equipmentID string
		itemID      string
		tier        *string
		level       int
		gemSlot1    *string
		gemSlot2    *string
		baseHP      int
		baseDMG     int
		baseDEF     int
		baseCrit    float64
		baseMV      int
	}

	var items []equippedRow
	tierCount := map[string]int{}

	for rows.Next() {
		var it equippedRow
		if err := rows.Scan(
			&it.equipmentID, &it.itemID, &it.tier, &it.level,
			&it.gemSlot1, &it.gemSlot2,
			&it.baseHP, &it.baseDMG, &it.baseDEF, &it.baseCrit, &it.baseMV,
		); err != nil {
			return nil, err
		}
		items = append(items, it)
		if it.tier != nil && *it.tier != "" {
			tierCount[*it.tier]++
		}
	}
	rows.Close()

	// Step 2: fetch all gem configs once for lookup
	gemConfigs, err := r.GetAllGemConfigs(ctx)
	if err != nil {
		return nil, err
	}
	type gemKey struct{ gemType string; gemLevel int }
	gemMap := map[gemKey]GemConfig{}
	for _, g := range gemConfigs {
		gemMap[gemKey{g.GemType, g.GemLevel}] = g
	}

	// Fetch gems for this player for slot lookups
	gemRows, err := r.GetPlayerGems(ctx, playerID)
	if err != nil {
		return nil, err
	}
	playerGemMap := map[string]PlayerGem{}
	for _, g := range gemRows {
		playerGemMap[g.GemID] = g
	}

	// Step 3: accumulate base stats with upgrade multiplier + gem stats
	totals := &EquipmentStats{}

	for _, it := range items {
		mult := upgradeMultiplier(it.level)

		totals.HP += int(math.Round(float64(it.baseHP) * mult))
		totals.Damage += int(math.Round(float64(it.baseDMG) * mult))
		totals.Defense += int(math.Round(float64(it.baseDEF) * mult))
		totals.CritPct += it.baseCrit * mult
		totals.MoveEnergy += int(math.Round(float64(it.baseMV) * mult))

		// Gem slots
		for _, slotGemID := range []*string{it.gemSlot1, it.gemSlot2} {
			if slotGemID == nil {
				continue
			}
			pg, ok := playerGemMap[*slotGemID]
			if !ok {
				continue
			}
			gc, ok := gemMap[gemKey{pg.GemType, pg.GemLevel}]
			if !ok {
				continue
			}
			applyGemStat(totals, pg.GemType, gc.StatValue)
		}
	}

	// Step 4: apply set bonuses
	setBonus, err := r.GetAllSetBonuses(ctx)
	if err != nil {
		return nil, err
	}
	// Group by tier, find max applicable bonus
	tierBonuses := map[string][]SetBonusConfig{}
	for _, sb := range setBonus {
		tierBonuses[sb.Tier] = append(tierBonuses[sb.Tier], sb)
	}

	for tier, count := range tierCount {
		bonuses := tierBonuses[tier]
		for _, sb := range bonuses {
			if count >= sb.PiecesRequired {
				totals.HP = int(math.Round(float64(totals.HP) * (1 + sb.BonusHPPct/100)))
				totals.Damage = int(math.Round(float64(totals.Damage) * (1 + sb.BonusDMGPct/100)))
				totals.Defense = int(math.Round(float64(totals.Defense) * (1 + sb.BonusDEFPct/100)))
				totals.CritPct += sb.BonusCritPct
			}
		}
	}

	return totals, nil
}

// upgradeMultiplier returns the stat multiplier for a given upgrade level.
// Formula: 1 + level*0.02 + milestone bonuses
// Milestones: level>=6 +0.10, level>=10 +0.20, level>=14 +0.40, level>=16 +1.00
func upgradeMultiplier(level int) float64 {
	m := 1.0 + float64(level)*0.02
	if level >= 6 {
		m += 0.10
	}
	if level >= 10 {
		m += 0.20
	}
	if level >= 14 {
		m += 0.40
	}
	if level >= 16 {
		m += 1.00
	}
	return m
}

// applyGemStat adds gem stat value to the appropriate EquipmentStats field based on gem type.
func applyGemStat(stats *EquipmentStats, gemType string, value float64) {
	switch gemType {
	case "hp":
		stats.HP += int(math.Round(value))
	case "damage":
		stats.Damage += int(math.Round(value))
	case "defense":
		stats.Defense += int(math.Round(value))
	case "crit":
		stats.CritPct += value
	case "move_energy":
		stats.MoveEnergy += int(math.Round(value))
	}
}

// ---------------------------------------------------------------------------
// Internal scan helpers
// ---------------------------------------------------------------------------

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
