package mapgen

import (
	"os"
	"path/filepath"
	"runtime"
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
