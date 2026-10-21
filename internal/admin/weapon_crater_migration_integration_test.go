package admin

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// Migration 029 halves Rookie's and Tanko's terrain damage (weapons and the characters' display
// copy) to configs/*.yaml's values, leaves Spark and Flora, keeps admin edits on reruns, and a
// later rerun of 023 does not undo it; the down migration restores the old values.
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

	_, self, _, _ := runtime.Caller(0)
	prev := gamedata.Data
	t.Cleanup(func() { gamedata.Data = prev })
	if err := gamedata.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
	terrain := func(table, key, id string) int {
		var v int
		if err := db.Pool.QueryRow(ctx, `SELECT terrain_damage FROM `+table+` WHERE `+key+` = $1`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for id, w := range gamedata.Data.Weapons {
		if got := terrain("config_weapons", "weapon_id", id); got != w.TerrainDamage {
			t.Errorf("weapon %s: migrated terrain damage %d, yaml %d", id, got, w.TerrainDamage)
		}
	}
	for id, c := range gamedata.Data.Characters {
		if got := terrain("config_characters", "character_id", id); got != c.TerrainDamage {
			t.Errorf("character %s: migrated terrain damage %d, yaml %d", id, got, c.TerrainDamage)
		}
	}
	if terrain("config_weapons", "weapon_id", "basic_rocket") != 25 || terrain("config_weapons", "weapon_id", "cannon_ball") != 65 {
		t.Fatal("not halved")
	}

	// An admin edit after the migration survives a rerun
	if _, err := db.Pool.Exec(ctx, `UPDATE config_weapons SET terrain_damage = 90 WHERE weapon_id = 'cannon_ball'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	if got := terrain("config_weapons", "weapon_id", "cannon_ball"); got != 90 {
		t.Fatalf("admin edit lost: %d", got)
	}

	// The down migration restores the full craters of the rows still at 029's values
	if _, err := db.Pool.Exec(ctx, readMigration(t, "029_weapon_craters_halved.down.sql")); err != nil {
		t.Fatal(err)
	}
	if terrain("config_weapons", "weapon_id", "basic_rocket") != 50 || terrain("config_characters", "character_id", "tanko") != 130 ||
		terrain("config_weapons", "weapon_id", "cannon_ball") != 90 {
		t.Fatal("down: not restored")
	}
}
