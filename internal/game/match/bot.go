package match

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"math"
	"math/big"
	"math/rand"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
)

// DefaultBotTier is used for matches that do not set a rank tier (casual rooms).
var DefaultBotTier = matchmaker.BotTierConfig{
	HitChance:     80,
	UseItemChance: 0.6,
	MovementSmart: 0.5,
}

const (
	botMinAngle = 5.0
	botMaxAngle = 175.0
	botMinPower = 10.0

	botCoarseAngleStep = 5.0
	botCoarsePowerStep = 5.0
	botRefineSpan      = 4.0 // ±span around a coarse candidate, searched at step 1
	botRefineTop       = 3   // coarse candidates refined

	botAllyBlastPenalty = 25.0 // explosion next to an ally digs out the ground under it

	botMaxSteps      = 3                       // medkit, move, shoot
	botFollowUpDelay = 1500 * time.Millisecond // let clients animate a heal or move before the next action

	// Idle time from TurnStarted to the bot's first action
	botTurnStartMinDelay = 1000 * time.Millisecond
	botTurnStartMaxDelay = 2500 * time.Millisecond

	// How long the bot visibly aims (PlayerAiming) before the shot is fired
	botAimMinDelay = 1200 * time.Millisecond
	botAimMaxDelay = 2400 * time.Millisecond
)

// randomDelay returns a uniform duration in [min, max].
func randomDelay(min, max time.Duration) time.Duration {
	return min + time.Duration(rand.Int63n(int64(max-min)+1))
}

// botStep performs one bot action on the match loop. Medkits and moves do not
// end the turn, so the bot schedules its next step after them.
func (m *Match) botStep(ctx context.Context, turn botTurnState, step int) {
	bot := m.State.Players[m.State.CurrentPlayerID]
	if m.resolving || bot == nil || !bot.IsBot || !bot.IsAlive {
		return
	}

	// Idle bots (tutorial target) and frozen bots just end their turn
	if step >= botMaxSteps || HasEffect(bot, "freeze") || bot.IsIdle {
		m.el.Log("EndTurn", bot.PlayerID, nil)
		m.endTurn(ctx)
		return
	}

	switch act := NewBotBrain(m.BotTier).DecideAction(m, bot, turn).(type) {
	case UseItemAction:
		m.el.Log("UseItem", bot.PlayerID, act)
		m.processUseItem(ctx, nil, act)
		turn.usedItem = true
	case MoveAction:
		m.el.Log("Move", bot.PlayerID, act)
		m.processMove(ctx, nil, act)
		turn.moved = true
	case ShootAction:
		// Aim visibly first; the shot follows unless the turn ends meanwhile
		delay := randomDelay(botAimMinDelay, botAimMaxDelay)
		m.selectBotAction(bot, act) // skill / shot item: its badge shows while it aims
		m.broadcastAiming(bot.PlayerID, act.Angle, delay)
		m.runOnTurnAfter(ctx, delay, func(ctx context.Context) {
			m.botShoot(ctx, bot.PlayerID, act)
		})
		return
	default:
		m.el.Log("EndTurn", bot.PlayerID, nil)
		m.endTurn(ctx)
		return
	}

	m.runOnTurnAfter(ctx, botFollowUpDelay, func(ctx context.Context) {
		m.botStep(ctx, turn, step+1)
	})
}

// botShoot fires the shot the bot aimed with PlayerAiming. runOnTurnAfter
// already dropped it if the match ended or the turn changed.
func (m *Match) botShoot(ctx context.Context, botID string, act ShootAction) {
	bot := m.State.Players[botID]
	if m.State.CurrentPlayerID != botID || m.resolving || bot == nil || !bot.IsAlive {
		return
	}
	m.el.Log("Shoot", botID, act)
	seq := m.turnSeq
	m.processShoot(ctx, nil, act)
	if m.turnSeq == seq && !m.resolving && m.State.Status == "in_progress" {
		m.endTurn(ctx) // shot was rejected; don't stall until the turn timer
	}
}

// broadcastAiming tells clients a player is aiming at angle (the angle the shot
// will be fired with) for duration before the shot. Power stays hidden.
func (m *Match) broadcastAiming(playerID string, angle float64, duration time.Duration) {
	ev := PlayerAiming{PlayerID: playerID, Angle: angle, DurationMs: int(duration.Milliseconds())}
	payload, _ := json.Marshal(ev)
	m.broadcast(ws.Message{Event: "PlayerAiming", Data: payload})
	m.el.Log("PlayerAiming", playerID, ev)
}

// BotBrain picks bot actions. Shots are aimed by simulating candidate
// trajectories against the live terrain, wind and weapon. Each shot then rolls
// against the tier's HitChance: a hit fires the best trajectory, a miss fires a
// nearby one that deals no damage.
type BotBrain struct {
	tier matchmaker.BotTierConfig
	rng  *rand.Rand
	roll func() int // hit roll in [0, 999]
}

func NewBotBrain(tier matchmaker.BotTierConfig) *BotBrain {
	return &BotBrain{tier: tier, rng: rand.New(rand.NewSource(time.Now().UnixNano())), roll: cryptoRoll}
}

// cryptoRoll draws a uniform number in [0, 999] from crypto/rand, so players
// cannot predict bot hits from the seed.
func cryptoRoll() int {
	n, err := crand.Int(crand.Reader, big.NewInt(1000))
	if err != nil {
		return 999 // crypto/rand.Reader does not fail since Go 1.24; count it as a miss
	}
	return int(n.Int64())
}

// rollHit reports whether this shot should hit: roll < HitChance×10, so 60%
// hits on 0–599 and 100% always hits.
func (b *BotBrain) rollHit() bool {
	return float64(b.roll()) < b.tier.HitChance*10
}

// botTurnState records what the bot already did this turn.
type botTurnState struct {
	usedItem bool
	moved    bool
}

// shotPlan is a simulated shot and how good its outcome is.
type shotPlan struct {
	angle, power float64
	damage       float64 // expected damage to enemies
	allyRisk     int     // allies inside the blast
	miss         float64 // distance from the explosion to the nearest enemy
	found        bool
}

func (s shotPlan) quality() float64 {
	return s.damage - botAllyBlastPenalty*float64(s.allyRisk)
}

// betterThan ranks by quality, then by how close a miss lands, then prefers arcs
// near 45°/135°.
func (s shotPlan) betterThan(o shotPlan) bool {
	if !o.found {
		return s.found
	}
	if !s.found {
		return false
	}
	if s.quality() != o.quality() {
		return s.quality() > o.quality()
	}
	if s.miss != o.miss {
		return s.miss < o.miss
	}
	return arcDeviation(s.angle) < arcDeviation(o.angle)
}

func arcDeviation(angle float64) float64 {
	return math.Min(math.Abs(angle-45), math.Abs(angle-135))
}

// DecideAction returns a UseItemAction, MoveAction or ShootAction for the bot,
// or nil when it cannot act (no weapon configured).
func (b *BotBrain) DecideAction(m *Match, bot *BattlePlayerState, turn botTurnState) interface{} {
	now := time.Now().Unix()

	if !turn.usedItem && b.wantsMedkit(bot) {
		return UseItemAction{ItemID: "medkit", ClientTimestamp: now}
	}

	weapon, ok := botWeapon(bot)
	if !ok {
		return nil
	}

	if bot.SkillEnergy >= skillEnergyMax {
		if act, ok := b.skillAction(m, bot, weapon, now); ok {
			return act
		}
	}

	best := b.bestShot(m, bot, bot.Position, weapon)

	// Every trajectory is blocked or falls short: look for a better spot first
	if best.quality() <= 0 && !turn.moved && bot.MoveEnergy > 0 && b.rng.Float64() < b.tier.MovementSmart {
		if targetX, ok := b.bestMove(m, bot, weapon, best); ok {
			dir := "right"
			if targetX < bot.Position.X {
				dir = "left"
			}
			return MoveAction{Direction: dir, TargetX: targetX, ClientTimestamp: now}
		}
	}

	if !best.found {
		best = shotPlan{angle: 45, power: 50}
		if b.nearestEnemyX(m, bot) < bot.Position.X {
			best.angle = 135
		}
	}

	shot := best
	if best.damage > 0 && !b.rollHit() {
		// Without a safe miss nearby (e.g. a huge blast radius) the best shot is fired
		if miss, ok := b.nearMiss(m, bot, best, weapon); ok {
			shot = miss
		}
	}
	return ShootAction{Angle: shot.angle, Power: shot.power, ActionMode: "weapon", ClientTimestamp: now}
}

// botMissOffsets are the angle/power nudges tried around the best shot to find
// a believable miss.
var botMissOffsets = [][2]float64{
	{0, -2}, {0, 2}, {0, -3}, {0, 3}, {0, -5}, {0, 5}, {-4, 0}, {4, 0},
	{0, -8}, {0, 8}, {-8, 0}, {8, 0}, {0, -12}, {0, 12}, {-15, 0}, {15, 0},
}

// botNearMissSlack is how far past the blast edge a deliberate miss may land.
const botNearMissSlack = 100.0

// nearMiss returns a shot close to best that lands just outside the enemy's
// blast radius, so the miss still looks aimed at them. It never damages an
// enemy or endangers an ally; among misses within botNearMissSlack it picks at
// random, otherwise the closest one.
func (b *BotBrain) nearMiss(m *Match, bot *BattlePlayerState, best shotPlan, weapon gamedata.WeaponConfig) (shotPlan, bool) {
	var near []shotPlan
	closest := shotPlan{}
	for _, off := range botMissOffsets {
		angle := math.Max(botMinAngle, math.Min(botMaxAngle, best.angle+off[0]))
		power := math.Max(botMinPower, math.Min(maxShotPower, best.power+off[1]))
		s := b.evalShot(m, bot, bot.Position, angle, power, weapon)
		if !s.found || s.damage > 0 || s.allyRisk > 0 {
			continue
		}
		if s.miss <= float64(weapon.ExplosionRadius)+botNearMissSlack {
			near = append(near, s)
		}
		if !closest.found || s.miss < closest.miss {
			closest = s
		}
	}
	if len(near) > 0 {
		return near[b.rng.Intn(len(near))], true
	}
	return closest, closest.found
}

func (b *BotBrain) wantsMedkit(bot *BattlePlayerState) bool {
	hasMedkit := false
	for _, it := range bot.Items {
		if it == "medkit" {
			hasMedkit = true
			break
		}
	}
	if !hasMedkit || bot.MaxHP <= 0 || float64(bot.HP)/float64(bot.MaxHP) >= 0.5 {
		return false
	}
	return b.rng.Float64() < b.tier.UseItemChance
}

func botWeapon(bot *BattlePlayerState) (gamedata.WeaponConfig, bool) {
	if gamedata.Data == nil {
		return gamedata.WeaponConfig{}, false
	}
	char, ok := gamedata.Data.Characters[bot.CharacterID]
	if !ok {
		return gamedata.WeaponConfig{}, false
	}
	weapon, ok := gamedata.Data.Weapons[char.WeaponID]
	return weapon, ok
}

// bestShot searches a coarse angle/power grid, then refines the best few
// candidates at 1° / 1 power resolution, for the bot standing at feet.
func (b *BotBrain) bestShot(m *Match, bot *BattlePlayerState, feet Vector2, weapon gamedata.WeaponConfig) shotPlan {
	var top []shotPlan // best first, at most botRefineTop
	for angle := botMinAngle; angle <= botMaxAngle; angle += botCoarseAngleStep {
		for power := botMinPower; power <= maxShotPower; power += botCoarsePowerStep {
			top = insertTop(top, b.evalShot(m, bot, feet, angle, power, weapon))
		}
	}

	best := shotPlan{}
	if len(top) > 0 {
		best = top[0]
	}
	for _, c := range top {
		for da := -botRefineSpan; da <= botRefineSpan; da++ {
			for dp := -botRefineSpan; dp <= botRefineSpan; dp++ {
				angle, power := c.angle+da, c.power+dp
				if angle < botMinAngle || angle > botMaxAngle || power < botMinPower || power > maxShotPower {
					continue
				}
				if s := b.evalShot(m, bot, feet, angle, power, weapon); s.betterThan(best) {
					best = s
				}
			}
		}
	}
	return best
}

func insertTop(top []shotPlan, s shotPlan) []shotPlan {
	if !s.found {
		return top
	}
	i := len(top)
	for i > 0 && s.betterThan(top[i-1]) {
		i--
	}
	if i >= botRefineTop {
		return top
	}
	top = append(top, shotPlan{})
	copy(top[i+1:], top[i:])
	top[i] = s
	if len(top) > botRefineTop {
		top = top[:botRefineTop]
	}
	return top
}

// evalShot simulates one shot exactly as processShoot would for the bot standing at feet
// (the shot starts at the body centre) and scores where it explodes.
func (b *BotBrain) evalShot(m *Match, bot *BattlePlayerState, feet Vector2, angle, power float64, weapon gamedata.WeaponConfig) shotPlan {
	r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOriginAt(m.Terrain, feet), angle, power, weapon,
		m.State.Wind, m.Terrain, m.State.Players, drillPierce(bot))
	if r.ExplosionPoint == nil {
		return shotPlan{}
	}
	ep := *r.ExplosionPoint
	radius := r.ExplosionRadius
	if m.Terrain.GetTerrainTypeAt(ep.X, ep.Y) == "fragile" {
		radius *= 2
	}

	s := shotPlan{angle: angle, power: power, miss: math.MaxFloat64, found: true}
	for _, p := range m.State.Players {
		if !p.IsAlive {
			continue
		}
		pos := p.Position
		if p.PlayerID == bot.PlayerID {
			pos = feet
		}
		dist := math.Hypot(pos.X-ep.X, pos.Y-ep.Y)
		if p.TeamID == bot.TeamID {
			if dist < radius {
				s.allyRisk++
			}
			continue
		}
		center := ep
		if p.PlayerID == r.HitPlayerID {
			center = p.Position // direct hit deals full damage
		}
		s.damage += float64(CalculateExplosionDamage(p.Position, center, float64(weapon.Damage+bot.DamageBonus), radius, p.Defense))
		s.miss = math.Min(s.miss, dist)
	}
	return s
}

// bestMove tries walking to a few spots in range and returns the walk target
// whose best shot beats current, if any; when no spot has a damaging shot it walks toward
// the nearest enemy instead. Walks that drop off the map are skipped.
func (b *BotBrain) bestMove(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig, current shotPlan) (float64, bool) {
	reach := float64(bot.MoveEnergy * 2) // processMove: 1 energy per 2px
	bestX, best, moved := 0.0, current, false
	for _, frac := range []float64{-1, -0.5, 0.5, 1} {
		targetX := bot.Position.X + frac*reach
		pos, ok := predictMove(m.Terrain, bot.Position, targetX)
		if !ok || math.Abs(pos.X-bot.Position.X) < 10 {
			continue // falls off the map, or blocked by a wall
		}
		if s := b.bestShot(m, bot, pos, weapon); s.betterThan(best) {
			bestX, best, moved = targetX, s, true
		}
	}
	if !moved && best.quality() <= 0 {
		// No spot in reach has a damaging shot: on a 2400 px map the enemy may simply be out
		// of range (a full-power shot carries ~2070 px), so close in on the nearest one.
		dir := 1.0
		if b.nearestEnemyX(m, bot) < bot.Position.X {
			dir = -1.0
		}
		for _, frac := range []float64{1, 0.5} {
			targetX := bot.Position.X + dir*frac*reach
			if pos, ok := predictMove(m.Terrain, bot.Position, targetX); ok && math.Abs(pos.X-bot.Position.X) >= 10 {
				return targetX, true
			}
		}
	}
	return bestX, moved
}

// predictMove mirrors processMove (WalkTo, then the ice slide) and returns where
// the player ends up. ok is false when any step of the walk drops to the bottom
// of the map, which WalkTo reports as y = Terrain.Height.
func predictMove(t *Terrain, from Vector2, targetX float64) (Vector2, bool) {
	pos, ok := walkChecked(t, from, targetX)
	if !ok || pos.X == from.X || t.GetTerrainTypeAt(pos.X, pos.Y) != "ice" {
		return pos, ok
	}
	slide := 50.0
	if pos.X < from.X {
		slide = -50.0
	}
	return walkChecked(t, pos, pos.X+slide)
}

// walkChecked walks one pixel at a time with WalkTo, which takes the same steps
// as a single WalkTo call, and stops at the first step that leaves the map.
func walkChecked(t *Terrain, from Vector2, targetX float64) (Vector2, bool) {
	endX := math.Round(targetX)
	if math.Round(from.X) == endX {
		return from, true // WalkTo leaves the position untouched
	}
	dir := 1.0
	if endX < from.X {
		dir = -1.0
	}
	pos := from
	for math.Round(pos.X) != endX {
		x, y := t.WalkTo(pos.X, pos.Y, math.Round(pos.X)+dir)
		if x == math.Round(pos.X) {
			break // blocked
		}
		pos = Vector2{X: x, Y: y}
		if y >= float64(t.Height) {
			return pos, false
		}
	}
	return pos, true
}

func (b *BotBrain) nearestEnemyX(m *Match, bot *BattlePlayerState) float64 {
	x, minDist := bot.Position.X+1, math.MaxFloat64
	for _, p := range m.State.Players {
		if !p.IsAlive || p.TeamID == bot.TeamID {
			continue
		}
		if d := math.Hypot(p.Position.X-bot.Position.X, p.Position.Y-bot.Position.Y); d < minDist {
			x, minDist = p.Position.X, d
		}
	}
	return x
}
