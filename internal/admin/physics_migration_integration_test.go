package admin

import (
	"context"
	"strconv"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// The admin seed's physics rows are gamedata.DefaultPhysics() (what the match uses when a row
// is missing).
func TestSeedPhysicsMatchesDefaults(t *testing.T) {
	d := gamedata.DefaultPhysics()
	want := map[string]float64{
		"physics.gravity":                     d.Gravity,
		"physics.projectile_speed_multiplier": d.ProjectileSpeedMultiplier,
		"physics.wind_scale":                  d.WindScale,
		"physics.player_hit_radius":           d.PlayerHitRadius,
		"physics.time_step":                   d.TimeStep,
		"physics.path_record_step":            d.PathRecordStep,
		"physics.max_flight_seconds":          d.MaxFlightSeconds,
	}
	seen := 0
	for _, s := range defaultSettings {
		w, ok := want[s.Key]
		if !ok {
			continue
		}
		seen++
		if v, err := strconv.ParseFloat(s.Value, 64); err != nil || v != w {
			t.Errorf("seed %s = %q, default %v", s.Key, s.Value, w)
		}
	}
	if seen != len(want) {
		t.Fatalf("seed has %d of the %d physics rows", seen, len(want))
	}
}

// Migration 032 turns the old physics rows (as the seed inserted them) into the seed's new
// values, whatever their spelling, leaves the other rows (a non-number among them), keeps admin
// edits on reruns; the down migration restores the rows still at 032's values.
func TestFasterShellsMigrationMatchesSeed(t *testing.T) {
	ctx := context.Background()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql")
	if _, err := db.Pool.Exec(ctx, `INSERT INTO game_settings (key, value) VALUES
		('physics.gravity', '200'), ('physics.projectile_speed_multiplier', '6'),
		('physics.wind_scale', '30.0'), ('physics.max_flight_seconds', '6.0'),
		('physics.player_hit_radius', '24.0'), ('physics.time_step', '0.02'),
		('matchmaking', '{"a": 1}')`); err != nil {
		t.Fatal(err)
	}
	up := readMigration(t, "032_faster_shells.up.sql")
	for i := 0; i < 2; i++ {
		if _, err := db.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	value := func(key string) string {
		var v string
		if err := db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = $1`, key).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	seed := map[string]string{}
	for _, s := range defaultSettings {
		seed[s.Key] = s.Value
	}
	for _, k := range []string{"physics.gravity", "physics.projectile_speed_multiplier", "physics.wind_scale", "physics.max_flight_seconds"} {
		if got := value(k); got != seed[k] {
			t.Errorf("%s: migrated %q, seed %q", k, got, seed[k])
		}
	}
	if value("physics.player_hit_radius") != "24.0" || value("physics.time_step") != "0.02" || value("matchmaking") != `{"a": 1}` {
		t.Fatal("other rows changed")
	}

	// An admin edit after the migration survives a rerun
	if _, err := db.Pool.Exec(ctx, `UPDATE game_settings SET value = '400' WHERE key = 'physics.gravity'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	if got := value("physics.gravity"); got != "400" {
		t.Fatalf("admin edit lost: %s", got)
	}

	// The down migration restores the rows still at 032's values
	if _, err := db.Pool.Exec(ctx, readMigration(t, "032_faster_shells.down.sql")); err != nil {
		t.Fatal(err)
	}
	if value("physics.projectile_speed_multiplier") != "6.0" || value("physics.wind_scale") != "30.0" ||
		value("physics.max_flight_seconds") != "6.0" || value("physics.gravity") != "400" {
		t.Fatal("down: not restored")
	}
}
