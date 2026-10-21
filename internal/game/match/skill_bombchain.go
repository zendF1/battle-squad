package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// Bomb chain (Tanko, skill heavy_bomb, effect bomb_chain): chainBaseCount + chainCountPerRank
// × rank bombs on the same trajectory, at least chainLaunchGap s apart; each deals chainEach(rank)
// × base: (chainTotalBase + chainTotalPerRank × rank) split over one bomb more than it fires
// (playtest 6 dropped a bomb to weaken the chain, keeping each bomb's damage); one target takes at most
// (chainCapBase + chainCapPerRank × rank) × base (before defense) from the whole chain.
// Every bomb flies the normal ballistic path of the shot (nothing steers it): a later one flies
// on through the craters of the earlier ones and explodes where that same arc now meets the
// terrain or a player, so a steep (lobbed) shot digs one deep shaft and a flat one carves a
// trench along its arc. Blast chainBlast × the weapon's; every bomb carves the same bowl as a
// normal shot of the weapon (playtest 5: was a circle 1.4 × the weapon's crater), and nothing
// caps how deep the chain digs (the bottom 48 px of the map were kept before): a column dug
// through to the bottom of the map lets a player standing in it fall out (out_of_map). Every
// enemy damaged is rooted.
//
// Replay coherence: the bombs are resolved one after another on the state the previous ones
// left (craters, landed positions), so each bomb explodes only once every fall started so far
// has landed (+ chainHitGap): a later bomb leaves late enough for that (pacedLaunch). The
// falls of the shot play chainFallSpeed × faster than free fall (slammed down the shaft), so
// the bombs keep a quick rhythm; the fall distance (and its damage) is the real one.
const (
	chainBaseCount    = 2 // playtest 6: was 3
	chainCountPerRank = 1
	chainLaunchGap    = 0.1
	chainTotalBase    = 1.8
	chainTotalPerRank = 0.15
	// Per-target cap of the whole chain (playtest round 2: was the total, 1.8 + 0.15 × rank).
	chainCapBase    = 1.2
	chainCapPerRank = 0.15
	chainBlast      = 1.3
	// The shot's falls play chainFallSpeed × faster than free fall; a bomb explodes at least
	// chainHitGap s after the last fall so far landed.
	chainFallSpeed = 3.0
	chainHitGap    = 0.08
	rootTurns      = 1
)

func init() {
	registerSkill(effectBombChain, resolveBombChain)
	registerBotSkill(effectBombChain, botBombChain)
}

func chainCount(rank int) int     { return chainBaseCount + chainCountPerRank*rank }
func chainTotal(rank int) float64 { return chainTotalBase + chainTotalPerRank*float64(rank) }

// chainEach is one bomb's damage (× base): chainTotal split over chainCount + 1 bombs, the share
// each had before playtest 6 removed one.
func chainEach(rank int) float64 { return chainTotal(rank) / float64(chainCount(rank)+1) }
func chainCap(rank int) float64   { return chainCapBase + chainCapPerRank*float64(rank) }

// bombChain: chainCount bombs on the same trajectory (same origin, angle, power and wind),
// launched at least chainLaunchGap s apart (pacedLaunch) and resolved in order, each on the
// terrain the previous ones left, each carving the weapon's own bowl.
func resolveBombChain(m *Match, s *skillShot) {
	player, action, skillID, weapon, base, shot, tl := s.Player, s.Action, s.Skill.SkillID, s.Weapon, s.Base, s.Shot, s.TL
	n := chainCount(player.SkillRank)
	each := chainEach(player.SkillRank) * base
	limit := newSkillCap(capOfBase(chainCap(player.SkillRank) * base))
	rooted := map[string]bool{}
	origin := shotOrigin(m.Terrain, player)
	pierce := drillPierce(player)
	tl.fallSpeed = chainFallSpeed
	launch := 0.0
	for i := 0; i < n; i++ {
		r := m.fireProjectile(player, origin, action.Angle, action.Power, weapon, m.State.Wind, pierce)
		if i > 0 {
			launch = pacedLaunch(launch, flightEnd(r), tl.landed())
		}
		delayPath(r, launch)
		r.SkillID, r.Kind = skillID, "bomb"
		r.ExplosionRadius = float64(weapon.ExplosionRadius) * chainBlast
		shot.Projectiles = append(shot.Projectiles, r)
		if r.ExplosionPoint == nil {
			continue
		}
		hit := m.explodeCapped(player, r, each, tl, limit)
		applyOnce(tl, flightEnd(r), player, hit, rooted, "root", rootTurns)
		m.settleFalls(player, flightEnd(r), tl)
	}
}

// pacedLaunch is when the next chain bomb leaves: chainLaunchGap after the previous one (which
// left at prev), or later when it would otherwise explode (flight s after leaving) before
// chainHitGap after landed, the end of the last fall so far.
func pacedLaunch(prev, flight, landed float64) float64 {
	return math.Max(prev+chainLaunchGap, landed+chainHitGap-flight)
}

// The bot's chain: every bomb flies the same arc, so a steep (lobbed) shot keeps hitting a
// target down one shaft while a flat one may carve on past it (with the weapon's own bowls,
// playtest 5, a flat shot at 600 px on grassland_valley reaches the cap too). The bot lobs (|angle - 90| <=
// chainBotLobSpan) when such a shot deals at least chainBotLobKeep of its best shot's damage.
// A lob whose first bomb flies longer than chainBotMaxFlight() s (chainBotFlightMargin under the
// simulation's MaxFlightSeconds, 4.5 s) is left out: the later, deeper bombs would pass it and
// vanish in the shaft. (Before the faster shells of migration 032: 4.5 s of 6 s; the same shot
// now flies ~0.67 × as long, so the old 1.5 s margin is ~1 s.)
const (
	chainBotLobSpan      = 30.0
	chainBotLobKeep      = 0.8
	chainBotFlightMargin = 1.0
)

// chainBotMaxFlight is the longest first flight of the bot's lobbed chain (s): 3.5 s.
func chainBotMaxFlight() float64 { return physics().MaxFlightSeconds - chainBotFlightMargin }

// shaftMiss is how far (x) from the enemy the shot hits, or else lands nearest, its arc flown
// without the players meets the terrain: where the later bombs dig the shaft the enemy falls
// into. A steep shell hits the top of a body wherever its arc passes over it, so the plain
// score (the explosion's distance to the feet) cannot tell an arc coming down onto the feet
// from one passing beside them.
func shaftMiss(m *Match, bot *BattlePlayerState, s shotPlan, weapon gamedata.WeaponConfig) float64 {
	origin := shotOrigin(m.Terrain, bot)
	r := SimulateProjectile(bot.PlayerID, bot.TeamID, origin, s.angle, s.power, weapon, m.State.Wind, m.Terrain, m.State.Players, drillPierce(bot))
	if r.ExplosionPoint == nil {
		return math.MaxFloat64
	}
	target := m.State.Players[r.HitPlayerID]
	if target == nil {
		for _, p := range m.State.Players {
			if p.IsAlive && p.TeamID != bot.TeamID && (target == nil ||
				math.Hypot(p.Position.X-r.ExplosionPoint.X, p.Position.Y-r.ExplosionPoint.Y) <
					math.Hypot(target.Position.X-r.ExplosionPoint.X, target.Position.Y-r.ExplosionPoint.Y)) {
				target = p
			}
		}
	}
	if target == nil {
		return math.MaxFloat64
	}
	ground := SimulateProjectile(bot.PlayerID, bot.TeamID, origin, s.angle, s.power, weapon, m.State.Wind, m.Terrain, nil, drillPierce(bot))
	if ground.ExplosionPoint == nil {
		return math.MaxFloat64
	}
	return math.Abs(ground.ExplosionPoint.X - target.Position.X)
}

// botBombChain: the best lobbed shot at an enemy (lobShot) when it is nearly as good as the
// best shot, else the best shot; it misses by the bot's hit chance like a normal shot.
func botBombChain(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	best := b.bestShot(m, bot, bot.Position, weapon)
	if !best.found || best.damage <= 0 {
		return 0, 0, false
	}
	shot := best
	if lob := b.lobShot(m, bot, weapon); lob.found && lob.damage >= chainBotLobKeep*best.damage {
		shot = lob
	}
	if !b.rollHit() {
		if miss, ok := b.nearMiss(m, bot, shot, weapon); ok {
			shot = miss
		}
	}
	return shot.angle, shot.power, true
}

// lobShot is the bot's steepest good shot: for every steep angle (5..chainBotLobSpan degrees
// off the vertical, both sides) its best power (a coarse grid, then refined; of the equally good
// fine powers the one with the smallest shaftMiss), and among those
// dealing at least chainBotLobKeep of the best one's damage the angle closest to the vertical
// (the bombs then dig straight down onto the target), flying at most chainBotMaxFlight() s.
func (b *BotBrain) lobShot(m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) shotPlan {
	var plans []shotPlan
	top := 0.0
	for off := 5.0; off <= chainBotLobSpan; off += 2.5 {
		for _, angle := range []float64{90 - off, 90 + off} {
			best := shotPlan{}
			for power := botMinPower; power <= maxShotPower; power += 2 {
				if s := b.evalShot(m, bot, bot.Position, angle, power, weapon); s.betterThan(best) {
					best = s
				}
			}
			if !best.found || best.damage <= 0 {
				continue
			}
			c := best
			var near []shotPlan // the fine powers around it
			for dp := -2.0; dp <= 2.0; dp += 0.25 {
				if power := c.power + dp; power >= botMinPower && power <= maxShotPower {
					s := b.evalShot(m, bot, bot.Position, angle, power, weapon)
					near = append(near, s)
					if s.betterThan(best) {
						best = s
					}
				}
			}
			// As good, the one whose shaft starts nearest the enemy's feet (shaftMiss)
			pick, pickMiss := best, shaftMiss(m, bot, best, weapon)
			for _, s := range near {
				if s.found && s.quality() == best.quality() {
					if d := shaftMiss(m, bot, s, weapon); d < pickMiss {
						pick, pickMiss = s, d
					}
				}
			}
			best = pick
			r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOrigin(m.Terrain, bot), best.angle, best.power, weapon,
				m.State.Wind, m.Terrain, m.State.Players, drillPierce(bot))
			if flightEnd(r) > chainBotMaxFlight() {
				continue
			}
			plans = append(plans, best)
			top = math.Max(top, best.damage)
		}
	}
	pick := shotPlan{}
	for _, s := range plans { // steepest first
		if s.damage < chainBotLobKeep*top {
			continue
		}
		if pick.found && math.Abs(s.angle-90) > math.Abs(pick.angle-90)+1e-9 {
			break
		}
		if s.betterThan(pick) {
			pick = s
		}
	}
	return pick
}
