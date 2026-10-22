package character

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"battle-squad/internal/game/gamedata"
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

// The response carries each owned character's battle stats, every character's
// base stats and the per-point multipliers, with the documented JSON keys.
func TestGetCharactersJSONIncludesStats(t *testing.T) {
	gamedata.UseData(t, &gamedata.GameData{
		Characters: map[string]gamedata.CharacterConfig{
			"tanko": {CharacterID: "tanko", HP: 1950, Defense: 80, Mobility: 80, WeaponID: "cannon_ball"},
			"spark": {CharacterID: "spark", HP: 1350, Defense: 40, Mobility: 120, WeaponID: "electric_orb"},
		},
		Weapons: map[string]gamedata.WeaponConfig{"cannon_ball": {Damage: 600}, "electric_orb": {Damage: 440}},
	})

	svc, db := newCharacterService(t, 1)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	addCharacter(t, db, "p1", "tanko", 0, 2, 1)

	req := httptest.NewRequest(http.MethodGet, "/player/characters", nil)
	req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, "p1"))
	rec := httptest.NewRecorder()
	NewHandler(svc).GetCharacters(rec, req)

	var env struct {
		Data struct {
			Characters []struct {
				Stats map[string]any `json:"stats"`
			} `json:"characters"`
			BaseStats       map[string]map[string]any `json:"baseStats"`
			StatMultipliers map[string]int            `json:"statMultipliers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	d := env.Data
	if len(d.Characters) != 1 {
		t.Fatalf("body %s", rec.Body.String())
	}
	s := d.Characters[0].Stats
	// migrations/006 seed: hp 2, skill_power 1 per point
	if s["maxHp"] != 1954.0 || s["defense"] != 80.0 || s["moveEnergy"] != 80.0 || s["skillPowerBonus"] != 1.0 ||
		s["weaponDamage"] != 600.0 || s["damageBonus"] != 0.0 || s["terrainDamageBonus"] != 0.0 {
		t.Fatalf("stats %v", s)
	}
	if _, ok := s["critChance"]; ok {
		t.Fatalf("critChance sent without crit equipment: %v", s)
	}
	fp, _ := s["fromPoints"].(map[string]any)
	fe, _ := s["fromEquipment"].(map[string]any)
	base, _ := s["base"].(map[string]any)
	if fp["maxHp"] != 4.0 || fp["moveEnergy"] != 0.0 || fe["maxHp"] != 0.0 || base["maxHp"] != 1950.0 || base["moveEnergy"] != 80.0 {
		t.Fatalf("parts base %v fromPoints %v fromEquipment %v", base, fp, fe)
	}
	if b := d.BaseStats["spark"]; len(d.BaseStats) != 2 || b["maxHp"] != 1350.0 || b["defense"] != 40.0 || b["moveEnergy"] != 120.0 || b["weaponDamage"] != 440.0 {
		t.Fatalf("baseStats %v", d.BaseStats)
	}
	want := map[string]int{"hp": 2, "damage": 5, "mobility": 1, "defense": 1, "skillPower": 1, "terrainDamage": 1}
	for k, v := range want {
		if d.StatMultipliers[k] != v {
			t.Fatalf("statMultipliers %v, want %v", d.StatMultipliers, want)
		}
	}
}
