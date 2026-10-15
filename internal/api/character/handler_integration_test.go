package character

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/observability"
)

func getCharacters(t *testing.T, h *Handler, playerID string) (int, CharactersResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/player/characters", nil)
	req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, playerID))
	rec := httptest.NewRecorder()
	h.GetCharacters(rec, req)
	var env struct {
		Success bool               `json:"success"`
		Data    CharactersResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, env.Data
}

// The reset fee comes from the character_progression config ResetStats charges.
func TestGetCharactersIncludesResetStatsCost(t *testing.T) {
	svc, db := newCharacterService(t, 1)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	addCharacter(t, db, "p1", "tanko", 2, 3, 0)

	code, resp := getCharacters(t, h, "p1")
	if code != http.StatusOK || len(resp.Characters) != 1 || resp.Characters[0].CharacterID != "tanko" {
		t.Fatalf("status %d characters %+v", code, resp.Characters)
	}
	if resp.ResetStatsCost != (ResetStatsCost{Currency: "coin", Amount: 500}) {
		t.Fatalf("resetStatsCost %+v, want the migrations/006 seed (500 coin)", resp.ResetStatsCost)
	}

	if _, err := db.Pool.Exec(context.Background(), `UPDATE game_settings SET value = '{"resetCostCurrency":"gem","resetCostAmount":70}' WHERE key = $1`, SettingsKeyProgression); err != nil {
		t.Fatal(err)
	}
	if _, resp = getCharacters(t, h, "p1"); resp.ResetStatsCost != (ResetStatsCost{Currency: "gem", Amount: 70}) {
		t.Fatalf("resetStatsCost %+v, want the edited config (70 gem)", resp.ResetStatsCost)
	}

	// No characters: an empty list (not null) and still the fee.
	_, resp = getCharacters(t, h, "nobody")
	if resp.Characters == nil || len(resp.Characters) != 0 || resp.ResetStatsCost.Amount != 70 {
		t.Fatalf("empty player: %+v", resp)
	}
}

func TestGetCharactersFallsBackToDefaultResetCost(t *testing.T) {
	svc, db := newCharacterService(t, 1)
	if _, err := db.Pool.Exec(context.Background(), `DELETE FROM game_settings WHERE key = $1`, SettingsKeyProgression); err != nil {
		t.Fatal(err)
	}
	resp, err := svc.GetCharacters(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	if want := LoadProgressionConfig("").ResetStatsCost(); resp.ResetStatsCost != want {
		t.Fatalf("resetStatsCost %+v, want the ResetStats default %+v", resp.ResetStatsCost, want)
	}
}
