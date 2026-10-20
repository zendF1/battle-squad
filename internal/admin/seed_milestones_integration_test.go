package admin

import (
	"context"
	"testing"

	"battle-squad/internal/shared/database/dbtest"
)

// SeedMilestones and migrations/026 seed the same milestones.
func TestSeedMilestonesMatchesMigration(t *testing.T) {
	migrations := []string{"001_init_schema.up.sql", "003_add_match_event_logs.up.sql", "010_equipment_system.up.sql", "026_milestones.up.sql"}
	seeded := dbtest.NewSchema(t, 0, migrations...)
	if _, err := seeded.Pool.Exec(context.Background(), `DELETE FROM missions WHERE type = 'milestone'`); err != nil {
		t.Fatal(err)
	}
	if err := SeedMilestones(context.Background(), seeded); err != nil {
		t.Fatal(err)
	}
	if err := SeedMilestones(context.Background(), seeded); err != nil { // rerun
		t.Fatal(err)
	}
	migrated := dbtest.NewSchema(t, 0, migrations...)
	q := `SELECT mission_id, type, track, target, required_value, sort_order, reward_coin, reward_gem, reward_items, description, is_active
	      FROM missions ORDER BY mission_id`
	a, b := dump(t, seeded, q), dump(t, migrated, q)
	if a == "" || a != b {
		t.Errorf("milestones differ:\nadmin seed: %s\nmigration:  %s", a, b)
	}
}
