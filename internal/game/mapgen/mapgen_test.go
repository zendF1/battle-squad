package mapgen

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func repoRoot() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "..")
}

// The migrations and configs/*.yaml are what the generator writes now: a layout edit needs
// `go run ./cmd/mapgen`.
func TestGeneratedFilesUpToDate(t *testing.T) {
	for path, want := range Files() {
		got, err := os.ReadFile(filepath.Join(repoRoot(), path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s is out of date: run go run ./cmd/mapgen", path)
		}
	}
}

// 047 (the boss v2 arenas) is frozen as applied: 047 + 049 is what ArenasV2 renders now, 049
// moving the mound's minion spawns from 047's (only while the row still has them).
func TestArenasV2Migrations(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(filepath.Join(repoRoot(), path))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	mound := ArenasV2[0]
	if mound.ID != "arena_lava_crab_mound" {
		t.Fatalf("ArenasV2[0] is %s", mound.ID)
	}
	old := mound
	old.Minions = [][2]int{{67, CrabMoundTop}, {82, CrabMoundTop}, {54, crabGround}, {95, crabGround}} // 047 (f1e239f)
	oldJSON, newJSON := sqlString(SpawnJSON(old.Map().SpawnPoints)), sqlString(SpawnJSON(mound.Map().SpawnPoints))
	frozen := read(ArenasV2UpFile)
	if !strings.Contains(frozen, oldJSON) || strings.Replace(frozen, oldJSON, newJSON, 1) != arenasV2UpSQL() {
		t.Fatalf("%s + 049 differ from ArenasV2", ArenasV2UpFile)
	}
	fix := read(ArenaSpawnsFixFile)
	if !strings.Contains(fix, "SET spawn_points = "+newJSON+"\n") || !strings.Contains(fix, "AND spawn_points = "+oldJSON+"::jsonb;") {
		t.Fatalf("%s does not move %s to %s", ArenaSpawnsFixFile, oldJSON, newJSON)
	}
}

func TestBrickIDs(t *testing.T) {
	seen := map[int]bool{}
	for _, b := range BrickTypes() {
		if seen[b.BrickTypeID] {
			t.Fatalf("brick id %d twice", b.BrickTypeID)
		}
		seen[b.BrickTypeID] = true
	}
	if len(seen) != LastBrickID || LastBrickID != 19 {
		t.Fatalf("%d brick types, last id %d", len(seen), LastBrickID)
	}
	for _, m := range []Material{Soft, Hard} {
		for s := Full; s < numShapes; s++ {
			id := BrickID(m, s)
			gm, gs := brickOf(id)
			if gm != m || gs != s {
				t.Errorf("brick %d: %v/%v, want %v/%v", id, gm, gs, m, s)
			}
			if want := m == Soft; BrickTypeMap()[id].Destructible != want {
				t.Errorf("brick %d destructible %v", id, !want)
			}
		}
	}
}

// Both teams get the same terrain: every map is its own mirror image.
func TestMapsAreMirrored(t *testing.T) {
	flip := map[Shape]Shape{SlopeR: SlopeL, SlopeL: SlopeR, CeilR: CeilL, CeilL: CeilR}
	for _, d := range Defs {
		tiles := d.Map().Tiles
		for r := range tiles {
			for c := 0; c < GridW/2; c++ {
				a, b := tiles[r][c], tiles[r][GridW-1-c]
				if (a == 0) != (b == 0) {
					t.Fatalf("%s: row %d col %d not mirrored", d.ID, r, c)
				}
				if a == 0 {
					continue
				}
				am, as := brickOf(a)
				bm, bs := brickOf(b)
				if f, ok := flip[as]; ok {
					as = f
				}
				if am != bm || as != bs {
					t.Fatalf("%s: row %d col %d: %d vs %d", d.ID, r, c, a, b)
				}
			}
		}
	}
}

func TestChamferIsland(t *testing.T) {
	g := &Grid{}
	g.Chamfer(0, 0, 5, 3, 1, 3, Soft)
	want := []string{
		"/####\\",
		"b####d",
		".b##d.",
		"..bd..",
	}
	sym := map[Shape]byte{Full: '#', SlopeR: '/', SlopeL: '\\', CeilR: 'b', CeilL: 'd'}
	for r, row := range want {
		for c := range row {
			got := byte('.')
			if x := g.cells[r][c]; x.solid {
				got = sym[x.shape]
			}
			if got != row[c] {
				t.Fatalf("row %d col %d: %q, want %q", r, c, got, row[c])
			}
		}
	}
}
