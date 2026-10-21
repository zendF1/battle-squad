package room

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

// "Đấu với máy" (QuickPlay against the AI bot) plays a random map, practice (the idle target
// dummy) always grassland_valley.
func TestQuickPlayMapID(t *testing.T) {
	prev := gamedata.Data
	t.Cleanup(func() { gamedata.Data = prev })
	gamedata.Data = &gamedata.GameData{Maps: map[string]gamedata.MapConfig{
		"grassland_valley": {}, "ice_bridge": {}, "pyramid": {},
	}}
	ai := QuickPlayPayload{Bot: "ai"}
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		seen[quickPlayMapID(ai, func(n int) int { return i % n })] = true
	}
	if len(seen) != 3 {
		t.Fatalf("AI quick play maps %v, want all 3", seen)
	}
	for _, p := range []QuickPlayPayload{{}, {Bot: "idle"}} {
		if got := quickPlayMapID(p, func(n int) int { return n - 1 }); got != PracticeMapID {
			t.Fatalf("practice map %q", got)
		}
	}
	gamedata.Data = &gamedata.GameData{Maps: map[string]gamedata.MapConfig{}}
	if got := quickPlayMapID(ai, func(n int) int { return 0 }); got != PracticeMapID {
		t.Fatalf("no maps loaded: %q", got)
	}
}
