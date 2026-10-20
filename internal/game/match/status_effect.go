package match

const netMoveEnergyCap = 50

func ApplyStatusEffect(player *BattlePlayerState, effect StatusEffect) {
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
	switch effect.EffectID {
	case "heal":
		player.HP += int(effect.Value)
		if player.HP > player.MaxHP {
			player.HP = player.MaxHP
		}
	case "freeze", "stun", "root":
		player.MoveEnergy = 0
	case "net":
		// net slows the target: MoveEnergy capped on their next turn (see ApplyTurnStartEffects)
		if player.MoveEnergy > netMoveEnergyCap {
			player.MoveEnergy = netMoveEnergyCap
		}
	}
}

// ApplyTurnStartEffects re-applies movement control after the player's move
// energy has been reset at the start of their turn: freeze, stun and root leave no move
// energy, net caps it.
func ApplyTurnStartEffects(player *BattlePlayerState) {
	if HasEffect(player, "freeze") || HasEffect(player, "stun") || HasEffect(player, "root") {
		player.MoveEnergy = 0
	} else if HasEffect(player, "net") && player.MoveEnergy > netMoveEnergyCap {
		player.MoveEnergy = netMoveEnergyCap
	}
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
// server ends it after skipTurnDelay): "stun" or "freeze", "" for none.
func skipsTurn(player *BattlePlayerState) string {
	for _, id := range []string{"stun", "freeze"} {
		if HasEffect(player, id) {
			return id
		}
	}
	return ""
}

// canMove reports whether the player's effects let it walk or teleport (root, stun and
// freeze do not).
func canMove(player *BattlePlayerState) bool {
	return skipsTurn(player) == "" && !HasEffect(player, "root")
}

// Cleanse removes the badEffects from the player and returns their ids, in list order.
func Cleanse(player *BattlePlayerState) []string {
	var removed []string
	kept := player.StatusEffects[:0:0]
	for _, e := range player.StatusEffects {
		if badEffects[e.EffectID] {
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
