package gamedata

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"battle-squad/internal/shared/ranked"
)

// Every rules row written with the defaults reads back as the defaults (the migration and the
// admin seed write g.Encode(DefaultRules())); no row also gives the defaults.
func TestRulesDefaultsRoundTrip(t *testing.T) {
	r, errs := RulesFromSettings(map[string]string{})
	if len(errs) != 0 || !reflect.DeepEqual(r, DefaultRules()) {
		t.Fatalf("no rows: %v %+v", errs, r)
	}
	settings := map[string]string{}
	for _, g := range RuleGroups {
		settings[g.Key] = g.Encode(DefaultRules())
		if !json.Valid([]byte(settings[g.Key])) {
			t.Fatalf("%s: %s", g.Key, settings[g.Key])
		}
	}
	r, errs = RulesFromSettings(settings)
	if len(errs) != 0 || !reflect.DeepEqual(r, DefaultRules()) {
		t.Fatalf("default rows: %v %+v", errs, r)
	}
	// every Rules field has a group and every group field a struct field (target / field panic)
	for _, g := range RuleGroups {
		g.FormValues(DefaultRules())
	}
	if len(RuleGroups) != reflect.TypeOf(Rules{}).NumField() {
		t.Fatalf("%d groups for %d Rules fields", len(RuleGroups), reflect.TypeOf(Rules{}).NumField())
	}
}

// A field of the wrong type or out of its range keeps its default; the others apply.
func TestRulesInvalidFieldsKeepDefaults(t *testing.T) {
	r, errs := RulesFromSettings(map[string]string{
		"rules.combat":      `{"lavaDamage": "x", "critMultiplier": 0.5, "burnMaxHpPercent": 20, "unknown": 1}`,
		"rules.skill_ranks": `{"byTierWord": {"lv10": 2, "bad": 99}}`,
		"rules.timings":     `not json`,
		"rules.player":      `{"defaultCharacterId": ""}`,
		"rules.terrain":     `{"playerHeight": 41.5}`,
	})
	if r.Combat.LavaDamage != 75 || r.Combat.CritMultiplier != 1.5 || r.Combat.BurnMaxHPPercent != 20 {
		t.Fatalf("combat %+v", r.Combat)
	}
	if r.SkillRanks.ByTierWord["lv10"] != 1 || r.Timings != DefaultRules().Timings || r.Player.DefaultCharacterID != "rookie" ||
		r.Terrain.PlayerHeight != 40 {
		t.Fatalf("rules %+v", r)
	}
	if len(errs) != 6 {
		t.Fatalf("%d errors: %v", len(errs), errs)
	}
}

// The admin form: every field required, numbers in range, maps as key=value lines, the
// cross-field rules.
func TestGroupParseForm(t *testing.T) {
	g, _ := RuleGroup("rules.aim_aid")
	form := g.FormValues(DefaultRules())
	if !strings.Contains(form["guideArcLength"], "bronze=700\n") {
		t.Fatalf("map form %q", form["guideArcLength"])
	}
	form["guideArcLength"] = "bronze=800\nsilver = 640\n\n"
	v, err := g.ParseForm(DefaultRules(), form)
	if err != nil {
		t.Fatal(err)
	}
	r, errs := RulesFromSettings(map[string]string{g.Key: v})
	if len(errs) != 0 || r.AimAid.GuideArcLength["bronze"] != 800 || r.AimAid.GuideArcLength["silver"] != 640 || len(r.AimAid.GuideArcLength) != 2 {
		t.Fatalf("%s %v %+v", v, errs, r.AimAid)
	}
	for _, bad := range []map[string]string{
		{"guideArcLength": "bronze"}, {"guideArcLength": "bronze=-1"}, {"hitToleranceMinPart": "2"}, {"hitToleranceMax": "x"},
		{"hitToleranceFarPx": ""},
	} {
		f := g.FormValues(DefaultRules())
		for k, v := range bad {
			f[k] = v
		}
		if _, err := g.ParseForm(DefaultRules(), f); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	eq, _ := RuleGroup("rules.equipment")
	f := eq.FormValues(DefaultRules())
	f["mergeMinCount"], f["mergeMaxCount"] = "5", "4"
	if _, err := eq.ParseForm(DefaultRules(), f); err == nil {
		t.Fatal("merge min above max accepted")
	}
	f = eq.FormValues(DefaultRules())
	f["maxUpgradeLevel"] = "16.5"
	if _, err := eq.ParseForm(DefaultRules(), f); err == nil {
		t.Fatal("fractional level accepted")
	}
	p, _ := RuleGroup("rules.player")
	f = p.FormValues(DefaultRules())
	f["defaultCharacterId"] = "  "
	if _, err := p.ParseForm(DefaultRules(), f); err == nil {
		t.Fatal("empty default character accepted")
	}
}

func TestScalarSettings(t *testing.T) {
	sc, ok := ScalarSettingOf("match.turn_time_seconds")
	if !ok {
		t.Fatal("turn time not registered")
	}
	for _, bad := range []string{"0", "20.5", "x", "601"} {
		if _, err := sc.Check(bad); err == nil {
			t.Fatalf("turn time %q accepted", bad)
		}
	}
	if x, err := sc.Check(" 25 "); err != nil || x != 25 {
		t.Fatal(x, err)
	}
	// every registered scalar parses in PhysicsFromSettings or is an equipment row
	for _, s := range ScalarSettings {
		if !strings.HasPrefix(s.Key, "equipment.") {
			p := PhysicsFromSettings(map[string]string{s.Key: "7"})
			if reflect.DeepEqual(p, DefaultPhysics()) {
				t.Fatalf("%s is not read", s.Key)
			}
		}
	}
}

// The formerly dead rows drive the match: match.*, move.*, fall.* (bare legacy names too).
func TestPhysicsFromSettingsGroupKeys(t *testing.T) {
	p := PhysicsFromSettings(map[string]string{
		"match.turn_time_seconds": "25", "match.idle_timeout_minutes": "5", "move.step_pixels": "3", "move.energy_cost_per_2px": "1.5",
		"fall.damage_threshold": "100", "fall.damage_per_pixel": "2",
	})
	if p.TurnTimeSeconds != 25 || p.IdleTimeoutMinutes != 5 || p.PxPerEnergy() != 2 || p.EnergyPerPx() != 0.5 ||
		p.FallDamageThreshold != 100 || p.FallDamagePerPixel != 2 {
		t.Fatalf("%+v", p)
	}
	p = PhysicsFromSettings(map[string]string{"turn_time_seconds": "30", "move.step_pixels": "0", "match.idle_timeout_minutes": "-1"})
	if p.TurnTimeSeconds != 30 || p.MoveStepPixels != 2 || p.IdleTimeoutMinutes != 2 {
		t.Fatalf("legacy / invalid %+v", p)
	}
	d := DefaultPhysics()
	if d.TurnTimeSeconds != 20 || d.PxPerEnergy() != 2 || d.EnergyPerPx() != 0.5 || d.FallDamageThreshold != 150 || d.FallDamagePerPixel != 3.75 {
		t.Fatalf("defaults %+v", d)
	}
}

// A skill's params override its effect type's defaults where valid; the admin check rejects
// unknown keys, values out of range and fractions of whole numbers.
func TestSkillTuning(t *testing.T) {
	for effect, specs := range SkillParamSpecs {
		defaults := DefaultSkillParams(effect)
		if len(defaults) != len(specs) {
			t.Fatalf("%s: %d specs for %d tuning fields", effect, len(specs), len(defaults))
		}
		for _, s := range specs {
			d, ok := defaults[s.Key]
			if !ok || s.check(d, ParamIsWhole(effect, s.Key)) != nil {
				t.Fatalf("%s.%s: default %v outside %v..%v", effect, s.Key, d, s.Min, s.Max)
			}
		}
		if err := ValidateSkillParams(effect, defaults); err != nil {
			t.Fatal(err)
		}
	}
	rain := RainTuningOf(SkillConfig{EffectType: EffectMissileRain, Params: map[string]float64{"baseCount": 20, "damage": 0.3, "cap": -1, "burnTurns": 1.5}})
	want := DefaultRainTuning()
	want.BaseCount, want.Damage = 20, 0.3
	if rain != want {
		t.Fatalf("rain %+v", rain)
	}
	if err := ValidateSkillParams(EffectLaser, map[string]float64{"halfWidth": 12, "bogus": 1}); err == nil {
		t.Fatal("unknown param accepted")
	}
	if err := ValidateSkillParams(EffectBombChain, map[string]float64{"baseCount": 2.5}); err == nil {
		t.Fatal("fractional count accepted")
	}
	if err := ValidateSkillParams("other", map[string]float64{"x": 1}); err == nil {
		t.Fatal("params of an effect type without tuning accepted")
	}
	form := map[string]string{}
	for k, v := range DefaultSkillParams(EffectBloom) {
		form["param_"+k] = FormatParam(v)
	}
	form["param_selfBase"] = "45"
	got, err := ParseParamsForm(EffectBloom, func(k string) string { return form[k] })
	if err != nil || got["selfBase"] != 45 || BloomTuningOf(SkillConfig{EffectType: EffectBloom, Params: got}).SelfBase != 45 {
		t.Fatal(got, err)
	}
	form["param_linger"] = ""
	if _, err := ParseParamsForm(EffectBloom, func(k string) string { return form[k] }); err == nil {
		t.Fatal("missing param accepted")
	}
	if DefaultBloomTuning().SeedTime() != 1.4 {
		t.Fatal("seed time")
	}
}

// The status rules come from the rows (the defaults without one); the skip order is the
// display order; invalid rank bands fall back to the defaults.
func TestSnapshotStatusAndBands(t *testing.T) {
	s := NewSnapshot(nil)
	if r := s.StatusRule("stun"); !r.Bad || !r.SkipsTurn || !r.BlocksMove {
		t.Fatalf("stun %+v", r)
	}
	if r := s.StatusRule("root"); !r.BlocksMove || r.SkipsTurn || (s.StatusRule("net") != StatusRule{Bad: true}) {
		t.Fatalf("root %+v", r)
	}
	if !reflect.DeepEqual(s.SkipTurnEffects(), []string{"stun", "freeze"}) || !reflect.DeepEqual(s.RankBands(), ranked.DefaultBands) {
		t.Fatalf("%v %v", s.SkipTurnEffects(), s.RankBands())
	}
	c := *s
	c.statusByID = nil
	c.Status = withDefaultStatus([]StatusEffectConfig{{EffectID: "net", Name: "Lưới", SortOrder: 5, SkipsTurn: true}, {EffectID: "stun", SortOrder: 9}})
	tiers := rankTiersWithDefaults(map[string]RankTierConfig{"silver": {Tier: "silver", Name: "Bạc", MinRating: 900, Divisions: 3, DivisionSize: 30}})
	c.RankTiers = tiers
	c.index()
	if r := c.StatusRule("stun"); r.Bad || r.SkipsTurn || r.BlocksMove {
		t.Fatalf("edited stun %+v", r)
	}
	if !reflect.DeepEqual(c.SkipTurnEffects(), []string{"net", "freeze"}) || !c.StatusRule("net").BlocksMove {
		t.Fatalf("skips %v", c.SkipTurnEffects())
	}
	if c.RankBands()[1].MinRating != 900 {
		t.Fatalf("bands %+v", c.RankBands())
	}
	c.statusByID = nil
	tiers[2].MinRating = 800 // below silver: not valid
	c.index()
	if !reflect.DeepEqual(c.RankBands(), ranked.DefaultBands) {
		t.Fatalf("invalid bands kept %+v", c.RankBands())
	}
}
