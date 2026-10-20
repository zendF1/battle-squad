package title

import (
	"encoding/json"
	"testing"

	"battle-squad/internal/api/milestone"
)

// Every title is granted by exactly one seeded milestone, and the other way round.
func TestDefaultsMatchMilestones(t *testing.T) {
	rarities := map[string]bool{RarityBlue: true, RarityOrange: true, RarityPurple: true, RarityRed: true}
	granted := map[string]int{}
	for _, titleID := range milestone.TitleRewards {
		granted[titleID]++
	}
	seen := map[string]bool{}
	prev := 0
	for _, d := range Defaults {
		if seen[d.ID] || !rarities[d.Rarity] || d.Name == "" || d.Description == "" || d.SortOrder <= prev {
			t.Errorf("bad default %+v", d)
		}
		seen[d.ID], prev = true, d.SortOrder
		if granted[d.ID] != 1 {
			t.Errorf("%s granted by %d milestones", d.ID, granted[d.ID])
		}
	}
	if len(Defaults) != 8 || len(granted) != len(Defaults) {
		t.Fatalf("%d titles, %d granted", len(Defaults), len(granted))
	}
}

func TestJSONShapes(t *testing.T) {
	raw, _ := json.Marshal(ListResponse{Titles: []Title{}, Equipped: nil})
	if string(raw) != `{"titles":[],"equipped":null}` {
		t.Fatalf("list %s", raw)
	}
	raw, _ = json.Marshal(EquipResponse{Equipped: &Ref{ID: "smith", Name: "Thợ rèn lành nghề", Rarity: "blue"}})
	if string(raw) != `{"equipped":{"id":"smith","name":"Thợ rèn lành nghề","rarity":"blue"}}` {
		t.Fatalf("equip %s", raw)
	}
}
