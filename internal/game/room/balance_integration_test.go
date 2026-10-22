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
	w := gamedata.Current().Data.Weapons[gamedata.Current().Data.Characters[characterID].WeaponID]
	return match.CalculateExplosionDamage(match.Vector2{}, match.Vector2{},
		float64(w.Damage+attacker.DamageBonus), float64(w.ExplosionRadius), defense)
}

// Balance targets for Rookie vs Rookie with the same gear (direct hits, no
// wind, no crit): no gear, Iron and Dragon kill in 4 hits, full Diamond +16
// in 6. Uses the real migrations (catalog, set bonuses, statMultipliers),
// configs/*.yaml base stats and the battle stats the room builds.
func TestBalanceHitsToKill(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	_, self, _, _ := runtime.Caller(0)
	gamedata.UseYAML(t, filepath.Join(filepath.Dir(self), "..", "..", "..", "configs"))

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

			s, _ := r.getActualStats(playerID, "rookie")
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

// identityCharacters are the four characters of configs/characters.yaml.
var identityCharacters = []string{"rookie", "tanko", "spark", "flora"}

// Character identities, hits to kill attacker × defender (direct hits, no wind,
// no crit) with no gear and with both wearing Dragon +0 (the lv40 set and the
// character's lv40 weapon): Spark kills fastest, Flora survives longest,
// Rookie in between; with no gear every matchup takes 3..8 hits.
func TestCharacterHitsToKillMatrix(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	_, self, _, _ := runtime.Caller(0)
	gamedata.UseYAML(t, filepath.Join(filepath.Dir(self), "..", "..", "..", "configs"))

	r := &Room{db: db}
	for gi, gear := range []struct {
		name    string
		level   int // 0 = no gear
		inRange bool
	}{{"no gear", 0, true}, {"Dragon +0", 40, false}} {
		t.Run(gear.name, func(t *testing.T) {
			stats := map[string]battlestats.Stats{}
			for _, c := range identityCharacters {
				playerID := fmt.Sprintf("id%d_%s", gi, c)
				dbtest.CreatePlayer(t, db, playerID, 0, 0)
				if _, err := db.Pool.Exec(context.Background(),
					`INSERT INTO player_characters (player_id, character_id) VALUES ($1, $2)`, playerID, c); err != nil {
					t.Fatal(err)
				}
				if gear.level > 0 {
					equipCatalogItem(t, db, playerID, c, fmt.Sprintf("%s_weapon_lv%d", c, gear.level), 0)
					for _, s := range []string{"armor", "helmet", "pants", "boots", "gloves"} {
						equipCatalogItem(t, db, playerID, c, fmt.Sprintf("normal_%s_lv%d", s, gear.level), 0)
					}
				}
				stats[c], _ = r.getActualStats(playerID, c)
				t.Logf("%-6s HP %5d DEF %3d damage %4d move energy %3d", c, stats[c].HP, stats[c].Defense,
					battlestats.WeaponDamage(c)+stats[c].DamageBonus, stats[c].TurnMoveEnergy())
			}

			hits := map[string]map[string]int{}
			row, col := map[string]int{}, map[string]int{}
			t.Logf("%-8s %7s %7s %7s %7s", "atk\\def", "rookie", "tanko", "spark", "flora")
			for _, a := range identityCharacters {
				hits[a] = map[string]int{}
				for _, d := range identityCharacters {
					h := int(math.Ceil(float64(stats[d].HP) / float64(directHit(a, stats[a], stats[d].Defense))))
					hits[a][d] = h
					row[a] += h
					col[d] += h
					if gear.inRange && (h < 3 || h > 8) {
						t.Errorf("%s kills %s in %d hits, want 3..8", a, d, h)
					}
				}
				t.Logf("%-8s %7d %7d %7d %7d", a, hits[a]["rookie"], hits[a]["tanko"], hits[a]["spark"], hits[a]["flora"])
			}
			for _, d := range identityCharacters {
				for _, a := range identityCharacters {
					if hits["spark"][d] > hits[a][d] {
						t.Errorf("%s kills %s in %d, faster than Spark's %d", a, d, hits[a][d], hits["spark"][d])
					}
					if hits[a]["flora"] < hits[a][d] {
						t.Errorf("%s lasts %d hits from %s, longer than Flora's %d", d, hits[a][d], a, hits[a]["flora"])
					}
				}
			}
			if !(row["spark"] < row["rookie"] && row["rookie"] < row["flora"]) {
				t.Errorf("hits dealt: Spark %d, Rookie %d, Flora %d; want Rookie in between", row["spark"], row["rookie"], row["flora"])
			}
			if !(col["spark"] < col["rookie"] && col["rookie"] < col["flora"]) {
				t.Errorf("hits survived: Spark %d, Rookie %d, Flora %d; want Rookie in between", col["spark"], col["rookie"], col["flora"])
			}
		})
	}
}
