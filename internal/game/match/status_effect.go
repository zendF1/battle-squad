package match

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
	case "freeze":
		player.MoveEnergy = 0
	}
}

func UpdatePlayerStatusEffects(player *BattlePlayerState) {
	// Filter out expired status effects (duration <= 0)
	activeEffects := []StatusEffect{}
	for _, e := range player.StatusEffects {
		if e.DurationTurn > 0 {
			activeEffects = append(activeEffects, e)
		}
	}
	player.StatusEffects = activeEffects
}

func TickPlayerStatusEffects(player *BattlePlayerState) {
	// Decr duration at the start of player's turn
	for idx := range player.StatusEffects {
		player.StatusEffects[idx].DurationTurn--
	}
}

func HasEffect(player *BattlePlayerState, effectID string) bool {
	for _, e := range player.StatusEffects {
		if e.EffectID == effectID && e.DurationTurn > 0 {
			return true
		}
	}
	return false
}
