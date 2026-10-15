package room

import (
	"context"
	"errors"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// fakeOwnership serves ownership from maps; err fails every lookup.
type fakeOwnership struct {
	characters map[string]bool
	items      map[string]bool
	err        error
	calls      int
}

func (f *fakeOwnership) ownsCharacter(_ context.Context, _, characterID string) (bool, error) {
	f.calls++
	return f.characters[characterID], f.err
}

func (f *fakeOwnership) ownedItems(_ context.Context, _ string, items []string) (map[string]bool, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	owned := map[string]bool{}
	for _, id := range items {
		if f.items[id] {
			owned[id] = true
		}
	}
	return owned, nil
}

func setLoadoutGameData(t *testing.T) {
	t.Helper()
	prev := gamedata.Data
	gamedata.Data = &gamedata.GameData{Items: map[string]gamedata.ItemConfig{
		"medkit":     {ItemID: "medkit", MaxUsePerMatch: 1},
		"teleport":   {ItemID: "teleport", MaxUsePerMatch: 1},
		"power_shot": {ItemID: "power_shot", MaxUsePerMatch: 2},
		"air_strike": {ItemID: "air_strike", MaxUsePerMatch: 1},
	}}
	t.Cleanup(func() { gamedata.Data = prev })
}

func TestValidateQuickPlayLoadout(t *testing.T) {
	setLoadoutGameData(t)
	owner := func() *fakeOwnership {
		return &fakeOwnership{
			characters: map[string]bool{"tanko": true},
			items:      map[string]bool{"medkit": true, "power_shot": true, "teleport": true},
		}
	}

	for _, tc := range []struct {
		name      string
		character string
		items     []string
		devUnlock bool
		wantErr   string // substring of the LoadoutError reason; "" means valid
	}{
		{"rookie no items", "rookie", []string{}, false, ""},
		{"owned character and items", "tanko", []string{"medkit", "power_shot", "power_shot"}, false, ""},
		{"unknown character", "wizard", []string{}, false, "Unknown character: wizard"},
		{"unknown character with dev unlock", "wizard", []string{}, true, "Unknown character: wizard"},
		{"character not owned", "spark", []string{}, false, "You do not own character: spark"},
		{"more than 3 items", "rookie", []string{"medkit", "teleport", "power_shot", "power_shot"}, false, "at most 3 items"},
		{"more than 3 items with dev unlock", "rookie", []string{"medkit", "teleport", "power_shot", "air_strike"}, true, "at most 3 items"},
		{"duplicate beyond maxUsePerMatch", "rookie", []string{"medkit", "medkit"}, false, "medkit can be brought at most 1"},
		{"duplicate beyond maxUsePerMatch with dev unlock", "rookie", []string{"power_shot", "power_shot", "power_shot"}, true, "power_shot can be brought at most 2"},
		{"unknown item", "rookie", []string{"medkit", "nuke"}, false, "Unknown item: nuke"},
		{"unknown item with dev unlock", "rookie", []string{"nuke"}, true, "Unknown item: nuke"},
		{"item not owned", "rookie", []string{"medkit", "air_strike"}, false, "You do not own item: air_strike"},
		{"dev unlock bypasses character ownership", "flora", []string{}, true, ""},
		{"dev unlock bypasses item ownership", "spark", []string{"air_strike", "medkit"}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			own := owner()
			p := QuickPlayPayload{Bot: "idle", CharacterID: tc.character, Items: tc.items}
			err := validateQuickPlayLoadout(context.Background(), own, "p1", p, tc.devUnlock)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("valid loadout rejected: %v", err)
				}
			} else {
				var loadoutErr *LoadoutError
				if !errors.As(err, &loadoutErr) {
					t.Fatalf("err = %v, want LoadoutError containing %q", err, tc.wantErr)
				}
				if !strings.Contains(loadoutErr.Reason, tc.wantErr) {
					t.Fatalf("reason = %q, want it to contain %q", loadoutErr.Reason, tc.wantErr)
				}
			}
			if tc.devUnlock && own.calls != 0 {
				t.Fatalf("dev unlock queried ownership %d time(s)", own.calls)
			}
		})
	}
}

func TestValidateQuickPlayLoadoutStoreFailureIsNotLoadoutError(t *testing.T) {
	setLoadoutGameData(t)
	own := &fakeOwnership{err: errors.New("db down")}
	for _, p := range []QuickPlayPayload{
		{CharacterID: "tanko", Items: []string{}},
		{CharacterID: "rookie", Items: []string{"medkit"}},
	} {
		err := validateQuickPlayLoadout(context.Background(), own, "p1", p, false)
		var loadoutErr *LoadoutError
		if err == nil || errors.As(err, &loadoutErr) {
			t.Fatalf("%+v: err = %v, want a non-loadout error", p, err)
		}
	}
}

func TestCheckCharacterOwnedRookieNeedsNoLookup(t *testing.T) {
	own := &fakeOwnership{}
	if err := checkCharacterOwned(context.Background(), own, "p1", "rookie"); err != nil || own.calls != 0 {
		t.Fatalf("rookie: err = %v, calls = %d", err, own.calls)
	}
}

func TestCheckItemsOwnedEmptySelection(t *testing.T) {
	own := &fakeOwnership{}
	if err := checkItemsOwned(context.Background(), own, "p1", nil); err != nil || own.calls != 0 {
		t.Fatalf("empty selection: err = %v, calls = %d", err, own.calls)
	}
}

func TestLoadoutErrorMessage(t *testing.T) {
	if got := (&LoadoutError{Reason: "r"}).Error(); got != "r" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&LoadoutError{PlayerID: "p1", Reason: "r"}).Error(); got != "player p1: r" {
		t.Errorf("Error() = %q", got)
	}
}
