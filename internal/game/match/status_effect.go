package match

import "battle-squad/internal/game/gamedata"

// How a status effect acts comes from the game data (config_status_effects: bad, skips_turn,
// blocks_move): an effect that skips the turn or blocks moving leaves no move energy, net caps
// it at the spider_net item value (Snapshot.NetMoveEnergyCap, 50).

// ApplyStatusEffect puts effect on player (a repeat only renews the duration) and applies its
// immediate part: an effect that blocks moving leaves no move energy, net caps it.
func ApplyStatusEffect(gd *gamedata.Snapshot, player *BattlePlayerState, effect StatusEffect) {
	// Check if player already has this effect. If so, merge or override duration
	found := false
	for idx, e := range player.StatusEffects {
		if e.EffectID == effect.EffectID {
			player.StatusEffects[idx].DurationTurn = effect.DurationTurn // override duration
			found = true
			break
		}
	}

	if !found {
		player.StatusEffects = append(player.StatusEffects, effect)
	}

	// Apply immediate effects if any
	switch {
	case effect.EffectID == "heal":
		player.HP += int(effect.Value)
		if player.HP > player.MaxHP {
			player.HP = player.MaxHP
		}
	case gd.StatusRule(effect.EffectID).BlocksMove:
		player.MoveEnergy = 0
	case effect.EffectID == "net":
		// net slows the target: MoveEnergy capped on their next turn (see ApplyTurnStartEffects)
		if limit := gd.NetMoveEnergyCap(); player.MoveEnergy > limit {
			player.MoveEnergy = limit
		}
	}
}

// ApplyTurnStartEffects re-applies movement control after the player's move energy has been
// reset at the start of their turn: an effect that blocks moving (freeze, stun, root) leaves no
// move energy, net caps it.
func ApplyTurnStartEffects(gd *gamedata.Snapshot, player *BattlePlayerState) {
	if blocksMove(gd, player) {
		player.MoveEnergy = 0
	} else if limit := gd.NetMoveEnergyCap(); HasEffect(player, "net") && player.MoveEnergy > limit {
		player.MoveEnergy = limit
	}
}

// blocksMove reports whether an active effect of the player blocks moving.
func blocksMove(gd *gamedata.Snapshot, player *BattlePlayerState) bool {
	for _, e := range player.StatusEffects {
		if e.DurationTurn > 0 && gd.StatusRule(e.EffectID).BlocksMove {
			return true
		}
	}
	return false
}

// TickPlayerStatusEffects runs at the end of the player's own turn: each effect
// loses one turn and expired ones are dropped. An effect with DurationTurn N
// therefore covers the target's next N turns (counting the current one when
// applied to the acting player).
func TickPlayerStatusEffects(player *BattlePlayerState) {
	player.StatusEffects = tickEffects(player.StatusEffects)
}

// tickEffects decrements every effect's duration and drops the expired ones.
func tickEffects(effects []StatusEffect) []StatusEffect {
	active := []StatusEffect{}
	for _, e := range effects {
		e.DurationTurn--
		if e.DurationTurn > 0 {
			active = append(active, e)
		}
	}
	return active
}

func HasEffect(player *BattlePlayerState, effectID string) bool {
	for _, e := range player.StatusEffects {
		if e.EffectID == effectID && e.DurationTurn > 0 {
			return true
		}
	}
	return false
}

// skipsTurn is the effect that skips the player's whole turn (no move, shot or item; the
// server ends it after rules.timings skipTurnDelaySec): the first active one of
// gd.SkipTurnEffects (stun, then freeze), "" for none.
func skipsTurn(gd *gamedata.Snapshot, player *BattlePlayerState) string {
	for _, id := range gd.SkipTurnEffects() {
		if HasEffect(player, id) {
			return id
		}
	}
	return ""
}

// canMove reports whether the player's effects let it walk or teleport (root, stun and
// freeze do not).
func canMove(gd *gamedata.Snapshot, player *BattlePlayerState) bool {
	return !blocksMove(gd, player)
}

// Cleanse removes the bad effects from the player and returns their ids, in list order.
func Cleanse(gd *gamedata.Snapshot, player *BattlePlayerState) []string {
	var removed []string
	kept := player.StatusEffects[:0:0]
	for _, e := range player.StatusEffects {
		if gd.StatusRule(e.EffectID).Bad {
			removed = append(removed, e.EffectID)
			continue
		}
		kept = append(kept, e)
	}
	if kept == nil {
		kept = []StatusEffect{}
	}
	player.StatusEffects = kept
	return removed
}
