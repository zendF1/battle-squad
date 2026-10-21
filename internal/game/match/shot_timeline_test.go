package match

import (
	"encoding/json"
	"math"
	"testing"

	"battle-squad/internal/game/ws"
)

// shotResolved drains c and returns the single ShotResolved it received, failing on
// any legacy per-shot event.
func shotResolved(t *testing.T, c *ws.Client) ShotResolved {
	t.Helper()
	var shots []ShotResolved
	for {
		select {
		case msg := <-c.Send:
			switch msg.Event {
			case "ProjectileResult", "PlayerDamaged", "SkillUsed", "PlayerMoved":
				t.Fatalf("legacy event %s sent alongside ShotResolved", msg.Event)
			case "ShotResolved":
				var s ShotResolved
				if err := json.Unmarshal(msg.Data, &s); err != nil {
					t.Fatalf("decode ShotResolved: %v", err)
				}
				shots = append(shots, s)
			}
		default:
			if len(shots) != 1 {
				t.Fatalf("got %d ShotResolved messages, want 1", len(shots))
			}
			return shots[0]
		}
	}
}

func drain(c *ws.Client) {
	for {
		select {
		case <-c.Send:
		default:
			return
		}
	}
}

// groundPlayers puts every player on the terrain surface below them and clears queued messages.
func groundPlayers(m *Match, clients map[string]*ws.Client) {
	for _, p := range m.State.Players {
		p.Position.Y = m.Terrain.GetLandingY(p.Position.X, 0)
	}
	for _, c := range clients {
		drain(c)
	}
}

func eventsOf(s ShotResolved, typ, playerID string) []ShotEvent {
	var out []ShotEvent
	for _, e := range s.Events {
		if e.Type == typ && (playerID == "" || e.PlayerID == playerID) {
			out = append(out, e)
		}
	}
	return out
}

func assertTimeline(t *testing.T, s ShotResolved) {
	t.Helper()
	for i := 1; i < len(s.Events); i++ {
		if s.Events[i].T < s.Events[i-1].T {
			t.Fatalf("events not sorted by t: %+v", s.Events)
		}
	}
	for _, e := range s.Events {
		if e.T+e.Duration > s.Duration+1e-9 {
			t.Fatalf("event %+v ends after duration %v", e, s.Duration)
		}
	}
	for _, p := range s.Projectiles {
		if end := p.Path[len(p.Path)-1].Time; end > s.Duration+1e-9 {
			t.Fatalf("projectile flight %v ends after duration %v", end, s.Duration)
		}
	}
}

func TestWeaponShotSendsTimeline(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter, target := m.State.Players[cur], m.State.Players[m.other(cur)]
	target.Position.X = shooter.Position.X + 15
	groundPlayers(m, clients)

	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	s := shotResolved(t, clients[m.other(cur)])
	assertTimeline(t, s)

	if s.ShooterID != cur || s.ActionMode != "weapon" || len(s.Projectiles) != 1 {
		t.Fatalf("unexpected shot header: %+v", s)
	}
	proj := s.Projectiles[0]
	explodes := eventsOf(s, "explode", "")
	if len(explodes) != 1 {
		t.Fatalf("got %d explode events, want 1", len(explodes))
	}
	ex := explodes[0]
	if ex.ProjectileID != proj.ProjectileID || ex.T != proj.Path[len(proj.Path)-1].Time {
		t.Fatalf("explode %+v does not match end of projectile flight", ex)
	}
	if ex.TerrainVersion != m.Terrain.Version || ex.Position == nil || ex.Radius != proj.ExplosionRadius {
		t.Fatalf("explode event missing terrain data: %+v", ex)
	}

	hits := eventsOf(s, "damage", target.PlayerID)
	if len(hits) == 0 {
		t.Fatalf("target in blast radius got no damage event: %+v", s.Events)
	}
	hit := hits[0]
	if hit.Cause != "explosion" || hit.ProjectileID != proj.ProjectileID || hit.T != ex.T {
		t.Fatalf("damage not attributed to the explosion: %+v", hit)
	}
	if hit.HP == nil || *hit.HP != target.HP || hit.Amount != target.MaxHP-target.HP {
		t.Fatalf("damage event %+v disagrees with state hp=%d", hit, target.HP)
	}
}

func TestFallEventsMatchLandingTime(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter := m.State.Players[cur]
	groundPlayers(m, clients)
	start := shooter.Position
	// Calm wind: the shot starts at the body centre, so its longer flight would drift off the crater.
	m.State.Wind = WindState{}

	// Blow the ground out from under the shooter so they fall into the crater.
	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	s := shotResolved(t, clients[cur])
	assertTimeline(t, s)

	falls := eventsOf(s, "fall", cur)
	if len(falls) != 1 {
		t.Fatalf("got %d fall events for shooter, want 1: %+v", len(falls), s.Events)
	}
	f := falls[0]
	if *f.From != start || *f.To != shooter.Position {
		t.Fatalf("fall %v -> %v, want %v -> %v", *f.From, *f.To, start, shooter.Position)
	}
	gravity := physics().Gravity
	if want := math.Sqrt(2 * (f.To.Y - f.From.Y) / gravity); math.Abs(f.Duration-want) > 1e-9 {
		t.Fatalf("fall duration %v, want %v", f.Duration, want)
	}
	if ex := eventsOf(s, "explode", ""); f.T != ex[0].T {
		t.Fatalf("fall starts at %v, want explosion time %v", f.T, ex[0].T)
	}
	for _, d := range eventsOf(s, "damage", cur) {
		if d.Cause == "fall" && math.Abs(d.T-(f.T+f.Duration)) > 1e-9 {
			t.Fatalf("fall damage at %v, want landing time %v", d.T, f.T+f.Duration)
		}
	}
}

func TestTeleportShotSendsTeleportEvent(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.Items = []string{"teleport"}
	groundPlayers(m, clients)
	start := p.Position
	m.State.Wind = WindState{}
	angle := 60.0 // towards the middle of the map, so it lands on it
	if p.Position.X > float64(m.Terrain.Width)/2 {
		angle = 120
	}

	item := "teleport"
	send(m, clients[cur], "Shoot", ShootAction{Angle: angle, Power: 30, ActionMode: "item", ItemID: &item})
	s := shotResolved(t, clients[cur])
	assertTimeline(t, s)

	tp := eventsOf(s, "teleport", cur)
	if len(tp) != 1 {
		t.Fatalf("got %d teleport events, want 1: %+v", len(tp), s.Events)
	}
	proj := s.Projectiles[0]
	if tp[0].T != proj.Path[len(proj.Path)-1].Time || *tp[0].From != start || *tp[0].To != p.Position {
		t.Fatalf("teleport event %+v, player at %v", tp[0], p.Position)
	}
	if len(eventsOf(s, "explode", "")) != 0 {
		t.Fatalf("teleport must not explode")
	}
}
