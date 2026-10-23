package match

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"sort"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// Boss v3: free summons, wide boss skills, leaping minions.
//
// Free summon (gamedata.BossSummon): at the start of the boss's turn, when a wave is due (round ≥
// firstRound, the previous wave dead, more than cooldownRounds rounds since the last summon, room
// under waves.maxAlive), the boss summons first, moverMoveDelaySec after TurnStarted, in a
// ShotResolved of its own (actionMode summon, summon events only), then (moverActDelaySec after
// it has played) takes its attack turn as before: the hover flight, ActionSelected, the skill's
// ShotResolved. A stunned or frozen boss still summons, then loses the attack.
//
// Skills: rain over the party's mean x or the whole map (target party / map), beam patterns
// radial (Count beams around the boss's body centre) and sweep (Count horizontal beams across
// the map at the party's heights), and the shockwave (explodes stepping along the ground to both
// map edges). A party member hit by several beams of one cast is hit once.
//
// Leapers (minion behavior leaper): on its turn a leaper within detonateRange of a party member
// that leapt on its previous turn detonates (the bomber's self_destruct); one with nobody within
// detonateRange leaps (UnitJumped) to stand next to the nearest party member (firm ground within
// detonateRange of their feet, on their islet; never into the void) and, with shootAfterLeap,
// also shoots; otherwise it shoots. BattlePlayerState.leapt is true from its leap until the end
// of its next turn.

// ActionSummon is ShotResolved.actionMode of the boss's free summon.
const ActionSummon = "summon"

// UnitJumped is sent when a leaper jumps from From to To in Duration seconds along an arc whose
// apex is Height px above the higher end; the server's position is already To.
type UnitJumped struct {
	PlayerID string  `json:"playerId"`
	From     Vector2 `json:"from"`
	To       Vector2 `json:"to"`
	Duration float64 `json:"duration"`
	Height   float64 `json:"height"`
}

// summonDue is the boss's summon when a wave is due this round, false otherwise.
func (m *Match) summonDue() (*gamedata.BossSummon, bool) {
	b := m.boss
	if b == nil {
		return nil, false
	}
	s := b.cfg.SummonConfig()
	switch {
	case s == nil || len(b.spawns) == 0 || b.round < s.FirstRound:
		return nil, false
	case b.lastSummon > 0 && b.round-b.lastSummon <= s.CooldownRounds:
		return nil, false
	case m.waveAlive() || m.aliveMinions() >= b.cfg.Waves.MaxAlive:
		return nil, false
	}
	return s, true
}

// bossFreeSummon summons the due wave in a ShotResolved of its own (actionMode summon) that does
// not end the turn; it returns the shot's duration, false when no wave was due.
func (m *Match) bossFreeSummon(boss *BattlePlayerState) (float64, bool) {
	s, ok := m.summonDue()
	if !ok || boss == nil || !boss.IsAlive {
		return 0, false
	}
	shot := &ShotResolved{ShooterID: boss.PlayerID, ActionMode: ActionSummon}
	tl := m.newTimeline()
	if m.bossSummon(s.MinionID, s.Count, tl) == 0 {
		return 0, false
	}
	m.boss.lastSummon = m.boss.round
	tl.reach(bossChargeTime)
	m.el.Log("BossSummon", boss.PlayerID, s)
	m.broadcastShot(shot, tl)
	return shot.Duration, true
}

// bossSummonThenAttack is the boss's turn when a wave is due: the summon, then once it has
// played the attack turn (the hover flight first for a hovering boss).
func (m *Match) bossSummonThenAttack(ctx context.Context) {
	boss := m.bossPlayer()
	if m.resolving || boss == nil || !boss.IsAlive || m.State.CurrentPlayerID != boss.PlayerID {
		return
	}
	dur, _ := m.bossFreeSummon(boss)
	next := m.bossAct
	if m.bossHovers(boss) {
		next = m.bossHoverTurn
	}
	m.runOnTurnAfter(ctx, secs(dur+moverActDelaySec), next)
}

// bossSkippedSummon is a stunned or frozen boss's turn when a wave is due: the summon, then the
// turn ends.
func (m *Match) bossSkippedSummon(ctx context.Context) {
	boss := m.bossPlayer()
	if m.resolving || boss == nil || !boss.IsAlive || m.State.CurrentPlayerID != boss.PlayerID {
		return
	}
	dur, _ := m.bossFreeSummon(boss)
	m.resolveTurnAfter(ctx, dur+m.gd.Rules.Timings.SkipTurnDelaySec, m.endTurn)
}

// rainCentre is where a rain falls and its spread: the map's centre (spread 0: half the map
// width), the party's mean position, or the skill's target.
func (m *Match) rainCentre(boss *BattlePlayerState, s gamedata.BossSkill, rng *rand.Rand) (Vector2, float64, bool) {
	w, h := float64(m.Terrain.Width), float64(m.Terrain.Height)
	switch s.Target {
	case gamedata.BossTargetMap:
		spread := s.Spread
		if spread <= 0 {
			spread = w / 2
		}
		return Vector2{X: w / 2, Y: h / 2}, spread, m.anyEnemy(boss)
	case gamedata.BossTargetParty:
		var sum Vector2
		n := 0
		for _, p := range m.playersByID() {
			if p.IsAlive && p.TeamID != boss.TeamID {
				sum.X, sum.Y = sum.X+p.Position.X, sum.Y+p.Position.Y
				n++
			}
		}
		if n == 0 {
			return Vector2{}, 0, false
		}
		return Vector2{X: sum.X / float64(n), Y: sum.Y / float64(n)}, s.Spread, true
	}
	target := m.bossTarget(boss, s.Target, rng)
	if target == nil {
		return Vector2{}, 0, false
	}
	return target.Position, s.Spread, true
}

// rainArcTopY is the y a boss-launched rain shell falls from after its arc (inside the map, so
// the clients see the whole flight).
const rainArcTopY = 20.0

// rainArcSamples is the number of path points of a shell's arc from the boss.
const rainArcSamples = 10

// rainArcs are the arcs of a boss-launched rain (launchFrom boss): shell k leaves the ring of
// radius bodySize × RainRingPerBodySize around the boss's body centre (at an angle 360° / n · k
// turned by a random phase) and flies a quadratic curve, bowed upwards, to (xs[k], topY), where
// its fall starts; the arc's path points (times from its launch) and how long it takes. The arc
// goes over the terrain (the shards fly free); only the fall hits.
func (m *Match) rainArcs(boss *BattlePlayerState, xs []float64, topY float64, rng *rand.Rand) ([][]ProjectileStep, []float64) {
	centre := bodyCentre(boss, shotOriginLift(m.Terrain))
	ring := bodyScale(boss) * gamedata.RainRingPerBodySize
	n := len(xs)
	phase := rng.Float64() * 2 * math.Pi
	arcs := make([][]ProjectileStep, n)
	times := make([]float64, n)
	for k := range xs {
		a := phase + 2*math.Pi*float64(k)/float64(n)
		p0 := Vector2{X: centre.X + ring*math.Cos(a), Y: centre.Y + ring*math.Sin(a)}
		p1 := Vector2{X: xs[k], Y: topY}
		c := Vector2{X: (p0.X + p1.X) / 2, Y: math.Min(p0.Y, p1.Y) - 120}
		dur := math.Round(math.Max(0.4, math.Min(1.1, 0.35+math.Hypot(p1.X-p0.X, p1.Y-p0.Y)/2200))*1000) / 1000
		times[k] = dur
		for i := 0; i < rainArcSamples; i++ { // the last sample is the fall's first point
			u := float64(i) / rainArcSamples
			pos := Vector2{X: (1-u)*(1-u)*p0.X + 2*(1-u)*u*c.X + u*u*p1.X, Y: (1-u)*(1-u)*p0.Y + 2*(1-u)*u*c.Y + u*u*p1.Y}
			vel := Vector2{X: (2*(1-u)*(c.X-p0.X) + 2*u*(p1.X-c.X)) / dur, Y: (2*(1-u)*(c.Y-p0.Y) + 2*u*(p1.Y-c.Y)) / dur}
			arcs[k] = append(arcs[k], ProjectileStep{Position: pos, Velocity: vel, Time: u * dur})
		}
	}
	return arcs, times
}

// minionShotKinds sets the kind of a minion's weapon shot projectiles to its config's
// projectileKind (drone_bolt, crab_spit, snowball, …).
func (m *Match) minionShotKinds(shot *ShotResolved) {
	p := m.State.Players[shot.ShooterID]
	if p == nil || shot.ActionMode != "weapon" {
		return
	}
	mc, ok := m.minionCfg(p)
	if !ok || mc.ProjectileKind == "" {
		return
	}
	for _, r := range shot.Projectiles {
		if r.Kind == "" {
			r.Kind = mc.ProjectileKind
		}
	}
}

// anyEnemy reports whether a party member of p's enemies lives.
func (m *Match) anyEnemy(p *BattlePlayerState) bool {
	for _, q := range m.State.Players {
		if q.IsAlive && q.TeamID != p.TeamID {
			return true
		}
	}
	return false
}

// beamSegment is one beam of a cast.
type beamSegment struct{ from, to Vector2 }

// beamHitsOnce resolves a cast of several beams fired at bossChargeTime: every party member
// within halfWidth of any of them takes the skill's damage once (when the first beam reaches
// it, bossBeamSpeed px/s) and its status.
func (m *Match) beamHitsOnce(boss *BattlePlayerState, s gamedata.BossSkill, beams []beamSegment, tl *shotTimeline) {
	type hit struct {
		p *BattlePlayerState
		t float64
	}
	first := map[string]*hit{}
	for _, b := range beams {
		tl.add(ShotEvent{T: bossChargeTime, Type: "beam", PlayerID: boss.PlayerID, From: vecPtr(b.from), To: vecPtr(b.to), Duration: bossBeamTime})
		for _, h := range m.laserHits(boss, s.HalfWidth, b.from, b.to) {
			t := bossChargeTime + math.Max(h.s, 0)/bossBeamSpeed
			if cur, ok := first[h.p.PlayerID]; !ok || t < cur.t {
				first[h.p.PlayerID] = &hit{h.p, t}
			}
		}
	}
	boss.ShotsFired++
	hits := make([]*hit, 0, len(first))
	for _, h := range first {
		hits = append(hits, h)
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].t != hits[j].t {
			return hits[i].t < hits[j].t
		}
		return hits[i].p.PlayerID < hits[j].p.PlayerID
	})
	k := m.gd.Physics.DefenseKOrDefault()
	raw := s.Damage * float64(m.boss.cfg.Damage)
	for _, h := range hits {
		p := h.p
		damage := int(math.Round(reduceByDefense(raw, p.Defense, k)))
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		boss.DamageDealt += damage
		boss.ShotsHit++
		tl.add(damageEvent(h.t, p, damage, shieldBefore, "laser"))
		if killed {
			boss.KillCount++
			tl.add(ShotEvent{T: h.t, Type: "die", PlayerID: p.PlayerID, Cause: "laser"})
			continue
		}
		if s.Status != "" {
			m.applyShotStatus(tl, h.t, boss, p, s.Status, s.StatusTurns)
		}
	}
}

// bossRadialBeams: Count beams evenly spaced around the boss's body centre (360° / Count apart,
// the fan turned by a random angle), each out to the map edge, through terrain.
func (m *Match) bossRadialBeams(boss *BattlePlayerState, s gamedata.BossSkill, tl *shotTimeline, rng *rand.Rand) {
	if !m.anyEnemy(boss) {
		return
	}
	origin := bodyCentre(boss, shotOriginLift(m.Terrain))
	n := max(s.Count, 1)
	step := 2 * math.Pi / float64(n)
	phase := rng.Float64() * step
	beams := make([]beamSegment, 0, n)
	for k := 0; k < n; k++ {
		a := phase + step*float64(k)
		dir := Vector2{X: math.Cos(a), Y: math.Sin(a)}
		beams = append(beams, beamSegment{origin, beamEnd(origin, dir, m.Terrain.Width, m.Terrain.Height)})
	}
	m.beamHitsOnce(boss, s, beams, tl)
}

// bossSweepBeams: Count horizontal beams across the whole map width (from the boss's side to the
// other edge) at the party members' body heights, the rest evenly spaced over their y range.
func (m *Match) bossSweepBeams(boss *BattlePlayerState, s gamedata.BossSkill, tl *shotTimeline) {
	lift := shotOriginLift(m.Terrain)
	var ys []float64
	for _, p := range m.playersByID() {
		if p.IsAlive && p.TeamID != boss.TeamID {
			ys = append(ys, bodyCentre(p, lift).Y)
		}
	}
	if len(ys) == 0 {
		return
	}
	w := float64(m.Terrain.Width)
	from, to := w, 0.0
	if boss.Position.X < w/2 {
		from, to = 0, w
	}
	var beams []beamSegment
	for _, y := range sweepLines(ys, max(s.Count, 1), float64(m.Terrain.Height)) {
		beams = append(beams, beamSegment{Vector2{X: from, Y: y}, Vector2{X: to, Y: y}})
	}
	m.beamHitsOnce(boss, s, beams, tl)
}

// sweepLines are n beam heights: n evenly spaced over the party's y range (± sweepMargin, inside
// the map), each party height taking the place of the nearest one still free, sorted.
func sweepLines(ys []float64, n int, height float64) []float64 {
	const sweepMargin = 90.0
	sorted := append([]float64(nil), ys...)
	sort.Float64s(sorted)
	lo := math.Max(sorted[0]-sweepMargin, 20)
	hi := math.Min(sorted[len(sorted)-1]+sweepMargin, height-20)
	lines := make([]float64, n)
	for i := range lines {
		lines[i] = lo + (hi-lo)*(float64(i)+0.5)/float64(n)
	}
	taken := make([]bool, n)
	for _, y := range sorted {
		best := -1
		for i := range lines {
			if !taken[i] && (best < 0 || math.Abs(lines[i]-y) < math.Abs(lines[best]-y)) {
				best = i
			}
		}
		if best < 0 {
			break // more party heights than beams
		}
		lines[best], taken[best] = y, true
	}
	sort.Float64s(lines)
	return lines
}

// bossShockwave: a slam at the boss's feet (an explode with blastRadius) at bossChargeTime, then
// an explode on the ground every step px both ways out to the map edges, interval s apart (each
// carving a crater px bowl); every grounded party member takes damage × the boss damage ×
// max(minFactor, 1 − distance / the farther edge's), reduced by defense, when the wave reaches
// its x. Falls settle after the last explode.
func (m *Match) bossShockwave(boss *BattlePlayerState, s gamedata.BossSkill, tl *shotTimeline) {
	if !m.anyEnemy(boss) {
		return
	}
	step, interval, minF := s.Step, s.Interval, s.MinFactor
	if step <= 0 {
		step = gamedata.DefaultShockwaveStep
	}
	if interval <= 0 {
		interval = gamedata.DefaultShockwaveInterval
	}
	if minF <= 0 {
		minF = gamedata.DefaultShockwaveMinFactor
	}
	w, h := float64(m.Terrain.Width), float64(m.Terrain.Height)
	x0 := boss.Position.X
	boss.ShotsFired++
	m.Terrain.CarveBowl(boss.Position.X, boss.Position.Y, s.Crater)
	tl.add(ShotEvent{T: bossChargeTime, Type: "explode", Position: vecPtr(boss.Position), Radius: s.Crater, Crater: CraterBowl,
		TerrainVersion: m.Terrain.Version, BlastRadius: s.Blast})
	last := bossChargeTime
	for _, dir := range []float64{-1, 1} {
		for k := 1; ; k++ {
			x, edge := x0+dir*step*float64(k), false
			if x <= 2 {
				x, edge = 2, true
			} else if x >= w-2 {
				x, edge = w-2, true
			}
			t := bossChargeTime + interval*float64(k)
			if y := m.Terrain.GetLandingY(x, 0); y < h {
				m.Terrain.CarveBowl(x, y, s.Crater)
				tl.add(ShotEvent{T: t, Type: "explode", Position: vecPtr(Vector2{X: x, Y: y}), Radius: s.Crater, Crater: CraterBowl,
					TerrainVersion: m.Terrain.Version})
				last = math.Max(last, t)
			}
			if edge {
				break
			}
		}
	}
	maxD := math.Max(x0, w-x0)
	k := m.gd.Physics.DefenseKOrDefault()
	base := s.Damage * float64(m.boss.cfg.Damage)
	for _, p := range m.playersByID() {
		if !p.IsAlive || p.TeamID == boss.TeamID || p.Flying {
			continue
		}
		d := math.Abs(p.Position.X - x0)
		f := math.Max(minF, 1-d/maxD)
		t := bossChargeTime + d/step*interval
		damage := int(math.Round(reduceByDefense(base*f, p.Defense, k)))
		if damage <= 0 {
			continue
		}
		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		boss.DamageDealt += damage
		boss.ShotsHit++
		tl.add(damageEvent(t, p, damage, shieldBefore, "explosion"))
		if killed {
			boss.KillCount++
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "explosion"})
		}
	}
	m.settleFalls(boss, last, tl)
}

// isLeaper reports whether p is a leaping minion.
func (m *Match) isLeaper(p *BattlePlayerState) bool {
	return m.boss != nil && p.Role == RoleMinion && p.Behavior == gamedata.MinionLeaper
}

// leaptLastTurn reports whether leaper p leapt on its previous turn (not this one).
func (m *Match) leaptLastTurn(p *BattlePlayerState) bool { return p.Leapt && p.LeapSeq != m.turnSeq }

// leaptThisTurn reports whether leaper p leapt this turn.
func (m *Match) leaptThisTurn(p *BattlePlayerState) bool { return p.Leapt && p.LeapSeq == m.turnSeq }

// leaperTurnEnded clears p's leapt at the end of its turn after the one it leapt on.
func (m *Match) leaperTurnEnded(p *BattlePlayerState) {
	if p != nil && p.Leapt && p.LeapSeq != m.turnSeq {
		p.Leapt = false
	}
}

// partyWithin reports whether a living party member's feet are within r px of p's.
func (m *Match) partyWithin(p *BattlePlayerState, r float64) bool {
	t := m.nearestParty(p)
	return t != nil && math.Hypot(t.Position.X-p.Position.X, t.Position.Y-p.Position.Y) <= r
}

// leapOffsets are the px beside the target a leaper tries to land at, best first.
var leapOffsets = []float64{50, 45, 55, 40, 60, 35, 30}

// leapSpot is where leaper p (config mc) lands next to target: firm ground (standable, ground
// under both sides of its feet) leapOffsets px beside the target's feet, within 40 px of their
// height (their islet) and within detonateRange of them, inside leapRange; the side p comes from
// first. False when there is none.
func (m *Match) leapSpot(p, target *BattlePlayerState, mc gamedata.MinionConfig) (Vector2, bool) {
	w, h := float64(m.Terrain.Width), float64(m.Terrain.Height)
	side := 1.0
	if p.Position.X < target.Position.X {
		side = -1
	}
	for _, d := range leapOffsets {
		for _, sgn := range []float64{side, -side} {
			x := target.Position.X + sgn*d
			if x < 10 || x > w-10 {
				continue
			}
			y := m.Terrain.SpawnY(x, target.Position.Y-40)
			to := Vector2{X: x, Y: y}
			switch {
			case y >= h || math.Abs(y-target.Position.Y) > 40 || !m.Terrain.CanStandAt(x, y) || !m.firmGround(x, y):
				continue
			case math.Hypot(to.X-target.Position.X, to.Y-target.Position.Y) > mc.DetonateRange-4:
				continue
			case mc.LeapRange > 0 && math.Hypot(to.X-p.Position.X, to.Y-p.Position.Y) > mc.LeapRange:
				continue
			}
			return to, true
		}
	}
	return Vector2{}, false
}

// firmGround reports whether there is ground under both sides of feet at (x, y): 8 px left and
// right the surface is within 16 px of y (not an islet's lip over the void).
func (m *Match) firmGround(x, y float64) bool {
	for _, dx := range []float64{-8, 8} {
		ly := m.Terrain.GetLandingY(x+dx, y-24)
		if ly >= float64(m.Terrain.Height) || math.Abs(ly-y) > 16 {
			return false
		}
	}
	return true
}

// leapUnit jumps leaper p to `to` (UnitJumped), marks it leapt and returns the jump's duration.
func (m *Match) leapUnit(p *BattlePlayerState, to Vector2) float64 {
	from := p.Position
	dist := math.Hypot(to.X-from.X, to.Y-from.Y)
	dur := math.Round(math.Max(0.6, math.Min(1.5, 0.45+dist/1100))*1000) / 1000
	height := math.Round(math.Max(80, math.Min(320, 70+0.25*math.Abs(to.X-from.X))))
	p.Position = to
	p.Leapt, p.LeapSeq = true, m.turnSeq
	ev := UnitJumped{PlayerID: p.PlayerID, From: from, To: to, Duration: dur, Height: height}
	payload, _ := json.Marshal(ev)
	m.broadcast(ws.Message{Event: "UnitJumped", Data: payload})
	m.el.Log("UnitJumped", p.PlayerID, ev)
	return dur
}

// leaperMove is leaper p's move: a leap next to the nearest party member when nobody is within
// detonateRange (and it does not detonate this turn); 0 when it stays.
func (m *Match) leaperMove(p *BattlePlayerState, mc gamedata.MinionConfig) float64 {
	target := m.nearestParty(p)
	if target == nil || m.detonateTarget(p) != nil || m.partyWithin(p, mc.DetonateRange) {
		return 0
	}
	to, ok := m.leapSpot(p, target, mc)
	if !ok {
		return 0
	}
	return m.leapUnit(p, to)
}

// leaperDodgeMargin is how far past a leapt leaper's detonateRange a dodging bot walks.
const leaperDodgeMargin = 15.0

// dodgeLeapers is a party bot's walk away from the leapt leapers about to detonate next to it
// (within their detonateRange): the shortest safe walk (never off the map) within its move
// energy that ends more than detonateRange + leaperDodgeMargin px from every one of them; false
// when none threatens it or no walk gets clear.
func (b *BotBrain) dodgeLeapers(m *Match, bot *BattlePlayerState, now int64) (MoveAction, bool) {
	if m.boss == nil || bot.Role != "" || !canMove(m.gd, bot) || bot.MoveEnergy <= 0 {
		return MoveAction{}, false
	}
	type threat struct {
		pos Vector2
		r   float64
	}
	var threats []threat
	for _, p := range m.playersByID() {
		if !p.IsAlive || !p.Leapt || !m.isLeaper(p) {
			continue
		}
		mc, _ := m.minionCfg(p)
		if !mc.LeapDetonates() {
			continue
		}
		if r := mc.DetonateRange + leaperDodgeMargin; math.Hypot(p.Position.X-bot.Position.X, p.Position.Y-bot.Position.Y) <= r {
			threats = append(threats, threat{p.Position, r})
		}
	}
	if len(threats) == 0 {
		return MoveAction{}, false
	}
	reach := float64(bot.MoveEnergy) * m.gd.Physics.PxPerEnergy()
	for d := 10.0; d <= reach; d += 10 {
		for _, sgn := range []float64{1, -1} {
			x := bot.Position.X + sgn*d
			pos, safe := predictMove(m.Terrain, bot.Position, x)
			if !safe {
				continue
			}
			clear := true
			for _, th := range threats {
				if math.Hypot(th.pos.X-pos.X, th.pos.Y-pos.Y) <= th.r {
					clear = false
					break
				}
			}
			if clear {
				return moveTo(bot, x, now), true
			}
		}
	}
	return MoveAction{}, false
}

// leaperRests reports whether leaper p ends its turn without shooting: it leapt this turn and
// does not shoot after a leap.
func (m *Match) leaperRests(p *BattlePlayerState) bool {
	if !m.isLeaper(p) || !m.leaptThisTurn(p) {
		return false
	}
	mc, _ := m.minionCfg(p)
	return !mc.ShootAfterLeap
}
