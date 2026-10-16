package room

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// allMigrations are the up migrations cmd/migrate runs, in order.
func allMigrations(t *testing.T) []string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(self), "..", "..", "..", "migrations", "*.up.sql"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	return names
}

// equipCatalogItem gives the player a seeded catalog item at an upgrade level,
// equipped on characterID.
func equipCatalogItem(t *testing.T, db *database.PostgresDB, playerID, characterID, itemID string, upgrade int) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO player_equipment (player_id, item_id, slot, category, tier, upgrade_level, is_equipped, equipped_on)
		SELECT $1, item_id, slot, category, tier, $3, TRUE, $4 FROM config_equipment_items WHERE item_id = $2`,
		playerID, itemID, upgrade, characterID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM player_equipment WHERE player_id = $1 AND item_id = $2`, playerID, itemID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("catalog item %s not seeded (%d, %v)", itemID, n, err)
	}
}

// directHit is the damage a direct hit (no wind, no crit) from attacker
// stats deals to a target with defense, as the match computes it.
func directHit(characterID string, attacker battlestats.Stats, defense int) int {
	w := gamedata.Data.Weapons[gamedata.Data.Characters[characterID].WeaponID]
	return match.CalculateExplosionDamage(match.Vector2{}, match.Vector2{},
		float64(w.Damage+attacker.DamageBonus), float64(w.ExplosionRadius), defense)
}

// Balance targets for Rookie vs Rookie with the same gear (direct hits, no
// wind, no crit): no gear, Iron and Dragon kill in 4 hits, full Diamond +16
// in 6. Uses the real migrations (catalog, set bonuses, statMultipliers),
// configs/*.yaml base stats and the battle stats the room builds.
func TestBalanceHitsToKill(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	prev := gamedata.Data
	_, self, _, _ := runtime.Caller(0)
	if err := gamedata.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gamedata.Data = prev })

	slots := []string{"armor", "helmet", "pants", "boots", "gloves"}
	normal := func(lv int) []string {
		ids := []string{fmt.Sprintf("rookie_weapon_lv%d", lv)}
		for _, s := range slots {
			ids = append(ids, fmt.Sprintf("normal_%s_lv%d", s, lv))
		}
		return ids
	}
	diamond := []string{"crafted_weapon_diamond"}
	for _, s := range slots {
		diamond = append(diamond, "crafted_"+s+"_diamond")
	}

	tests := []struct {
		name     string
		items    []string
		upgrade  int
		points   [2]int // allocated hp, damage
		wantPts  battlestats.EquipmentPoints
		wantHits int
	}{
		{name: "no gear", wantHits: 4},
		{name: "Iron +0", items: normal(10), wantPts: battlestats.EquipmentPoints{HP: 80, Damage: 10, Defense: 8, Mobility: 5, Crit: 2}, wantHits: 4},
		{name: "Iron +16", items: normal(10), upgrade: 16, wantPts: battlestats.EquipmentPoints{HP: 241, Damage: 30, Defense: 24, Mobility: 15, Crit: 6.04}, wantHits: 4},
		{name: "Dragon +0", items: normal(40), wantPts: battlestats.EquipmentPoints{HP: 435, Damage: 50, Defense: 44, Mobility: 20, Crit: 11}, wantHits: 4},
		{name: "Dragon +16", items: normal(40), upgrade: 16, wantPts: battlestats.EquipmentPoints{HP: 1313, Damage: 151, Defense: 132, Mobility: 60, Crit: 33.22}, wantHits: 5},
		{name: "Diamond +16 set", items: diamond, upgrade: 16, wantPts: battlestats.EquipmentPoints{HP: 3284, Damage: 378, Defense: 297, Mobility: 121, Crit: 76.44}, wantHits: 6},
		// Lv80, no gear: all 79 points in one stat (pointsPerLevel 1), shot by / shooting the same build
		{name: "Lv80 79 points HP", points: [2]int{79, 0}, wantHits: 4},
		{name: "Lv80 79 points damage", points: [2]int{0, 79}, wantHits: 2},
	}

	r := &Room{db: db}
	t.Logf("%-22s %6s %6s %4s %6s %5s", "Rookie vs same", "HP", "raw", "DEF", "hit", "hits")
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			playerID := fmt.Sprintf("bal%d", i)
			dbtest.CreatePlayer(t, db, playerID, 0, 0)
			if _, err := db.Pool.Exec(context.Background(), `
				INSERT INTO player_characters (player_id, character_id, bonus_hp, bonus_damage) VALUES ($1, 'rookie', $2, $3)`,
				playerID, tt.points[0], tt.points[1]); err != nil {
				t.Fatal(err)
			}
			for _, id := range tt.items {
				equipCatalogItem(t, db, playerID, "rookie", id, tt.upgrade)
			}

			pts, err := battlestats.LoadEquipmentPoints(context.Background(), db.Pool, playerID, "rookie")
			if err != nil {
				t.Fatal(err)
			}
			got := pts["rookie"]
			if math.Abs(got.Crit-tt.wantPts.Crit) > 1e-6 {
				t.Fatalf("equipment crit %v, want %v", got.Crit, tt.wantPts.Crit)
			}
			got.Crit = tt.wantPts.Crit
			if got != tt.wantPts {
				t.Fatalf("equipment points %+v, want %+v", got, tt.wantPts)
			}

			s := r.getActualStats(playerID, "rookie")
			hit := directHit("rookie", s, s.Defense)
			hits := int(math.Ceil(float64(s.HP) / float64(hit)))
			raw := battlestats.WeaponDamage("rookie") + s.DamageBonus
			t.Logf("%-22s %6d %6d %4d %6d %5d", tt.name, s.HP, raw, s.Defense, hit, hits)
			if hits != tt.wantHits {
				t.Fatalf("%s: %d HP / %d per hit = %d hits, want %d", tt.name, s.HP, hit, hits, tt.wantHits)
			}
		})
	}
}
