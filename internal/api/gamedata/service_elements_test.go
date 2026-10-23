package gamedata

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	gd "battle-squad/internal/game/gamedata"
)

// The elements (054): the five with what each beats, the rule percentages, each character's and
// boss's element, each weapon's bounces and the bounce physics.
func TestBuildElements(t *testing.T) {
	snap := testSnapshot()
	c := snap.Data.Characters["rookie"]
	c.Element = gd.ElementFire
	snap.Data.Characters["rookie"] = c
	w := snap.Data.Weapons["basic_rocket"]
	w.Bounces = 1
	snap.Data.Weapons["basic_rocket"] = w
	snap = snap.WithBosses(gd.DefaultBosses())
	g := Build(snap)
	want := []Element{{"metal", "Kim", "wood", 1}, {"wood", "Mộc", "earth", 2}, {"water", "Thủy", "fire", 3}, {"fire", "Hỏa", "metal", 4},
		{"earth", "Thổ", "water", 5}}
	if !reflect.DeepEqual(g.Elements, want) {
		t.Fatalf("elements %+v", g.Elements)
	}
	if g.ElementRules != (ElementRules{PvpStrongPct: 10, PvpWeakPct: 10, BossStrongPct: 20, BossTakenPct: 10}) {
		t.Fatalf("element rules %+v", g.ElementRules)
	}
	if g.Characters[0].Element != "fire" || g.Weapons[0].Bounces != 1 || g.Weapons[1].Bounces != 0 {
		t.Fatalf("character %+v weapons %+v", g.Characters[0], g.Weapons)
	}
	els := map[string]string{}
	for _, b := range g.Bosses {
		els[b.BossID] = b.Element
	}
	if els["steel_robot"] != "metal" || els["lava_crab"] != "fire" || els["ice_witch"] != "water" {
		t.Fatalf("boss elements %v", els)
	}
	if g.Physics.Bounce != (Bounce{Restitution: 0.55, Friction: 0.85}) {
		t.Fatalf("bounce %+v", g.Physics.Bounce)
	}
	raw, _ := json.Marshal(g)
	for _, k := range []string{`"elements":[{"elementId":"metal","name":"Kim","beats":"wood","sortOrder":1}`,
		`"elementRules":{"pvpStrongPct":10,"pvpWeakPct":10,"bossStrongPct":20,"bossTakenPct":10}`, `"element":"fire"`} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("payload lacks %s", k)
		}
	}
}
