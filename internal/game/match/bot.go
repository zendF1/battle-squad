package match

import (
	"math"
	"math/rand"
	"time"
)

type BotBrain struct {
	difficulty string // easy, normal
}

func NewBotBrain(difficulty string) *BotBrain {
	return &BotBrain{difficulty: difficulty}
}

func (b *BotBrain) DecideAction(
	botState *BattlePlayerState,
	match *MatchState,
) interface{} {
	// Simple seed for bot calculations
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 1. If HP is low, check if Medkit is available in items
	hasMedkit := false
	for _, it := range botState.Items {
		if it == "medkit" {
			hasMedkit = true
			break
		}
	}
	if botState.HP < 40 && hasMedkit && r.Float64() < 0.6 {
		// Use Medkit
		return UseItemAction{
			ItemID:          "medkit",
			ClientTimestamp: time.Now().Unix(),
		}
	}

	// 2. Find closest alive enemy
	var target *BattlePlayerState
	minDist := math.MaxFloat64

	for _, p := range match.Players {
		if !p.IsAlive || p.TeamID == botState.TeamID {
			continue
		}

		dx := p.Position.X - botState.Position.X
		dy := p.Position.Y - botState.Position.Y
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < minDist {
			minDist = dist
			target = p
		}
	}

	if target == nil {
		// No enemies found, skip turn or shoot randomly
		return ShootAction{
			Angle:           45.0,
			Power:           50.0,
			ActionMode:      "weapon",
			ClientTimestamp: time.Now().Unix(),
		}
	}

	// 3. Calculate firing angle to hit the target
	// Target X, Y relative to Bot
	dx := target.Position.X - botState.Position.X
	dy := target.Position.Y - botState.Position.Y // relative Y (Y-down)
	
	// Firing upwards in screen coords is negative Y, so we use -dy for standard math
	angleRad := math.Atan2(-dy, dx)
	angleDeg := angleRad * 180.0 / math.Pi

	// Limit firing range angles between 10 and 170 degrees (upwards)
	if angleDeg < 0 {
		angleDeg += 360
	}
	if angleDeg > 180 {
		// wrap back to facing target
		if dx < 0 {
			angleDeg = 135
		} else {
			angleDeg = 45
		}
	}

	// 4. Inject difficulty errors
	errorRange := 15.0 // Easy bot has +/- 15 degrees error
	if b.difficulty == "normal" {
		errorRange = 8.0 // Normal bot has +/- 8 degrees error
	}
	offset := (r.Float64() - 0.5) * 2.0 * errorRange
	angleDeg += offset

	// 5. Estimate power based on distance
	// Close: power 30-50, far: power 60-90
	power := 40.0
	if minDist > 800 {
		power = 80.0
	} else if minDist > 400 {
		power = 60.0
	}
	// Add slight power randomization
	power += (r.Float64() - 0.5) * 10.0
	if power < 20 {
		power = 20
	} else if power > 100 {
		power = 100
	}

	return ShootAction{
		Angle:           angleDeg,
		Power:           power,
		ActionMode:      "weapon",
		ClientTimestamp: time.Now().Unix(),
	}
}
