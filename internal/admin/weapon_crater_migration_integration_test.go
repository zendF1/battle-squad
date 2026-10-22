package admin

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// Migrations 029 (halves Rookie's and Tanko's terrain damage) and 039 (craters scaled to the
// characters: 20 / 30 / 20 / 24) bring the weapons and the characters' display
// copy to configs/*.yaml's values, keep admin edits on reruns, and a
// later rerun of 023 does not undo them; the down migrations restore the old values.
func TestWeaponCraterMigrationMatchesSeed(t *testing.T) {
	ctx := context.Background()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql")
	// The rows as 023 left them
	if _, err := db.Pool.Exec(ctx, `INSERT INTO config_weapons (weapon_id, name, damage, explosion_radius, terrain_damage, projectile_weight, wind_influence, multi_hit)
		VALUES ('basic_rocket', 'Basic Rocket', 480, 40, 50, 1.0, 1.0, 1),
		       ('cannon_ball', 'Cannon Ball', 420, 60, 130, 1.5, 0.5, 1),
		       ('electric_orb', 'Electric Orb', 620, 30, 25, 0.8, 1.3, 1),
		       ('seed_bomb', 'Seed Bomb', 380, 40, 30, 0.9, 1.0, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO config_characters (character_id, name, role, hp, damage, mobility, defense, skill_power, terrain_damage, weapon_id, skill_id)
		VALUES ('rookie', 'Rookie', 'balanced', 1500, 480, 100, 50, 50, 50, 'basic_rocket', 'triple_shot'),
		       ('tanko', 'Tanko', 'demolisher', 1650, 420, 80, 60, 65, 130, 'cannon_ball', 'heavy_bomb'),
		       ('spark', 'Spark', 'striker', 1250, 620, 120, 30, 80, 25, 'electric_orb', 'shock_field'),
		       ('flora', 'Flora', 'sustain', 1900, 380, 100, 90, 40, 30, 'seed_bomb', 'healing_bloom')`); err != nil {
		t.Fatal(err)
	}
	up := readMigration(t, "029_weapon_craters_halved.up.sql")
	for i := 0; i < 2; i++ {
		if _, err := db.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, readMigration(t, "023_character_identities.up.sql")); err != nil {
		t.Fatal(err)
	}
	if terrain := func(id string) (v int) {
		db.Pool.QueryRow(ctx, `SELECT terrain_damage FROM config_weapons WHERE weapon_id = $1`, id).Scan(&v)
		return v
	}; terrain("basic_rocket") != 25 || terrain("cannon_ball") != 65 {
		t.Fatal("029 did not halve the craters")
	}
	small := readMigration(t, "039_small_craters.up.sql")
	for i := 0; i < 2; i++ {
		if _, err := db.Pool.Exec(ctx, small); err != nil {
			t.Fatal(err)
		}
	}

	_, self, _, _ := runtime.Caller(0)
	gamedata.UseYAML(t, filepath.Join(filepath.Dir(self), "..", "..", "configs"))
	terrain := func(table, key, id string) int {
		var v int
		if err := db.Pool.QueryRow(ctx, `SELECT terrain_damage FROM `+table+` WHERE `+key+` = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for id, w := range gamedata.Current().Data.Weapons {
		if got := terrain("config_weapons", "weapon_id", id); got != w.TerrainDamage {
			t.Errorf("weapon %s: migrated terrain damage %d, yaml %d", id, got, w.TerrainDamage)
		}
	}
	for id, c := range gamedata.Current().Data.Characters {
		if got := terrain("config_characters", "character_id", id); got != c.TerrainDamage {
			t.Errorf("character %s: migrated terrain damage %d, yaml %d", id, got, c.TerrainDamage)
		}
	}
	if terrain("config_weapons", "weapon_id", "basic_rocket") != 20 || terrain("config_weapons", "weapon_id", "cannon_ball") != 30 ||
		terrain("config_weapons", "weapon_id", "electric_orb") != 20 || terrain("config_weapons", "weapon_id", "seed_bomb") != 24 {
		t.Fatal("039 did not shrink the craters")
	}

	// An admin edit after the migration survives a rerun
	if _, err := db.Pool.Exec(ctx, `UPDATE config_weapons SET terrain_damage = 90 WHERE weapon_id = 'cannon_ball'`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{up, small} {
		if _, err := db.Pool.Exec(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if got := terrain("config_weapons", "weapon_id", "cannon_ball"); got != 90 {
		t.Fatalf("admin edit lost: %d", got)
	}

	// 039's down brings the rows still at its values back to 029's, then 029's down restores
	// the full craters of the rows still at 029's values
	if _, err := db.Pool.Exec(ctx, readMigration(t, "039_small_craters.down.sql")); err != nil {
		t.Fatal(err)
	}
	if terrain("config_weapons", "weapon_id", "basic_rocket") != 25 || terrain("config_weapons", "weapon_id", "seed_bomb") != 30 ||
		terrain("config_characters", "character_id", "tanko") != 65 || terrain("config_weapons", "weapon_id", "cannon_ball") != 90 {
		t.Fatal("039 down: not restored")
	}
	if _, err := db.Pool.Exec(ctx, readMigration(t, "029_weapon_craters_halved.down.sql")); err != nil {
		t.Fatal(err)
	}
	if terrain("config_weapons", "weapon_id", "basic_rocket") != 50 || terrain("config_characters", "character_id", "tanko") != 130 ||
		terrain("config_weapons", "weapon_id", "cannon_ball") != 90 {
		t.Fatal("down: not restored")
	}
}
