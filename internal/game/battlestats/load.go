package battlestats

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// Querier is a pgx pool or transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// game_settings keys for the upgrade milestone bonuses.
var milestoneKeys = []string{
	"equipment.milestone_6_bonus",
	"equipment.milestone_10_bonus",
	"equipment.milestone_14_bonus",
	"equipment.milestone_16_bonus",
}

// LoadMilestones reads the milestone bonuses; absent or unparsable keys keep
// their defaults.
func LoadMilestones(ctx context.Context, q Querier) (Milestones, error) {
	ms := DefaultMilestones()
	targets := map[string]*float64{
		milestoneKeys[0]: &ms.Bonus6,
		milestoneKeys[1]: &ms.Bonus10,
		milestoneKeys[2]: &ms.Bonus14,
		milestoneKeys[3]: &ms.Bonus16,
	}
	rows, err := q.Query(ctx, `SELECT key, value FROM game_settings WHERE key = ANY($1)`, milestoneKeys)
	if err != nil {
		return ms, fmt.Errorf("load equipment milestones: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key, val string
		if err := rows.Scan(&key, &val); err != nil {
			return ms, fmt.Errorf("scan equipment milestone: %w", err)
		}
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			*targets[key] = parsed
		}
	}
	return ms, rows.Err()
}

// LoadEquipped returns the player's equipped items grouped by the character
// they are equipped on; characterID "" loads every character.
func LoadEquipped(ctx context.Context, q Querier, playerID, characterID string) (map[string][]EquippedItem, error) {
	// Gems are joined in so no second query runs while rows hold a connection.
	rows, err := q.Query(ctx,
		`SELECT pe.equipped_on, cei.stat_hp, cei.stat_damage, cei.stat_defense, cei.stat_crit, cei.stat_move_energy,
		        pe.upgrade_level, pe.category, COALESCE(pe.tier, ''),
		        g1.gem_type, cg1.stat_value, g2.gem_type, cg2.stat_value
		 FROM player_equipment pe
		 JOIN config_equipment_items cei ON pe.item_id = cei.item_id
		 LEFT JOIN player_gems g1 ON g1.gem_id = pe.gem_slot_1
		 LEFT JOIN config_gems cg1 ON cg1.gem_type = g1.gem_type AND cg1.gem_level = g1.gem_level
		 LEFT JOIN player_gems g2 ON g2.gem_id = pe.gem_slot_2
		 LEFT JOIN config_gems cg2 ON cg2.gem_type = g2.gem_type AND cg2.gem_level = g2.gem_level
		 WHERE pe.player_id = $1 AND pe.is_equipped = TRUE AND pe.equipped_on IS NOT NULL
		   AND ($2 = '' OR pe.equipped_on = $2)`,
		playerID, characterID)
	if err != nil {
		return nil, fmt.Errorf("load equipped items: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]EquippedItem)
	for rows.Next() {
		var on string
		var it EquippedItem
		var gemType1, gemType2 *string
		var gemVal1, gemVal2 *float64
		if err := rows.Scan(&on, &it.HP, &it.Damage, &it.Defense, &it.Crit, &it.MoveEnergy,
			&it.UpgradeLevel, &it.Category, &it.Tier, &gemType1, &gemVal1, &gemType2, &gemVal2); err != nil {
			return nil, fmt.Errorf("scan equipped item: %w", err)
		}
		for _, g := range [2]struct {
			t *string
			v *float64
		}{{gemType1, gemVal1}, {gemType2, gemVal2}} {
			if g.t != nil && g.v != nil {
				it.Gems = append(it.Gems, Gem{Type: *g.t, Value: *g.v})
			}
		}
		out[on] = append(out[on], it)
	}
	return out, rows.Err()
}

// LoadSetBonuses returns config_set_bonuses grouped by tier.
func LoadSetBonuses(ctx context.Context, q Querier) (map[string][]SetBonus, error) {
	rows, err := q.Query(ctx,
		`SELECT tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct
		 FROM config_set_bonuses ORDER BY tier, pieces_required`)
	if err != nil {
		return nil, fmt.Errorf("load set bonuses: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]SetBonus)
	for rows.Next() {
		var tier string
		var b SetBonus
		if err := rows.Scan(&tier, &b.PiecesRequired, &b.HPPct, &b.DMGPct, &b.DEFPct, &b.CritPct); err != nil {
			return nil, fmt.Errorf("scan set bonus: %w", err)
		}
		out[tier] = append(out[tier], b)
	}
	return out, rows.Err()
}

// LoadEquipmentPoints returns the equipment points of each character the
// player has items equipped on (characterID "" = all); Compute converts them.
func LoadEquipmentPoints(ctx context.Context, q Querier, playerID, characterID string) (map[string]EquipmentPoints, error) {
	equipped, err := LoadEquipped(ctx, q, playerID, characterID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]EquipmentPoints, len(equipped))
	if len(equipped) == 0 {
		return out, nil
	}
	setBonuses, err := LoadSetBonuses(ctx, q)
	if err != nil {
		return nil, err
	}
	ms, err := LoadMilestones(ctx, q)
	if err != nil {
		return nil, err
	}
	for charID, items := range equipped {
		out[charID] = SumEquipment(items, setBonuses, ms)
	}
	return out, nil
}
