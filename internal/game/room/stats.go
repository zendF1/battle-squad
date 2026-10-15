package room

import (
	"battle-squad/internal/api/character"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/match"
)

// battleStats are the stats a player brings into a match.
type battleStats = battlestats.Stats

// statPoints are the points a player allocated to a character
// (player_characters bonus_* columns).
type statPoints = battlestats.Points

// baseStats are a character's config HP and defense with no bonuses.
func baseStats(characterID string) battleStats {
	return battlestats.Base(characterID)
}

// addStatPoints converts allocated points to battle stats with the configured
// per-point multipliers and adds them to s.
func addStatPoints(s battleStats, pts statPoints, cfg character.ProgressionConfig) battleStats {
	return s.Add(battlestats.FromPoints(pts, cfg.Multipliers()))
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
		MoveEnergy:         battlestats.BaseMoveEnergy + s.MoveEnergyBonus,
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
