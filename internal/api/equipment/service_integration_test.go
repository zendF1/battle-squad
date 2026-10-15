package equipment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

const catalogMigration = "016_default_equipment_catalog.up.sql"

var equipmentMigrations = []string{
	"001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
	"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql",
	catalogMigration, "017_equipment_purchase_idempotency.up.sql",
}

// newEquipmentService builds the schema with the default catalog. maxConns 1
// makes a pool query made while a tx holds the connection hang the test.
func newEquipmentService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, equipmentMigrations...)
	return NewService(NewRepository(db), economy.NewRepository(), db), db
}

func addCharacter(t *testing.T, db *database.PostgresDB, playerID, characterID string, level int) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO player_characters (player_id, character_id, level) VALUES ($1, $2, $3)`,
		playerID, characterID, level); err != nil {
		t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr model.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err %v, want %s", err, code)
	}
}

func countRows(t *testing.T, db *database.PostgresDB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func ownedEquipment(t *testing.T, db *database.PostgresDB, playerID string) int {
	t.Helper()
	return countRows(t, db, `SELECT COUNT(*) FROM player_equipment WHERE player_id = $1`, playerID)
}

func equipped(t *testing.T, db *database.PostgresDB, equipmentID string) (bool, *string) {
	t.Helper()
	var isEquipped bool
	var on *string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT is_equipped, equipped_on FROM player_equipment WHERE equipment_id = $1`, equipmentID).Scan(&isEquipped, &on); err != nil {
		t.Fatal(err)
	}
	return isEquipped, on
}

// callHandler runs an equipment handler as playerID and returns the status and
// the decoded envelope.
func callHandler(t *testing.T, fn http.HandlerFunc, playerID string, body any) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/", &buf)
	if playerID != "" {
		req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, playerID))
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, env
}

func errorCode(env map[string]any) string {
	e, _ := env["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestCatalogMigrationSeedsAndIsIdempotent(t *testing.T) {
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "010_equipment_system.up.sql")
	ctx := context.Background()

	// Rows seeded by cmd/admin before 016: old required levels, material_id key.
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO config_equipment_items (item_id, name, slot, category, tier, required_level, gem_slots, stat_damage, price_coin) VALUES
			('normal_weapon_lv20', 'Steel Sword', 'weapon', 'normal', NULL, 20, 1, 15, 1500),
			('normal_armor_lv30', 'Mithril Vest', 'armor', 'normal', NULL, 3, 1, 0, 3500),
			('crafted_boots_titan', 'Titan Boots', 'boots', 'crafted', 'titan', 55, 2, 0, 0);
		INSERT INTO config_crafting_recipes (recipe_id, result_item_id, materials) VALUES
			('recipe_boots_titan', 'crafted_boots_titan', '[{"material_id": "titan_ore", "quantity": 30}]')`); err != nil {
		t.Fatal(err)
	}

	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", catalogMigration))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE config_equipment_items SET price_coin = 999 WHERE item_id = 'normal_weapon_lv10'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("rerun: %v", err)
	}

	counts := map[string]int{
		"config_equipment_items WHERE category = 'normal'":  24,
		"config_equipment_items WHERE category = 'crafted'": 24,
		"config_upgrade_rates":                              16,
		"config_stones":                                     12,
		"config_gems":                                       40,
		"config_set_bonuses":                                12,
		"config_materials":                                  20,
		"config_crafting_recipes":                           24,
	}
	for table, want := range counts {
		if got := countRows(t, db, "SELECT COUNT(*) FROM "+table); got != want {
			t.Errorf("%s: %d rows, want %d", table, got, want)
		}
	}

	levels := map[string]int{
		"normal_weapon_lv10":     1,
		"normal_weapon_lv20":     4, // remapped from the old default 20
		"normal_helmet_lv30":     7,
		"normal_gloves_lv40":     10,
		"normal_armor_lv30":      3, // admin-edited level kept
		"crafted_weapon_silver":  2,
		"crafted_armor_gold":     5,
		"crafted_boots_titan":    8, // remapped from 55
		"crafted_gloves_diamond": 10,
	}
	for id, want := range levels {
		if got := countRows(t, db, `SELECT required_level FROM config_equipment_items WHERE item_id = $1`, id); got != want {
			t.Errorf("%s required_level %d, want %d", id, got, want)
		}
	}
	if got := countRows(t, db, `SELECT price_coin FROM config_equipment_items WHERE item_id = 'normal_weapon_lv10'`); got != 999 {
		t.Errorf("rerun overwrote the edited price: %d", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_crafting_recipes, jsonb_array_elements(materials) e WHERE NOT e ? 'materialId' OR e ? 'material_id'`); got != 0 {
		t.Errorf("%d recipe materials without the materialId key", got)
	}
	if got := countRows(t, db, `SELECT (materials->0->>'quantity')::int FROM config_crafting_recipes WHERE recipe_id = 'recipe_weapon_gold'`); got != 30 {
		t.Errorf("gold weapon recipe quantity %d, want 30", got)
	}
	if got := countRows(t, db, `SELECT stat_value::int FROM config_gems WHERE gem_type = 'hp' AND gem_level = 10`); got != 660 {
		t.Errorf("hp gem level 10 value %d, want 660", got)
	}
}

func TestCatalogEndpointReturnsActiveRows(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	h := NewHandler(svc)
	if _, err := db.Pool.Exec(context.Background(), `UPDATE config_equipment_items SET is_active = FALSE WHERE item_id = 'normal_boots_lv40'`); err != nil {
		t.Fatal(err)
	}

	code, env := callHandler(t, h.GetCatalog, "p1", nil)
	if code != http.StatusOK {
		t.Fatalf("status %d body %v", code, env)
	}
	data, _ := env["data"].(map[string]any)
	items, _ := data["items"].([]any)
	rates, _ := data["upgradeRates"].([]any)
	bonuses, _ := data["setBonuses"].([]any)
	stones, _ := data["stones"].([]any)
	if len(items) != 47 || len(rates) != 16 || len(bonuses) != 12 || len(stones) != 12 {
		t.Fatalf("items %d rates %d setBonuses %d stones %d, want 47 16 12 12", len(items), len(rates), len(bonuses), len(stones))
	}
	var crafted int
	for _, it := range items {
		m := it.(map[string]any)
		if m["itemId"] == "normal_boots_lv40" {
			t.Fatal("inactive item served")
		}
		if m["category"] == "crafted" {
			crafted++
		}
		if m["itemId"] == "normal_weapon_lv20" && (m["requiredLevel"] != 4.0 || m["statDamage"] != 15.0 || m["priceCoin"] != 1500.0 || m["name"] != "Steel Sword") {
			t.Fatalf("normal_weapon_lv20 %v", m)
		}
	}
	if crafted != 24 {
		t.Fatalf("crafted items %d, want 24", crafted)
	}
	r0 := rates[0].(map[string]any)
	if r0["fromLevel"] != 0.0 || r0["toLevel"] != 1.0 || r0["maxPercent"] != 80.0 || r0["failResetTo"] != 0.0 || r0["upgradeCost"] != 1.0 {
		t.Fatalf("first upgrade rate %v", r0)
	}
}

func TestBuyEquipment(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 1_000, 0)
	dbtest.CreatePlayer(t, db, "poor", 499, 0)

	// normal_weapon_lv10 costs 500 coin
	code, env := callHandler(t, h.BuyEquipment, "poor", BuyEquipmentRequest{ItemID: "normal_weapon_lv10", IdempotencyKey: "k"})
	if code != http.StatusBadRequest || errorCode(env) != "SHOP_INSUFFICIENT_BALANCE" {
		t.Fatalf("poor: status %d body %v", code, env)
	}
	if coin, _ := dbtest.Balance(t, db, "poor"); coin != 499 || ownedEquipment(t, db, "poor") != 0 {
		t.Fatalf("failed buy charged or granted: coin %d", coin)
	}

	code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: "normal_weapon_lv10", IdempotencyKey: "k1"})
	if code != http.StatusOK {
		t.Fatalf("buy: status %d body %v", code, env)
	}
	first, _ := env["data"].(map[string]any)["equipmentId"].(string)
	if first == "" {
		t.Fatalf("no equipmentId: %v", env)
	}

	// Same key + item: the first equipment back, no second charge
	code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: "normal_weapon_lv10", IdempotencyKey: "k1"})
	if code != http.StatusOK || env["data"].(map[string]any)["equipmentId"] != first {
		t.Fatalf("replay: status %d body %v, want equipmentId %s", code, env, first)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 500 || ownedEquipment(t, db, "p1") != 1 {
		t.Fatalf("replay charged again: coin %d owned %d", coin, ownedEquipment(t, db, "p1"))
	}

	// Same key, other item
	code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: "normal_helmet_lv10", IdempotencyKey: "k1"})
	if code != http.StatusConflict || errorCode(env) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("key reuse: status %d body %v", code, env)
	}

	code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: "normal_helmet_lv10", IdempotencyKey: string(bytes.Repeat([]byte("k"), 65))})
	if code != http.StatusBadRequest || errorCode(env) != "BAD_REQUEST" {
		t.Fatalf("long key: status %d body %v", code, env)
	}
	code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: "crafted_weapon_silver"})
	if code != http.StatusBadRequest || errorCode(env) != "BAD_REQUEST" {
		t.Fatalf("crafted: status %d body %v", code, env)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE config_equipment_items SET is_active = FALSE WHERE item_id = 'normal_gloves_lv10'`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"normal_gloves_lv10", "no_such_item"} {
		code, env = callHandler(t, h.BuyEquipment, "p1", BuyEquipmentRequest{ItemID: id})
		if code != http.StatusNotFound || errorCode(env) != "EQUIP_NOT_FOUND" {
			t.Fatalf("%s: status %d body %v", id, code, env)
		}
	}

	// No key: every call buys (old clients)
	if _, err := db.Pool.Exec(ctx, `UPDATE player_profiles SET coin = 1000 WHERE player_id = 'p1'`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_helmet_lv10"}); err != nil {
			t.Fatalf("keyless buy %d: %v", i, err)
		}
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 400 {
		t.Fatalf("coin %d after two keyless 300-coin buys from 1000, want 400", coin)
	}
}

func TestBuyEquipmentSameKeyConcurrentChargesOnce(t *testing.T) {
	svc, db := newEquipmentService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)

	const n = 5
	var wg sync.WaitGroup
	ids := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = svc.BuyEquipment(context.Background(), "p1", BuyEquipmentRequest{ItemID: "normal_armor_lv10", IdempotencyKey: "same"})
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("request %d got %s, want %s", i, ids[i], ids[0])
		}
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 9_600 || ownedEquipment(t, db, "p1") != 1 {
		t.Fatalf("coin %d owned %d, want 9600 and 1", coin, ownedEquipment(t, db, "p1"))
	}
}

func TestEquipRulesAndSlotSwap(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 100_000, 0)
	addCharacter(t, db, "p1", "tanko", 1)
	addCharacter(t, db, "p1", "spark", 1)
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO config_equipment_items (item_id, name, slot, category, required_level, character_id, gem_slots, stat_hp, price_coin)
		VALUES ('test_spark_helmet', 'Spark Cap', 'helmet', 'normal', 1, 'spark', 1, 5, 10)`); err != nil {
		t.Fatal(err)
	}
	buy := func(itemID string) string {
		t.Helper()
		id, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: itemID})
		if err != nil {
			t.Fatalf("buy %s: %v", itemID, err)
		}
		return id
	}
	sword1 := buy("normal_weapon_lv10")
	sword4 := buy("normal_weapon_lv20") // required level 4
	sparkCap := buy("test_spark_helmet")

	wantCode(t, svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sparkCap, CharacterID: "tanko"}), "EQUIP_WRONG_CHARACTER")
	wantCode(t, svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sword4, CharacterID: "tanko"}), "EQUIP_LEVEL_REQUIRED")
	wantCode(t, svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sword1, CharacterID: "flora"}), "CHARACTER_NOT_FOUND")
	wantCode(t, svc.EquipItem(ctx, "p2", EquipRequest{EquipmentID: sword1, CharacterID: "tanko"}), "EQUIP_NOT_OWNED")
	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sparkCap, CharacterID: "spark"}); err != nil {
		t.Fatalf("equip spark cap on spark: %v", err)
	}

	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sword1, CharacterID: "tanko"}); err != nil {
		t.Fatalf("equip sword1: %v", err)
	}
	wantCode(t, svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sword1, CharacterID: "spark"}), "EQUIP_ALREADY_EQUIPPED")

	// Level 4 reaches the Lv4 tier; equipping into the taken slot swaps
	if _, err := db.Pool.Exec(ctx, `UPDATE player_characters SET level = 4 WHERE player_id = 'p1' AND character_id = 'tanko'`); err != nil {
		t.Fatal(err)
	}
	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: sword4, CharacterID: "tanko"}); err != nil {
		t.Fatalf("equip sword4: %v", err)
	}
	if on, _ := equipped(t, db, sword1); on {
		t.Fatal("old weapon still equipped after the swap")
	}
	if on, char := equipped(t, db, sword4); !on || char == nil || *char != "tanko" {
		t.Fatalf("new weapon equipped %v on %v", on, char)
	}

	if err := svc.UnequipItem(ctx, "p1", UnequipRequest{EquipmentID: sword4}); err != nil {
		t.Fatalf("unequip: %v", err)
	}
	if on, char := equipped(t, db, sword4); on || char != nil {
		t.Fatalf("after unequip equipped %v on %v", on, char)
	}
	wantCode(t, svc.UnequipItem(ctx, "p1", UnequipRequest{EquipmentID: sword4}), "EQUIP_NOT_EQUIPPED")
	wantCode(t, svc.UnequipItem(ctx, "p2", UnequipRequest{EquipmentID: sparkCap}), "EQUIP_NOT_OWNED")
}

// GET /player/characters fromEquipment follows equip and unequip.
func TestCharacterStatsFollowEquipment(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	chars := character.NewService(character.NewRepository(db), economy.NewRepository(), db)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)
	addCharacter(t, db, "p1", "tanko", 1)

	fromEquipment := func() character.StatValues {
		t.Helper()
		resp, err := chars.GetCharacters(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Characters) != 1 || resp.Characters[0].Stats == nil {
			t.Fatalf("characters %+v", resp.Characters)
		}
		return resp.Characters[0].Stats.FromEquipment
	}
	if fe := fromEquipment(); fe != (character.StatValues{}) {
		t.Fatalf("fromEquipment before equip %+v", fe)
	}

	vest, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_armor_lv10"}) // hp 30, def 3
	if err != nil {
		t.Fatal(err)
	}
	sword, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_weapon_lv10"}) // dmg 8
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{vest, sword} {
		if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: id, CharacterID: "tanko"}); err != nil {
			t.Fatal(err)
		}
	}
	if fe := fromEquipment(); fe.MaxHP != 30 || fe.Defense != 3 || fe.DamageBonus != 8 {
		t.Fatalf("fromEquipment after equip %+v, want hp 30 def 3 dmg 8", fe)
	}

	if err := svc.UnequipItem(ctx, "p1", UnequipRequest{EquipmentID: vest}); err != nil {
		t.Fatal(err)
	}
	if fe := fromEquipment(); fe.MaxHP != 0 || fe.Defense != 0 || fe.DamageBonus != 8 {
		t.Fatalf("fromEquipment after unequip %+v, want only the sword", fe)
	}
}

func TestDismantleRefusesEquipped(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 10_000, 0)
	addCharacter(t, db, "p1", "tanko", 1)
	id, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_boots_lv10"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.EquipItem(ctx, "p1", EquipRequest{EquipmentID: id, CharacterID: "tanko"}); err != nil {
		t.Fatal(err)
	}

	code, env := callHandler(t, h.Dismantle, "p1", DismantleRequest{EquipmentID: id})
	if code != http.StatusConflict || errorCode(env) != "EQUIP_EQUIPPED" {
		t.Fatalf("dismantle equipped: status %d body %v", code, env)
	}
	if ownedEquipment(t, db, "p1") != 1 {
		t.Fatal("equipped item was dismantled")
	}

	if err := svc.UnequipItem(ctx, "p1", UnequipRequest{EquipmentID: id}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DismantleEquipment(ctx, "p1", DismantleRequest{EquipmentID: id}); err != nil {
		t.Fatalf("dismantle after unequip: %v", err)
	}
	if ownedEquipment(t, db, "p1") != 0 {
		t.Fatal("item still owned after dismantle")
	}
}

// Recipes are read with materialId (migration 016) or material_id (older admin seed).
func TestCraftReadsBothRecipeKeys(t *testing.T) {
	svc, db := newEquipmentService(t, 1)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO config_crafting_recipes (recipe_id, result_item_id, materials)
		VALUES ('legacy_recipe', 'crafted_armor_silver', '[{"material_id": "silver_ore", "quantity": 2}]');
		INSERT INTO player_materials (player_id, material_id, quantity) VALUES
			('p1', 'silver_ore', 12), ('p1', 'woven_thread', 10), ('p1', 'beast_hide', 10),
			('p1', 'crystal_powder', 10), ('p1', 'silver_blueprint', 10)`); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CraftEquipment(ctx, "p1", CraftRequest{RecipeID: "recipe_armor_silver"}); err != nil {
		t.Fatalf("craft seeded recipe: %v", err)
	}
	if _, err := svc.CraftEquipment(ctx, "p1", CraftRequest{RecipeID: "legacy_recipe"}); err != nil {
		t.Fatalf("craft legacy recipe: %v", err)
	}
	if got := countRows(t, db, `SELECT quantity FROM player_materials WHERE player_id = 'p1' AND material_id = 'silver_ore'`); got != 0 {
		t.Fatalf("silver_ore left %d, want 0", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM player_equipment WHERE player_id = 'p1' AND category = 'crafted' AND tier = 'silver'`); got != 2 {
		t.Fatalf("crafted items %d, want 2", got)
	}
	wantCode(t, func() error {
		_, err := svc.CraftEquipment(ctx, "p1", CraftRequest{RecipeID: "recipe_armor_silver"})
		return err
	}(), "EQUIP_INSUFFICIENT_MATERIALS")

	recipes, err := svc.GetCraftingRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recipes {
		for _, m := range r.Materials {
			if m.MaterialID == "" || m.Quantity <= 0 {
				t.Fatalf("recipe %s material %+v", r.RecipeID, m)
			}
		}
	}
}

// The down migration removes the default catalog but keeps what players own.
func TestCatalogDownMigrationKeepsOwnedRows(t *testing.T) {
	svc, db := newEquipmentService(t, 0)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 1_000, 0)
	if _, err := svc.BuyEquipment(ctx, "p1", BuyEquipmentRequest{ItemID: "normal_weapon_lv10"}); err != nil {
		t.Fatal(err)
	}
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "016_default_equipment_catalog.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatalf("down: %v", err)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_equipment_items`); got != 1 {
		t.Fatalf("items left %d, want the owned one", got)
	}
	if got := countRows(t, db, `SELECT COUNT(*) FROM config_crafting_recipes`) + countRows(t, db, `SELECT COUNT(*) FROM config_upgrade_rates`); got != 0 {
		t.Fatalf("recipes + rates left %d", got)
	}
}
