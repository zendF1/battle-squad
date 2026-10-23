package migrate_test

import (
	"context"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"battle-squad/configs"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/auth"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/migrate"
	"battle-squad/migrations"
)

func opts() migrate.Options {
	return migrate.Options{Migrations: migrations.FS, Configs: configs.FS}
}

func names(t *testing.T) []string {
	t.Helper()
	migs, err := migrate.Load(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	return migrate.Names(migs)
}

func run(t *testing.T, db *database.PostgresDB, o migrate.Options) migrate.Result {
	t.Helper()
	res, err := migrate.Run(context.Background(), db.Pool, o)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// referenceData is every reference table's rows (config_*, game_settings, missions, shop,
// IAP products), timestamps left out, sorted.
func referenceData(t *testing.T, db *database.PostgresDB) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := db.Pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = current_schema()
		AND (tablename LIKE 'config\_%' OR tablename IN ('game_settings', 'missions', 'shop_offers', 'iap_products'))`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, n)
	}
	rows.Close()
	out := map[string]string{}
	for _, tbl := range tables {
		var v string
		if err := db.Pool.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(x ORDER BY x::text), '[]')::text FROM (
			SELECT to_jsonb(t) - ARRAY['created_at', 'updated_at'] AS x FROM `+tbl+` t) s`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		out[tbl] = v
	}
	return out
}

func count(t *testing.T, db *database.PostgresDB, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func revision(t *testing.T, db *database.PostgresDB) int {
	return count(t, db, `SELECT revision FROM gamedata_revision WHERE id = 1`)
}

// cmd/migrate alone turns an empty database into the complete, current game data; a second
// run changes nothing.
func TestFreshDatabase(t *testing.T) {
	db := dbtest.NewSchema(t, 0)
	ctx := context.Background()
	res := run(t, db, opts())
	if !reflect.DeepEqual(res.Applied, names(t)) || res.Seeded == 0 || res.Admin != migrate.AdminMissing {
		t.Fatalf("first run: %+v", res)
	}

	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	yaml, err := gamedata.LoadYAMLFS(configs.FS)
	if err != nil {
		t.Fatal(err)
	}
	d := snap.Data
	if len(d.Characters) != 5 {
		t.Fatalf("characters: %d", len(d.Characters))
	}
	for _, id := range []string{"rookie", "tanko", "spark", "flora", "tide"} {
		c, ok := d.Characters[id]
		if !ok || c.Element == "" {
			t.Fatalf("character %s: %+v", id, c)
		}
		if _, ok := d.Weapons[c.WeaponID]; !ok {
			t.Fatalf("%s: weapon %s missing", id, c.WeaponID)
		}
		if _, ok := d.Skills[c.SkillID]; !ok {
			t.Fatalf("%s: skill %s missing", id, c.SkillID)
		}
		if !reflect.DeepEqual(c, yaml.Data.Characters[id]) || !reflect.DeepEqual(d.Weapons[c.WeaponID], yaml.Data.Weapons[c.WeaponID]) ||
			!reflect.DeepEqual(d.Skills[c.SkillID], yaml.Data.Skills[c.SkillID]) {
			t.Fatalf("%s differs from configs/*.yaml:\n%+v\n%+v", id, c, yaml.Data.Characters[id])
		}
	}
	if len(d.Items) != len(yaml.Data.Items) || len(d.Items) == 0 {
		t.Fatalf("items: %d, yaml %d", len(d.Items), len(yaml.Data.Items))
	}
	if len(d.Maps) < len(yaml.Data.Maps) {
		t.Fatalf("maps: %d, yaml %d", len(d.Maps), len(yaml.Data.Maps))
	}
	if len(snap.Bosses) != 3 || len(snap.Elements) != 5 || len(snap.Modes) == 0 || len(snap.GunKinds) < 5 {
		t.Fatalf("bosses %d elements %d modes %d gun kinds %d", len(snap.Bosses), len(snap.Elements), len(snap.Modes), len(snap.GunKinds))
	}
	if !reflect.DeepEqual(snap.Rules, gamedata.DefaultRules()) || !reflect.DeepEqual(*snap.Physics, *gamedata.DefaultPhysics()) {
		t.Fatalf("rules / physics are not the defaults:\n%+v\n%+v", snap.Rules, snap.Physics)
	}
	for table, min := range map[string]int{"shop_offers": 1, "iap_products": 3, "missions": 1, "config_titles": 1,
		"config_equipment_items": 1, "config_upgrade_rates": 16, "config_stones": 12, "config_gems": 40, "config_crafting_recipes": 24,
		"config_rank_tiers": 6, "config_status_effects": 1, "config_tips": 1} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+table); n < min {
			t.Errorf("%s: %d rows, want >= %d", table, n, min)
		}
	}
	// Every character's guns are sold (a player can buy Tide's)
	if n := count(t, db, `SELECT COUNT(DISTINCT character_id) FROM config_equipment_items WHERE slot = 'weapon' AND is_active AND character_id IS NOT NULL`); n != 5 {
		t.Fatalf("characters with guns: %d", n)
	}
	// No player data
	for _, table := range []string{"accounts", "player_profiles", "player_characters", "gift_codes", "match_histories", "admin_users"} {
		if n := count(t, db, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Errorf("%s: %d rows on a fresh database", table, n)
		}
	}

	// A second run is a no-op
	before, rev := referenceData(t, db), revision(t, db)
	res = run(t, db, opts())
	if len(res.Applied) != 0 || res.Seeded != 0 || res.Admin != migrate.AdminMissing {
		t.Fatalf("second run: %+v", res)
	}
	if !reflect.DeepEqual(before, referenceData(t, db)) || revision(t, db) != rev {
		t.Fatal("a second run changed the data")
	}
	plan, err := migrate.Inspect(ctx, db.Pool, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(migrate.Pending(plan.States)) != 0 || plan.SeedMissing != 0 || !plan.NoAdmin {
		t.Fatalf("plan after the runs: %+v", plan)
	}
}

// A database migrated the old way (every file, then the admin seed, no tracking table) gets
// every file recorded and keeps its data, admin edits included.
func TestExistingDatabaseBootstrap(t *testing.T) {
	all := names(t)
	db := dbtest.NewSchema(t, 0, all...)
	ctx := context.Background()
	if _, err := migrate.SeedAll(ctx, db.Pool, configs.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE config_characters SET hp = 1234 WHERE character_id = 'rookie'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE config_tips SET text = 'Mẹo của admin' WHERE tip_id = 'tip_01'`); err != nil {
		t.Fatal(err)
	}
	before := referenceData(t, db)

	res := run(t, db, opts())
	if !reflect.DeepEqual(res.Applied, all) || res.Seeded != 0 {
		t.Fatalf("bootstrap run: applied %d seeded %d", len(res.Applied), res.Seeded)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schema_migrations`); n != len(all) {
		t.Fatalf("recorded %d of %d files", n, len(all))
	}
	if after := referenceData(t, db); !reflect.DeepEqual(before, after) {
		for k := range before {
			if before[k] != after[k] {
				t.Errorf("%s changed", k)
			}
		}
	}
	if res := run(t, db, opts()); len(res.Applied) != 0 || res.Seeded != 0 {
		t.Fatalf("second run: %+v", res)
	}
}

// changedFS is migrations.FS with one file's content replaced (and optionally a new file).
func changedFS(t *testing.T, name, sql string, extra map[string]string) fs.FS {
	t.Helper()
	m := fstest.MapFS{}
	for _, n := range names(t) {
		raw, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatal(err)
		}
		m[n] = &fstest.MapFile{Data: raw}
	}
	if name != "" {
		m[name] = &fstest.MapFile{Data: []byte(sql)}
	}
	for n, s := range extra {
		m[n] = &fstest.MapFile{Data: []byte(s)}
	}
	return m
}

// An applied file whose content changed is refused (nothing applied, not even a new file)
// unless -allow-changed, which records the new checksum without rerunning it; a new file
// runs once.
func TestChangedMigrationRefused(t *testing.T) {
	db := dbtest.NewSchema(t, 0)
	ctx := context.Background()
	run(t, db, opts())

	o := opts()
	o.Migrations = changedFS(t, "002_add_account_role.up.sql", "-- edited\nSELECT 1;", map[string]string{
		"999_test_new.up.sql": "CREATE TABLE IF NOT EXISTS test_new_table (id INT);"})
	_, err := migrate.Run(ctx, db.Pool, o)
	if err == nil || !strings.Contains(err.Error(), "002_add_account_role.up.sql") {
		t.Fatalf("changed file accepted: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE filename = '999_test_new.up.sql'`); n != 0 {
		t.Fatal("a new file ran despite the refusal")
	}

	o.AllowChanged = true
	res, err := migrate.Run(ctx, db.Pool, o)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Applied, []string{"999_test_new.up.sql"}) {
		t.Fatalf("applied %v", res.Applied)
	}
	migs, _ := migrate.Load(o.Migrations)
	var sum string
	for _, m := range migs {
		if m.Name == "002_add_account_role.up.sql" {
			sum = m.Checksum
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM schema_migrations WHERE filename = '002_add_account_role.up.sql' AND checksum = $1`, sum); n != 1 {
		t.Fatal("the new checksum is not recorded")
	}
	o.AllowChanged = false
	if res := run(t, db, o); len(res.Applied) != 0 {
		t.Fatalf("rerun applied %v", res.Applied)
	}
}

// The bootstrap creates exactly one admin with a bcrypt hash, never changes it afterwards,
// and refuses half or weak credentials.
func TestAdminBootstrap(t *testing.T) {
	db := dbtest.NewSchema(t, 0)
	ctx := context.Background()
	o := opts()
	o.AdminEmail = "x@example.com"
	if _, err := migrate.Run(ctx, db.Pool, o); err == nil {
		t.Fatal("an email without a password was accepted")
	}
	o.AdminPassword = "short"
	if _, err := migrate.Run(ctx, db.Pool, o); err == nil {
		t.Fatal("a short password was accepted")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM admin_users`); n != 0 {
		t.Fatalf("%d admins after refused runs", n)
	}

	o.AdminEmail, o.AdminPassword = "  Ops@Example.COM ", "a-long-enough-secret"
	if res := run(t, db, o); res.Admin != migrate.AdminCreated {
		t.Fatalf("admin: %s", res.Admin)
	}
	o.AdminEmail, o.AdminPassword = "other@example.com", "another-long-secret"
	if res := run(t, db, o); res.Admin != migrate.AdminExists {
		t.Fatalf("rerun admin: %s", res.Admin)
	}
	var email, hash string
	if err := db.Pool.QueryRow(ctx, `SELECT email, password_hash FROM admin_users`).Scan(&email, &hash); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM admin_users`); n != 1 {
		t.Fatalf("%d admins", n)
	}
	if email != "ops@example.com" || !auth.CheckAdminPassword(hash, "a-long-enough-secret") || strings.Contains(hash, "secret") {
		t.Fatalf("admin row: %s %s", email, hash)
	}
}
