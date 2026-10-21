package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// Bomb chain (Tanko, skill heavy_bomb, effect bomb_chain): chainBaseCount + chainCountPerRank
// × rank bombs on the same trajectory, at least chainLaunchGap s apart; (chainTotalBase +
// chainTotalPerRank × rank) × base split evenly over them, which is also the per-target cap.
// Every bomb flies the normal ballistic path of the shot (nothing steers it): a later one flies
// on through the craters of the earlier ones and explodes where that same arc now meets the
// terrain or a player, so a steep (lobbed) shot digs one deep shaft and a flat one carves a
// trench along its arc. Blast chainBlast × the
// weapon's, crater chainCraterFirst × for the first bomb, each later one × chainCraterDecay of
// the previous; the craters keep the bottom chainFloorMargin px of the map. Every enemy
// damaged is rooted.
//
// Replay coherence: the bombs are resolved one after another on the state the previous ones
// left (craters, landed positions), so each bomb explodes only once every fall started so far
// has landed (+ chainHitGap): a later bomb leaves late enough for that (pacedLaunch). The
// falls of the shot play chainFallSpeed × faster than free fall (slammed down the shaft), so
// the bombs keep a quick rhythm; the fall distance (and its damage) is the real one.
const (
	chainBaseCount    = 3
	chainCountPerRank = 1
	chainLaunchGap    = 0.1
	chainTotalBase    = 1.8
	chainTotalPerRank = 0.15
	chainBlast        = 1.3
	chainCraterFirst  = 1.4
	chainCraterDecay  = 0.85
	chainFloorMargin  = 48.0
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

// chainCraterScale is bomb i's crater (0-based) as a multiple of the weapon's.
func chainCraterScale(i int) float64 {
	s := chainCraterFirst
	for k := 0; k < i; k++ {
		s *= chainCraterDecay
	}
	return s
}

// bombChain: chainCount bombs on the same trajectory (same origin, angle, power and wind),
// launched at least chainLaunchGap s apart (pacedLaunch) and resolved in order, each on the
// terrain the previous ones left. The craters keep the bottom chainFloorMargin px.
func resolveBombChain(m *Match, s *skillShot) {
	player, action, skillID, weapon, base, shot, tl := s.Player, s.Action, s.Skill.SkillID, s.Weapon, s.Base, s.Shot, s.TL
	n := chainCount(player.SkillRank)
	total := chainTotal(player.SkillRank) * base
	limit := newSkillCap(capOfBase(total))
	rooted := map[string]bool{}
	origin := shotOrigin(m.Terrain, player)
	floor := float64(m.Terrain.Height) - chainFloorMargin
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
		r.CraterRadius = weaponCraterRadius(weapon) * chainCraterScale(i)
		r.floorY = floor
		shot.Projectiles = append(shot.Projectiles, r)
		if r.ExplosionPoint == nil {
			continue
		}
		hit := m.explodeCapped(player, r, total/float64(n), tl, limit)
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
// target down one shaft while a flat one carves on past it. The bot lobs (|angle - 90| <=
// chainBotLobSpan) when such a shot deals at least chainBotLobKeep of its best shot's damage.
// A lob whose first bomb flies longer than chainBotMaxFlight s is left out: the later, deeper
// bombs would pass the simulation's MaxFlightSeconds (6 s) and vanish in the shaft.
const (
	chainBotLobSpan   = 30.0
	chainBotLobKeep   = 0.8
	chainBotMaxFlight = 4.5
)

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
// off the vertical, both sides) its best power (a coarse grid, then refined), and among those
// dealing at least chainBotLobKeep of the best one's damage the angle closest to the vertical
// (the bombs then dig straight down onto the target), flying at most chainBotMaxFlight s.
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
			for dp := -2.0; dp <= 2.0; dp += 0.25 {
				if power := c.power + dp; power >= botMinPower && power <= maxShotPower {
					if s := b.evalShot(m, bot, bot.Position, angle, power, weapon); s.betterThan(best) {
						best = s
					}
				}
			}
			r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOrigin(m.Terrain, bot), best.angle, best.power, weapon,
				m.State.Wind, m.Terrain, m.State.Players, drillPierce(bot))
			if flightEnd(r) > chainBotMaxFlight {
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
