package match

import (
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// Every skill effect the match resolves has client rules (GET /gamedata): an aim kind, its
// tuning and the description numbers of every rank, matching what the skill plays.
func TestEverySkillHasClientRules(t *testing.T) {
	gd := gamedata.NewSnapshot(nil)
	maxRank := gd.Rules.MaxSkillRank()
	if maxRank != 4 {
		t.Fatalf("max skill rank %d", maxRank)
	}
	skill := func(effect string) gamedata.SkillConfig { return gamedata.SkillConfig{EffectType: effect} }
	for effect := range skillResolvers {
		if strings.HasPrefix(effect, "test_") { // registered by tests
			continue
		}
		r := SkillRulesOf(gd, skill(effect))
		if r.AimKind == "" || len(r.Params) == 0 || len(r.Ranks) != maxRank+1 || len(r.Ranks[0]) == 0 {
			t.Fatalf("%s: %+v", effect, r)
		}
	}
	for rank := 0; rank <= maxRank; rank++ {
		if got := SkillRulesOf(gd, skill(effectMissileRain)).Ranks[rank]["count"]; got != float64(rainCount(defRain, rank)) {
			t.Fatalf("rain rank %d count %v", rank, got)
		}
		if got := SkillRulesOf(gd, skill(effectBloom)).Ranks[rank]["selfPct"]; got != float64(bloomSelfPercent(defBloom, rank)) {
			t.Fatalf("bloom rank %d self %v", rank, got)
		}
	}
	if SkillRulesOf(gd, skill(effectLaser)).AimKind != AimLine || SkillRulesOf(gd, skill(effectBloom)).AimKind != AimNone ||
		SkillRulesOf(gd, skill("new")).AimKind != AimArc {
		t.Fatal("aim kinds")
	}

	// A skill's params change its numbers (and only its own)
	tuned := gamedata.SkillConfig{EffectType: effectMissileRain, Params: map[string]float64{"baseCount": 20, "damage": 0.3, "bogus": 1}}
	r := SkillRulesOf(gd, tuned)
	if r.Ranks[0]["count"] != 20 || r.Ranks[1]["count"] != 24 || r.Ranks[0]["damagePct"] != 30 || r.Params["baseCount"] != 20 {
		t.Fatalf("tuned rain %+v", r.Ranks[0])
	}
	if _, ok := r.Params["bogus"]; ok {
		t.Fatal("unknown param sent")
	}
	// An out-of-range or fractional count keeps the default
	bad := gamedata.SkillConfig{EffectType: effectMissileRain, Params: map[string]float64{"baseCount": 12.5, "damage": -1}}
	if r := SkillRulesOf(gd, bad); r.Ranks[0]["count"] != 12 || r.Ranks[0]["damagePct"] != 25 {
		t.Fatalf("invalid params %+v", r.Ranks[0])
	}
}

// The status rules are the ones the match applies.
func TestStatusRules(t *testing.T) {
	gd := gamedata.NewSnapshot(nil)
	for _, e := range gd.Status {
		id := e.EffectID
		p := &BattlePlayerState{StatusEffects: []StatusEffect{{EffectID: id, DurationTurn: 1}}}
		r := gd.StatusRule(id)
		if r.SkipsTurn != (skipsTurn(gd, p) != "") || r.BlocksMove != !canMove(gd, p) || r.Bad != (len(Cleanse(gd, p)) == 1) {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	if !InstantItem("medkit") || !InstantItem("wind_stopper") || InstantItem("teleport") || InstantItem("air_strike") || !TargetXItem("air_strike") {
		t.Fatal("item kinds")
	}
	if WeaponCraterRadius(gd, 40, 30) != 18 || WeaponCraterRadius(gd, 33, 0) != 33 {
		t.Fatal("crater radius")
	}
}
