package match

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"battle-squad/internal/game/ws"
)

// shotTimeline collects the events of one shot and the time the last of them ends.
type shotTimeline struct {
	events []ShotEvent
	end    float64
	// landing is when each player's last fall of the shot ends: a later fall of the same
	// player starts then, so falls play in order (bomb chain).
	landing map[string]float64
	// fallSpeed > 1 plays this shot's falls that many times faster than free fall (bomb chain:
	// the targets are slammed down the shaft between two bombs); <= 1 is free fall.
	fallSpeed float64
}

// fallTime is how long a fall of distance px takes in this shot.
func (tl *shotTimeline) fallTime(distance float64) float64 {
	if tl.fallSpeed > 1 {
		return fallDuration(distance) / tl.fallSpeed
	}
	return fallDuration(distance)
}

// landed is when the last fall of the shot so far ends (0: nobody fell).
func (tl *shotTimeline) landed() float64 {
	end := 0.0
	for _, t := range tl.landing {
		end = max(end, t)
	}
	return end
}

func (tl *shotTimeline) add(e ShotEvent) {
	tl.events = append(tl.events, e)
	tl.reach(e.T + e.Duration)
}

// reach extends the timeline to at least t (e.g. the end of a projectile's flight).
func (tl *shotTimeline) reach(t float64) {
	if t > tl.end {
		tl.end = t
	}
}

func (tl *shotTimeline) sorted() []ShotEvent {
	sort.SliceStable(tl.events, func(i, j int) bool { return tl.events[i].T < tl.events[j].T })
	return tl.events
}

func flightEnd(r *ProjectileResult) float64 {
	if len(r.Path) == 0 {
		return 0
	}
	return r.Path[len(r.Path)-1].Time
}

// fallDuration is the time a body takes to free-fall distance px under the configured gravity.
func fallDuration(distance float64) float64 {
	gravity := physics().Gravity
	if distance <= 0 || gravity <= 0 {
		return 0
	}
	return math.Sqrt(2 * distance / gravity)
}

func intPtr(v int) *int { return &v }

func vecPtr(v Vector2) *Vector2 { return &v }

// playersByID returns the match players in a stable order so timelines are deterministic.
func (m *Match) playersByID() []*BattlePlayerState {
	ps := make([]*BattlePlayerState, 0, len(m.State.Players))
	for _, p := range m.State.Players {
		ps = append(ps, p)
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].PlayerID < ps[j].PlayerID })
	return ps
}

// hurtPlayer subtracts damage from p, the shield first, clamping HP at 0, and reports whether
// it killed p.
func hurtPlayer(p *BattlePlayerState, damage int) bool {
	if p.Shield > 0 && damage > 0 {
		absorbed := min(p.Shield, damage)
		p.Shield -= absorbed
		damage -= absorbed
	}
	p.HP -= damage
	if p.HP > 0 {
		return false
	}
	p.HP = 0
	p.IsAlive = false
	return true
}

// killPlayer kills p outright (out of the map): HP and shield to 0. It returns what p lost.
func killPlayer(p *BattlePlayerState) int {
	lost := p.HP + p.Shield
	p.HP, p.Shield = 0, 0
	p.IsAlive = false
	return lost
}

// damageEvent is the damage event for p after a hit that took amount; it carries the shield
// when p had one before the hit (shieldBefore > 0).
func damageEvent(t float64, p *BattlePlayerState, amount, shieldBefore int, cause string) ShotEvent {
	e := ShotEvent{T: t, Type: "damage", PlayerID: p.PlayerID, Amount: amount, HP: intPtr(p.HP), Cause: cause}
	if shieldBefore > 0 {
		e.Shield = intPtr(p.Shield)
	}
	return e
}

// skillCap limits the damage a whole skill deals one target (missile rain, bomb chain).
type skillCap struct {
	limit func(p *BattlePlayerState) int
	dealt map[string]int
}

func newSkillCap(limit func(p *BattlePlayerState) int) *skillCap {
	return &skillCap{limit: limit, dealt: map[string]int{}}
}

// clamp returns how much of damage p may still take and counts it as dealt.
func (c *skillCap) clamp(p *BattlePlayerState, damage int) int {
	if c == nil {
		return damage
	}
	left := c.limit(p) - c.dealt[p.PlayerID]
	if damage > left {
		damage = max(left, 0)
	}
	c.dealt[p.PlayerID] += damage
	return damage
}

// capOfBase is a skillCap limit of factor × base damage, reduced by the target's defense.
func capOfBase(base float64) func(p *BattlePlayerState) int {
	return func(p *BattlePlayerState) int { return int(math.Round(ReduceByDefense(base, p.Defense))) }
}

// explodeProjectile resolves r's explosion at the end of its flight: it carves the crater and
// applies splash damage to the shooter's enemies. It returns the enemies the blast damaged.
func (m *Match) explodeProjectile(shooter *BattlePlayerState, r *ProjectileResult, baseDamage float64, tl *shotTimeline) []*BattlePlayerState {
	return m.explodeCapped(shooter, r, baseDamage, tl, nil)
}

// explodeCapped is explodeProjectile with the damage each target takes limited by limit (nil:
// none); the crater keeps the rows y >= r.floorY when set.
func (m *Match) explodeCapped(shooter *BattlePlayerState, r *ProjectileResult, baseDamage float64, tl *shotTimeline, limit *skillCap) []*BattlePlayerState {
	t := flightEnd(r)
	crater := craterRadius(shooter, r.CraterRadius)
	m.Terrain.DestroyCircleAbove(r.ExplosionPoint.X, r.ExplosionPoint.Y, crater, r.floorY)
	tl.add(ShotEvent{
		T: t, Type: "explode", ProjectileID: r.ProjectileID,
		Position: vecPtr(*r.ExplosionPoint), Radius: crater, Floor: r.floorY, TerrainVersion: m.Terrain.Version,
	})

	var hit []*BattlePlayerState
	for _, p := range m.playersByID() {
		// No friendly fire — skip teammates
		if !p.IsAlive || p.TeamID == shooter.TeamID {
			continue
		}

		// Direct hit: use player's own position as explosion center (distance=0, full damage)
		explosionCenter := *r.ExplosionPoint
		if p.PlayerID == r.HitPlayerID {
			explosionCenter = p.Position
		}

		damage := CalculateExplosionDamage(p.Position, explosionCenter, baseDamage, r.ExplosionRadius, p.Defense)
		damage, critical := ApplyCritical(damage, shooter.CritChance)
		damage = limit.clamp(p, damage)
		if damage <= 0 {
			continue
		}

		shieldBefore := p.Shield
		killed := hurtPlayer(p, damage)
		hit = append(hit, p)
		shooter.DamageDealt += damage
		shooter.ShotsHit++
		if killed {
			shooter.KillCount++
		}

		e := damageEvent(t, p, damage, shieldBefore, "explosion")
		e.ProjectileID, e.Critical = r.ProjectileID, critical
		tl.add(e)
		if killed {
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "explosion"})
		}
	}
	return hit
}

// settleFalls drops every living player onto the terrain left by an explosion at time t,
// applying fall damage on landing. Falling off the map kills instantly. A player still falling
// from an earlier explosion of the shot falls again once it has landed.
func (m *Match) settleFalls(shooter *BattlePlayerState, t float64, tl *shotTimeline) {
	for _, p := range m.playersByID() {
		if !p.IsAlive {
			continue
		}
		landY := m.Terrain.GetLandingY(p.Position.X, p.Position.Y)
		if landY <= p.Position.Y {
			continue
		}

		from := p.Position
		distance := landY - from.Y
		p.Position.Y = landY
		fall := tl.fallTime(distance)
		start := max(t, tl.landing[p.PlayerID])
		landed := start + fall
		if tl.landing == nil {
			tl.landing = map[string]float64{}
		}
		tl.landing[p.PlayerID] = landed
		tl.add(ShotEvent{T: start, Type: "fall", PlayerID: p.PlayerID, From: vecPtr(from), To: vecPtr(p.Position), Duration: fall})

		if landY >= float64(m.Terrain.Height) {
			shieldBefore := p.Shield
			lost := killPlayer(p)
			shooter.KillCount++
			tl.add(damageEvent(landed, p, lost, shieldBefore, "out_of_map"))
			tl.add(ShotEvent{T: landed, Type: "die", PlayerID: p.PlayerID, Cause: "out_of_map"})
			continue
		}

		if damage := CalculateFallDamage(distance); damage > 0 {
			shieldBefore := p.Shield
			killed := hurtPlayer(p, damage)
			tl.add(damageEvent(landed, p, damage, shieldBefore, "fall"))
			if killed {
				tl.add(ShotEvent{T: landed, Type: "die", PlayerID: p.PlayerID, Cause: "fall"})
			}
		}
	}
}

// applyShotStatus applies a status effect caused by the shot and records it at time t.
func applyShotStatus(tl *shotTimeline, t float64, source, target *BattlePlayerState, effectID string, turns int) {
	ApplyStatusEffect(target, StatusEffect{
		EffectID:       effectID,
		TargetPlayerID: target.PlayerID,
		DurationTurn:   turns,
		SourcePlayerID: source.PlayerID,
	})
	tl.add(ShotEvent{T: t, Type: "status", PlayerID: target.PlayerID, EffectID: effectID, Turns: turns})
}

// grantHitEnergy gives skillEnergyOnHit, once for the whole shot, to every living enemy of the
// shooter the shot took HP or shield from.
func (m *Match) grantHitEnergy(shooter *BattlePlayerState, tl *shotTimeline) {
	hit := map[string]bool{}
	for _, e := range tl.events {
		if e.Type == "damage" && e.Amount > 0 {
			hit[e.PlayerID] = true
		}
	}
	for id := range hit {
		p, ok := m.State.Players[id]
		if !ok || !p.IsAlive || p.TeamID == shooter.TeamID {
			continue
		}
		addSkillEnergy(p, skillEnergyOnHit)
	}
}

// addSkillEnergy adds n skill energy to p, capped at skillEnergyMax.
func addSkillEnergy(p *BattlePlayerState, n int) {
	p.SkillEnergy = min(p.SkillEnergy+n, skillEnergyMax)
}

// broadcastShot finalizes shot from tl and sends it to every client.
func (m *Match) broadcastShot(shot *ShotResolved, tl *shotTimeline) {
	if shot.Projectiles == nil {
		shot.Projectiles = []*ProjectileResult{}
	}
	for _, r := range shot.Projectiles {
		tl.reach(flightEnd(r))
	}
	shooter := m.State.Players[shot.ShooterID]
	m.grantHitEnergy(shooter, tl)
	shot.Events = tl.sorted()
	if shot.Events == nil {
		shot.Events = []ShotEvent{}
	}
	shot.Duration = tl.end
	shot.SkillEnergy = shooter.SkillEnergy
	shot.SkillEnergies = make(map[string]int, len(m.State.Players))
	for id, p := range m.State.Players {
		shot.SkillEnergies[id] = p.SkillEnergy
	}
	shooter.ShotTurns++

	payload, _ := json.Marshal(shot)
	m.broadcast(ws.Message{Event: "ShotResolved", Data: payload})
	m.el.Log("ShotResolved", shot.ShooterID, shot.Events)
}

// finishShot broadcasts the shot, then ends the match or schedules the end of the turn once
// clients have replayed it.
func (m *Match) finishShot(ctx context.Context, shot *ShotResolved, tl *shotTimeline) {
	m.broadcastShot(shot, tl)
	m.checkWinCondition(ctx)
	if m.State.Status == "in_progress" {
		m.scheduleEndTurn(ctx, shot.Duration)
	}
}
