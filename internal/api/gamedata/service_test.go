package gamedata

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	gd "battle-squad/internal/game/gamedata"
)

func testSnapshot() *gd.Snapshot {
	snap := gd.NewSnapshot(&gd.GameData{
		Characters: map[string]gd.CharacterConfig{"rookie": {CharacterID: "rookie", Name: "Rookie", WeaponID: "basic_rocket", SkillID: "triple_shot"}},
		Weapons: map[string]gd.WeaponConfig{
			"basic_rocket": {WeaponID: "basic_rocket", ExplosionRadius: 40, TerrainDamage: 20},
			"no_crater":    {WeaponID: "no_crater", ExplosionRadius: 33},
		},
		Skills: map[string]gd.SkillConfig{
			"heavy_bomb": {SkillID: "heavy_bomb", EffectType: "bomb_chain", Description: "{count} quả, mỗi quả {eachPct}%"},
			"new_skill":  {SkillID: "new_skill", EffectType: "unknown_effect"},
		},
		Items: map[string]gd.ItemConfig{"medkit": {ItemID: "medkit", SortOrder: 1}, "freeze_bomb": {ItemID: "freeze_bomb", SortOrder: 2},
			"air_strike": {ItemID: "air_strike", SortOrder: 3}, "brand_new": {ItemID: "brand_new", SortOrder: 4}},
	})
	snap.Settings = map[string]string{
		"physics.gravity": "445", "physics.wind_scale": "67", "physics.projectile_speed_multiplier": "9.6",
		"character_levels":    `{"levels":[{"level":3,"expRequired":400},{"level":2,"expRequired":200}]}`,
		"equipment.sell_rate": "2", // out of (0, 1]: the default
	}
	snap.Physics = gd.PhysicsFromSettings(snap.Settings)
	snap.Status = []gd.StatusEffectConfig{
		{EffectID: "freeze", Name: "Bị đóng băng", SortOrder: 1, Bad: true, SkipsTurn: true, BlocksMove: true},
		{EffectID: "admin_effect", Name: "Mới", SortOrder: 2},
	}
	for _, d := range gd.DefaultStatusEffects {
		if d.EffectID != "freeze" {
			snap.Status = append(snap.Status, gd.StatusEffectConfig{EffectID: d.EffectID, Name: d.EffectID, SortOrder: 20 + d.SortOrder,
				Bad: d.Bad, SkipsTurn: d.SkipsTurn, BlocksMove: d.BlocksMove})
		}
	}
	for i := range snap.RankTiers {
		snap.RankTiers[i].Name = ""
		if snap.RankTiers[i].Tier == "gold" {
			snap.RankTiers[i].Name = "Vàng"
		}
	}
	snap.Modes = []gd.GameModeConfig{{ModeID: "bot", Kind: "lobby", Name: "Đấu với máy", Bot: "ai", Category: "pvp", Icon: "robot", PartySize: 1, Enabled: true},
		{ModeID: "hidden", Kind: "lobby", Name: "Ẩn", Queue: "casual_2v2", Category: "pvp", PartySize: 2},
		{ModeID: "pvp_1v1", Kind: "match", Name: "Đấu 1v1"}}
	snap.ModeCategories = []gd.ModeCategoryConfig{{ID: "pvp", Name: "PvP", SortOrder: 1}, {ID: "boss", Name: "Boss", SortOrder: 2}}
	snap.GunKinds = []gd.GunKindConfig{{ID: "cannon", Name: "Đại pháo"}}
	snap.Slots = []gd.EquipmentSlotConfig{{ID: "helmet", Name: "Mũ"}}
	snap.TypeWords = []gd.TypeWordConfig{{Word: "shock rifle", Name: "Súng điện", GunKind: "shock_rifle"}}
	return snap.WithData(snap.Data) // re-indexed
}

// Build joins the rows with the match's rules; unknown ids (an admin's new skill, item or
// effect) still come out, with neutral rules.
func TestBuild(t *testing.T) {
	g := Build(testSnapshot())
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
	// a disabled lobby mode is left out
	if len(g.Modes.Lobby) != 1 || g.Modes.Lobby[0] != (LobbyMode{ModeID: "bot", Name: "Đấu với máy", Bot: "ai", Category: "pvp", Icon: "robot", PartySize: 1}) ||
		len(g.Modes.Match) != 1 || !reflect.DeepEqual(g.Modes.Categories, []ModeCategory{{"pvp", "PvP"}, {"boss", "Boss"}}) {
		t.Fatalf("modes %+v", g.Modes)
	}
	// no null arrays: a client can index every list
	if g.GunKinds[0] != (GunKind{ID: "cannon", Name: "Đại pháo"}) || g.Equipment.Slots[0] != (EquipmentSlot{ID: "helmet", Name: "Mũ"}) ||
		g.Equipment.TypeWords[0] != (TypeWord{Word: "shock rifle", Name: "Súng điện", GunKind: "shock_rifle"}) {
		t.Fatalf("texts %+v %+v %+v", g.GunKinds, g.Equipment.Slots, g.Equipment.TypeWords)
	}
	body, _ := json.Marshal(Build(gd.NewSnapshot(nil)))
	if strings.Contains(string(body), "null") {
		t.Fatalf("null in an empty payload: %s", body)
	}
}

// The version is a content hash: equal content, equal version; any change, a new one.
func TestVersion(t *testing.T) {
	a, b := Build(testSnapshot()), Build(testSnapshot())
	if version(a) != version(b) || len(version(a)) != 16 {
		t.Fatalf("%s %s", version(a), version(b))
	}
	a.Version = "ignored"
	if version(a) != version(b) {
		t.Fatal("the version hashes itself")
	}
	src := testSnapshot()
	src.Physics.Gravity = 446
	if version(Build(src)) == version(b) {
		t.Fatal("a gravity change kept the version")
	}
	src = testSnapshot()
	src.Data.Items["medkit"] = gd.ItemConfig{ItemID: "medkit", DisplayName: "Hộp cứu thương", SortOrder: 1}
	if version(Build(src)) == version(b) {
		t.Fatal("a name change kept the version")
	}
}
