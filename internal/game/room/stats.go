package room

import (
	"battle-squad/internal/api/character"
	"battle-squad/internal/game/match"
)

// battleStats are the stats a player brings into a match.
type battleStats struct {
	HP                 int
	Defense            int
	DamageBonus        int     // flat, added to weapon damage
	MoveEnergyBonus    int     // flat, added to the 100 move energy each turn
	CritChance         float64 // percent
	SkillPowerBonus    int     // percent, scales skill damage and healing
	TerrainDamageBonus int     // percent, grows the crater radius
}

// statPoints are the points a player allocated to a character
// (player_characters bonus_* columns).
type statPoints struct {
	HP, Damage, Mobility, Defense, SkillPower, TerrainDamage int
}

// addStatPoints converts allocated points to battle stats with the configured
// per-point multipliers and adds them to s.
func addStatPoints(s battleStats, pts statPoints, cfg character.ProgressionConfig) battleStats {
	s.HP += pts.HP * cfg.StatMultiplier(character.StatHP)
	s.DamageBonus += pts.Damage * cfg.StatMultiplier(character.StatDamage)
	s.MoveEnergyBonus += pts.Mobility * cfg.StatMultiplier(character.StatMobility)
	s.Defense += pts.Defense * cfg.StatMultiplier(character.StatDefense)
	s.SkillPowerBonus += pts.SkillPower * cfg.StatMultiplier(character.StatSkillPower)
	s.TerrainDamageBonus += pts.TerrainDamage * cfg.StatMultiplier(character.StatTerrainDamage)
	return s
}

// newBattlePlayer builds a player's match state from the room entry and stats.
func newBattlePlayer(p RoomPlayer, spawn match.Vector2, s battleStats, isBot bool) *match.BattlePlayerState {
	return &match.BattlePlayerState{
		PlayerID:           p.PlayerID,
		DisplayName:        p.DisplayName,
		TeamID:             p.TeamID,
		CharacterID:        p.CharacterID,
		HP:                 s.HP,
		MaxHP:              s.HP,
		Defense:            s.Defense,
		DamageBonus:        s.DamageBonus,
		Position:           spawn,
		MoveEnergy:         100 + s.MoveEnergyBonus,
		MoveEnergyBonus:    s.MoveEnergyBonus,
		CritChance:         s.CritChance,
		SkillPowerBonus:    s.SkillPowerBonus,
		TerrainDamageBonus: s.TerrainDamageBonus,
		Items:              p.Items,
		StatusEffects:      []match.StatusEffect{},
		IsAlive:            true,
		IsBot:              isBot,
	}
}
