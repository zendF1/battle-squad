package match

import (
	"math"
	"math/rand"
	"time"

	"battle-squad/internal/game/matchmaker"
)

// SmartBotBrain is a state-based bot AI that evaluates and scores possible
// actions (shoot, move, use item) then picks the highest-scoring one, with
// noise injected according to the rank-tier BotTierConfig.
type SmartBotBrain struct {
	tierConfig matchmaker.BotTierConfig
	rng        *rand.Rand
}

// NewSmartBotBrain creates a SmartBotBrain configured for the given tier.
func NewSmartBotBrain(tierConfig matchmaker.BotTierConfig) *SmartBotBrain {
	return &SmartBotBrain{
		tierConfig: tierConfig,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// DecideAction evaluates the current match state for the bot and returns one
// of ShootAction, MoveAction, or UseItemAction.
func (b *SmartBotBrain) DecideAction(botState *BattlePlayerState, matchState *MatchState) interface{} {
	target := b.findClosestEnemy(botState, matchState)

	// Score each action category.
	shootScore := b.scoreShoot(botState, target)
	moveScore := b.scoreMove(botState, target)
	useItemScore := b.scoreUseItem(botState)

	// Add decision noise: uniform random in [0, DecisionNoise).
	noise := float64(b.tierConfig.DecisionNoise)
	shootScore += b.rng.Float64() * noise
	moveScore += b.rng.Float64() * noise
	useItemScore += b.rng.Float64() * noise

	// Pick the highest-scoring action.
	switch {
	case useItemScore >= shootScore && useItemScore >= moveScore:
		if action := b.buildUseItemAction(botState); action != nil {
			return *action
		}
		// Fall through to shoot if no item available.
		fallthrough
	case shootScore >= moveScore:
		return b.buildShootAction(botState, matchState, target)
	default:
		return b.buildMoveAction(botState, target)
	}
}

// ---------------------------------------------------------------------------
// Enemy detection
// ---------------------------------------------------------------------------

// findClosestEnemy returns the alive enemy player closest to the bot, or nil
// when no enemies remain.
func (b *SmartBotBrain) findClosestEnemy(botState *BattlePlayerState, matchState *MatchState) *BattlePlayerState {
	var closest *BattlePlayerState
	minDist := math.MaxFloat64

	for _, p := range matchState.Players {
		if !p.IsAlive || p.TeamID == botState.TeamID || p.PlayerID == botState.PlayerID {
			continue
		}
		dx := p.Position.X - botState.Position.X
		dy := p.Position.Y - botState.Position.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < minDist {
			minDist = dist
			closest = p
		}
	}
	return closest
}

// ---------------------------------------------------------------------------
// Scoring helpers
// ---------------------------------------------------------------------------

// scoreShoot returns a score for the shoot action. Highest when the target is
// in the sweet-spot range (200–1000 px). Returns 0 when there is no target.
func (b *SmartBotBrain) scoreShoot(botState *BattlePlayerState, target *BattlePlayerState) float64 {
	if target == nil {
		return 0
	}
	dx := target.Position.X - botState.Position.X
	dy := target.Position.Y - botState.Position.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	switch {
	case dist >= 200 && dist <= 1000:
		return 80 // ideal range
	case dist < 200:
		return 40 // too close – still shootable but not ideal
	default:
		return 55 // far but worth trying
	}
}

// scoreMove returns a score for a move action.
//   - Retreat (move away) scores high when close AND HP is low.
//   - Advance (move toward) scores high when the target is very far away.
//
// The base score is scaled by MovementSmart so smarter bots move more
// purposefully.
func (b *SmartBotBrain) scoreMove(botState *BattlePlayerState, target *BattlePlayerState) float64 {
	if target == nil {
		return 10
	}
	dx := target.Position.X - botState.Position.X
	dy := target.Position.Y - botState.Position.Y
	dist := math.Sqrt(dx*dx + dy*dy)

	hpRatio := float64(botState.HP) / float64(max(botState.MaxHP, 1))

	var base float64
	switch {
	case dist < 150 && hpRatio < 0.4:
		base = 75 // retreat: dangerously close and wounded
	case dist > 1200:
		base = 65 // advance: enemy too far to hit reliably
	case dist < 150:
		base = 50 // close but healthy – still consider repositioning
	default:
		base = 20 // neutral position, movement not strongly indicated
	}

	return base * b.tierConfig.MovementSmart
}

// scoreUseItem returns a score for using a consumable. High when HP is low and
// the bot has a medkit. Uses UseItemChance as a probability gate.
func (b *SmartBotBrain) scoreUseItem(botState *BattlePlayerState) float64 {
	if !b.hasMedkit(botState) {
		return 0
	}
	// Only activate when HP is genuinely low.
	hpRatio := float64(botState.HP) / float64(max(botState.MaxHP, 1))
	if hpRatio >= 0.5 {
		return 0
	}
	// Probability gate – models the chance the bot "thinks" to use the item.
	if b.rng.Float64() > b.tierConfig.UseItemChance {
		return 0
	}

	// Score scales with how hurt the bot is.
	urgency := (0.5 - hpRatio) / 0.5 // 0.0 at 50% HP → 1.0 at 0% HP
	return 60 + urgency*40            // 60–100
}

// ---------------------------------------------------------------------------
// Action builders
// ---------------------------------------------------------------------------

// buildShootAction computes the ideal angle toward target, applies wind
// compensation (proportional to MovementSmart / 2 as a proxy for aim smarts),
// then adds AccuracyError and PowerError noise.
func (b *SmartBotBrain) buildShootAction(botState *BattlePlayerState, matchState *MatchState, target *BattlePlayerState) ShootAction {
	now := time.Now().Unix()

	if target == nil {
		// No target: fire a random shot so the turn is not wasted.
		return ShootAction{
			Angle:           30 + b.rng.Float64()*120,
			Power:           40 + b.rng.Float64()*30,
			ActionMode:      "weapon",
			ClientTimestamp: now,
		}
	}

	dx := target.Position.X - botState.Position.X
	dy := target.Position.Y - botState.Position.Y

	// Standard math angle (Y-up). Screen coords have Y increasing downward,
	// so negate dy.
	angleRad := math.Atan2(-dy, dx)
	angleDeg := angleRad * 180.0 / math.Pi
	if angleDeg < 0 {
		angleDeg += 360
	}
	// Clamp to the upward firing arc (0°–180°).
	if angleDeg > 180 {
		if dx < 0 {
			angleDeg = 135
		} else {
			angleDeg = 45
		}
	}

	// Wind compensation: smarter bots (higher MovementSmart) correct for wind.
	// windCompensation is proportional to the tier's intelligence proxy.
	windCompensationSkill := b.tierConfig.MovementSmart / 2.0
	windOffset := float64(matchState.Wind.Direction) * float64(matchState.Wind.Power) * windCompensationSkill * 2.0
	angleDeg -= windOffset // compensate by aiming slightly against the wind

	// Accuracy noise: uniform in [-AccuracyError, +AccuracyError].
	angleDeg += (b.rng.Float64()*2 - 1) * b.tierConfig.AccuracyError

	// Power estimation from distance.
	dist := math.Sqrt(dx*dx + dy*dy)
	var basePower float64
	switch {
	case dist > 800:
		basePower = 80
	case dist > 400:
		basePower = 60
	default:
		basePower = 40
	}
	// Power noise: uniform in [-PowerError, +PowerError].
	basePower += (b.rng.Float64()*2 - 1) * b.tierConfig.PowerError
	basePower = math.Max(15, math.Min(100, basePower))

	return ShootAction{
		Angle:           angleDeg,
		Power:           basePower,
		ActionMode:      "weapon",
		ClientTimestamp: now,
	}
}

// buildMoveAction decides whether to advance toward or retreat from target
// based on HP ratio, then returns a MoveAction with an appropriate TargetX.
func (b *SmartBotBrain) buildMoveAction(botState *BattlePlayerState, target *BattlePlayerState) MoveAction {
	now := time.Now().Unix()

	if target == nil {
		// No target: stay put (move nowhere meaningful).
		return MoveAction{
			Direction:       "right",
			TargetX:         botState.Position.X + 50,
			ClientTimestamp: now,
		}
	}

	hpRatio := float64(botState.HP) / float64(max(botState.MaxHP, 1))
	dx := target.Position.X - botState.Position.X

	var direction string
	var targetX float64

	if hpRatio < 0.4 {
		// Retreat: move away from the enemy.
		if dx > 0 {
			direction = "left"
			targetX = botState.Position.X - 150
		} else {
			direction = "right"
			targetX = botState.Position.X + 150
		}
	} else {
		// Advance: move toward the enemy but stop at comfortable distance.
		desiredDist := 400.0
		if dx > 0 {
			direction = "right"
			targetX = target.Position.X - desiredDist
		} else {
			direction = "left"
			targetX = target.Position.X + desiredDist
		}
	}

	return MoveAction{
		Direction:       direction,
		TargetX:         targetX,
		ClientTimestamp: now,
	}
}

// buildUseItemAction returns a UseItemAction for the medkit if the bot has
// one and HP is below 50%, otherwise nil.
func (b *SmartBotBrain) buildUseItemAction(botState *BattlePlayerState) *UseItemAction {
	hpRatio := float64(botState.HP) / float64(max(botState.MaxHP, 1))
	if hpRatio >= 0.5 {
		return nil
	}
	if !b.hasMedkit(botState) {
		return nil
	}
	return &UseItemAction{
		ItemID:          "medkit",
		ClientTimestamp: time.Now().Unix(),
	}
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

func (b *SmartBotBrain) hasMedkit(botState *BattlePlayerState) bool {
	for _, it := range botState.Items {
		if it == "medkit" {
			return true
		}
	}
	return false
}

// max is a local integer helper (math.Max operates on float64).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
