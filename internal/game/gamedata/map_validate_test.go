package gamedata

import "testing"

func validTileMap() MapConfig {
	return MapConfig{
		MapID: "m", GridWidth: 2, GridHeight: 2, CellSize: 16,
		Tiles:                 [][]int{{0, 0}, {1, 1}},
		SpawnPoints:           []SpawnPoint{{X: 8, Y: 8}},
		DefaultWindPowerRange: []float64{0, 4},
	}
}

func TestValidateMap(t *testing.T) {
	bricks := map[int]BrickTypeConfig{1: {BrickTypeID: 1}}

	cases := []struct {
		name    string
		mutate  func(m *MapConfig)
		bricks  map[int]BrickTypeConfig
		wantErr bool
	}{
		{"valid tile map", func(m *MapConfig) {}, bricks, false},
		{"valid legacy map", func(m *MapConfig) {
			m.Tiles = nil
			m.Width, m.Height = 1600, 900
			m.SpawnPoints = []SpawnPoint{{X: 1200, Y: 500}}
		}, bricks, false},
		{"extra tile row", func(m *MapConfig) { m.Tiles = append(m.Tiles, []int{1, 1}) }, bricks, true},
		{"short tile row", func(m *MapConfig) { m.Tiles[1] = []int{1} }, bricks, true},
		{"negative grid", func(m *MapConfig) { m.GridHeight = -2 }, bricks, true},
		{"zero cell size", func(m *MapConfig) { m.CellSize = 0 }, bricks, true},
		{"too large", func(m *MapConfig) { m.GridWidth = 100000 }, bricks, true},
		{"unknown brick", func(m *MapConfig) { m.Tiles[1][0] = 9 }, bricks, true},
		{"unknown brick unchecked without types", func(m *MapConfig) { m.Tiles[1][0] = 9 }, nil, false},
		{"negative brick", func(m *MapConfig) { m.Tiles[1][0] = -1 }, nil, true},
		{"spawn outside", func(m *MapConfig) { m.SpawnPoints[0].Y = 32 }, bricks, true},
		{"wind min above max", func(m *MapConfig) { m.DefaultWindPowerRange = []float64{3, 1} }, bricks, true},
		{"wind single value", func(m *MapConfig) { m.DefaultWindPowerRange = []float64{3} }, bricks, true},
		{"legacy negative size", func(m *MapConfig) { m.Tiles = nil; m.Width = -5 }, bricks, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validTileMap()
			tc.mutate(&m)
			err := ValidateMap(m, tc.bricks)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateMap() err = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidateMapEmptyFirstRowIsTileMode(t *testing.T) {
	m := validTileMap()
	m.Width, m.Height = 32, 32
	m.Tiles = [][]int{{}, {1, 1}}
	if err := ValidateMap(m, nil); err == nil {
		t.Error("expected error for tile matrix with an empty first row")
	}
}

func TestMapPixelSize(t *testing.T) {
	cases := []struct {
		name string
		m    MapConfig
		w, h int
	}{
		{"tiles", MapConfig{GridWidth: 10, GridHeight: 5, CellSize: 16, Tiles: [][]int{{}}, Width: 1600, Height: 900}, 160, 80},
		{"legacy", MapConfig{GridWidth: 10, GridHeight: 5, CellSize: 16, Width: 1600, Height: 900}, 1600, 900},
		{"legacy from grid", MapConfig{GridWidth: 10, GridHeight: 5, CellSize: 16}, 160, 80},
	}
	for _, tc := range cases {
		if w, h := tc.m.PixelSize(); w != tc.w || h != tc.h {
			t.Errorf("%s: PixelSize() = %dx%d, want %dx%d", tc.name, w, h, tc.w, tc.h)
		}
	}
}

func TestTeamSpawns(t *testing.T) {
	m := MapConfig{Width: 640, Height: 400, SpawnPoints: []SpawnPoint{{X: 10}, {X: 20}, {X: 30}, {X: 40}}}
	t1, t2 := m.TeamSpawns()
	if len(t1) != 2 || len(t2) != 2 || t1[0].X != 10 || t2[0].X != 30 {
		t.Errorf("split = %v / %v", t1, t2)
	}

	// No spawns: fallbacks sit inside the map, one near each side.
	m.SpawnPoints = nil
	t1, t2 = m.TeamSpawns()
	if len(t1) != 1 || len(t2) != 1 || t1[0].X != 80 || t2[0].X != 560 {
		t.Errorf("fallback = %v / %v, want X 80 / 560", t1, t2)
	}

	// One spawn: team 2 gets it, team 1 falls back.
	m.SpawnPoints = []SpawnPoint{{X: 300, Y: 50}}
	t1, t2 = m.TeamSpawns()
	if t1[0].X != 80 || t2[0].X != 300 {
		t.Errorf("single spawn = %v / %v", t1, t2)
	}
}

func TestGameDataValidateSkipsInvalidMaps(t *testing.T) {
	good := validTileMap()
	good.MapID = "good"
	good.Tiles[1][0] = 99 // unknown brick: tolerated at load, NewTerrain fills it as a square
	bad := validTileMap()
	bad.MapID = "bad"
	bad.GridHeight = 7

	gd := &GameData{Maps: map[string]MapConfig{"good": good, "bad": bad}}
	if err := gd.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
	if _, ok := gd.Maps["good"]; !ok {
		t.Error("expected valid map to be kept")
	}
	if _, ok := gd.Maps["bad"]; ok {
		t.Error("expected invalid map to be dropped")
	}
}

func TestMapThemeAndDisplay(t *testing.T) {
	if (MapConfig{}).ThemeOrDefault() != "grassland" || (MapConfig{Theme: "lava_world"}).ThemeOrDefault() != "grassland" ||
		(MapConfig{Theme: "city_night"}).ThemeOrDefault() != "city_night" {
		t.Fatal("ThemeOrDefault")
	}
	UseForTest(t, NewSnapshot(&GameData{Maps: map[string]MapConfig{"ice_bridge": {MapID: "ice_bridge", Name: "Cầu băng", Theme: "ice"}, "x": {MapID: "x"}}}))
	if n, th := MapDisplay("ice_bridge"); n != "Cầu băng" || th != "ice" {
		t.Fatalf("ice_bridge: %q %q", n, th)
	}
	if n, th := MapDisplay("x"); n != "x" || th != "grassland" {
		t.Fatalf("unnamed: %q %q", n, th)
	}
	if n, th := MapDisplay("missing"); n != "missing" || th != "grassland" {
		t.Fatalf("missing: %q %q", n, th)
	}
}

// configs/bricks.yaml loads as the brick types (the YAML mirror of config_brick_types), and
// the maps keep their theme.
func TestLoadGameDataReadsBricksAndThemes(t *testing.T) {
	snap, err := LoadYAML("../../../configs")
	if err != nil {
		t.Fatal(err)
	}
	Data, BrickTypes := snap.Data, snap.Bricks
	if b, ok := BrickTypes[2]; !ok || b.Destructible || len(BrickTypes) < 19 {
		t.Fatalf("brick types %v", BrickTypes)
	}
	if b := BrickTypes[6]; !b.Destructible || len(b.Border.Top) != 2 {
		t.Fatalf("brick 6 %+v", b)
	}
	if m := Data.Maps["ice_bridge"]; m.Theme != "ice" || m.Name != "Cầu băng" || !m.HasTiles() {
		t.Fatalf("ice_bridge %+v", m.MapID)
	}
	if m := Data.Maps["grassland_valley"]; m.ThemeOrDefault() != "grassland" || m.HasTiles() {
		t.Fatal("grassland_valley")
	}
	if len(Data.Maps) != 24 { // 2 legacy, 16 battle maps, 3 phase 1 + 2 v2 + 1 v3 boss arenas
		t.Fatalf("%d maps, want 24", len(Data.Maps))
	}
	for _, b := range snap.Bosses {
		if !snap.IsBossArena(b.MapID) || Data.Maps[b.MapID].MapID != b.MapID {
			t.Fatalf("boss %s arena %s not loaded", b.BossID, b.MapID)
		}
	}
}
