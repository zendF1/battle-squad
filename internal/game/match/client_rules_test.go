package match

import (
	"strings"
	"testing"
)

// Every skill effect the match resolves has client rules (GET /gamedata): an aim kind, its
// tuning and the description numbers of every rank, matching what the skill plays.
func TestEverySkillHasClientRules(t *testing.T) {
	maxRank := Rules().MaxSkillRank
	if maxRank != 4 {
		t.Fatalf("max skill rank %d", maxRank)
	}
	for effect := range skillResolvers {
		if strings.HasPrefix(effect, "test_") { // registered by tests
			continue
		}
		r := SkillRulesOf(effect)
		if r.AimKind == "" || len(r.Params) == 0 || len(r.Ranks) != maxRank+1 || len(r.Ranks[0]) == 0 {
			t.Fatalf("%s: %+v", effect, r)
		}
	}
	for rank := 0; rank <= maxRank; rank++ {
		if got := SkillRulesOf(effectMissileRain).Ranks[rank]["count"]; got != float64(rainCount(rank)) {
			t.Fatalf("rain rank %d count %v", rank, got)
		}
		if got := SkillRulesOf(effectBloom).Ranks[rank]["selfPct"]; got != float64(bloomSelfPercent(rank)) {
			t.Fatalf("bloom rank %d self %v", rank, got)
		}
	}
	if SkillRulesOf(effectLaser).AimKind != AimLine || SkillRulesOf(effectBloom).AimKind != AimNone || SkillRulesOf("new").AimKind != AimArc {
		t.Fatal("aim kinds")
	}
}

// The status rules are the ones the match applies.
func TestStatusRules(t *testing.T) {
	for _, id := range StatusEffectIDs {
		p := &BattlePlayerState{StatusEffects: []StatusEffect{{EffectID: id, DurationTurn: 1}}}
		r := StatusRuleOf(id)
		if r.SkipsTurn != (skipsTurn(p) != "") || r.BlocksMove != !canMove(p) || r.Bad != (len(Cleanse(p)) == 1) {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	if !InstantItem("medkit") || !InstantItem("wind_stopper") || InstantItem("teleport") || InstantItem("air_strike") || !TargetXItem("air_strike") {
		t.Fatal("item kinds")
	}
	if WeaponCraterRadius(40, 30) != 18 || WeaponCraterRadius(33, 0) != 33 {
		t.Fatal("crater radius")
	}
}
