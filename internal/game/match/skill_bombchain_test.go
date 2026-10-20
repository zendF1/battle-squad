package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// TestBombChain: rank 2 → 5 bombs on one trajectory 0.3 s apart, craters shrinking ×0.85, the
// later ones exploding deeper; damage split evenly and capped at the total; the enemy rooted;
// its falls play one after another.
func TestBombChain(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 415})
	m.State.Players["p1"].SkillRank = 2
	enemy := m.State.Players["p2"]
	s := fireSkill(t, m, clients, "p1", 90, 10)
	if len(s.Projectiles) != 5 {
		t.Fatalf("%d bombs, want 5", len(s.Projectiles))
	}
	w := gamedata.Data.Weapons["cannon_ball"]
	start := s.Projectiles[0].Path[0].Position
	lastY := -1.0
	for i, r := range s.Projectiles {
		if r.Kind != "bomb" || r.Path[0].Position != start || math.Abs(r.LaunchDelay-0.3*float64(i)) > 1e-9 || r.Path[0].Time != r.LaunchDelay {
			t.Fatalf("bomb %d: kind %q start %v launch %v", i, r.Kind, r.Path[0].Position, r.LaunchDelay)
		}
		if r.ExplosionRadius != float64(w.ExplosionRadius)*chainBlast ||
			math.Abs(r.CraterRadius-weaponCraterRadius(w)*1.4*math.Pow(0.85, float64(i))) > 1e-9 {
			t.Fatalf("bomb %d blast %v crater %v", i, r.ExplosionRadius, r.CraterRadius)
		}
		if r.ExplosionPoint == nil || r.ExplosionPoint.Y < lastY {
			t.Fatalf("bomb %d explodes at %v, above the previous %v", i, r.ExplosionPoint, lastY)
		}
		lastY = r.ExplosionPoint.Y
	}
	total := chainTotal(2) * float64(w.Damage)
	got := totalDamage(s, "p2")
	var blast int
	for _, e := range eventsOf(s, "damage", "p2") {
		if e.Cause == "explosion" {
			blast += e.Amount
		}
	}
	t.Logf("rank 2 chain: %d explosion damage (cap %.0f before defense), %d in all, enemy hp %d, at y %v",
		blast, total, got, enemy.HP, enemy.Position.Y)
	if blast <= 0 || blast > capOfBase(total)(enemy) {
		t.Fatalf("chain explosion damage %d, cap %d", blast, capOfBase(total)(enemy))
	}
	if first := eventsOf(s, "damage", "p2")[0]; first.Amount > int(math.Round(ReduceByDefense(total/5, enemy.Defense)))+1 {
		t.Fatalf("one bomb dealt %d, more than its share", first.Amount)
	}
	if enemy.IsAlive && (!hasStatus(s, "p2", "root") || !HasEffect(enemy, "root")) {
		t.Fatal("enemy not rooted")
	}
	end := 0.0
	for _, f := range eventsOf(s, "fall", "p2") {
		if f.T < end-1e-9 {
			t.Fatalf("fall at %v starts before the previous landing %v", f.T, end)
		}
		end = f.T + f.Duration
	}
	if len(eventsOf(s, "fall", "p2")) < 2 {
		t.Fatalf("enemy fell %d times, want it dropped again by later bombs", len(eventsOf(s, "fall", "p2")))
	}
}

// TestBombChainKeepsFloor: seven bombs dig through 400 px of ground but stop chainFloorMargin
// above the bottom of the map; nobody falls out of it.
func TestBombChainKeepsFloor(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 415})
	m.State.Players["p1"].SkillRank = 4
	m.State.Players["p1"].TerrainDamageBonus = 60
	s := fireSkill(t, m, clients, "p1", 90, 10)
	floor := float64(m.Terrain.Height) - chainFloorMargin
	deepest := 0.0
	for _, e := range eventsOf(s, "explode", "") {
		if e.Floor != floor {
			t.Fatalf("explode floor %v, want %v", e.Floor, floor)
		}
		deepest = math.Max(deepest, e.Position.Y+e.Radius)
	}
	if deepest <= floor {
		t.Fatalf("craters reach %v only; the test needs them past the floor %v", deepest, floor)
	}
	for x := 300; x <= 500; x++ {
		for y := int(floor); y < m.Terrain.Height; y++ {
			if !m.Terrain.IsSolid(float64(x), float64(y)) {
				t.Fatalf("(%d, %d) carved below the floor", x, y)
			}
		}
	}
	for _, e := range s.Events {
		if e.Cause == "out_of_map" {
			t.Fatalf("out of map death: %+v", e)
		}
	}
}

// TestBombChainPlungesIntoTheHole: aimed at a target across flat ground, the later bombs drop
// down the first one's hole instead of flying on past it, and hit the target again.
func TestBombChainPlungesIntoTheHole(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 300}, skillPlayer{"p2", 2, "rookie", 900})
	shooter := m.State.Players["p1"]
	shooter.SkillRank = 4
	w, _ := botWeapon(shooter)
	best := NewBotBrain(perfectAim).bestShot(m, shooter, shooter.Position, w)
	s := fireSkill(t, m, clients, "p1", best.angle, best.power)
	first := s.Projectiles[0].ExplosionPoint
	if first == nil || len(s.Projectiles) != 7 {
		t.Fatalf("chain %+v", s.Projectiles)
	}
	for i, r := range s.Projectiles[1:] {
		if r.ExplosionPoint == nil || math.Abs(r.ExplosionPoint.X-first.X) > chainHoleEnter || r.ExplosionPoint.Y < first.Y {
			t.Fatalf("bomb %d explodes at %v, not down the hole at %v", i+1, r.ExplosionPoint, first)
		}
	}
	if n := len(eventsOf(s, "damage", "p2")); n < 3 {
		t.Fatalf("target hit %d times", n)
	}
	t.Logf("aimed chain: %d damage in %d hits", totalDamage(s, "p2"), len(eventsOf(s, "damage", "p2")))
}

func TestChainCountsPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if got, want := chainCount(rank), 3+rank; got != want {
			t.Errorf("chain count rank %d = %d, want %d", rank, got, want)
		}
		if got, want := chainTotal(rank), 1.8+0.15*float64(rank); math.Abs(got-want) > 1e-9 {
			t.Errorf("chain total rank %d = %v, want %v", rank, got, want)
		}
	}
	if s := chainCraterScale(2); math.Abs(s-1.4*0.85*0.85) > 1e-9 {
		t.Fatalf("third bomb crater scale %v", s)
	}
}
