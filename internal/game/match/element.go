package match

import "battle-squad/internal/game/gamedata"

// Elements (054, gamedata/elements.go): every unit carries an element (BattlePlayerState.Element:
// its character's, the boss's for the boss and its minions). Every attack damage from a unit to a
// unit (shells, skills, beams, rains, the nova, detonations and death blasts, the %-of-max-HP
// blasts included) is multiplied by elementFactor on the raw damage, where the other damage
// factors (power_shot, skill power, a skill's share) sit: before defense, the critical roll, the
// boss's minion shield and the target's shield, and the skill caps grow with it (capOfBase).
// Falls, out_of_map, the turn-start burn and lava and heals are not attacks: no element.
//
// PvP (every match but boss mode, bots included): the attacker's element beats the target's ×
// (1 + pvpStrongPct %) "strong", the target's beats the attacker's × (1 − pvpWeakPct %) "weak".
// Boss mode: a party member hitting a boss-side unit whose element its own beats × (1 +
// bossStrongPct %) "strong"; a boss-side unit hitting a party member its element beats × (1 +
// bossTakenPct %) "strong" (the attack is strong); never weaker. Same team or no element: × 1.

// Element effects (ShotEvent.ElementEffect).
const (
	ElementStrong = "strong"
	ElementWeak   = "weak"
)

// elementOf is p's element: its own, else its boss's (the boss side) or its character's.
func elementOf(gd *gamedata.Snapshot, p *BattlePlayerState) string {
	if p.Element != "" {
		return p.Element
	}
	if p.Role != "" {
		if b, ok := gd.Boss(p.BossID); ok {
			return b.Element
		}
		return ""
	}
	return gd.Data.Characters[p.CharacterID].Element
}

// assignElements sets every unit's element that has none (NewMatchWithData).
func assignElements(gd *gamedata.Snapshot, players map[string]*BattlePlayerState) {
	for _, p := range players {
		p.Element = elementOf(gd, p)
	}
}

// elementFactor is the element multiplier of an attack by att on tgt and its effect ("" neutral).
func (m *Match) elementFactor(att, tgt *BattlePlayerState) (float64, string) {
	if att == nil || tgt == nil || att == tgt || att.TeamID == tgt.TeamID {
		return 1, ""
	}
	a, t := elementOf(m.gd, att), elementOf(m.gd, tgt)
	r := m.gd.Rules.Elements
	if m.boss != nil {
		switch {
		case att.Role == "" && tgt.Role != "" && m.gd.ElementBeats(a, t):
			return 1 + float64(r.BossStrongPct)/100, ElementStrong
		case att.Role != "" && tgt.Role == "" && m.gd.ElementBeats(a, t):
			return 1 + float64(r.BossTakenPct)/100, ElementStrong
		}
		return 1, ""
	}
	switch {
	case m.gd.ElementBeats(a, t):
		return 1 + float64(r.PvpStrongPct)/100, ElementStrong
	case m.gd.ElementBeats(t, a):
		return 1 - float64(r.PvpWeakPct)/100, ElementWeak
	}
	return 1, ""
}
