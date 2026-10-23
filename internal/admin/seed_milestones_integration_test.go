package admin

import (
	"context"
	"testing"

	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/migrate"
)

var milestoneSeedMigrations = []string{"001_init_schema.up.sql", "003_add_match_event_logs.up.sql", "010_equipment_system.up.sql",
	"026_milestones.up.sql", "027_titles.up.sql"}

// SeedMilestones and migrations/026 + 027 seed the same milestones (title items included).
func TestSeedMilestonesMatchesMigration(t *testing.T) {
	seeded := dbtest.NewSchema(t, 0, milestoneSeedMigrations...)
	if _, err := seeded.Pool.Exec(context.Background(), `DELETE FROM missions WHERE type = 'milestone'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate.SeedMilestones(context.Background(), seeded.Pool); err != nil {
		t.Fatal(err)
	}
	if err := migrate.SeedMilestones(context.Background(), seeded.Pool); err != nil { // rerun
		t.Fatal(err)
	}
	migrated := dbtest.NewSchema(t, 0, milestoneSeedMigrations...)
	q := `SELECT mission_id, type, track, target, required_value, sort_order, reward_coin, reward_gem, reward_items, description, is_active
	      FROM missions ORDER BY mission_id`
	a, b := dump(t, seeded, q), dump(t, migrated, q)
	if a == "" || a != b {
		t.Errorf("milestones differ:\nadmin seed: %s\nmigration:  %s", a, b)
	}
}

// Rows seeded before titles (026 only) get their title item from SeedMilestones, once.
func TestSeedMilestonesAttachesTitles(t *testing.T) {
	ctx := context.Background()
	old := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "003_add_match_event_logs.up.sql", "010_equipment_system.up.sql", "026_milestones.up.sql")
	for i := 0; i < 2; i++ {
		if err := migrate.SeedMilestones(ctx, old.Pool); err != nil {
			t.Fatal(err)
		}
	}
	migrated := dbtest.NewSchema(t, 0, milestoneSeedMigrations...)
	q := `SELECT mission_id, reward_items FROM missions ORDER BY mission_id`
	if a, b := dump(t, old, q), dump(t, migrated, q); a != b {
		t.Errorf("reward items differ:\nadmin seed: %s\nmigration:  %s", a, b)
	}
}

// SeedTitles and migrations/027 seed the same titles.
func TestSeedTitlesMatchesMigration(t *testing.T) {
	ctx := context.Background()
	seeded := dbtest.NewSchema(t, 0, milestoneSeedMigrations...)
	if _, err := seeded.Pool.Exec(ctx, `DELETE FROM config_titles`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrate.SeedTitles(ctx, seeded.Pool); err != nil {
			t.Fatal(err)
		}
	}
	migrated := dbtest.NewSchema(t, 0, milestoneSeedMigrations...)
	q := `SELECT title_id, name, rarity, sort_order, description, is_active FROM config_titles ORDER BY title_id`
	a, b := dump(t, seeded, q), dump(t, migrated, q)
	if a == "" || a != b {
		t.Errorf("titles differ:\nadmin seed: %s\nmigration:  %s", a, b)
	}
}
