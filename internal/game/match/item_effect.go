package match

import (
	"context"
	"errors"

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
		// +30 HP
		player.HP += 30
		if player.HP > player.MaxHP {
			player.HP = player.MaxHP
		}
		log.Info().Str("playerId", playerID).Msg("applied Medkit")

	case "teleport":
		if targetPos == nil {
			return errors.New("teleport requires target position")
		}
		// Landing check on terrain
		landY := terrain.GetLandingY(targetPos.X, targetPos.Y)
		player.Position.X = targetPos.X
		player.Position.Y = landY
		log.Info().Str("playerId", playerID).Interface("pos", player.Position).Msg("applied Teleport")

	case "wind_stopper":
		// Active global effect
		effect := StatusEffect{
			EffectID:       "wind_stop",
			TargetPlayerID: "",
			DurationTurn:   2,
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

	// Consume item from player inventory
	player.Items = append(player.Items[:itemIdx], player.Items[itemIdx+1:]...)

	return nil
}
