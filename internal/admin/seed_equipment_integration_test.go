package admin

import (
	"context"
	"testing"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// SeedEquipmentConfig and migrations/016 + 019 + 020 must seed the same equipment
// catalog (design required levels, per-character guns, retired generic weapons).
func TestSeedEquipmentConfigMatchesMigration(t *testing.T) {
	ctx := context.Background()
	seeded := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "010_equipment_system.up.sql")
	if err := SeedEquipmentConfig(ctx, seeded); err != nil {
		t.Fatal(err)
	}
	migrated := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "010_equipment_system.up.sql",
		"016_default_equipment_catalog.up.sql", "019_equipment_design_levels.up.sql", "020_gun_weapon_names.up.sql")

	queries := map[string]string{
		"items": `SELECT item_id, name, slot, category, tier, required_level, character_id, gem_slots, stat_hp, stat_damage,
		                 stat_defense, stat_crit, stat_move_energy, price_coin, price_gem, is_active
		          FROM config_equipment_items ORDER BY item_id`,
		"upgrade rates": `SELECT * FROM config_upgrade_rates ORDER BY from_level`,
		"stones":        `SELECT * FROM config_stones ORDER BY stone_level`,
		"gems":          `SELECT * FROM config_gems ORDER BY gem_type, gem_level`,
		"set bonuses":   `SELECT * FROM config_set_bonuses ORDER BY tier, pieces_required`,
		"materials":     `SELECT material_id, name, description, source, price_gem, tier, is_active FROM config_materials ORDER BY material_id`,
		"recipes":       `SELECT recipe_id, result_item_id, materials, is_active FROM config_crafting_recipes ORDER BY recipe_id`,
	}
	for name, q := range queries {
		a, b := dump(t, seeded, q), dump(t, migrated, q)
		if a == "" || a != b {
			t.Errorf("%s differ:\nadmin seed: %s\nmigration:  %s", name, a, b)
		}
	}
}

func dump(t *testing.T, db *database.PostgresDB, query string) string {
	t.Helper()
	var out *string
	if err := db.Pool.QueryRow(context.Background(), `SELECT string_agg(r::text, E'\n' ORDER BY r::text) FROM (`+query+`) r`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	if out == nil {
		return ""
	}
	return *out
}
