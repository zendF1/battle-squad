package match

import "math"

// ClientRules are the match rules the client mirrors that are still Go constants in this
// package (GET /gamedata serves them). Phase S2 moves them into the DB: then only Rules()
// changes where it reads them, its callers stay the same.
type ClientRules struct {
	ShotOriginLift    float64 // px above the feet a shot starts (the body centre)
	CollisionStepPx   float64 // checkSegmentCollisions sub-step
	OutOfBoundsLeft   float64 // a flight ends past x < −OutOfBoundsLeft,
	OutOfBoundsRight  float64 // x > width + OutOfBoundsRight
	OutOfBoundsBelow  float64 // or y > height + OutOfBoundsBelow
	MaxShotPower      float64
	NoAimAngle        float64 // what a skill that ignores the aim is sent with
	NoAimPower        float64
	PowerShotFactor   float64
	LavaDamage        int
	PlayerHeight      int
	MaxClimbPerStep   int
	CraterPxPerTD     float64 // crater radius px per weapon terrain damage point
	BowlDepth         float64
	BowlCeiling       float64
	MaxTerrainBonus   int // % the terrain damage bonus grows a crater by, at most
	FallThresholdPx   float64
	FallDamagePerPx   float64
	MovePxPerEnergy   float64
	MoveEnergyPerPx   float64
	NetMoveEnergyCap  int
	TurnSeconds       int
	ActionIntervalSec float64
	BotFollowUpSec    float64
	SkipTurnDelaySec  float64
	SkillEnergyMax    int
	SkillEnergyTurn   int
	SkillEnergyOnHit  int
	ShieldCapPercent  int
	BurnMaxHPPercent  int
	AirStrikeAngle    float64
	AirStrikePower    float64
	// WeaponTierRanks maps the tier word ending a weapon item id to its skill rank
	// (WeaponSkillRank); any other id (or none) is rank 0.
	WeaponTierRanks map[string]int
	MaxSkillRank    int
}

// Rules returns the current ClientRules.
func Rules() ClientRules {
	ranks := make(map[string]int, len(weaponTierRanks))
	maxRank := 0
	for k, v := range weaponTierRanks {
		ranks[k] = v
		maxRank = max(maxRank, v)
	}
	return ClientRules{
		ShotOriginLift:    ShotOriginLift,
		CollisionStepPx:   collisionStepPx,
		OutOfBoundsLeft:   outOfBoundsSide,
		OutOfBoundsRight:  outOfBoundsSide,
		OutOfBoundsBelow:  outOfBoundsBelow,
		MaxShotPower:      maxShotPower,
		NoAimAngle:        NoAimAngle,
		NoAimPower:        NoAimPower,
		PowerShotFactor:   powerShotFactor,
		LavaDamage:        lavaDamage,
		PlayerHeight:      PlayerHeight,
		MaxClimbPerStep:   maxClimbPerStep,
		CraterPxPerTD:     craterPxPerTerrainDamage,
		BowlDepth:         bowlDepth,
		BowlCeiling:       bowlCeiling,
		MaxTerrainBonus:   MaxTerrainDamageBonus,
		FallThresholdPx:   fallDamageThreshold,
		FallDamagePerPx:   fallDamageMultiplier,
		MovePxPerEnergy:   movePxPerEnergy,
		MoveEnergyPerPx:   moveEnergyPerPx,
		NetMoveEnergyCap:  netMoveEnergyCap,
		TurnSeconds:       turnSeconds,
		ActionIntervalSec: actionRateLimit.Seconds(),
		BotFollowUpSec:    botFollowUpDelay.Seconds(),
		SkipTurnDelaySec:  skipTurnDelay,
		SkillEnergyMax:    skillEnergyMax,
		SkillEnergyTurn:   skillEnergyPerTurn,
		SkillEnergyOnHit:  skillEnergyOnHit,
		ShieldCapPercent:  shieldCapPercent,
		BurnMaxHPPercent:  burnMaxHPPercent,
		AirStrikeAngle:    airStrikeAngle,
		AirStrikePower:    airStrikePower,
		WeaponTierRanks:   ranks,
		MaxSkillRank:      maxRank,
	}
}

// WeaponCraterRadius is the crater (px) a weapon with these numbers carves on normal ground
// (weaponCraterRadius).
func WeaponCraterRadius(explosionRadius, terrainDamage int) float64 {
	if terrainDamage <= 0 {
		return float64(explosionRadius)
	}
	return float64(terrainDamage) * craterPxPerTerrainDamage
}

// StatusRule is how the match treats a status effect.
type StatusRule struct {
	Bad        bool // Cleanse (the bloom) removes it
	SkipsTurn  bool // the player's whole turn is skipped
	BlocksMove bool // the player cannot walk or teleport
}

// StatusEffectIDs are the status effects the match puts on players (and wind_stop on the
// match), in display order.
var StatusEffectIDs = []string{
	"burn", "root", "stun", "freeze", "net",
	"power_shot", "drill_bomb", "spider_net", "freeze_bomb", "air_strike", "wind_stop",
}

// StatusRuleOf is the rule of a status effect id (all false for an unknown one).
func StatusRuleOf(effectID string) StatusRule {
	skips := false
	for _, id := range skipTurnEffects {
		if id == effectID {
			skips = true
		}
	}
	return StatusRule{Bad: badEffects[effectID], SkipsTurn: skips, BlocksMove: skips || effectID == rootEffect}
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
// (fired at once with NoAimAngle / NoAimPower).
const (
	AimArc  = "arc"
	AimLine = "line"
	AimNone = "none"
)

// SkillRules are what the client needs of a skill effect type: how it is aimed, its tuning
// constants (Params) and, per skill rank 0..MaxSkillRank, the numbers its description shows
// (Ranks[r]).
type SkillRules struct {
	AimKind string
	Params  map[string]float64
	Ranks   []map[string]float64
}

// pct is x as a whole percent (round half away from zero).
func pct(x float64) float64 { return math.Round(x * 100) }

// SkillRulesOf returns the rules of a skill effect type; an unknown one is an arc shot with no
// numbers.
func SkillRulesOf(effectType string) SkillRules {
	maxRank := Rules().MaxSkillRank
	ranks := func(f func(r int) map[string]float64) []map[string]float64 {
		out := make([]map[string]float64, 0, maxRank+1)
		for r := 0; r <= maxRank; r++ {
			out = append(out, f(r))
		}
		return out
	}
	switch effectType {
	case effectMissileRain:
		return SkillRules{AimKind: AimArc,
			Params: map[string]float64{
				"baseCount": rainBaseCount, "countPerRank": rainCountPerRank,
				"spreadBase": rainSpreadBase, "spreadPerRank": rainSpreadPerRank, "focus": rainFocus,
				"delay": rainDelay, "interval": rainInterval, "startY": rainStartY, "dropSpeed": rainDropSpeed,
				"damage": rainDamage, "blast": rainBlast, "crater": rainCrater, "cap": rainCap,
				"burnTurns": burnTurns, "burnMaxHpPercent": burnMaxHPPercent,
			},
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"count": float64(rainCount(r)), "countPerRank": rainCountPerRank, "spread": rainSpread(r),
					"damagePct": pct(rainDamage), "capPct": pct(rainCap), "burnPct": burnMaxHPPercent,
				}
			})}
	case effectBombChain:
		return SkillRules{AimKind: AimArc,
			Params: map[string]float64{
				"baseCount": chainBaseCount, "countPerRank": chainCountPerRank, "launchGap": chainLaunchGap,
				"totalBase": chainTotalBase, "totalPerRank": chainTotalPerRank,
				"capBase": chainCapBase, "capPerRank": chainCapPerRank, "blast": chainBlast,
				"fallSpeed": chainFallSpeed, "hitGap": chainHitGap, "rootTurns": rootTurns,
			},
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"count": float64(chainCount(r)), "countPerRank": chainCountPerRank,
					"eachPct": pct(chainEach(r)), "capPct": pct(chainCap(r)),
				}
			})}
	case effectLaser:
		return SkillRules{AimKind: AimLine,
			Params: map[string]float64{
				"halfWidth": laserHalfWidth, "firstBase": laserFirstBase, "firstPerRank": laserFirstPerRank,
				"decay": laserDecay, "chargeTime": laserChargeTime, "beamTime": laserBeamTime,
				"sweepSpeed": laserSweepSpeed, "stunTurns": stunTurns,
			},
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{"firstPct": pct(laserFirst(r)), "decayPct": pct(1 - laserDecay)}
			})}
	case effectBloom:
		return SkillRules{AimKind: AimNone,
			Params: map[string]float64{
				"selfBase": bloomSelfBase, "allyBase": bloomAllyBase, "perRank": bloomPerRank,
				"riseTime": bloomRiseTime, "crossTime": bloomCrossTime, "fallTime": bloomFallTime,
				"seedTime": bloomSeedTime, "skyY": bloomSkyY, "fallSpeed": bloomFallSpeed, "linger": bloomLinger,
				"shieldCapPercent": shieldCapPercent,
			},
			Ranks: ranks(func(r int) map[string]float64 {
				return map[string]float64{
					"selfPct": float64(bloomSelfPercent(r)), "allyPct": float64(bloomAllyPercent(r)),
					"perRankPct": bloomPerRank, "shieldCapPct": shieldCapPercent,
				}
			})}
	}
	return SkillRules{AimKind: AimArc, Params: map[string]float64{}, Ranks: ranks(func(int) map[string]float64 { return map[string]float64{} })}
}
