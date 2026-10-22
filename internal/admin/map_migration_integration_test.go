package admin

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/mapgen"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// mapSchema is config_maps / config_brick_types as the admin seed left them before 036: the 5
// default bricks (serial ids) and the 3 legacy maps (steel_base with no tiles).
func mapSchema(t *testing.T) *database.PostgresDB {
	t.Helper()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "007_map_editor.up.sql",
		"008_brick_border_v2.up.sql", "009_map_rank_tier.up.sql")
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO config_brick_types (name, image_id, destructible, color) VALUES
		('Dirt', '', true, '#8B4513'), ('Rock', '', false, '#808080'), ('Ice', '', true, '#87CEEB'),
		('Lava', '', false, '#FF4500'), ('Fragile', '', true, '#DEB887')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO config_maps (map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points, min_rank_tier) VALUES
		('grassland_valley', 'Grassland Valley', 1600, 900, '[0, 4]', '[]', '[{"X": 200, "Y": 400}, {"X": 1400, "Y": 400}]', 'bronze'),
		('frozen_peak', 'Frozen Peak', 1600, 900, '[1, 4]', '[]', '[{"X": 300, "Y": 350}, {"X": 1300, "Y": 350}]', 'silver'),
		('steel_base', 'Steel Base', 1600, 900, '[0, 2]', '[{"Type": "lava", "YRange": [700, 900], "Hardness": 90}]', '[{"X": 250, "Y": 300}, {"X": 1350, "Y": 300}]', 'gold')`); err != nil {
		t.Fatal(err)
	}
	return db
}

func execMigration(t *testing.T, db *database.PostgresDB, name string) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), readMigration(t, name)); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// storedMap reads a config_maps row back as the game loads it.
func storedMap(t *testing.T, db *database.PostgresDB, id string) gamedata.MapConfig {
	t.Helper()
	var m gamedata.MapConfig
	var wind, tiles, spawns []byte
	err := db.Pool.QueryRow(context.Background(), `SELECT map_id, name, theme, width, height, grid_width, grid_height, cell_size,
		default_wind_power_range, tiles, spawn_points, min_rank_tier, description FROM config_maps WHERE map_id = $1`, id).
		Scan(&m.MapID, &m.Name, &m.Theme, &m.Width, &m.Height, &m.GridWidth, &m.GridHeight, &m.CellSize, &wind, &tiles, &spawns, &m.MinRankTier, &m.Description)
	if err != nil {
		t.Fatalf("map %s: %v", id, err)
	}
	for _, x := range []struct {
		raw []byte
		v   any
	}{{wind, &m.DefaultWindPowerRange}, {tiles, &m.Tiles}, {spawns, &m.SpawnPoints}} {
		if err := json.Unmarshal(x.raw, x.v); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Tiles) == 0 {
		m.Tiles = nil
	}
	return m
}

// applyMaps runs the map migrations as cmd/migrate does: 036, the frozen 037 (100 x 56) and 038.
func applyMaps(t *testing.T, db *database.PostgresDB) {
	t.Helper()
	for _, f := range []string{"036_map_themes.up.sql", "037_battle_maps.up.sql", "038_wide_maps.up.sql"} {
		execMigration(t, db, f)
	}
}

// Migration 037 (frozen) inserts the maps at 100 x 56; 038 brings them to the generator's wide
// layouts, so 036 / 037 / 038 store the generated maps exactly as configs/maps.yaml has them, with
// the brick types of configs/bricks.yaml, replace the legacy steel_base, name the legacy maps in
// Vietnamese, and keep every row an admin edited on reruns; down / up round-trips.
func TestBattleMapMigrations(t *testing.T) {
	ctx := context.Background()
	db := mapSchema(t)
	execMigration(t, db, "036_map_themes.up.sql")
	execMigration(t, db, "037_battle_maps.up.sql")
	var steel037 string
	var narrow int
	db.Pool.QueryRow(ctx, `SELECT tiles::text FROM config_maps WHERE map_id = 'steel_base'`).Scan(&steel037)
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_maps WHERE grid_width = 100 AND grid_height = 56 AND jsonb_array_length(tiles) = 56`).Scan(&narrow)
	if narrow != len(mapgen.Defs) {
		t.Fatalf("037 left %d maps at 100 x 56, want %d", narrow, len(mapgen.Defs))
	}
	var before time.Time
	db.Pool.QueryRow(ctx, `SELECT updated_at FROM config_maps WHERE map_id = 'coconut_isles'`).Scan(&before)
	for i := 0; i < 2; i++ {
		applyMaps(t, db)
	}

	for _, want := range mapgen.Maps() {
		got := storedMap(t, db, want.MapID)
		if got.Width != 2400 || got.Height != 1120 || !reflect.DeepEqual(got, want) {
			t.Errorf("map %s stored differently from the generator (%dx%d)", want.MapID, got.Width, got.Height)
		}
	}
	var after time.Time
	db.Pool.QueryRow(ctx, `SELECT updated_at FROM config_maps WHERE map_id = 'coconut_isles'`).Scan(&after)
	if !after.After(before) {
		t.Fatalf("038 kept updated_at %v (GET /maps ETag would not change)", after)
	}
	var n int
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_maps`).Scan(&n)
	if n != 18 {
		t.Fatalf("%d maps, want 18", n)
	}
	if m := storedMap(t, db, "grassland_valley"); m.Name != "Thung lũng cỏ" || m.Theme != "grassland" || m.Tiles != nil || m.Width != 1600 {
		t.Fatalf("grassland_valley %q %q %d", m.Name, m.Theme, m.Width)
	}
	if m := storedMap(t, db, "frozen_peak"); m.Name != "Đỉnh băng" || m.Theme != "grassland" || m.Width != 1600 {
		t.Fatalf("frozen_peak %q %q %d", m.Name, m.Theme, m.Width)
	}

	// brick types: as bricks.yaml (ids 1..19), the sequence past them
	repo := NewRepository(db, nil)
	bricks, err := repo.GetBrickTypes(ctx)
	if err != nil || len(bricks) != mapgen.LastBrickID {
		t.Fatalf("%d brick types, %v", len(bricks), err)
	}
	want := mapgen.BrickTypeMap()
	for _, b := range bricks {
		var border gamedata.BrickBorder
		json.Unmarshal(b.Border, &border)
		w := want[b.BrickTypeID]
		if b.Name != w.Name || b.Destructible != w.Destructible || b.Color != w.Color || !reflect.DeepEqual(border, w.Border) {
			t.Errorf("brick %d: %+v", b.BrickTypeID, b)
		}
	}
	id, err := repo.InsertBrickType(ctx, &ConfigBrickType{Name: "New", Border: json.RawMessage(`{}`), Color: "#000"})
	if err != nil || id != mapgen.LastBrickID+1 {
		t.Fatalf("next brick id %d, %v", id, err)
	}

	// Every stored map passes the editor's save checks with the stored brick types.
	types := map[int]gamedata.BrickTypeConfig{}
	for _, b := range bricks {
		bt := gamedata.BrickTypeConfig{BrickTypeID: b.BrickTypeID, Destructible: b.Destructible}
		json.Unmarshal(b.Border, &bt.Border)
		types[b.BrickTypeID] = bt
	}
	for _, d := range mapgen.Defs {
		if err := gamedata.ValidateMap(storedMap(t, db, d.ID), types); err != nil {
			t.Error(err)
		}
	}

	// The editor saves without a theme keep it; with one, it changes. Edits survive reruns.
	ice, err := repo.GetMap(ctx, "ice_bridge")
	if err != nil || ice.Theme != "ice" {
		t.Fatalf("GetMap %v %v", ice, err)
	}
	ice.Name, ice.Theme, ice.Tiles = "Cầu băng 2", "", json.RawMessage(`[[1]]`)
	if err := repo.UpsertMap(ctx, ice); err != nil {
		t.Fatal(err)
	}
	if m, _ := repo.GetMap(ctx, "ice_bridge"); m.Theme != "ice" || m.Name != "Cầu băng 2" {
		t.Fatalf("theme lost on save: %q", m.Theme)
	}
	ice.Theme = "sky"
	repo.UpsertMap(ctx, ice)
	if _, err := db.Pool.Exec(ctx, `UPDATE config_maps SET tiles = '[[2]]' WHERE map_id = 'steel_base'`); err != nil {
		t.Fatal(err)
	}
	applyMaps(t, db)
	if m := storedMap(t, db, "ice_bridge"); m.Name != "Cầu băng 2" || m.Theme != "sky" || len(m.Tiles) != 1 {
		t.Fatal("ice_bridge edit lost on rerun")
	}
	if m := storedMap(t, db, "steel_base"); len(m.Tiles) != 1 {
		t.Fatal("steel_base edit lost on rerun")
	}

	// 038's down restores nothing
	execMigration(t, db, "038_wide_maps.down.sql")
	if m := storedMap(t, db, "pyramid"); m.GridWidth != mapgen.GridW || len(m.Tiles) != mapgen.GridH {
		t.Fatalf("038 down changed pyramid: %d x %d", m.GridWidth, len(m.Tiles))
	}

	// 037 / 036 down (steel_base as 037 left it): the new maps and shapes go, steel_base is legacy
	// again, no theme column; then up again
	if _, err := db.Pool.Exec(ctx, `UPDATE config_maps SET tiles = $1::jsonb, grid_width = 100, grid_height = 56 WHERE map_id = 'steel_base'`, steel037); err != nil {
		t.Fatal(err)
	}
	execMigration(t, db, "037_battle_maps.down.sql")
	execMigration(t, db, "036_map_themes.down.sql")
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_maps`).Scan(&n)
	var steelTiles, steelName string
	db.Pool.QueryRow(ctx, `SELECT tiles::text, name FROM config_maps WHERE map_id = 'steel_base'`).Scan(&steelTiles, &steelName)
	var gvName string
	db.Pool.QueryRow(ctx, `SELECT name FROM config_maps WHERE map_id = 'grassland_valley'`).Scan(&gvName)
	var hasTheme bool
	db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'config_maps' AND column_name = 'theme' AND table_schema = current_schema())`).Scan(&hasTheme)
	var shapes int
	db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_brick_types WHERE brick_type_id BETWEEN 6 AND 19`).Scan(&shapes)
	if n != 3 || steelTiles != "[]" || steelName != "Steel Base" || gvName != "Grassland Valley" || hasTheme || shapes != 0 {
		t.Fatalf("down: %d maps, steel_base %s %q, %q, theme %v, %d shapes", n, steelTiles, steelName, gvName, hasTheme, shapes)
	}
	applyMaps(t, db)
	if m := storedMap(t, db, "steel_base"); m.Theme != "steel" || m.GridWidth != mapgen.GridW || len(m.Tiles) != mapgen.GridH {
		t.Fatal("up after down: steel_base not replaced")
	}
}

// 038 leaves a row that is no longer 037's: an admin who resized a map, or changed its theme
// while it was still 100 cells wide, keeps it.
func TestWideMapsMigrationKeepsEditedRows(t *testing.T) {
	ctx := context.Background()
	db := mapSchema(t)
	execMigration(t, db, "036_map_themes.up.sql")
	execMigration(t, db, "037_battle_maps.up.sql")
	if _, err := db.Pool.Exec(ctx, `UPDATE config_maps SET theme = 'stone' WHERE map_id = 'graveyard'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE config_maps SET grid_width = 2, grid_height = 1, tiles = '[[1, 1]]' WHERE map_id = 'escape'`); err != nil {
		t.Fatal(err)
	}
	execMigration(t, db, "038_wide_maps.up.sql")
	if m := storedMap(t, db, "graveyard"); m.GridWidth != 100 || m.Theme != "stone" {
		t.Fatalf("graveyard widened over a theme edit: %d, %q", m.GridWidth, m.Theme)
	}
	if m := storedMap(t, db, "escape"); m.GridWidth != 2 || len(m.Tiles) != 1 {
		t.Fatalf("escape resize lost: %d", m.GridWidth)
	}
	if m := storedMap(t, db, "pyramid"); m.GridWidth != mapgen.GridW {
		t.Fatalf("pyramid not widened: %d", m.GridWidth)
	}
}

// A steel_base someone already drew in the map editor is not replaced.
func TestBattleMapMigrationKeepsEditorSteelBase(t *testing.T) {
	db := mapSchema(t)
	if _, err := db.Pool.Exec(context.Background(), `UPDATE config_maps SET tiles = '[[1, 1]]', grid_width = 2, grid_height = 1 WHERE map_id = 'steel_base'`); err != nil {
		t.Fatal(err)
	}
	applyMaps(t, db)
	if m := storedMap(t, db, "steel_base"); len(m.Tiles) != 1 || m.Theme != "grassland" {
		t.Fatalf("editor steel_base replaced: %d rows, theme %q", len(m.Tiles), m.Theme)
	}
}
