package match

import (
	"math"
	"sort"

	"battle-squad/internal/game/gamedata"
)

// Bot item use. Every bot item follows the human rules: instant items (medkit, wind_stopper)
// go through UseItem, shot items (teleport, power_shot, drill_bomb, spider_net, freeze_bomb,
// air_strike) through Shoot with actionMode "item", each consumed once from the bot's hand.
//
//   - medkit: at or below rules.bots medkitHpPercent of max HP (wantsMedkit);
//   - teleport: no shot from here damages an enemy and no walk in reach fixes it: fired to a
//     safe spot that has a damaging shot (teleportShot);
//   - drill_bomb: no shot from here damages, but a drilling one does;
//   - air_strike: no shot from here damages, but a strike over an enemy does;
//   - wind_stopper: no shot damages with the wind, one does without it;
//   - power_shot, freeze_bomb, spider_net: on a shot that hits (hitItem).
//
// The random items roll the tier's UseItemChance each time their rule applies; the medkit
// and the teleport do not.

// botHitItems are the items fired on a shot that already hits, in order of preference.
var botHitItems = []string{"power_shot", "freeze_bomb", "spider_net"}

// botBlockedItems are the items that open a shot when none from here damages.
var botBlockedItems = []string{"drill_bomb", "air_strike", "wind_stopper"}

// BotUsableItems are the items the bot knows how to use: the pool of its random item when
// rules.bots randomItems is empty.
var BotUsableItems = append(append([]string{"medkit", "teleport"}, botHitItems...), botBlockedItems...)

const (
	botTeleportBucketPx  = 40.0 // one candidate teleport landing per this many px of x
	botTeleportMinShift  = 60.0 // a teleport moves the bot at least this far
	botTeleportCheckStep = 10.0 // angle / power step of the shots checked from a landing
	botAirStrikeSpan     = 60.0 // an air strike is aimed within ±this of an enemy
	botAirStrikeStep     = 5.0
)

// wantsItem rolls the tier's UseItemChance for a random item the bot holds.
func (b *BotBrain) wantsItem(bot *BattlePlayerState, itemID string) bool {
	return hasItem(bot, itemID) && b.rng.Float64() < b.tier.UseItemChance
}

// hitItem is the damage item to fire with a shot that hits, "" for none.
func (b *BotBrain) hitItem(bot *BattlePlayerState) string {
	for _, it := range botHitItems {
		if hasItem(bot, it) {
			if b.rng.Float64() < b.tier.UseItemChance {
				return it
			}
			return ""
		}
	}
	return ""
}

// unblock is what the bot does when no shot from where it stands damages an enemy: walk to a
// spot with one (when the tier's MovementSmart roll lets it), else open one with an item,
// teleport to a safe spot with one, or walk to a better spot / toward the nearest enemy.
// false: fire the best (closest) shot from here.
func (b *BotBrain) unblock(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig, best shotPlan, turn botTurnState, now int64) (interface{}, bool) {
	var better, closeIn moveOption
	if !turn.moved && bot.MoveEnergy > 0 && canMove(m.gd, bot) {
		better, closeIn = b.bestMove(m, bot, weapon, best)
	}
	smart := b.rng.Float64() < b.tier.MovementSmart
	if better.ok && better.shot.quality() > 0 {
		if smart {
			return moveTo(bot, better.targetX, now), true
		}
		return nil, false // walking fixes it, but this bot does not see it
	}

	if act, ok := b.blockedItemAction(m, bot, weapon, turn, now); ok {
		return act, true
	}

	if plan, _, ok := b.teleportShot(m, bot, weapon); ok {
		item := "teleport"
		return ShootAction{Angle: plan.angle, Power: plan.power, ActionMode: "item", ItemID: &item, ClientTimestamp: now}, true
	}

	if smart {
		if better.ok {
			return moveTo(bot, better.targetX, now), true
		}
		if closeIn.ok {
			return moveTo(bot, closeIn.targetX, now), true
		}
	}
	return nil, false
}

// blockedItemAction uses a held item that gives the bot a damaging shot from here: a
// drilling shot, an air strike, or the wind stopper (then the shot follows next step). The
// item shots roll the tier's HitChance like a weapon shot and fire a near miss on a miss.
func (b *BotBrain) blockedItemAction(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig, turn botTurnState, now int64) (interface{}, bool) {
	env := b.env(m, bot)
	if b.wantsItem(bot, "drill_bomb") {
		drill := shotEnv{wind: env.wind, pierce: m.gd.ItemValue("drill_bomb", gamedata.DefaultDrillPierce)}
		if s := b.bestShotIn(m, bot, bot.Position, weapon, drill); s.quality() > 0 {
			if !b.rollHit() {
				if miss, ok := b.nearMissIn(m, bot, s, weapon, drill); ok {
					s = miss
				}
			}
			item := "drill_bomb"
			return ShootAction{Angle: s.angle, Power: s.power, ActionMode: "item", ItemID: &item, ClientTimestamp: now}, true
		}
	}
	if b.wantsItem(bot, "air_strike") {
		if x, ok := b.airStrikeX(m, bot, weapon, env); ok {
			item := "air_strike"
			aim := m.gd.Rules.Aim // the strike ignores the aim
			return ShootAction{Angle: aim.NoAimAngle, Power: aim.NoAimPower, ActionMode: "item", ItemID: &item, TargetX: &x,
				ClientTimestamp: now}, true
		}
	}
	if !turn.used("wind_stopper") && m.State.Wind.Power != 0 && b.wantsItem(bot, "wind_stopper") {
		calm := shotEnv{pierce: env.pierce}
		if s := b.bestShotIn(m, bot, bot.Position, weapon, calm); s.quality() > 0 {
			return UseItemAction{ItemID: "wind_stopper", ClientTimestamp: now}, true
		}
	}
	return nil, false
}

// airStrikeX is the column of the best air strike (falling from the top of the map at the
// rules.items angle and power, with the wind) within botAirStrikeSpan of an enemy, false when
// none damages an enemy without endangering an ally. On a miss roll it is moved off the
// enemies to a harmless column nearby when there is one.
func (b *BotBrain) airStrikeX(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig, env shotEnv) (float64, bool) {
	items := m.gd.Rules.Items
	width := float64(m.Terrain.Width)
	strike := func(x float64) shotPlan {
		return b.scoreShot(m, bot, bot.Position, Vector2{X: x}, items.AirStrikeAngle, items.AirStrikePower, weapon, shotEnv{wind: env.wind})
	}
	var columns []float64
	for _, p := range m.State.Players {
		if !p.IsAlive || p.TeamID == bot.TeamID {
			continue
		}
		for dx := -botAirStrikeSpan; dx <= botAirStrikeSpan; dx += botAirStrikeStep {
			if x := p.Position.X + dx; x >= 0 && x <= width {
				columns = append(columns, x)
			}
		}
	}
	sort.Float64s(columns)
	best, bestX := shotPlan{}, 0.0
	for _, x := range columns {
		if s := strike(x); s.betterThan(best) {
			best, bestX = s, x
		}
	}
	if best.quality() <= 0 {
		return 0, false
	}
	if !b.rollHit() {
		radius := float64(weapon.ExplosionRadius)
		for _, off := range []float64{radius + 20, -(radius + 20), radius + 50, -(radius + 50)} {
			x := math.Max(0, math.Min(width, bestX+off))
			if s := strike(x); s.found && s.damage <= 0 && s.allyRisk == 0 {
				return x, true
			}
		}
	}
	return bestX, true
}

// teleportShot aims the bot's teleport at a safe spot (safeLanding) from which a shot damages
// an enemy: every coarse angle / power is simulated exactly as shootTeleport fires it (the
// weapon's projectile, the wind, no drilling), one landing is kept per botTeleportBucketPx of
// x, and the landing with the best coarse shot wins. false: the bot holds no teleport, may not
// move, or no landing has a damaging shot.
func (b *BotBrain) teleportShot(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (shotPlan, Vector2, bool) {
	if !hasItem(bot, "teleport") || !canMove(m.gd, bot) {
		return shotPlan{}, Vector2{}, false
	}
	type landing struct {
		angle, power float64
		at           Vector2
	}
	byBucket := map[int]landing{}
	maxPower := m.gd.Rules.Aim.PowerMax
	origin := shotOrigin(m.Terrain, bot)
	for angle := botMinAngle; angle <= botMaxAngle; angle += botCoarseAngleStep {
		for power := botMinPower; power <= maxPower; power += botCoarsePowerStep {
			r := SimulateProjectile(bot.PlayerID, bot.TeamID, origin, angle, power, weapon, m.State.Wind, m.Terrain, m.State.Players, 0)
			at, ok := teleportLanding(m.Terrain, r)
			if !ok || math.Abs(at.X-bot.Position.X) < botTeleportMinShift || !b.safeLanding(m, bot, at) {
				continue
			}
			k := int(math.Floor(at.X / botTeleportBucketPx))
			if _, seen := byBucket[k]; !seen {
				byBucket[k] = landing{angle: angle, power: power, at: at}
			}
		}
	}
	keys := make([]int, 0, len(byBucket))
	for k := range byBucket {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	var chosen landing
	best := shotPlan{}
	env := shotEnv{wind: m.State.Wind}
	for _, k := range keys {
		l := byBucket[k]
		if s := b.quickShot(m, bot, l.at, weapon, env); s.quality() > 0 && s.betterThan(best) {
			best, chosen = s, l
		}
	}
	if !best.found {
		return shotPlan{}, Vector2{}, false
	}
	return shotPlan{angle: chosen.angle, power: chosen.power, found: true}, chosen.at, true
}

// quickShot is the best shot from feet on a botTeleportCheckStep grid (cheaper than bestShot,
// enough to tell whether a spot has a damaging shot).
func (b *BotBrain) quickShot(m *Match, bot *BattlePlayerState, feet Vector2, weapon gamedata.WeaponConfig, env shotEnv) shotPlan {
	best := shotPlan{}
	for angle := botMinAngle; angle <= botMaxAngle; angle += botTeleportCheckStep {
		for power := botMinPower; power <= m.gd.Rules.Aim.PowerMax; power += botTeleportCheckStep {
			if s := b.evalShotIn(m, bot, feet, angle, power, weapon, env); s.betterThan(best) {
				best = s
			}
		}
	}
	return best
}

// safeLanding reports whether the bot may teleport to at: on top of solid ground inside the
// map with room for its body (CanStandAt, not inside a wall), ground on both sides of its feet
// (not the lip of a bottomless hole), no lava within rules.bots teleportLavaMarginPx, and
// every enemy at least teleportEnemyMarginPx away horizontally (out of the crater line of
// their shots at it).
func (b *BotBrain) safeLanding(m *Match, bot *BattlePlayerState, at Vector2) bool {
	t, rules := m.Terrain, m.gd.Rules.Bots
	h := float64(t.Height)
	if at.X < 0 || at.X >= float64(t.Width) || at.Y >= h {
		return false
	}
	if !t.CanStandAt(at.X, at.Y) || t.SpawnY(at.X, at.Y) != at.Y {
		return false
	}
	lift := float64(t.rules().PlayerHeight)
	for _, dx := range []float64{-12, 12} {
		if t.GetLandingY(at.X+dx, at.Y-lift) >= h {
			return false
		}
	}
	margin := float64(rules.TeleportLavaMarginPx)
	for dx := -margin; dx <= margin; dx += 4 {
		if t.GetTerrainTypeAt(at.X+dx, at.Y) == "lava" || t.GetTerrainTypeAt(at.X+dx, at.Y-1) == "lava" {
			return false
		}
	}
	for _, p := range m.State.Players {
		if p.IsAlive && p.TeamID != bot.TeamID && math.Abs(p.Position.X-at.X) < float64(rules.TeleportEnemyMarginPx) {
			return false
		}
	}
	return true
}

// teleportLanding is where a fired teleport puts its player (shootTeleport): on the ground
// under where its projectile exploded; false when it explodes nowhere or below the map.
func teleportLanding(t *Terrain, r *ProjectileResult) (Vector2, bool) {
	if r.ExplosionPoint == nil || r.ExplosionPoint.Y >= float64(t.Height) {
		return Vector2{}, false
	}
	landY := t.GetLandingY(r.ExplosionPoint.X, r.ExplosionPoint.Y)
	if landY >= float64(t.Height) {
		return Vector2{}, false
	}
	return Vector2{X: r.ExplosionPoint.X, Y: landY}, true
}
