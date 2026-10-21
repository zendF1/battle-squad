package equipment

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// addPiece gives playerID a piece in slot at level and returns its id.
func addPiece(t *testing.T, db *database.PostgresDB, playerID, itemID, slot string, level int) string {
	t.Helper()
	var id string
	if err := db.Pool.QueryRow(context.Background(),
		`INSERT INTO player_equipment (player_id, item_id, slot, category, upgrade_level) VALUES ($1, $2, $3, 'normal', $4) RETURNING equipment_id`,
		playerID, itemID, slot, level).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func addStones(t *testing.T, db *database.PostgresDB, playerID string, stoneLevel, qty int) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(),
		`INSERT INTO player_stones (player_id, stone_level, quantity) VALUES ($1, $2, $3)
		 ON CONFLICT (player_id, stone_level) DO UPDATE SET quantity = player_stones.quantity + EXCLUDED.quantity`,
		playerID, stoneLevel, qty); err != nil {
		t.Fatal(err)
	}
}

func pieceLevel(t *testing.T, db *database.PostgresDB, id string) int {
	t.Helper()
	return countRows(t, db, `SELECT upgrade_level FROM player_equipment WHERE equipment_id = $1`, id)
}

func carefulLogs(t *testing.T, db *database.PostgresDB, playerID string) int {
	t.Helper()
	return countRows(t, db, `SELECT COUNT(*) FROM economy_transactions WHERE player_id = $1 AND source = 'careful_upgrade'`, playerID)
}

func intPtr(v int) *int { return &v }

// 2 × level 9 (6561) = 13122 power: 26% of +13→+14's 50000, capped at 20%.
var maxRateStones1314 = []StoneInput{{StoneLevel: 9, Quantity: 2}}

func TestCarefulUpgradeChargesGemsOnSuccessAndFail(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 1_000)
	id := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 13)
	addStones(t, db, "p1", 9, 4)

	// Success: +13 → +14, 20% × 1.4 = 28%, 120 gem
	svc.roll = func() float64 { return 0.279 }
	code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, Careful: true, ExpectedLevel: intPtr(13)})
	data, _ := env["data"].(map[string]any)
	if code != http.StatusOK || data["success"] != true || data["newLevel"] != 14.0 || data["percent"] != 28.0 ||
		data["careful"] != true || data["gemSpent"] != 120.0 || data["gemBalance"] != 880.0 {
		t.Fatalf("success: status %d body %v", code, env)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 880 || carefulLogs(t, db, "p1") != 1 || stoneCount(t, db, "p1")[9] != 2 {
		t.Fatalf("after success: gem %d logs %d stones %v", gem, carefulLogs(t, db, "p1"), stoneCount(t, db, "p1"))
	}
	var amount, after int
	var ref string
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT amount, balance_after, ref_id FROM economy_transactions WHERE player_id = 'p1' AND source = 'careful_upgrade'`).Scan(&amount, &after, &ref); err != nil {
		t.Fatal(err)
	}
	if amount != -120 || after != 880 || ref != id {
		t.Fatalf("log amount %d after %d ref %s", amount, after, ref)
	}

	// Fail: +14 → +15 costs 180 gem; 2 × level 9 = 16% capped at 15%, × 1.5 = 22.5%; resets to +14
	svc.roll = func() float64 { return 0.225 }
	code, env = callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, Careful: true})
	data, _ = env["data"].(map[string]any)
	if code != http.StatusOK || data["success"] != false || data["newLevel"] != 14.0 || data["percent"] != 22.5 ||
		data["gemSpent"] != 180.0 || data["gemBalance"] != 700.0 {
		t.Fatalf("fail: status %d body %v", code, env)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 700 || carefulLogs(t, db, "p1") != 2 || len(stoneCount(t, db, "p1")) != 0 {
		t.Fatalf("after fail: gem %d logs %d stones %v", gem, carefulLogs(t, db, "p1"), stoneCount(t, db, "p1"))
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM equipment_upgrade_log WHERE equipment_id = $1`, id); n != 2 {
		t.Fatalf("%d upgrade logs, want 2", n)
	}
}

func TestCarefulUpgradeRefusalsChangeNothing(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 119)
	id := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 13)
	addStones(t, db, "p1", 9, 2)

	unchanged := func(label string) {
		t.Helper()
		_, gem := dbtest.Balance(t, db, "p1")
		if gem != 119 || pieceLevel(t, db, id) != 13 || stoneCount(t, db, "p1")[9] != 2 || carefulLogs(t, db, "p1") != 0 ||
			countRows(t, db, `SELECT COUNT(*) FROM equipment_upgrade_log`) != 0 {
			t.Fatalf("%s: gem %d level %d stones %v", label, gem, pieceLevel(t, db, id), stoneCount(t, db, "p1"))
		}
	}

	// 119 gem < 120
	code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, Careful: true})
	if code != http.StatusBadRequest || errorCode(env) != "SHOP_INSUFFICIENT_BALANCE" {
		t.Fatalf("insufficient gems: status %d body %v", code, env)
	}
	unchanged("insufficient gems")

	// The level the client saw is stale
	code, env = callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, ExpectedLevel: intPtr(12)})
	if code != http.StatusConflict || errorCode(env) != "EQUIP_MODIFIED" {
		t.Fatalf("expectedLevel: status %d body %v", code, env)
	}
	unchanged("expectedLevel")

	// Price 0 or bonus 0: careful not offered for the step
	for _, set := range []string{"careful_gem_cost = 0", "careful_gem_cost = 100, careful_bonus_percent = 0"} {
		if _, err := db.Pool.Exec(ctx, `UPDATE config_upgrade_rates SET `+set+` WHERE from_level = 13`); err != nil {
			t.Fatal(err)
		}
		code, env = callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, Careful: true})
		if code != http.StatusBadRequest || errorCode(env) != "EQUIP_CAREFUL_UNAVAILABLE" {
			t.Fatalf("unavailable (%s): status %d body %v", set, code, env)
		}
		unchanged("unavailable " + set)
	}

	// Enough gems but not enough stones: the gem debit rolls back too
	if _, err := db.Pool.Exec(ctx, `UPDATE config_upgrade_rates SET careful_gem_cost = 100, careful_bonus_percent = 40 WHERE from_level = 13`); err != nil {
		t.Fatal(err)
	}
	code, env = callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: []StoneInput{{StoneLevel: 9, Quantity: 3}}, Careful: true})
	if code != http.StatusBadRequest || errorCode(env) != "EQUIP_INSUFFICIENT_STONES" {
		t.Fatalf("insufficient stones: status %d body %v", code, env)
	}
	unchanged("insufficient stones")
}

func TestNonCarefulUpgradeUnchanged(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 1_000)
	id := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 13)
	addStones(t, db, "p1", 9, 2)

	svc.roll = func() float64 { return 0.19 }
	code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314})
	data, _ := env["data"].(map[string]any)
	if _, hasBalance := data["gemBalance"]; code != http.StatusOK || data["percent"] != 20.0 || data["success"] != true ||
		data["careful"] != false || data["gemSpent"] != 0.0 || hasBalance {
		t.Fatalf("status %d body %v", code, env)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 1_000 || carefulLogs(t, db, "p1") != 0 || pieceLevel(t, db, id) != 14 {
		t.Fatalf("gem %d level %d", gem, pieceLevel(t, db, id))
	}
}

// Two identical careful requests (a double tap, or a retry racing the first)
// with expectedLevel: one rolls, the other is EQUIP_MODIFIED; stones and gems
// are charged once. +13 moves on both outcomes (+14 or back to +10).
func TestCarefulUpgradeConcurrentChargesOnce(t *testing.T) {
	db := dbtest.NewSchema(t, 0, equipmentMigrations...)
	svc := newServiceOn(db)
	dbtest.CreatePlayer(t, db, "p1", 0, 1_000)
	id := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 13)
	addStones(t, db, "p1", 9, 4)

	const n = 2
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.UpgradeEquipment(context.Background(), "p1",
				UpgradeRequest{EquipmentID: id, Stones: maxRateStones1314, Careful: true, ExpectedLevel: intPtr(13)})
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
		} else {
			wantCode(t, err, "EQUIP_MODIFIED")
		}
	}
	if ok != 1 {
		t.Fatalf("%d succeeded, want 1: %v", ok, errs)
	}
	if _, gem := dbtest.Balance(t, db, "p1"); gem != 880 || carefulLogs(t, db, "p1") != 1 || stoneCount(t, db, "p1")[9] != 2 {
		t.Fatalf("gem %d logs %d stones %v", gem, carefulLogs(t, db, "p1"), stoneCount(t, db, "p1"))
	}
}

// The same stones give a weapon a third of an armor piece's percent (below
// maxPercent); the careful price is per step, not multiplied.
func TestWeaponUpgradeCostsTripleStonePower(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	h := NewHandler(svc)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 1_000)
	armor := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 8)
	weapon := addPiece(t, db, "p1", "rookie_weapon_lv10", "weapon", 8)
	addStones(t, db, "p1", 6, 10)
	svc.roll = func() float64 { return 0.99 }

	// +8 → +9: cost 1200, max 45%. 2 × level 6 = 486 power
	upgrade := func(id string, careful bool) map[string]any {
		t.Helper()
		code, env := callHandler(t, h.Upgrade, "p1", UpgradeRequest{EquipmentID: id, Stones: []StoneInput{{StoneLevel: 6, Quantity: 2}}, Careful: careful})
		if code != http.StatusOK {
			t.Fatalf("status %d body %v", code, env)
		}
		data, _ := env["data"].(map[string]any)
		return data
	}
	if p := upgrade(armor, false)["percent"]; p != 40.5 { // 486 × 100 / 1200
		t.Fatalf("armor percent %v, want 40.5", p)
	}
	if p := upgrade(weapon, false)["percent"]; p != 13.5 { // 486 × 100 / 3600
		t.Fatalf("weapon percent %v, want 13.5", p)
	}
	// Failed rolls reset both to +6; +6 → +7: cost 180, careful 10 gem, ×1.3
	data := upgrade(weapon, true) // 486 × 100 / 540 = 90 → max 56, × 1.3 = 72.8
	if data["percent"] != 72.8 || data["gemSpent"] != 10.0 {
		t.Fatalf("weapon careful %v", data)
	}

	// Configured multiplier, and an invalid one falls back to 3
	for raw, want := range map[string]float64{"2": 20.25, "0.5": 13.5} {
		if _, err := db.Pool.Exec(ctx, `UPDATE game_settings SET value = $1 WHERE key = 'equipment.weapon_upgrade_cost_multiplier'`, raw); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE player_equipment SET upgrade_level = 8 WHERE equipment_id = $1`, weapon); err != nil {
			t.Fatal(err)
		}
		if p := upgrade(weapon, false)["percent"]; p != want {
			t.Fatalf("multiplier %s: percent %v, want %v", raw, p, want)
		}
	}
}

func TestCatalogCarefulAndWeaponFields(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	h := NewHandler(newServiceOn(db))
	ctx := context.Background()

	_, env := callHandler(t, h.GetCatalog, "p1", nil)
	data, _ := env["data"].(map[string]any)
	rates, _ := data["upgradeRates"].([]any)
	if _, top := data["carefulBonusPercent"]; top || data["weaponUpgradeCostMultiplier"] != 3.0 || len(rates) != 16 {
		t.Fatalf("catalog %v", data)
	}
	for i, r := range rates {
		rr := r.(map[string]any)
		if rr["carefulGemCost"] != wantCarefulGem[i] || rr["carefulBonusPercent"] != wantCarefulBonus(i) {
			t.Fatalf("step %d careful %v / %v, want %v / %v", i, rr["carefulGemCost"], rr["carefulBonusPercent"], wantCarefulGem[i], wantCarefulBonus(i))
		}
	}

	if _, err := db.Pool.Exec(ctx, `UPDATE game_settings SET value = '2' WHERE key = 'equipment.weapon_upgrade_cost_multiplier'`); err != nil {
		t.Fatal(err)
	}
	_, env = callHandler(t, h.GetCatalog, "p1", nil)
	if data, _ = env["data"].(map[string]any); data["weaponUpgradeCostMultiplier"] != 2.0 {
		t.Fatalf("configured %v", data["weaponUpgradeCostMultiplier"])
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM game_settings WHERE key = 'equipment.weapon_upgrade_cost_multiplier'`); err != nil {
		t.Fatal(err)
	}
	_, env = callHandler(t, h.GetCatalog, "p1", nil)
	if data, _ = env["data"].(map[string]any); data["weaponUpgradeCostMultiplier"] != 3.0 {
		t.Fatalf("default %v", data["weaponUpgradeCostMultiplier"])
	}
}

var wantCarefulGem = []float64{2, 2, 3, 5, 5, 10, 10, 10, 15, 20, 30, 60, 80, 120, 180, 250}

func wantCarefulBonus(fromLevel int) float64 {
	switch {
	case fromLevel <= 9:
		return 30
	case fromLevel <= 13:
		return 40
	}
	return 50
}

func carefulMigrationSQL(t *testing.T) string {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", carefulMigration))
	if err != nil {
		t.Fatal(err)
	}
	return string(sql)
}

func carefulStep(t *testing.T, db *database.PostgresDB, fromLevel int) (gem int, bonus float64) {
	t.Helper()
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT careful_gem_cost, careful_bonus_percent::float8 FROM config_upgrade_rates WHERE from_level = $1`, fromLevel).Scan(&gem, &bonus); err != nil {
		t.Fatal(err)
	}
	return gem, bonus
}

// Rerunning 025 (cmd/migrate reruns every file) keeps admin edits, including
// a step set back to 0.
func TestCarefulMigrationIsIdempotent(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `UPDATE config_upgrade_rates SET careful_gem_cost = 0 WHERE from_level = 13;
		UPDATE config_upgrade_rates SET careful_gem_cost = 999, careful_bonus_percent = 75 WHERE from_level = 15;
		UPDATE game_settings SET value = '2' WHERE key = 'equipment.weapon_upgrade_cost_multiplier'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, carefulMigrationSQL(t)); err != nil {
		t.Fatal(err)
	}
	if gem, bonus := carefulStep(t, db, 13); gem != 0 || bonus != 40 {
		t.Fatalf("step 13 %d / %v, want the admin's 0 / 40", gem, bonus)
	}
	if gem, bonus := carefulStep(t, db, 15); gem != 999 || bonus != 75 {
		t.Fatalf("step 15 %d / %v, want 999 / 75", gem, bonus)
	}
	if got := countRows(t, db, `SELECT value::int FROM game_settings WHERE key = 'equipment.weapon_upgrade_cost_multiplier'`); got != 2 {
		t.Fatalf("multiplier %d, want 2", got)
	}
}

// A database that ran an earlier draft of 025 (careful_gem_cost only, other
// prices) ends at the current prices and bonuses.
func TestCarefulMigrationUpgradesDraftColumn(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations[:len(equipmentMigrations)-3]...) // without 025, 026 and 034
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `ALTER TABLE config_upgrade_rates ADD COLUMN careful_gem_cost INT NOT NULL DEFAULT 0;
		UPDATE config_upgrade_rates SET careful_gem_cost = 300`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, carefulMigrationSQL(t)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if gem, bonus := carefulStep(t, db, i); float64(gem) != wantCarefulGem[i] || bonus != wantCarefulBonus(i) {
			t.Fatalf("step %d %d / %v, want %v / %v", i, gem, bonus, wantCarefulGem[i], wantCarefulBonus(i))
		}
	}
}
