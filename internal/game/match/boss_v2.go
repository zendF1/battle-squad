package match

import (
	"context"
	"encoding/json"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// Boss v2: mobile bosses and bomber minions (gamedata.MinionConfig behavior / movement,
// BossConfig flying / hover).
//
// A mover is a minion that is a bomber or flies. Its turn: TurnStarted, moverMoveDelaySec later
// its move (a walk of at most walkRange px: PlayerMoved, as a player's; a flight of at most
// flyRange px: UnitFlew), then moverActDelaySec after
// the move has played its strike: a bomber within detonateRange of a party player detonates (a
// ShotResolved actionMode self_destruct), else it fires a shot (PlayerAiming, then the shot
// moverActDelaySec later) aimed by the bot AI with its weak damage. Other minions play the bot
// AI as before (movement none: it never walks).
//
// A hovering boss (flying with a hover band) flies to a new random point of the band at the
// start of each of its turns (UnitFlew moverMoveDelaySec after TurnStarted), then shows its
// skill (ActionSelected) moverActDelaySec after landing and casts it bossCastDelaySec later.
//
// Flying units never fall: settleFalls skips them, so craters under them do not move them, and
// they never leave the map.
//
// Blasts: a detonation hits the party within explodeRadius; when a minion with explodeOnDeath
// dies (not by its own detonation, not with the boss), it blasts at its position right after its
// die event in the same ShotResolved, hitting the party and the other minions (not the boss), so
// blasts chain.

const (
	moverMoveDelaySec = 0.6   // TurnStarted → the move (a walk, a flight, the boss's hover)
	moverActDelaySec  = 0.4   // the move's end → the strike (a detonation, the aimed shot, ActionSelected)
	minionWalkSpeed   = 150.0 // px/s the server gives a minion's walk to play on the clients
	blastTime         = 0.2   // s into a self_destruct / death_blast ShotResolved: the blast
	bossFlyLift       = 100.0 // px a flying boss without a hover band floats over its spawn ground
	pillarFreeRadius  = 80.0  // px: a pillar top with a grounded unit nearer than this is taken
	shooterStandoff   = 300.0 // px a flying shooter keeps from its target

	// ActionSelfDestruct is ShotResolved.actionMode of a bomber's detonation; ActionDeathBlast
	// of a death blast outside a shot (a minion burnt to death at the start of its turn).
	ActionSelfDestruct = "self_destruct"
	ActionDeathBlast   = "death_blast"
	// CauseSelfDestruct / CauseDeathBlast are the damage / die causes of those blasts.
	CauseSelfDestruct = "self_destruct"
	CauseDeathBlast   = "death_blast"
)

// UnitFlew is sent when a flying unit (a drone, the witch) flies from From to To in Duration
// seconds (distance / its fly speed); the server's position is already To.
type UnitFlew struct {
	PlayerID string  `json:"playerId"`
	From     Vector2 `json:"from"`
	To       Vector2 `json:"to"`
	Duration float64 `json:"duration"`
}

// turnRNG is a random source of this turn for salt (deterministic per match and turn, like
// skillRNG, but not the same sequence).
func (m *Match) turnRNG(salt string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(m.State.MatchID + ":" + salt + ":" + strconv.Itoa(m.State.TurnIndex)))
	return rand.New(rand.NewSource(int64(h.Sum64())))
}

// minionCfg is minion p's config with its defaults filled in.
func (m *Match) minionCfg(p *BattlePlayerState) (gamedata.MinionConfig, bool) {
	if m.boss == nil || p.Role != RoleMinion {
		return gamedata.MinionConfig{}, false
	}
	mc, ok := m.boss.cfg.Minion(p.MinionID)
	return mc.Normalized(), ok
}

// isMover reports whether p plays the mover turn: a bomber, a leaper or a flying minion.
func (m *Match) isMover(p *BattlePlayerState) bool {
	return m.boss != nil && p.Role == RoleMinion &&
		(p.Behavior == gamedata.MinionBomber || p.Behavior == gamedata.MinionLeaper || p.Movement == gamedata.MinionMoveFly)
}

// bossHovers reports whether p is a flying boss with a hover band.
func (m *Match) bossHovers(p *BattlePlayerState) bool {
	return m.boss != nil && p.Role == RoleBoss && p.Flying && m.boss.cfg.Hover != nil
}

// placeBossSide puts the boss's side where it starts: the initial minions on random free pillar
// tops on a pillar arena, the fliers in the air over their spawns, a flying boss in its band.
// The party is already placed (NewMatch landed every grounded unit).
func (m *Match) placeBossSide() {
	b := m.boss
	rng := m.turnRNG("setup")
	for _, id := range b.minions {
		p := m.State.Players[id]
		if sp, ok := m.freePillar(rng); ok {
			p.Position = Vector2{X: sp.X, Y: m.Terrain.SpawnY(sp.X, sp.Y)}
		}
		if p.Flying {
			p.Position.Y = m.Terrain.SpawnY(p.Position.X, p.Position.Y)
		}
		m.liftFlier(p)
	}
	boss := m.bossPlayer()
	if boss == nil || !boss.Flying {
		return
	}
	ground := m.Terrain.SpawnY(boss.Position.X, boss.Position.Y)
	pos := Vector2{X: boss.Position.X, Y: math.Min(ground, float64(m.Terrain.Height)) - bossFlyLift}
	if h := b.cfg.Hover; h != nil {
		pos.X = math.Max(h.MinX, math.Min(h.MaxX, pos.X))
		pos.Y = math.Max(h.MinY, math.Min(h.MaxY, pos.Y))
	}
	boss.Position = m.clampFlier(boss, pos)
}

// minionMoveEnergy is minion p's move energy this turn: none for one that stays put or flies,
// else its energy, at most walkRange px of walking.
func (m *Match) minionMoveEnergy(p *BattlePlayerState) int {
	mc, ok := m.minionCfg(p)
	if !ok || mc.Movement != gamedata.MinionMoveWalk {
		return 0
	}
	return min(p.MoveEnergy, int(math.Ceil(mc.WalkRange/m.gd.Physics.PxPerEnergy())))
}

// liftFlier raises a flying minion standing on the ground (its spawn) to its hover height.
func (m *Match) liftFlier(p *BattlePlayerState) {
	if !p.Flying || p.Role != RoleMinion {
		return
	}
	mc, _ := m.minionCfg(p)
	p.Position = m.hoverOver(p, p.Position, mc.HoverHeight)
}

// freePillar is a random pillar top no grounded living unit stands on (pillarFreeRadius px);
// false when the arena has no pillars or none is free.
func (m *Match) freePillar(rng *rand.Rand) (Vector2, bool) {
	var free []Vector2
	for _, sp := range m.boss.pillars {
		taken := false
		for _, p := range m.State.Players {
			if p.IsAlive && !p.Flying && math.Hypot(p.Position.X-sp.X, p.Position.Y-sp.Y) < pillarFreeRadius {
				taken = true
				break
			}
		}
		if !taken {
			free = append(free, sp)
		}
	}
	if len(free) == 0 {
		return Vector2{}, false
	}
	return free[rng.Intn(len(free))], true
}

// clampFlier keeps a flier's position inside the map with its whole body below the top.
func (m *Match) clampFlier(p *BattlePlayerState, pos Vector2) Vector2 {
	scale := bodyScale(p)
	r := m.gd.Physics.PlayerHitRadius * scale
	top := shotOriginLift(m.Terrain)*scale + r + 8
	pos.X = math.Max(r, math.Min(float64(m.Terrain.Width)-r, pos.X))
	pos.Y = math.Max(top, math.Min(float64(m.Terrain.Height)-20, pos.Y))
	return pos
}

// hoverOver is where flier p hovers near pt: height px over the ground under pt (the top of the
// terrain pt is in, else the first surface below it); over a hole to the bottom at pt's height,
// at most 3 × height px above the bottom. Clamped to the map.
func (m *Match) hoverOver(p *BattlePlayerState, pt Vector2, height float64) Vector2 {
	pt = m.clampFlier(p, pt)
	ground := m.Terrain.SpawnY(pt.X, pt.Y)
	if ground < float64(m.Terrain.Height) {
		pt.Y = ground - height
	} else {
		pt.Y = math.Min(pt.Y, float64(m.Terrain.Height)-3*height)
	}
	return m.clampFlier(p, pt)
}

// nearestParty is the living party member nearest p, nil for none.
func (m *Match) nearestParty(p *BattlePlayerState) *BattlePlayerState {
	var best *BattlePlayerState
	bestD := math.MaxFloat64
	for _, q := range m.playersByID() {
		if !q.IsAlive || q.TeamID == p.TeamID {
			continue
		}
		if d := math.Hypot(q.Position.X-p.Position.X, q.Position.Y-p.Position.Y); d < bestD {
			best, bestD = q, d
		}
	}
	return best
}

// detonateTarget is the party member a bomber p is within detonateRange of (the nearest), nil
// when it is not a bomber or nobody is that close. A leaper detonates only on the turn after its
// leap (leapDetonateNextTurn).
func (m *Match) detonateTarget(p *BattlePlayerState) *BattlePlayerState {
	mc, ok := m.minionCfg(p)
	switch {
	case !ok:
		return nil
	case mc.Behavior == gamedata.MinionLeaper:
		if !mc.LeapDetonates() || !m.leaptLastTurn(p) {
			return nil
		}
	case mc.Behavior != gamedata.MinionBomber:
		return nil
	}
	t := m.nearestParty(p)
	if t == nil || math.Hypot(t.Position.X-p.Position.X, t.Position.Y-p.Position.Y) > mc.DetonateRange {
		return nil
	}
	return t
}

// flyUnit moves flier p to `to` and sends UnitFlew; it returns the flight's duration (distance /
// speed px/s, 0 when it stays).
func (m *Match) flyUnit(p *BattlePlayerState, to Vector2, speed float64) float64 {
	from := p.Position
	dist := math.Hypot(to.X-from.X, to.Y-from.Y)
	if dist < 1 {
		return 0
	}
	if speed <= 0 {
		speed = gamedata.DefaultFlySpeed
	}
	dur := math.Round(dist/speed*1000) / 1000
	p.Position = to
	ev := UnitFlew{PlayerID: p.PlayerID, From: from, To: to, Duration: dur}
	payload, _ := json.Marshal(ev)
	m.broadcast(ws.Message{Event: "UnitFlew", Data: payload})
	m.el.Log("UnitFlew", p.PlayerID, ev)
	return dur
}

// flyStep is where flier p goes toward goal this turn: along the straight line, at most rangePx
// px, hovering height px over the ground there.
func (m *Match) flyStep(p *BattlePlayerState, goal Vector2, rangePx, height float64) Vector2 {
	cur := p.Position
	dx, dy := goal.X-cur.X, goal.Y-cur.Y
	to := goal
	if d := math.Hypot(dx, dy); d > rangePx && d > 0 {
		to = Vector2{X: cur.X + dx*rangePx/d, Y: cur.Y + dy*rangePx/d}
	}
	return m.hoverOver(p, to, height)
}

// minionMove moves mover p toward the nearest party member (a flight, or a bomber's walk that
// never drops into a hole to the bottom) and returns how long the move takes to play (0: no
// move).
func (m *Match) minionMove(ctx context.Context, p *BattlePlayerState) float64 {
	mc, ok := m.minionCfg(p)
	target := m.nearestParty(p)
	if !ok || target == nil || !canMove(m.gd, p) {
		return 0
	}
	if mc.Behavior == gamedata.MinionLeaper {
		return m.leaperMove(p, mc)
	}
	switch p.Movement {
	case gamedata.MinionMoveFly:
		goal := Vector2{X: target.Position.X, Y: target.Position.Y - mc.HoverHeight}
		if mc.Behavior != gamedata.MinionBomber { // a flying shooter keeps its distance
			side := 1.0
			if p.Position.X < target.Position.X {
				side = -1
			}
			goal.X = target.Position.X + side*shooterStandoff
		}
		return m.flyUnit(p, m.flyStep(p, goal, mc.FlyRange, mc.HoverHeight), mc.FlySpeed)
	case gamedata.MinionMoveWalk:
		if mc.Behavior != gamedata.MinionBomber || m.detonateTarget(p) != nil {
			return 0
		}
		dir := 1.0
		if target.Position.X < p.Position.X {
			dir = -1
		}
		goalX := target.Position.X - dir*24 // beside the target
		if reach := math.Min(float64(p.MoveEnergy)*m.gd.Physics.PxPerEnergy(), mc.WalkRange); math.Abs(goalX-p.Position.X) > reach {
			goalX = p.Position.X + dir*reach
		}
		for _, f := range []float64{1, 0.75, 0.5, 0.25} {
			x := p.Position.X + (goalX-p.Position.X)*f
			pos, safe := predictMove(m.Terrain, p.Position, x)
			if !safe || math.Abs(pos.X-p.Position.X) < 1 {
				continue
			}
			from := p.Position
			m.processMove(ctx, nil, moveTo(p, x, time.Now().Unix()))
			return math.Max(math.Abs(p.Position.X-from.X)/minionWalkSpeed+0.2, 0.3)
		}
	}
	return 0
}

// moverTurn is a mover's turn after TurnStarted: the move, then (once it has played) the strike.
func (m *Match) moverTurn(ctx context.Context) {
	p := m.State.Players[m.State.CurrentPlayerID]
	if m.resolving || p == nil || !p.IsAlive || !m.isMover(p) {
		return
	}
	dur := m.minionMove(ctx, p)
	if m.resolving || !p.IsAlive {
		return
	}
	m.runOnTurnAfter(ctx, secs(dur), m.moverStrike)
}

// moverStrike detonates the bomber moverActDelaySec from now when it is in range, else aims
// (PlayerAiming) and fires its shot moverActDelaySec later.
func (m *Match) moverStrike(ctx context.Context) {
	p := m.State.Players[m.State.CurrentPlayerID]
	if m.resolving || p == nil || !p.IsAlive || !m.isMover(p) {
		return
	}
	if m.detonateTarget(p) != nil {
		m.runOnTurnAfter(ctx, secs(moverActDelaySec), func(ctx context.Context) { m.selfDestruct(ctx, p) })
		return
	}
	if m.leaperRests(p) {
		m.resolveTurnAfter(ctx, moverActDelaySec, m.endTurn)
		return
	}
	act, ok := m.minionShot(p)
	if !ok {
		m.endTurn(ctx)
		return
	}
	delay := secs(moverActDelaySec)
	m.broadcastAiming(p.PlayerID, act.Angle, delay)
	m.runOnTurnAfter(ctx, delay, func(ctx context.Context) { m.botShoot(ctx, p.PlayerID, act) })
}

// playMoverTurn is a mover's whole turn at once (tests, bot matches): move, then detonate or
// shoot.
func (m *Match) playMoverTurn(ctx context.Context, p *BattlePlayerState) {
	m.minionMove(ctx, p)
	if m.resolving || !p.IsAlive {
		return
	}
	if m.detonateTarget(p) != nil {
		m.selfDestruct(ctx, p)
		return
	}
	if m.leaperRests(p) {
		return
	}
	if act, ok := m.minionShot(p); ok {
		m.processShoot(ctx, nil, act)
	}
}

// minionShot is minion p's shot from where it is: the bot AI's best shot (the closest off-map
// one when none explodes), a near miss when its hit roll fails.
func (m *Match) minionShot(p *BattlePlayerState) (ShootAction, bool) {
	weapon, ok := botWeapon(m.gd, p)
	if !ok {
		return ShootAction{}, false
	}
	tier := m.BotTier
	if hc := m.minionHitChance(p); hc > 0 {
		tier.HitChance = hc
	}
	b := NewBotBrain(tier)
	best := b.bestShot(m, p, p.Position, weapon)
	if !best.found {
		best = b.closestOffMapShot(m, p, weapon)
	}
	shot := best
	if best.damage > 0 && !b.rollHit() {
		if miss, ok := b.nearMiss(m, p, best, weapon); ok {
			shot = miss
		}
	}
	return ShootAction{Angle: shot.angle, Power: shot.power, ActionMode: "weapon", ClientTimestamp: time.Now().Unix()}, true
}

// selfDestruct is bomber p's detonation: a ShotResolved (actionMode self_destruct, no
// projectiles) with its blast (explode, damage cause self_destruct), its die and the falls.
func (m *Match) selfDestruct(ctx context.Context, p *BattlePlayerState) {
	if m.resolving || !p.IsAlive || m.State.CurrentPlayerID != p.PlayerID {
		return
	}
	mc, _ := m.minionCfg(p)
	shot := &ShotResolved{ShooterID: p.PlayerID, ActionMode: ActionSelfDestruct}
	tl := m.newTimeline()
	killPlayer(p)
	p.ShotsFired++
	m.minionBlast(p, mc, p, blastTime, CauseSelfDestruct, false, tl)
	tl.add(ShotEvent{T: blastTime, Type: "die", PlayerID: p.PlayerID, Cause: CauseSelfDestruct})
	m.settleFalls(p, blastTime, tl)
	m.el.Log("SelfDestruct", p.PlayerID, nil)
	m.finishShot(ctx, shot, tl)
}

// minionBlast is the blast of minion src (config mc) at its position at t: an explode event
// (radius the crater, blastRadius the damage radius), then every living unit of the party within
// explodeRadius of it (with hitMinions also the other minions; never the boss or src) takes
// explodeDamagePct % (× deathBlastDamageFactor for cause death_blast) of its own max HP
// (explodeDamageOf target) or of src's (self), full at the body edge 0 px away (blastDistance)
// down to half at the radius, no defense, shield first (damage events with cause; die). Damage to minions counts as minion damage of credit (a party shooter).
func (m *Match) minionBlast(src *BattlePlayerState, mc gamedata.MinionConfig, credit *BattlePlayerState, t float64, cause string,
	hitMinions bool, tl *shotTimeline) {
	point := src.Position
	m.Terrain.CarveBowl(point.X, point.Y, mc.ExplodeCrater)
	tl.add(ShotEvent{T: t, Type: "explode", PlayerID: src.PlayerID, Position: vecPtr(point), Radius: mc.ExplodeCrater, Crater: CraterBowl,
		TerrainVersion: m.Terrain.Version, BlastRadius: mc.ExplodeRadius})
	hitRadius, lift := m.gd.Physics.PlayerHitRadius, shotOriginLift(m.Terrain)
	for _, p := range m.playersByID() {
		if !p.IsAlive || p == src || p.Role == RoleBoss || (p.TeamID == src.TeamID && !(hitMinions && p.Role == RoleMinion)) {
			continue
		}
		d := blastDistance(p, point, hitRadius, lift)
		if d > mc.ExplodeRadius {
			continue
		}
		base := p.MaxHP
		if mc.ExplodeDamageOf == gamedata.ExplodeOfSelf {
			base = src.MaxHP
		}
		pct := mc.ExplodeDamagePct
		if cause == CauseDeathBlast {
			pct *= mc.DeathBlastDamageFactor // a death blast is weaker than a detonation (the snowman's 0.5)
		}
		ef, effect := m.elementFactor(src, p)
		damage := int(math.Round(float64(base) * pct / 100 * (1 - 0.5*d/mc.ExplodeRadius) * ef))
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		src.DamageDealt += damage
		src.ShotsHit++
		if p.Role == RoleMinion && credit != nil {
			m.noteBossSideDamage(credit, p, damage)
		}
		e := damageEvent(t, p, damage, shieldBefore, cause)
		e.ElementEffect = effect
		tl.add(e)
		if killed {
			src.KillCount++
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: cause})
		}
	}
}

// deathBlasts adds the blast of every minion with explodeOnDeath that died in the shot (not by
// its own detonation, not with the boss, not below the map) right after its die event, then the
// falls; blasts that kill more of them chain. It reports whether any blast happened.
func (m *Match) deathBlasts(shooter *BattlePlayerState, tl *shotTimeline) bool {
	if m.boss == nil {
		return false
	}
	blasted := map[string]bool{}
	any := false
	for i := 0; i < len(tl.events); i++ {
		e := tl.events[i]
		if e.Type != "die" || e.Cause == CauseSelfDestruct || e.Cause == BossResultDefeated || blasted[e.PlayerID] {
			continue
		}
		p := m.State.Players[e.PlayerID]
		mc, ok := m.minionCfg(p)
		if p == nil || !ok || !mc.ExplodeOnDeath || p.Position.Y >= float64(m.Terrain.Height) {
			continue
		}
		blasted[p.PlayerID] = true
		m.minionBlast(p, mc, shooter, e.T, CauseDeathBlast, true, tl)
		m.settleFalls(shooter, e.T, tl)
		any = true
	}
	return any
}

// renumberTerrainVersions gives the explode events of sorted events the shot's terrain versions
// in time order: death blasts carve after the rest of the shot but may play earlier, and the
// clients count their carves in play order (carving commutes, so the masks agree).
func renumberTerrainVersions(events []ShotEvent) {
	var idx []int
	var versions []uint64
	for i, e := range events {
		if e.Type == "explode" && e.TerrainVersion > 0 {
			idx = append(idx, i)
			versions = append(versions, e.TerrainVersion)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for k, i := range idx {
		events[i].TerrainVersion = versions[k]
	}
}

// turnStartDeathBlast: minion p died at the start of its turn (burn); with explodeOnDeath it
// blasts in a ShotResolved of its own (actionMode death_blast), and the turn ends after it. It
// reports whether it did.
func (m *Match) turnStartDeathBlast(ctx context.Context, p *BattlePlayerState) bool {
	mc, ok := m.minionCfg(p)
	if !ok || p.IsAlive || !mc.ExplodeOnDeath || p.Position.Y >= float64(m.Terrain.Height) {
		return false
	}
	shot := &ShotResolved{ShooterID: p.PlayerID, ActionMode: ActionDeathBlast}
	tl := m.newTimeline()
	m.minionBlast(p, mc, nil, blastTime, CauseDeathBlast, true, tl)
	m.settleFalls(p, blastTime, tl)
	m.finishShot(ctx, shot, tl)
	return true
}

// bossHover flies a hovering boss to a random point of its band at least minMove px from where
// it is (the farthest of the tries when none is), returning the flight's duration.
func (m *Match) bossHover(boss *BattlePlayerState) float64 {
	if !m.bossHovers(boss) || !canMove(m.gd, boss) {
		return 0
	}
	h := m.boss.cfg.Hover.Normalized()
	rng := m.turnRNG("hover")
	cur := boss.Position
	best, bestD := cur, -1.0
	for i := 0; i < 48; i++ {
		pt := m.clampFlier(boss, Vector2{X: h.MinX + rng.Float64()*(h.MaxX-h.MinX), Y: h.MinY + rng.Float64()*(h.MaxY-h.MinY)})
		d := math.Hypot(pt.X-cur.X, pt.Y-cur.Y)
		if d >= h.MinMove {
			best = pt
			break
		}
		if d > bestD {
			best, bestD = pt, d
		}
	}
	return m.flyUnit(boss, best, h.FlySpeed)
}

// bossHoverTurn is a hovering boss's turn after TurnStarted: the flight, then its skill.
func (m *Match) bossHoverTurn(ctx context.Context) {
	boss := m.bossPlayer()
	if m.resolving || boss == nil || !boss.IsAlive || m.State.CurrentPlayerID != boss.PlayerID {
		return
	}
	dur := m.bossHover(boss)
	m.runOnTurnAfter(ctx, secs(dur+moverActDelaySec), m.bossAct)
}
