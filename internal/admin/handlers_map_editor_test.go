package admin

import (
	"encoding/json"
	"testing"
)

func TestIsLegacyTiles(t *testing.T) {
	for raw, want := range map[string]bool{
		``:              true,
		`null`:          true,
		`[]`:            true,
		`[[]]`:          false,
		`[[0,0],[0,1]]`: false,
	} {
		if got := isLegacyTiles(json.RawMessage(raw)); got != want {
			t.Errorf("isLegacyTiles(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestAllAir(t *testing.T) {
	if !allAir([][]int{{0, 0}, {0, 0}}) {
		t.Error("expected all-zero grid to be all air")
	}
	if allAir([][]int{{0, 0}, {0, 3}}) {
		t.Error("expected grid with a brick not to be all air")
	}
}
