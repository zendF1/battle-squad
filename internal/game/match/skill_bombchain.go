package match

import (
	"math"

	"battle-squad/internal/game/gamedata"
)

// Bomb chain (Tanko, skill heavy_bomb, effect bomb_chain): chainBaseCount + chainCountPerRank
// × rank bombs on the same trajectory, chainLaunchGap s apart; (chainTotalBase +
// chainTotalPerRank × rank) × base split evenly over them, which is also the per-target cap.
// The later bombs plunge into the first one's hole (plungeIntoHole). Blast chainBlast × the
// weapon's, crater chainCraterFirst × for the first bomb, each later one × chainCraterDecay of
// the previous; the craters keep the bottom chainFloorMargin px of the map. Every enemy
// damaged is rooted.
const (
	chainBaseCount    = 3
	chainCountPerRank = 1
	chainLaunchGap    = 0.3
	chainTotalBase    = 1.8
	chainTotalPerRank = 0.15
	chainBlast        = 1.3
	chainCraterFirst  = 1.4
	chainCraterDecay  = 0.85
	chainFloorMargin  = 48.0
	// A later bomb passing within chainHoleEnter px of the first bomb's explosion point falls
	// straight down the hole from there.
	chainHoleEnter = 40.0
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
// launched chainLaunchGap s apart and resolved in order; the later ones fly into the first
// one's hole, plunge down it (plungeIntoHole) and dig deeper. The craters keep the bottom
// chainFloorMargin px.
func resolveBombChain(m *Match, s *skillShot) {
	player, action, skillID, weapon, base, shot, tl := s.Player, s.Action, s.Skill.SkillID, s.Weapon, s.Base, s.Shot, s.TL
	n := chainCount(player.SkillRank)
	total := chainTotal(player.SkillRank) * base
	limit := newSkillCap(capOfBase(total))
	rooted := map[string]bool{}
	origin := shotOrigin(m.Terrain, player)
	floor := float64(m.Terrain.Height) - chainFloorMargin
	pierce := drillPierce(player)
	var hole *Vector2
	for i := 0; i < n; i++ {
		r := m.fireProjectile(player, origin, action.Angle, action.Power, weapon, m.State.Wind, pierce)
		if hole != nil {
			m.plungeIntoHole(player, r, *hole, weapon)
		} else if r.ExplosionPoint != nil {
			h := *r.ExplosionPoint
			hole = &h
		}
		delayPath(r, chainLaunchGap*float64(i))
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

// plungeIntoHole bends a later chain bomb down into the first bomb's hole: from the point of
// its flight closest to the hole's centre (within chainHoleEnter px) it falls straight down (no
// wind, at least its speed there), exploding where that drop ends. A bomb that never gets
// that close keeps its flight.
func (m *Match) plungeIntoHole(player *BattlePlayerState, r *ProjectileResult, hole Vector2, weapon gamedata.WeaponConfig) {
	k, best := -1, chainHoleEnter
	for i, st := range r.Path {
		if d := math.Hypot(st.Position.X-hole.X, st.Position.Y-hole.Y); d <= best {
			k, best = i, d
		}
	}
	if k < 0 {
		return
	}
	st := r.Path[k]
	drop := weapon
	drop.WindInfluence = 0
	power := math.Min(math.Max(math.Hypot(st.Velocity.X, st.Velocity.Y)/6, rainDropPower), maxShotPower)
	fall := SimulateProjectile(player.PlayerID, player.TeamID, st.Position, 270, power, drop, WindState{}, m.Terrain, m.State.Players, 0)
	path := append([]ProjectileStep{}, r.Path[:k+1]...)
	for _, p := range fall.Path[1:] {
		p.Time += st.Time
		path = append(path, p)
	}
	r.Path, r.ExplosionPoint, r.HitPlayerID = path, fall.ExplosionPoint, fall.HitPlayerID
}

// botBombChain: aimed like the bot's best normal shot at an enemy (aimedSkill).
func botBombChain(b *BotBrain, m *Match, bot *BattlePlayerState, weapon gamedata.WeaponConfig) (float64, float64, bool) {
	return b.aimedSkill(m, bot, weapon)
}
