package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// TestMissileRain: the flare lands at the shooter's feet (straight up), the missiles fall
// straight down around it, rainInterval apart, resolved in landing order; the enemy next to
// it takes damage within the cap and burns. No explosion for the flare.
func TestMissileRain(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 460})
	shooter, enemy := m.State.Players["p1"], m.State.Players["p2"]
	shooter.SkillRank = 4
	s := fireSkill(t, m, clients, "p1", 90, 10)

	flare := s.Projectiles[0]
	if flare.Kind != "flare" || flare.ExplosionPoint == nil {
		t.Fatalf("first projectile %+v, want the landed flare", flare)
	}
	flares := eventsOf(s, "flare", "")
	if len(flares) != 1 || flares[0].T != flightEnd(flare) || *flares[0].Position != *flare.ExplosionPoint {
		t.Fatalf("flare events %+v", flares)
	}
	for _, e := range eventsOf(s, "explode", "") {
		if e.ProjectileID == flare.ProjectileID {
			t.Fatal("the flare exploded")
		}
	}
	rain := eventsOf(s, "rain", "")
	if len(rain) != 1 || rain[0].Count != 14 || rain[0].Radius != 150 || math.Abs(rain[0].T-(flightEnd(flare)+rainDelay)) > 1e-9 {
		t.Fatalf("rain events %+v", rain)
	}
	missiles := s.Projectiles[1:]
	if len(missiles) != 14 {
		t.Fatalf("%d missiles, want 14 at rank 4", len(missiles))
	}
	launches := map[int]bool{}
	w := gamedata.Data.Weapons["basic_rocket"]
	lastEnd := 0.0
	for _, r := range missiles {
		k := int(math.Round((r.LaunchDelay - rain[0].T) / rainInterval))
		if math.Abs(r.LaunchDelay-(rain[0].T+rainInterval*float64(k))) > 1e-9 || r.Path[0].Time != r.LaunchDelay || launches[k] {
			t.Fatalf("missile launch %v (k %d) not on the %v s grid", r.LaunchDelay, k, rainInterval)
		}
		launches[k] = true
		if r.Kind != "missile" || r.Path[0].Position.Y != rainStartY {
			t.Fatalf("missile %+v", r)
		}
		if dx := math.Abs(r.Path[0].Position.X - flare.ExplosionPoint.X); dx > 150 {
			t.Fatalf("missile column %v off the flare by %v", r.Path[0].Position.X, dx)
		}
		end := r.Path[len(r.Path)-1].Position
		if math.Abs(end.X-r.Path[0].Position.X) > 0.5 {
			t.Fatalf("missile drifted from %v to %v: not straight down", r.Path[0].Position.X, end.X)
		}
		if r.ExplosionRadius != float64(w.ExplosionRadius)*rainBlast || r.CraterRadius != weaponCraterRadius(w)*rainCrater {
			t.Fatalf("missile blast %v crater %v", r.ExplosionRadius, r.CraterRadius)
		}
		if flightEnd(r) < lastEnd {
			t.Fatal("missiles not resolved in landing order")
		}
		lastEnd = flightEnd(r)
	}
	got := totalDamage(s, "p2")
	limit := capOfBase(rainCap * 480)(enemy)
	t.Logf("rank 4 rain: %d damage to the enemy 60 px from the flare (cap %d), hp %d", got, limit, enemy.HP)
	if got <= 0 || got > limit {
		t.Fatalf("rain damage %d, want 1..%d", got, limit)
	}
	if !hasStatus(s, "p2", "burn") || !HasEffect(enemy, "burn") || hasStatus(s, "p1", "burn") {
		t.Fatalf("burn: events %+v", eventsOf(s, "status", ""))
	}
}

// TestMissileRainColumnsAreDeterministic: the columns come from the match + turn seed.
func TestMissileRainColumnsAreDeterministic(t *testing.T) {
	cols := func(match string, turn int) []float64 {
		m := &Match{State: MatchState{MatchID: match, TurnIndex: turn}}
		return rainColumns(m.skillRNG(), 10, 800, 135, 1600)
	}
	a, b, c := cols("m1", 3), cols("m1", 3), cols("m1", 4)
	same := func(x, y []float64) bool {
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	if !same(a, b) || same(a, c) {
		t.Fatalf("columns %v / %v / %v", a, b, c)
	}
	for _, x := range a {
		if x < 800-135 || x > 800+135 {
			t.Fatalf("column %v outside the spread", x)
		}
	}
}

func TestMissileRainFlareOffMapDropsNothing(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 60}, skillPlayer{"p2", 2, "rookie", 1300})
	s := fireSkill(t, m, clients, "p1", 170, 100)
	if len(s.Projectiles) != 1 || s.Projectiles[0].ExplosionPoint != nil {
		t.Fatalf("projectiles %+v, want only the lost flare", s.Projectiles)
	}
	if len(s.Events) != 0 {
		t.Fatalf("events %+v, want none", s.Events)
	}
}

func TestBotRainAimedLikeItsBestShot(t *testing.T) {
	for _, char := range []string{"rookie", "tanko"} {
		m, bot := botSkillMatch(t, skillPlayer{"b", 1, char, 300}, skillPlayer{"e1", 2, "rookie", 900})
		brain := NewBotBrain(perfectAim)
		w, _ := botWeapon(bot)
		best := brain.bestShot(m, bot, bot.Position, w)
		act, ok := brain.DecideAction(m, bot, botTurnState{}).(ShootAction)
		if !ok || act.ActionMode != "skill" || act.Angle != best.angle || act.Power != best.power {
			t.Fatalf("%s: bot skill %+v, best shot %+v", char, act, best)
		}
	}
}

func TestRainCountsPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if got, want := rainCount(rank), 6+2*rank; got != want {
			t.Errorf("rain count rank %d = %d, want %d", rank, got, want)
		}
		if got, want := rainSpread(rank), 90+15*float64(rank); got != want {
			t.Errorf("rain spread rank %d = %v, want %v", rank, got, want)
		}
	}
}
