package gamedata

import (
	"battle-squad/internal/api/equipment"
	"battle-squad/internal/game/battlestats"
	gd "battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/room"
	"battle-squad/internal/shared/ranked"
)

// The accessor layer: the rules GET /gamedata sends that are still Go constants. Each comes
// from the package that plays it (match, room, equipment, battlestats, matchmaker, ranked,
// game/gamedata); clientTuning holds the few the server does not play but the client must not
// hardcode. Phase S2 moves them into the DB: only these accessors change where they read.

// clientTuning are the rules only the client applies (no server code reads them).
type clientTuning struct {
	AngleMin, AngleMax float64
	GuideArcLength     map[string]float64
	HitTolerance       HitTolerance
	CoinsPerGem        int
}

// clientRules returns the client-only tuning: the slingshot angle range, the aim guide's
// length per rank tier and its "hit" tolerance (playtest values of match/aim/aim_aid.gd), and
// the base gem → coin rate the shop compares coin packs with.
func clientRules() clientTuning {
	return clientTuning{
		AngleMin: 0, AngleMax: 180,
		GuideArcLength: map[string]float64{
			"bronze": 700, "silver": 650, "gold": 600, "platinum": 550, "diamond": 500, "master": 450,
		},
		HitTolerance: HitTolerance{Max: 20, MinPart: 0.2, FarPx: 1200, HighPx: 700},
		CoinsPerGem:  100,
	}
}

// matchRules are the match's constants (match.Rules).
func matchRules() match.ClientRules { return match.Rules() }

// skillRules are a skill effect type's aim, tuning and per-rank numbers.
func skillRules(effectType string) match.SkillRules { return match.SkillRulesOf(effectType) }

// statusRule is how the match treats a status effect.
func statusRule(effectID string) match.StatusRule { return match.StatusRuleOf(effectID) }

// statusEffectIDs are the effects the match knows, in display order.
func statusEffectIDs() []string { return match.StatusEffectIDs }

// itemAim is how an item is used: instant, an arc shot or a map column.
func itemAim(itemID string) (instant bool, aimKind string) {
	switch {
	case match.InstantItem(itemID):
		return true, ItemAimNone
	case match.TargetXItem(itemID):
		return false, ItemAimTargetX
	}
	return false, ItemAimArc
}

// loadoutRules: how many items a player brings, the default character.
func loadoutRules() (maxItems int, defaultCharacter string) {
	return room.MaxLoadoutItems, room.DefaultCharacterID
}

// rankBands are the rank tiers' rating ranges (ranked.Bands), lowest first.
func rankBands() []ranked.Band { return ranked.Bands }

// rankedQueue is the matchmaking mode / queue id.
func rankedQueue() string { return match.RankedMode }

// teamSlots is how many players a ranked team has.
func teamSlots() int { return matchmaker.TeamSlots }

// playerExpPerLevel: level N needs N × this EXP for N + 1.
func playerExpPerLevel() int { return match.PlayerExpPerLevel }

// equipmentRules are the upgrade and merge limits.
func equipmentRules() (step float64, maxLevel, maxStone, mergeMin, mergeMax int) {
	return battlestats.UpgradeStepPerLevel, equipment.MaxUpgradeLevel, equipment.MaxStoneLevel,
		equipment.MergeMinCount, equipment.MergeMaxCount
}

// defaultMoveEnergy is a character's base move energy when unconfigured.
func defaultMoveEnergy() int { return gd.DefaultMoveEnergy }

// mapThemes are the themes the client draws and the default one.
func mapThemes() (def string, themes []string) {
	return gd.DefaultMapTheme, append([]string(nil), gd.MapThemes...)
}
