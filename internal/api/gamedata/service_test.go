package gamedata

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func testSource() *source {
	return &source{
		Settings: map[string]string{
			"physics.gravity": "445", "physics.wind_scale": "67", "physics.projectile_speed_multiplier": "9.6",
			"character_levels":    `{"levels":[{"level":3,"expRequired":400},{"level":2,"expRequired":200}]}`,
			"equipment.sell_rate": "2", // out of (0, 1]: the default
		},
		Characters: []Character{{CharacterID: "rookie", Name: "Rookie", WeaponID: "basic_rocket", SkillID: "triple_shot"}},
		Weapons: []Weapon{
			{WeaponID: "basic_rocket", ExplosionRadius: 40, TerrainDamage: 20},
			{WeaponID: "no_crater", ExplosionRadius: 33},
		},
		Skills: []Skill{
			{SkillID: "heavy_bomb", EffectType: "bomb_chain", Description: "{count} quả, mỗi quả {eachPct}%"},
			{SkillID: "new_skill", EffectType: "unknown_effect"},
		},
		Items:         []Item{{ItemID: "medkit"}, {ItemID: "freeze_bomb"}, {ItemID: "air_strike"}, {ItemID: "brand_new"}},
		StatusEffects: []statusText{{EffectID: "freeze", Name: "Bị đóng băng"}, {EffectID: "admin_effect", Name: "Mới"}},
		RankTiers:     map[string]string{"gold": "Vàng"},
		Modes:         []modeRow{{ModeID: "bot", Kind: "lobby", Name: "Đấu với máy", Bot: "ai"}, {ModeID: "pvp_1v1", Kind: "match", Name: "Đấu 1v1"}},
	}
}

// build joins the rows with the match's rules; unknown ids (an admin's new skill, item or
// effect) still come out, with neutral rules.
func TestBuild(t *testing.T) {
	g := build(testSource())
	if g.Weapons[0].CraterRadius != 12 || g.Weapons[1].CraterRadius != 33 {
		t.Fatalf("craters %+v", g.Weapons)
	}
	chain := g.Skills[0]
	if chain.AimKind != "arc" || len(chain.Ranks) != 5 || chain.Ranks[0].Rank != 0 || chain.Ranks[4].Rank != 4 {
		t.Fatalf("bomb chain %+v", chain)
	}
	// the client's former describe(): roundi((180 + 15 r) / (3 + r)) and 120 + 15 r
	for r, rk := range chain.Ranks {
		if rk.Values["count"] != float64(2+r) || rk.Values["eachPct"] != math.Round((180+15*float64(r))/float64(3+r)) ||
			rk.Values["capPct"] != float64(120+15*r) {
			t.Fatalf("rank %d %+v", r, rk.Values)
		}
	}
	if s := g.Skills[1]; s.AimKind != "arc" || len(s.Ranks) != 5 || len(s.Ranks[0].Values) != 0 || s.Params == nil {
		t.Fatalf("unknown skill %+v", s)
	}
	inst := map[string]string{}
	for _, it := range g.Items {
		inst[it.ItemID] = it.AimKind
		if it.Instant != (it.AimKind == "none") {
			t.Fatalf("%s instant %v aim %s", it.ItemID, it.Instant, it.AimKind)
		}
	}
	if inst["medkit"] != "none" || inst["freeze_bomb"] != "arc" || inst["air_strike"] != "targetX" || inst["brand_new"] != "none" {
		t.Fatalf("items %v", inst)
	}
	// the rows first (texts), then the effects the match knows without a row
	if g.StatusEffects[0].EffectID != "freeze" || !g.StatusEffects[0].SkipsTurn || g.StatusEffects[1].EffectID != "admin_effect" ||
		g.StatusEffects[1].Bad || len(g.StatusEffects) != 12 || g.StatusEffects[2] != (StatusEffect{EffectID: "burn", Name: "burn", Bad: true}) {
		t.Fatalf("status effects %+v", g.StatusEffects)
	}
	if g.Rank.Tiers[0].Name != "bronze" || g.Rank.Tiers[2].Name != "Vàng" || g.Rank.Tiers[2].MinRating != 1200 {
		t.Fatalf("tiers %+v", g.Rank.Tiers)
	}
	if lv := g.Levels.Character; lv.MaxLevel != 3 || len(lv.Levels) != 3 || lv.Levels[0] != (LevelEntry{1, 0}) || lv.Levels[2] != (LevelEntry{3, 400}) {
		t.Fatalf("levels %+v", lv)
	}
	if g.Equipment.SellRate != 0.25 || g.Equipment.MergeRatePerItem != 25 || g.Equipment.WeaponUpgradeCostMultiplier != 3 {
		t.Fatalf("equipment settings %+v", g.Equipment)
	}
	if len(g.Modes.Lobby) != 1 || g.Modes.Lobby[0].Bot != "ai" || len(g.Modes.Match) != 1 {
		t.Fatalf("modes %+v", g.Modes)
	}
	// no null arrays: a client can index every list
	body, _ := json.Marshal(build(&source{Settings: map[string]string{}, RankTiers: map[string]string{}}))
	if strings.Contains(string(body), "null") {
		t.Fatalf("null in an empty payload: %s", body)
	}
}

// The version is a content hash: equal content, equal version; any change, a new one.
func TestVersion(t *testing.T) {
	a, b := build(testSource()), build(testSource())
	if version(a) != version(b) || len(version(a)) != 16 {
		t.Fatalf("%s %s", version(a), version(b))
	}
	a.Version = "ignored"
	if version(a) != version(b) {
		t.Fatal("the version hashes itself")
	}
	src := testSource()
	src.Settings["physics.gravity"] = "446"
	if version(build(src)) == version(b) {
		t.Fatal("a gravity change kept the version")
	}
	src = testSource()
	src.Items[0].Name = "Hộp cứu thương"
	if version(build(src)) == version(b) {
		t.Fatal("a name change kept the version")
	}
}
