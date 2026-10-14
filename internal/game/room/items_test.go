package room

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

func TestItemCountsEnforcesPerMatchLimit(t *testing.T) {
	gamedata.Data = &gamedata.GameData{Items: map[string]gamedata.ItemConfig{
		"medkit":     {ItemID: "medkit", MaxUsePerMatch: 1},
		"power_shot": {ItemID: "power_shot", MaxUsePerMatch: 2},
	}}

	if _, err := itemCounts([]string{"medkit", "medkit"}); err == nil {
		t.Error("duplicate medkit accepted")
	}
	if _, err := itemCounts([]string{"unknown", "unknown"}); err == nil {
		t.Error("duplicate unconfigured item accepted (default limit is 1)")
	}
	counts, err := itemCounts([]string{"power_shot", "power_shot", "medkit"})
	if err != nil {
		t.Fatalf("valid selection rejected: %v", err)
	}
	if counts["power_shot"] != 2 || counts["medkit"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}
