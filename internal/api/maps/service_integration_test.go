package maps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"battle-squad/internal/game/mapgen"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"

	"github.com/go-chi/chi/v5"
)

func mapsDB(t *testing.T) *Handler {
	t.Helper()
	db := dbtest.NewSchema(t, 0, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "007_map_editor.up.sql",
		"008_brick_border_v2.up.sql", "009_map_rank_tier.up.sql")
	if _, err := db.Pool.Exec(context.Background(), `INSERT INTO config_maps (map_id, name, width, height, default_wind_power_range, terrain_layers, spawn_points)
		VALUES ('grassland_valley', 'Grassland Valley', 1600, 900, '[0, 4]', '[]', '[{"X": 200, "Y": 400}, {"X": 1400, "Y": 400}]')`); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"036_map_themes.up.sql", "037_battle_maps.up.sql"} {
		if _, err := db.Pool.Exec(context.Background(), readFile(t, f)); err != nil {
			t.Fatal(err)
		}
	}
	return NewHandler(NewService(NewRepository(db)))
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	for path, content := range mapgen.Files() {
		if path == "migrations/"+name {
			return content
		}
	}
	t.Fatalf("no generated %s", name)
	return ""
}

func serve(h *Handler, req *http.Request) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Get("/maps", h.List)
	r.Get("/maps/{mapId}", h.Get)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// GET /maps lists every map with its name, theme, size and version, and the brick types; an
// unchanged list answers If-None-Match with 304. GET /maps/{id} adds the tiles and spawns.
func TestMapsEndpoints(t *testing.T) {
	h := mapsDB(t)
	rec := serve(h, httptest.NewRequest("GET", "/maps", nil))
	if rec.Code != 200 {
		t.Fatalf("GET /maps: %d %s", rec.Code, rec.Body)
	}
	var env struct {
		Data MapList `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	list := env.Data
	if len(list.Maps) != 17 || len(list.BrickTypes) != 19 || list.Version == "" || rec.Header().Get("ETag") != `"`+list.Version+`"` {
		t.Fatalf("%d maps, %d bricks, version %q, etag %q", len(list.Maps), len(list.BrickTypes), list.Version, rec.Header().Get("ETag"))
	}
	byID := map[string]Map{}
	for _, m := range list.Maps {
		byID[m.MapID] = m
	}
	ice := byID["ice_bridge"]
	if ice.Name != "Cầu băng" || ice.Theme != "ice" || ice.Width != 1600 || ice.Height != 896 || ice.CellSize != 16 ||
		ice.GridWidth != 100 || ice.GridHeight != 56 || ice.MinRankTier != "bronze" || ice.WindRange != [2]float64{1.5, 4} ||
		!ice.HasTiles || ice.Version <= 0 {
		t.Fatalf("ice_bridge %+v", ice)
	}
	if gv := byID["grassland_valley"]; gv.Name != "Thung lũng cỏ" || gv.Theme != "grassland" || gv.HasTiles || gv.Width != 1600 || gv.Height != 900 {
		t.Fatalf("grassland_valley %+v", gv)
	}
	if b := list.BrickTypes[1]; b.BrickTypeID != 2 || b.Destructible || b.Name != "Rock" {
		t.Fatalf("brick 2 %+v", b)
	}

	req := httptest.NewRequest("GET", "/maps", nil)
	req.Header.Set("If-None-Match", `"`+list.Version+`"`)
	if rec := serve(h, req); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Fatalf("If-None-Match: %d", rec.Code)
	}

	rec = serve(h, httptest.NewRequest("GET", "/maps/pyramid", nil))
	var detail struct {
		Data MapDetail `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || rec.Code != 200 {
		t.Fatalf("GET /maps/pyramid: %d %v", rec.Code, err)
	}
	var want mapgen.Def
	for _, d := range mapgen.Defs {
		if d.ID == "pyramid" {
			want = d
		}
	}
	wm := want.Map()
	if !reflect.DeepEqual(detail.Data.Tiles, wm.Tiles) || len(detail.Data.SpawnPoints) != 4 ||
		detail.Data.SpawnPoints[0].X != wm.SpawnPoints[0].X || detail.Data.Theme != "desert" {
		t.Fatalf("pyramid detail %+v", detail.Data.Map)
	}
	rec = serve(h, httptest.NewRequest("GET", "/maps/grassland_valley", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || len(detail.Data.Tiles) != 0 || detail.Data.SpawnPoints[1].X != 1400 {
		t.Fatalf("legacy detail %+v %v", detail.Data, err)
	}

	rec = serve(h, httptest.NewRequest("GET", "/maps/nope", nil))
	if rec.Code != 404 || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("missing map: %d", rec.Code)
	}
	var appErr model.AppError
	if _, err := h.service.Get(context.Background(), "nope"); !errors.As(err, &appErr) || appErr.Code != model.ErrMapNotFound.Code {
		t.Fatalf("missing map error %v", err)
	}

	// An edit changes the map's and the list's version.
	if _, err := h.service.repo.db.Pool.Exec(context.Background(), `UPDATE config_maps SET theme = 'sky', updated_at = updated_at + interval '1 second' WHERE map_id = 'ice_bridge'`); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.List(context.Background())
	if err != nil || after.Version == list.Version {
		t.Fatalf("version unchanged after an edit: %v", err)
	}
	for _, m := range after.Maps {
		if m.MapID == "ice_bridge" && (m.Theme != "sky" || m.Version <= ice.Version) {
			t.Fatalf("edited ice_bridge %+v", m)
		}
	}
}
