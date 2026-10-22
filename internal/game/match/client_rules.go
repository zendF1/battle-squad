package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// What GET /gamedata needs of the match's rules that is not a plain game data value: the crater
// a weapon carves, how items and skills are aimed, and the numbers a skill's description shows
// per rank. Every number comes from the snapshot it is given, so the payload and the matches
// that play with that snapshot agree.

// WeaponCraterRadius is the crater (px) a weapon with these numbers carves on normal ground:
// terrainDamage × rules.terrain craterPxPerTerrainDamage, or the blast radius without terrain
// damage.
func WeaponCraterRadius(gd *gamedata.Snapshot, explosionRadius, terrainDamage int) float64 {
	if terrainDamage <= 0 {
		return float64(explosionRadius)
	}
	return float64(terrainDamage) * gd.Rules.Terrain.CraterPxPerTerrainDamage
}

// InstantItem reports whether an item acts as soon as it is used (UseItem; medkit,
// wind_stopper) instead of arming the next shot (Shoot actionMode "item"; teleport and the
// shot modifiers).
func InstantItem(itemID string) bool {
	return itemID != "teleport" && !IsShotModifierItem(itemID)
}

// TargetXItem reports whether an item's shot is aimed at a map column (Shoot.targetX).
func TargetXItem(itemID string) bool { return itemID == "air_strike" }

// Skill aim kinds: the slingshot arc, a straight line in any direction (power ignored), none
// (fired at once with rules.aim noAimAngle / noAimPower).
const (
	AimArc  = "arc"
	AimLine = "line"
	AimNone = "none"
)

// SkillRules are what the client needs of a skill: how it is aimed, its tuning (Params: the
// effect type's numbers with the skill's config_skills.params applied, plus the shared rules
// it uses) and, per skill rank 0..MaxSkillRank, the numbers its description shows (Ranks[r]).
type SkillRules struct {
	AimKind string
	Params  map[string]float64
	Ranks   []map[string]float64
}

// pct is x as a whole percent (round half away from zero).
func pct(x float64) float64 { return math.Round(x * 100) }

// SkillRulesOf returns the rules of a skill in gd; an unknown effect type is an arc shot with
// no numbers.
func SkillRulesOf(gd *gamedata.Snapshot, skill gamedata.SkillConfig) SkillRules {
	maxRank := gd.Rules.MaxSkillRank()
	combat := gd.Rules.Combat
	ranks := func(f func(r int) map[string]float64) []map[string]float64 {
		out := make([]map[string]float64, 0, maxRank+1)
		for r := 0; r <= maxRank; r++ {
			out = append(out, f(r))
		}
		return out
	}
	switch skill.EffectType {
	case effectMissileRain:
		t := gamedata.RainTuningOf(skill)
		params := gamedata.TuningParams(t)
		params["burnMaxHpPercent"] = float64(combat.BurnMaxHPPercent)
		return SkillRules{AimKind: AimArc, Params: params,
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"count": float64(rainCount(t, r)), "countPerRank": float64(t.CountPerRank), "spread": rainSpread(t, r),
					"damagePct": pct(t.Damage), "capPct": pct(t.Cap), "burnPct": float64(combat.BurnMaxHPPercent),
				}
			})}
	case effectBombChain:
		t := gamedata.ChainTuningOf(skill)
		return SkillRules{AimKind: AimArc, Params: gamedata.TuningParams(t),
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"count": float64(chainCount(t, r)), "countPerRank": float64(t.CountPerRank),
					"eachPct": pct(chainEach(t, r)), "capPct": pct(chainCap(t, r)),
				}
			})}
	case effectLaser:
		t := gamedata.LaserTuningOf(skill)
		return SkillRules{AimKind: AimLine, Params: gamedata.TuningParams(t),
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{"firstPct": pct(laserFirst(t, r)), "decayPct": pct(1 - t.Decay)}
			})}
	case effectBloom:
		t := gamedata.BloomTuningOf(skill)
		params := gamedata.TuningParams(t)
		params["seedTime"] = t.SeedTime()
		params["shieldCapPercent"] = float64(combat.ShieldCapPercent)
		return SkillRules{AimKind: AimNone, Params: params,
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"selfPct": float64(bloomSelfPercent(t, r)), "allyPct": float64(bloomAllyPercent(t, r)),
					"perRankPct": float64(t.PerRank), "shieldCapPct": float64(combat.ShieldCapPercent),
				}
			})}
	}
	return SkillRules{AimKind: AimArc, Params: map[string]float64{}, Ranks: ranks(func(int) map[string]float64 { return map[string]float64{} })}
}
