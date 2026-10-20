package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/ws"
)

// laserMatch: Spark at 200 with a wall at x 380..420 in front, enemies at 600 and 1000, an
// ally at 800, all in line on flat ground.
func laserMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "spark", 200}, skillPlayer{"e1", 2, "rookie", 600},
		skillPlayer{"a1", 1, "rookie", 800}, skillPlayer{"e2", 2, "rookie", 1000})
	for x := 380; x <= 420; x++ {
		for y := 380; y < identitySurface; y++ {
			setBit(m.Terrain.Mask, x, y, m.Terrain.Width)
			setBit(m.Terrain.DestructibleMask, x, y, m.Terrain.Width)
		}
	}
	return m, clients
}

// TestLaserHitsThroughTerrainInOrder: the beam goes through the wall to the map edge, hits
// both enemies nearest first, the second ×0.8, skips the ally, carves nothing, moves nobody,
// and stuns both.
func TestLaserHitsThroughTerrainInOrder(t *testing.T) {
	m, clients := laserMatch(t)
	m.State.Players["p1"].SkillRank = 4
	version := m.Terrain.Version
	before := map[string]Vector2{}
	for id, p := range m.State.Players {
		before[id] = p.Position
	}
	s := fireSkill(t, m, clients, "p1", 0, 0)

	beams := eventsOf(s, "beam", "p1")
	if len(beams) != 1 || beams[0].To.X != float64(m.Terrain.Width) || beams[0].From.X != 200 {
		t.Fatalf("beam %+v", beams)
	}
	d1, d2 := eventsOf(s, "damage", "e1"), eventsOf(s, "damage", "e2")
	if len(d1) != 1 || len(d2) != 1 || d1[0].Cause != "laser" || d2[0].T <= d1[0].T {
		t.Fatalf("laser damage e1 %+v e2 %+v", d1, d2)
	}
	base := 620.0 * laserFirst(4)
	if want := int(math.Round(ReduceByDefense(base, 50))); d1[0].Amount != want {
		t.Fatalf("first hit %d, want %d", d1[0].Amount, want)
	}
	if want := int(math.Round(ReduceByDefense(base*0.8, 50))); d2[0].Amount != want {
		t.Fatalf("second hit %d, want %d (×0.8)", d2[0].Amount, want)
	}
	t.Logf("rank 4 laser: %d then %d", d1[0].Amount, d2[0].Amount)
	if len(eventsOf(s, "damage", "a1")) != 0 {
		t.Fatal("ally hit")
	}
	if m.Terrain.Version != version || !m.Terrain.IsSolid(400, 470) || len(eventsOf(s, "explode", "")) != 0 || len(eventsOf(s, "fall", "")) != 0 {
		t.Fatal("the laser carved terrain or moved players")
	}
	for id, p := range m.State.Players {
		if p.Position != before[id] {
			t.Fatalf("%s moved", id)
		}
	}
	if !hasStatus(s, "e1", "stun") || !hasStatus(s, "e2", "stun") {
		t.Fatal("enemies not stunned")
	}
	if len(s.Projectiles) != 0 {
		t.Fatalf("laser projectiles %+v", s.Projectiles)
	}
}

func TestLaserMissesOffTheBeam(t *testing.T) {
	m, clients := laserMatch(t)
	s := fireSkill(t, m, clients, "p1", 60, 50)
	if n := len(eventsOf(s, "damage", "")); n != 0 {
		t.Fatalf("%d hits aiming at the sky", n)
	}
}

func TestBotLaserLinesUpTwoEnemies(t *testing.T) {
	m, bot := botSkillMatch(t, skillPlayer{"b", 1, "spark", 200}, skillPlayer{"e1", 2, "rookie", 600},
		skillPlayer{"e2", 2, "rookie", 1100})
	brain := NewBotBrain(perfectAim)
	act, ok := brain.DecideAction(m, bot, botTurnState{}).(ShootAction)
	if !ok || act.ActionMode != "skill" || laserHitCount(m, bot, act.Angle) != 2 {
		t.Fatalf("bot laser %+v hits %d", act, laserHitCount(m, bot, act.Angle))
	}
}

func TestLaserFirstHitPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if got, want := laserFirst(rank), 1.5+0.1*float64(rank); math.Abs(got-want) > 1e-9 {
			t.Errorf("laser first rank %d = %v, want %v", rank, got, want)
		}
	}
}
