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
	_, gravity, _, _, _, _ := getPhysics()
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

// hurtPlayer subtracts damage from p, clamping at 0, and reports whether it killed p.
func hurtPlayer(p *BattlePlayerState, damage int) bool {
	p.HP -= damage
	if p.HP > 0 {
		return false
	}
	p.HP = 0
	p.IsAlive = false
	return true
}

// explodeProjectile resolves r's explosion at the end of its flight: it carves the crater and
// applies splash damage to the shooter's enemies. It returns the enemies the blast damaged.
func (m *Match) explodeProjectile(shooter *BattlePlayerState, r *ProjectileResult, baseDamage float64, tl *shotTimeline) []*BattlePlayerState {
	t := flightEnd(r)
	crater := craterRadius(shooter, r.ExplosionRadius)
	m.Terrain.DestroyCircle(r.ExplosionPoint.X, r.ExplosionPoint.Y, crater)
	tl.add(ShotEvent{
		T: t, Type: "explode", ProjectileID: r.ProjectileID,
		Position: vecPtr(*r.ExplosionPoint), Radius: crater, TerrainVersion: m.Terrain.Version,
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
		if damage <= 0 {
			continue
		}

		killed := hurtPlayer(p, damage)
		hit = append(hit, p)
		shooter.DamageDealt += damage
		shooter.ShotsHit++
		if killed {
			shooter.KillCount++
		}

		tl.add(ShotEvent{
			T: t, Type: "damage", PlayerID: p.PlayerID, ProjectileID: r.ProjectileID,
			Amount: damage, HP: intPtr(p.HP), Critical: critical, Cause: "explosion",
		})
		if killed {
			tl.add(ShotEvent{T: t, Type: "die", PlayerID: p.PlayerID, Cause: "explosion"})
		}
	}
	return hit
}

// settleFalls drops every living player onto the terrain left by an explosion at time t,
// applying fall damage on landing. Falling off the map kills instantly.
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
		fall := fallDuration(distance)
		landed := t + fall
		tl.add(ShotEvent{T: t, Type: "fall", PlayerID: p.PlayerID, From: vecPtr(from), To: vecPtr(p.Position), Duration: fall})

		if landY >= float64(m.Terrain.Height) {
			remainingHP := p.HP
			hurtPlayer(p, remainingHP)
			shooter.KillCount++
			tl.add(ShotEvent{T: landed, Type: "damage", PlayerID: p.PlayerID, Amount: remainingHP, HP: intPtr(0), Cause: "out_of_map"})
			tl.add(ShotEvent{T: landed, Type: "die", PlayerID: p.PlayerID, Cause: "out_of_map"})
			continue
		}

		if damage := CalculateFallDamage(distance); damage > 0 {
			killed := hurtPlayer(p, damage)
			tl.add(ShotEvent{T: landed, Type: "damage", PlayerID: p.PlayerID, Amount: damage, HP: intPtr(p.HP), Cause: "fall"})
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

// broadcastShot finalizes shot from tl and sends it to every client.
func (m *Match) broadcastShot(shot *ShotResolved, tl *shotTimeline) {
	if shot.Projectiles == nil {
		shot.Projectiles = []*ProjectileResult{}
	}
	for _, r := range shot.Projectiles {
		tl.reach(flightEnd(r))
	}
	shot.Events = tl.sorted()
	if shot.Events == nil {
		shot.Events = []ShotEvent{}
	}
	shot.Duration = tl.end
	shot.SkillEnergy = m.State.Players[shot.ShooterID].SkillEnergy

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
