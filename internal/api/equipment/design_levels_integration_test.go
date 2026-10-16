package equipment

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

const oldCharacterLevels = `{"levels":[{"level":2,"expRequired":200},{"level":3,"expRequired":400},{"level":4,"expRequired":700},{"level":5,"expRequired":1100},{"level":6,"expRequired":1600},{"level":7,"expRequired":2200},{"level":8,"expRequired":3000},{"level":9,"expRequired":4000},{"level":10,"expRequired":5200}]}`

func runMigration(t *testing.T, db *database.PostgresDB, file string) {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
}

// tableDump is every config_equipment_items row and the level table, without
// updated_at: equal dumps mean a rerun changed nothing.
func tableDump(t *testing.T, db *database.PostgresDB) string {
	t.Helper()
	var out string
	if err := db.Pool.QueryRow(context.Background(), `
		SELECT (SELECT string_agg(r::text, E'\n' ORDER BY r.item_id) FROM (
		            SELECT item_id, name, slot, category, tier, required_level, character_id, gem_slots, stat_hp, stat_damage,
		                   stat_defense, stat_crit, stat_move_energy, price_coin, price_gem, is_active
		            FROM config_equipment_items) r)
		       || E'\n' || (SELECT value FROM game_settings WHERE key = 'character_levels')`).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func characterLevels(t *testing.T, db *database.PostgresDB) map[int]int {
	t.Helper()
	var raw string
	if err := db.Pool.QueryRow(context.Background(), `SELECT value FROM game_settings WHERE key = 'character_levels'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	cfg := character.LoadLevelsConfig(raw)
	out := make(map[int]int, len(cfg.Levels))
	for _, l := range cfg.Levels {
		out[l.Level] = l.ExpRequired
	}
	return out
}

// 019 restores the 1/4/7/10 and 2/5/8/10 remap of an earlier 016 slot by slot,
// only where the whole slot still holds the remapped defaults, retires the
// unchanged generic weapons once, adds the per-character weapons, extends the
// level table, and a rerun (016 + 019, as cmd/migrate does) changes nothing.
func TestDesignLevelsMigrationRestoresRemapAndIsIdempotent(t *testing.T) {
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", catalogMigration)
	ctx := context.Background()

	// The state the old 016 left, plus admin edits: armor lv20 and crafted
	// gloves titan levels, weapon lv30 price.
	if _, err := db.Pool.Exec(ctx, `
		UPDATE config_equipment_items SET required_level = CASE
			WHEN item_id LIKE 'normal_%_lv10' THEN 1 WHEN item_id LIKE 'normal_%_lv20' THEN 4
			WHEN item_id LIKE 'normal_%_lv30' THEN 7 WHEN item_id LIKE 'normal_%_lv40' THEN 10
			WHEN tier = 'silver' THEN 2 WHEN tier = 'gold' THEN 5 WHEN tier = 'titan' THEN 8 ELSE 10 END;
		UPDATE config_equipment_items SET required_level = 5 WHERE item_id = 'normal_armor_lv20';
		UPDATE config_equipment_items SET required_level = 9 WHERE item_id = 'crafted_gloves_titan';
		UPDATE config_equipment_items SET price_coin = 4100 WHERE item_id = 'normal_weapon_lv30'`); err != nil {
		t.Fatal(err)
	}
	runMigration(t, db, designLevelsMigration)

	levels := map[string]int{
		"normal_weapon_lv10": 10, "normal_weapon_lv40": 40, "normal_helmet_lv20": 20, "normal_gloves_lv30": 30,
		"normal_boots_lv40": 40, "normal_pants_lv10": 10,
		// armor was edited: the whole slot is left as it is
		"normal_armor_lv10": 1, "normal_armor_lv20": 5, "normal_armor_lv30": 7, "normal_armor_lv40": 10,
		"crafted_weapon_silver": 15, "crafted_armor_gold": 35, "crafted_boots_titan": 55, "crafted_helmet_diamond": 75,
		// crafted gloves were edited
		"crafted_gloves_silver": 2, "crafted_gloves_gold": 5, "crafted_gloves_titan": 9, "crafted_gloves_diamond": 10,
	}
	for id, want := range levels {
		if got := countRows(t, db, `SELECT required_level FROM config_equipment_items WHERE item_id = $1`, id); got != want {
			t.Errorf("%s required_level %d, want %d", id, got, want)
		}
	}

	// Generic weapons retired unless edited; other normal slots stay sold
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_equipment_items WHERE item_id LIKE 'normal_weapon_%' AND NOT is_active`); got != 3 {
		t.Errorf("%d generic weapons inactive, want 3 (lv30 was edited)", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_equipment_items WHERE category = 'normal' AND slot <> 'weapon' AND is_active AND character_id IS NULL`); got != 20 {
		t.Errorf("%d shared non-weapon items active, want 20", got)
	}
	// 16 per-character weapons with the generic tier's stats and price
	if got := countRows(t, db, `
		SELECT COUNT(*) FROM config_equipment_items w
		JOIN (VALUES (10, 8, 0, 500), (20, 15, 1, 1500), (30, 25, 2, 4000), (40, 38, 3, 10000)) AS g(lv, dmg, crit, price)
		  ON w.item_id = w.character_id || '_weapon_lv' || g.lv
		WHERE w.character_id IN ('rookie', 'tanko', 'spark', 'flora') AND w.slot = 'weapon' AND w.category = 'normal'
		  AND w.required_level = g.lv AND w.stat_damage = g.dmg AND w.stat_crit = g.crit AND w.price_coin = g.price
		  AND w.gem_slots = 1 AND w.is_active`); got != 16 {
		t.Errorf("%d per-character weapons, want 16", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_equipment_items WHERE character_id IS NOT NULL AND slot <> 'weapon'`); got != 0 {
		t.Errorf("%d per-character non-weapon items, want 0", got)
	}

	lv := characterLevels(t, db)
	want := map[int]int{2: 200, 3: 400, 4: 700, 5: 1100, 6: 1600, 7: 2200, 8: 3000, 9: 4000, 10: 5200,
		11: 6425, 15: 11575, 20: 18575, 30: 34450, 40: 52825, 55: 85075, 75: 136825, 80: 151325}
	if len(lv) != 79 {
		t.Errorf("%d levels, want 79 (2..80)", len(lv))
	}
	for l, exp := range want {
		if lv[l] != exp {
			t.Errorf("level %d expRequired %d, want %d", l, lv[l], exp)
		}
	}
	for l := 3; l <= 80; l++ {
		if step, prev := lv[l]-lv[l-1], lv[l-1]-lv[l-2]; l > 3 && step < prev {
			t.Errorf("level %d step %d smaller than the previous step %d", l, step, prev)
		}
	}

	// Admin edits after the first run survive reruns: a re-activated generic
	// weapon, a per-character weapon price, a level set back to a remapped value.
	if _, err := db.Pool.Exec(ctx, `
		UPDATE config_equipment_items SET is_active = TRUE WHERE item_id = 'normal_weapon_lv10';
		UPDATE config_equipment_items SET price_coin = 1 WHERE item_id = 'rookie_weapon_lv20';
		UPDATE config_equipment_items SET required_level = 1 WHERE item_id = 'normal_helmet_lv10'`); err != nil {
		t.Fatal(err)
	}
	before := tableDump(t, db)
	for i := 0; i < 2; i++ {
		runMigration(t, db, catalogMigration)
		runMigration(t, db, designLevelsMigration)
	}
	if after := tableDump(t, db); after != before {
		t.Fatalf("rerun changed rows:\nbefore %s\nafter  %s", before, after)
	}
}

// Only the exact 10-level default of migration 006 is replaced.
func TestDesignLevelsMigrationKeepsEditedLevelTable(t *testing.T) {
	for name, value := range map[string]string{
		"custom":    `{"levels":[{"level":2,"expRequired":100},{"level":3,"expRequired":300}]}`,
		"reordered": `{"levels": [{"level": 2, "expRequired": 200}]}`,
	} {
		db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
			"010_equipment_system.up.sql")
		if _, err := db.Pool.Exec(context.Background(), `UPDATE game_settings SET value = $1 WHERE key = 'character_levels'`, value); err != nil {
			t.Fatal(err)
		}
		runMigration(t, db, designLevelsMigration)
		var got string
		if err := db.Pool.QueryRow(context.Background(), `SELECT value FROM game_settings WHERE key = 'character_levels'`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != value {
			t.Errorf("%s: level table overwritten: %s", name, got)
		}
	}

	// The default is replaced, and the result is valid JSON
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql")
	runMigration(t, db, designLevelsMigration)
	var raw string
	if err := db.Pool.QueryRow(context.Background(), `SELECT value FROM game_settings WHERE key = 'character_levels'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var parsed character.CharacterLevelsConfig
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.Levels) != 79 || raw == oldCharacterLevels {
		t.Fatalf("level table %s (%v)", raw, err)
	}
}

// GET /shop/equipment?characterId= sells the shared gear plus that
// character's weapons only; crafted and retired items are not sold.
func TestShopEquipmentPerCharacterWeapons(t *testing.T) {
	svc, _ := newEquipmentService(t, 1)
	ctx := context.Background()

	items, err := svc.GetShopEquipment(ctx, "rookie")
	if err != nil {
		t.Fatal(err)
	}
	weapons := 0
	for _, it := range items {
		if it.Category != "normal" || !it.IsActive {
			t.Fatalf("%s sold: category %s active %v", it.ItemID, it.Category, it.IsActive)
		}
		if it.Slot == "weapon" {
			weapons++
			if it.CharacterID == nil || *it.CharacterID != "rookie" {
				t.Fatalf("weapon %s sold to rookie", it.ItemID)
			}
		} else if it.CharacterID != nil {
			t.Fatalf("%s is per character", it.ItemID)
		}
	}
	if len(items) != 24 || weapons != 4 {
		t.Fatalf("rookie shop %d items, %d weapons; want 24 and 4", len(items), weapons)
	}

	shared, err := svc.GetShopEquipment(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range shared {
		if it.Slot == "weapon" {
			t.Fatalf("weapon %s sold without a character", it.ItemID)
		}
	}
	if len(shared) != 20 {
		t.Fatalf("shared shop %d items, want 20", len(shared))
	}
}

// A generic weapon bought before 019 is inactive but still equips on any
// character and counts in the battle stats.
func TestOwnedRetiredWeaponStillEquips(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	chars := character.NewService(character.NewRepository(db), economy.NewRepository(), db)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	addCharacter(t, db, "p1", "tanko", 10)
	addCharacter(t, db, "p1", "spark", 10)
	var id string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO player_equipment (player_id, item_id, slot, category)
		VALUES ('p1', 'normal_weapon_lv10', 'weapon', 'normal') RETURNING equipment_id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_equipment_items WHERE item_id = 'normal_weapon_lv10' AND NOT is_active`); got != 1 {
		t.Fatal("normal_weapon_lv10 should be inactive")
	}

	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: id, CharacterID: "tanko"}); err != nil {
		t.Fatalf("equip retired weapon: %v", err)
	}
	resp, err := chars.GetCharacters(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range resp.Characters {
		if c.CharacterID == "tanko" && (c.Stats == nil || c.Stats.FromEquipment.DamageBonus != 8) {
			t.Fatalf("tanko stats %+v, want damage bonus 8 from the retired weapon", c.Stats)
		}
	}
	if err := svc.UnequipItem(ctx, "p1", UnequipRequest{EquipmentID: id}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: id, CharacterID: "spark"}); err != nil {
		t.Fatalf("equip retired weapon on another character: %v", err)
	}
}
