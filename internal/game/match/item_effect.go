package match

import (
	"context"
	"errors"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/observability"
)

func ApplyImmediateItem(
	ctx context.Context,
	match *MatchState,
	playerID string,
	itemID string,
	targetPos *Vector2,
	terrain *Terrain,
) error {
	log := observability.FromContext(ctx)

	player, ok := match.Players[playerID]
	if !ok {
		return errors.New("player not found")
	}

	// Verify player has item
	hasItem := false
	itemIdx := -1
	for idx, it := range player.Items {
		if it == itemID {
			hasItem = true
			itemIdx = idx
			break
		}
	}
	if !hasItem {
		return errors.New("player does not own item")
	}

	switch itemID {
	case "medkit":
		player.HP += int(itemValue("medkit", defaultMedkitHeal))
		if player.HP > player.MaxHP {
			player.HP = player.MaxHP
		}
		log.Info().Str("playerId", playerID).Msg("applied Medkit")

	case "wind_stopper":
		// Active global effect
		effect := StatusEffect{
			EffectID:       "wind_stop",
			TargetPlayerID: "",
			DurationTurn:   int(itemValue("wind_stopper", 2)),
			SourcePlayerID: playerID,
		}
		match.ActiveEffects = append(match.ActiveEffects, effect)
		match.Wind.Power = 0
		match.Wind.Direction = 0
		log.Info().Str("playerId", playerID).Msg("applied Wind Stopper")

	default:
		// Other items (power_shot, drill_bomb, spider_net, freeze_bomb, air_strike) are shot-modifiers
		// handled when shooting. We apply them to player's active StatusEffects list.
		var duration = 1 // lasts 1 turn (current turn)
		switch itemID {
		case "power_shot":
			ApplyStatusEffect(player, StatusEffect{EffectID: "power_shot", TargetPlayerID: playerID, DurationTurn: duration, Value: 1.5, SourcePlayerID: playerID})
		case "drill_bomb":
			ApplyStatusEffect(player, StatusEffect{EffectID: "drill_bomb", TargetPlayerID: playerID, DurationTurn: duration, SourcePlayerID: playerID})
		case "spider_net":
			ApplyStatusEffect(player, StatusEffect{EffectID: "spider_net", TargetPlayerID: playerID, DurationTurn: duration, SourcePlayerID: playerID})
		case "freeze_bomb":
			ApplyStatusEffect(player, StatusEffect{EffectID: "freeze_bomb", TargetPlayerID: playerID, DurationTurn: duration, SourcePlayerID: playerID})
		case "air_strike":
			ApplyStatusEffect(player, StatusEffect{EffectID: "air_strike", TargetPlayerID: playerID, DurationTurn: duration, SourcePlayerID: playerID})
		default:
			return errors.New("unsupported item type")
		}
		log.Info().Str("playerId", playerID).Str("itemId", itemID).Msg("applied shot modifier item")
	}

	consumeItem(player, itemIdx)

	return nil
}

// shotModifierItems arm the player's next shot instead of acting immediately.
var shotModifierItems = map[string]bool{
	"power_shot":  true,
	"drill_bomb":  true,
	"spider_net":  true,
	"freeze_bomb": true,
	"air_strike":  true,
}

// IsShotModifierItem reports whether itemID modifies a shot rather than acting on use.
func IsShotModifierItem(itemID string) bool {
	return shotModifierItems[itemID]
}

// consumeItem removes the item at idx from the player's hand and records it as
// spent so its reservation is consumed when the match ends.
func consumeItem(player *BattlePlayerState, idx int) {
	player.UsedItems = append(player.UsedItems, player.Items[idx])
	player.Items = append(player.Items[:idx], player.Items[idx+1:]...)
}

// itemValue returns the configured value of an item, or fallback if it isn't configured.
func itemValue(itemID string, fallback float64) float64 {
	if gamedata.Data != nil {
		if it, ok := gamedata.Data.Items[itemID]; ok && it.Value > 0 {
			return it.Value
		}
	}
	return fallback
}
