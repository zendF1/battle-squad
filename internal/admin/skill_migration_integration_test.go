package admin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// The four skills as they were before migration 028 (configs/skills.yaml of the first design
// with 023's multipliers).
const oldSkillRows = `INSERT INTO config_skills
	(skill_id, character_id, name, cooldown_turn, effect_type, projectile_count, status_effect_id, damage_multiplier)
	VALUES ('triple_shot', 'rookie', 'Triple Shot', 2, 'multi_projectile', 3, '', 1.25),
	       ('heavy_bomb', 'tanko', 'Heavy Bomb', 3, 'single_large_bomb', 1, '', 1.1),
	       ('shock_field', 'spark', 'Shock Field', 3, 'debuff', 1, 'net', 1.3),
	       ('healing_bloom', 'flora', 'Healing Bloom', 3, 'heal', 1, 'heal', 0.0)`

func readMigration(t *testing.T, name string) string {
	t.Helper()
	_, self, _, _ := runtime.Caller(0)
	sql, err := os.ReadFile(filepath.Join(filepath.Dir(self), "..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(sql)
}

// Migrations 028, 030, 031 then 033 turn the old rows into configs/skills.yaml's (what the admin seed
// inserts on a fresh database), and reruns keep later admin edits.
func TestSkillRedesignMigrationMatchesSeed(t *testing.T) {
	ctx := context.Background()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql")
	if _, err := db.Pool.Exec(ctx, oldSkillRows); err != nil {
		t.Fatal(err)
	}
	up := readMigration(t, "028_skill_redesign.up.sql") + "\n" + readMigration(t, "030_rain_tune.up.sql") + "\n" + readMigration(t, "031_chain_cap.up.sql") + "\n" + readMigration(t, "033_chain_one_bomb_less.up.sql")
	for i := 0; i < 2; i++ {
		if _, err := db.Pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}

	_, self, _, _ := runtime.Caller(0)
	prev := gamedata.Data
	t.Cleanup(func() { gamedata.Data = prev })
	if err := gamedata.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
	for id, s := range gamedata.Data.Skills {
		var name, effect, status, desc string
		var count int
		var mult float64
		if err := db.Pool.QueryRow(ctx, `SELECT name, effect_type, projectile_count, status_effect_id, damage_multiplier, description
			FROM config_skills WHERE skill_id = $1`, id).Scan(&name, &effect, &count, &status, &mult, &desc); err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprint(name, effect, count, status, mult, desc)
		want := fmt.Sprint(s.Name, s.EffectType, s.ProjectileCount, s.StatusEffectID, s.DamageMultiplier, s.Description)
		if got != want {
			t.Errorf("%s: migrated %q, yaml %q", id, got, want)
		}
	}

	// An admin edit after the migration survives a rerun
	if _, err := db.Pool.Exec(ctx, `UPDATE config_skills SET name = 'Mưa lửa', damage_multiplier = 1.2 WHERE skill_id = 'triple_shot'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, up); err != nil {
		t.Fatal(err)
	}
	var name string
	var mult float64
	if err := db.Pool.QueryRow(ctx, `SELECT name, damage_multiplier FROM config_skills WHERE skill_id = 'triple_shot'`).Scan(&name, &mult); err != nil {
		t.Fatal(err)
	}
	if name != "Mưa lửa" || mult != 1.2 {
		t.Fatalf("admin edit lost: %q %v", name, mult)
	}

	// 033's down restores 031's chain row, 031's down 028's chain description, 030's down 028's
	// rain row, 028's down the first design
	if _, err := db.Pool.Exec(ctx, readMigration(t, "033_chain_one_bomb_less.down.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, readMigration(t, "031_chain_cap.down.sql")); err != nil {
		t.Fatal(err)
	}
	var chainDesc string
	if err := db.Pool.QueryRow(ctx, `SELECT description FROM config_skills WHERE skill_id = 'heavy_bomb'`).Scan(&chainDesc); err != nil {
		t.Fatal(err)
	}
	if chainDesc != "Bắn liên tiếp bom lớn cùng một đường bay (3 quả, +1 mỗi cấp súng), đào sâu một hố; tổng 180% sát thương (+15% mỗi cấp); địch trúng bị cầm chân lượt sau." {
		t.Fatalf("031 down: chain description %q", chainDesc)
	}
	if _, err := db.Pool.Exec(ctx, readMigration(t, "030_rain_tune.down.sql")); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, `SELECT projectile_count FROM config_skills WHERE skill_id = 'triple_shot'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 6 {
		t.Fatalf("030 down: rain count %d", count)
	}
	if _, err := db.Pool.Exec(ctx, readMigration(t, "028_skill_redesign.down.sql")); err != nil {
		t.Fatal(err)
	}
	var effect string
	if err := db.Pool.QueryRow(ctx, `SELECT effect_type FROM config_skills WHERE skill_id = 'shock_field'`).Scan(&effect); err != nil {
		t.Fatal(err)
	}
	if effect != "debuff" {
		t.Fatalf("down: shock_field effect %q", effect)
	}
}
