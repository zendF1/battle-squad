package equipment

import (
	"context"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/api/milestone"
	"battle-squad/internal/shared/database/dbtest"
)

// A successful upgrade raises the milestone peaks inside the upgrade tx; a later fail reset
// or selling the piece does not lower them.
func TestUpgradePeakSurvivesFailResetAndSell(t *testing.T) {
	db := dbtest.NewSchema(t, 1, equipmentMigrations...)
	svc := newServiceOn(db)
	ms := milestone.NewService(milestone.NewRepository(db), economy.NewRepository(), db)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	armor := addPiece(t, db, "p1", "normal_armor_lv10", "armor", 2)
	addStones(t, db, "p1", 3, 2)
	addStones(t, db, "p1", 9, 1)

	progress := func(id string) (int, string) {
		t.Helper()
		list, err := ms.List(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		for _, tr := range list.Tracks {
			for _, n := range tr.Nodes {
				if n.ID == id {
					return n.Progress, n.State
				}
			}
		}
		t.Fatalf("%s not listed", id)
		return 0, ""
	}

	svc.roll = func() float64 { return 0 }
	if res, err := svc.UpgradeEquipment(ctx, "p1", UpgradeRequest{EquipmentID: armor, Stones: []StoneInput{{StoneLevel: 3, Quantity: 1}}}); err != nil || !res.Success || res.NewLevel != 3 {
		t.Fatalf("upgrade %+v %v", res, err)
	}
	svc.roll = func() float64 { return 0.999 }
	if res, err := svc.UpgradeEquipment(ctx, "p1", UpgradeRequest{EquipmentID: armor, Stones: []StoneInput{{StoneLevel: 3, Quantity: 1}}}); err != nil || res.Success || res.NewLevel != 3 {
		// +3 → +4 fails back to +3 (fail_reset_to 3)
		t.Fatalf("failed upgrade %+v %v", res, err)
	}
	if countRows(t, db, `SELECT value FROM player_stat_counters WHERE player_id = 'p1' AND counter = 'upgrade_peak'`) != 3 {
		t.Fatal("peak not raised to 3")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE player_equipment SET upgrade_level = 0 WHERE equipment_id = $1`, armor); err != nil {
		t.Fatal(err)
	}
	if p, s := progress("ms_upgrade_03"); p != 3 || s != milestone.StateClaimable {
		t.Fatalf("after the reset: %d %s", p, s)
	}
	if _, err := svc.SellEquipment(ctx, "p1", SellRequest{EquipmentID: armor}); err != nil {
		t.Fatal(err)
	}
	if p, s := progress("ms_upgrade_03"); p != 3 || s != milestone.StateClaimable {
		t.Fatalf("after the reset and the sale: %d %s", p, s)
	}

	// A weapon reaching +14 raises both peaks
	weapon := addPiece(t, db, "p1", "gun_test", "weapon", 13)
	svc.roll = func() float64 { return 0 }
	if res, err := svc.UpgradeEquipment(ctx, "p1", UpgradeRequest{EquipmentID: weapon, Stones: []StoneInput{{StoneLevel: 9, Quantity: 1}}}); err != nil || !res.Success {
		t.Fatalf("weapon upgrade %+v %v", res, err)
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM player_equipment WHERE equipment_id = $1`, weapon); err != nil {
		t.Fatal(err)
	}
	if p, s := progress("ms_weapon_14"); p != 14 || s != milestone.StateClaimable {
		t.Fatalf("weapon 14: %d %s", p, s)
	}
	if p, _ := progress("ms_upgrade_14"); p != 14 {
		t.Fatalf("upgrade 14: %d", p)
	}
}
