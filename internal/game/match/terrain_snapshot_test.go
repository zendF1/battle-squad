package match

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// decodeMaskRLE mirrors the client decoder: alternating air/solid run lengths as uvarints.
func decodeMaskRLE(t *testing.T, s string, n int) []bool {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	out := make([]bool, 0, n)
	solid := false
	for len(data) > 0 {
		run, k := binary.Uvarint(data)
		if k <= 0 {
			t.Fatalf("bad varint")
		}
		data = data[k:]
		for i := uint64(0); i < run; i++ {
			out = append(out, solid)
		}
		solid = !solid
	}
	if len(out) != n {
		t.Fatalf("decoded %d pixels, want %d", len(out), n)
	}
	return out
}

func TestTerrainSnapshotRoundTrip(t *testing.T) {
	gamedata.BrickTypes = map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	}
	terrain := NewTerrain(gamedata.MapConfig{
		MapID: "test_snap", GridWidth: 4, GridHeight: 3, CellSize: 16,
		Tiles: [][]int{{0, 0, 0, 0}, {0, 1, 1, 0}, {2, 2, 1, 1}},
	})
	terrain.DestroyCircle(24, 24, 6)

	snap := terrain.Snapshot()
	if snap.Width != 64 || snap.Height != 48 || snap.Version != 1 {
		t.Fatalf("unexpected snapshot header %+v", snap)
	}

	n := snap.Width * snap.Height
	solid := decodeMaskRLE(t, snap.Solid, n)
	destructible := decodeMaskRLE(t, snap.Destructible, n)
	for y := 0; y < snap.Height; y++ {
		for x := 0; x < snap.Width; x++ {
			i := y*snap.Width + x
			if solid[i] != getBit(terrain.Mask, x, y, terrain.Width) {
				t.Fatalf("solid mismatch at (%d,%d)", x, y)
			}
			if destructible[i] != getBit(terrain.DestructibleMask, x, y, terrain.Width) {
				t.Fatalf("destructible mismatch at (%d,%d)", x, y)
			}
		}
	}
}

func TestDestroyCircleBumpsVersion(t *testing.T) {
	terrain := NewTerrain(gamedata.MapConfig{MapID: "grassland_valley", Width: 100, Height: 100})
	terrain.DestroyCircle(10, 10, 5) // sky: nothing destroyed, still one terrain operation
	terrain.DestroyCircle(50, 99, 5)
	if terrain.Version != 2 {
		t.Errorf("version = %d, want 2", terrain.Version)
	}
}
