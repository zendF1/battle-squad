package match

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// elementMatch is a PvP match on flat ground: Tide (water) "w" and Flora (wood) "wd" against
// Rookie (fire) "f", Tanko (earth) "e" and Spark (metal) "m".
func elementMatch(t *testing.T) *Match {
	t.Helper()
	m, _ := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"wd", 1, "flora", 300},
		skillPlayer{"f", 2, "rookie", 600}, skillPlayer{"e", 2, "tanko", 800}, skillPlayer{"m", 2, "spark", 1000})
	for _, p := range m.State.Players {
		p.CritChance = 0
	}
	return m
}

// Every unit gets its character's element; the cycle metal → wood → earth → water → fire → metal.
func TestElementsOfCharacters(t *testing.T) {
	m := elementMatch(t)
	want := map[string]string{"w": "water", "wd": "wood", "f": "fire", "e": "earth", "m": "metal"}
	for id, el := range want {
		if got := m.State.Players[id].Element; got != el {
			t.Fatalf("%s: element %q, want %q", id, got, el)
		}
	}
	gd := m.gd
	for a, b := range map[string]string{"metal": "wood", "wood": "earth", "earth": "water", "water": "fire", "fire": "metal"} {
		if !gd.ElementBeats(a, b) || gd.ElementBeats(b, a) {
			t.Fatalf("%s should beat %s (only)", a, b)
		}
	}
	if gd.ElementBeats("water", "water") || gd.ElementBeats("", "fire") {
		t.Fatal("an element beats itself or none")
	}
}

// PvP: +pvpStrongPct on the element one beats, −pvpWeakPct on the one that beats it, × 1 else;
// teammates (and oneself) never.
func TestElementFactorPvP(t *testing.T) {
	m := elementMatch(t)
	p := m.State.Players
	cases := []struct {
		att, tgt string
		factor   float64
		effect   string
	}{
		{"w", "f", 1.1, ElementStrong}, // water beats fire
		{"w", "e", 0.9, ElementWeak},   // earth beats water
		{"w", "m", 1, ""},              // neutral
		{"f", "w", 0.9, ElementWeak},
		{"e", "w", 1.1, ElementStrong},
		{"m", "wd", 1.1, ElementStrong}, // metal beats wood
		{"wd", "e", 1.1, ElementStrong}, // wood beats earth
		{"f", "m", 1, ""},               // fire beats metal, but a teammate
		{"w", "wd", 1, ""},              // a teammate
		{"w", "w", 1, ""},
	}
	for _, c := range cases {
		f, e := m.elementFactor(p[c.att], p[c.tgt])
		if math.Abs(f-c.factor) > 1e-9 || e != c.effect {
			t.Errorf("%s → %s: %v %q, want %v %q", c.att, c.tgt, f, e, c.factor, c.effect)
		}
	}
	// the percentages are rules.elements
	r := m.gd.Rules.Clone()
	r.Elements = gamedata.ElementRules{PvpStrongPct: 25, PvpWeakPct: 40, BossStrongPct: 0, BossTakenPct: 0}
	m.gd = m.gd.WithRules(r)
	if f, _ := m.elementFactor(p["w"], p["f"]); math.Abs(f-1.25) > 1e-9 {
		t.Fatalf("strong 25 %%: %v", f)
	}
	if f, _ := m.elementFactor(p["w"], p["e"]); math.Abs(f-0.6) > 1e-9 {
		t.Fatalf("weak 40 %%: %v", f)
	}
}

// A shell's damage carries the element factor (before defense) and its damage event the effect.
func TestElementDamageEvents(t *testing.T) {
	m := elementMatch(t)
	w := m.State.Players["w"]
	k := m.gd.Physics.DefenseKOrDefault()
	for id, effect := range map[string]string{"f": ElementStrong, "e": ElementWeak, "m": ""} {
		p := m.State.Players[id]
		p.HP, p.MaxHP = 10000, 10000
		tl := m.newTimeline()
		r := &ProjectileResult{ProjectileID: "x", HitPlayerID: id, ExplosionPoint: vecPtr(bodyCentre(p, 20)), ExplosionRadius: 30,
			Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
		m.explodeProjectile(w, r, 1000, tl)
		ef, _ := m.elementFactor(w, p)
		want := int(math.Round(reduceByDefense(1000*ef, p.Defense, k)))
		var got []ShotEvent
		for _, e := range tl.events {
			if e.Type == "damage" && e.PlayerID == id {
				got = append(got, e)
			}
		}
		if len(got) != 1 || got[0].Amount != want || got[0].ElementEffect != effect {
			t.Fatalf("%s: events %+v, want %d %q", id, got, want, effect)
		}
		raw, _ := json.Marshal(got[0])
		if has := strings.Contains(string(raw), `"elementEffect"`); has != (effect != "") ||
			effect != "" && !strings.Contains(string(raw), `"elementEffect":"`+effect+`"`) {
			t.Fatalf("%s: JSON %s", id, raw)
		}
	}
}

// The skill caps grow with the element factor: a capped rain / chain still deals the element's
// share more (or less).
func TestElementScalesSkillCaps(t *testing.T) {
	m := elementMatch(t)
	w, f, e := m.State.Players["w"], m.State.Players["f"], m.State.Players["e"]
	k := m.gd.Physics.DefenseKOrDefault()
	limit := m.capOfBase(w, 1000)
	if got, want := limit(f), int(math.Round(reduceByDefense(1100, f.Defense, k))); got != want {
		t.Fatalf("strong cap %d, want %d", got, want)
	}
	if got, want := limit(e), int(math.Round(reduceByDefense(900, e.Defense, k))); got != want {
		t.Fatalf("weak cap %d, want %d", got, want)
	}
}

// Falls and the turn-start burn are not attacks: no element.
func TestElementsSkipFallsAndBurn(t *testing.T) {
	m := elementMatch(t)
	w, f := m.State.Players["w"], m.State.Players["f"]
	f.Position.Y -= 400 // in the air over the ground: it falls 400 px
	tl := m.newTimeline()
	m.settleFalls(w, 0, tl)
	want := calculateFallDamage(m.gd.Physics, 400)
	var fall []ShotEvent
	for _, e := range tl.events {
		if e.Type == "damage" && e.PlayerID == "f" {
			fall = append(fall, e)
		}
	}
	if want <= 0 || len(fall) != 1 || fall[0].Amount != want || fall[0].ElementEffect != "" {
		t.Fatalf("fall %+v, want %d without element", fall, want)
	}

	// burn: 10 % of the max HP at the start of its turn, whoever burnt it
	f.HP = f.MaxHP
	m.applyShotStatus(m.newTimeline(), 0, w, f, "burn", 1)
	m.State.CurrentPlayerID = "f"
	m.startTurn(context.Background())
	if lost, want := f.MaxHP-f.HP, int(math.Round(float64(f.MaxHP)*float64(m.gd.Rules.Combat.BurnMaxHPPercent)/100)); lost != want {
		t.Fatalf("burn took %d, want %d", lost, want)
	}
}

// Boss mode: a character whose element beats the boss's deals the boss and its minions (which
// carry the boss's element) +bossStrongPct; the boss side deals a character its element beats
// +bossTakenPct; nothing else changes (no penalty for a character the boss beats).
func TestElementFactorBossMode(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "steel_robot", 4) // rookie (fire), tanko (earth), spark (metal), flora (wood) vs the metal robot
	boss := m.bossPlayer()
	drone := m.State.Players["minion_drone_1"]
	if boss.Element != "metal" || drone.Element != "metal" {
		t.Fatalf("robot %q, drone %q", boss.Element, drone.Element)
	}
	p := func(i int) *BattlePlayerState { return m.State.Players[[]string{"p0", "p1", "p2", "p3"}[i]] }
	cases := []struct {
		att, tgt *BattlePlayerState
		factor   float64
		effect   string
	}{
		{p(0), boss, 1.2, ElementStrong},  // fire beats metal: the boss
		{p(0), drone, 1.2, ElementStrong}, // and its minions
		{p(1), boss, 1, ""},               // earth: neutral
		{p(3), boss, 1, ""},               // metal beats wood: no penalty for the character
		{boss, p(3), 1.1, ElementStrong},  // the boss on the character it beats
		{drone, p(3), 1.1, ElementStrong}, // its minions too
		{boss, p(0), 1, ""},               // fire beats metal: no penalty for the boss either
		{boss, p(2), 1, ""},
	}
	for i, c := range cases {
		f, e := m.elementFactor(c.att, c.tgt)
		if math.Abs(f-c.factor) > 1e-9 || e != c.effect {
			t.Errorf("case %d %s → %s: %v %q, want %v %q", i, c.att.PlayerID, c.tgt.PlayerID, f, e, c.factor, c.effect)
		}
	}

	// a summoned minion carries the element, and its summon event shows it
	killWave(m)
	m.boss.round = 5
	drainMsgs(watch)
	if _, ok := m.bossFreeSummon(boss); !ok {
		t.Fatal("no summon")
	}
	s := shotOf(t, watch)
	sum := shotEventsOf(s, "summon")
	if len(sum) == 0 || sum[0].Element != "metal" || m.State.Players[sum[0].PlayerID].Element != "metal" {
		t.Fatalf("summon events %+v", sum)
	}
}

// MatchStarted / MatchStateSync carry every unit's element.
func TestElementInMatchState(t *testing.T) {
	m := elementMatch(t)
	raw, _ := json.Marshal(m.State.Players["w"])
	if !strings.Contains(string(raw), `"element":"water"`) {
		t.Fatalf("state %s", raw)
	}
}
