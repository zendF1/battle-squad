package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// TestBombChain: rank 2 → 5 bombs on one trajectory at least chainLaunchGap (0.1 s) apart,
// craters shrinking ×0.85; fired straight up they come down the same column, the later ones
// exploding deeper; damage split evenly and capped at the total; the enemy rooted; its falls
// play one after another.
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
		if r.Kind != "bomb" || r.Path[0].Position != start || r.Path[0].Time != r.LaunchDelay ||
			(i == 0 && r.LaunchDelay != 0) || (i > 0 && r.LaunchDelay < s.Projectiles[i-1].LaunchDelay+chainLaunchGap-1e-9) {
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
	assertChainCoherent(t, s)
}

// assertChainCoherent: no bomb explodes while a fall of the shot is still playing (every fall
// started before an explosion has landed chainHitGap before it), so a hit never lands on a
// target the client still draws falling; the falls play chainFallSpeed × faster than free fall.
func assertChainCoherent(t *testing.T, s ShotResolved) {
	t.Helper()
	for _, x := range eventsOf(s, "explode", "") {
		for _, f := range eventsOf(s, "fall", "") {
			if f.T < x.T && f.T+f.Duration > x.T-chainHitGap+1e-9 {
				t.Fatalf("bomb %s explodes at %.3f while %s falls %.3f..%.3f", x.ProjectileID, x.T, f.PlayerID, f.T, f.T+f.Duration)
			}
		}
	}
	for _, f := range eventsOf(s, "fall", "") {
		if want := fallDuration(f.To.Y-f.From.Y) / chainFallSpeed; math.Abs(f.Duration-want) > 1e-9 {
			t.Fatalf("fall of %v px takes %v, want %v", f.To.Y-f.From.Y, f.Duration, want)
		}
	}
}

// TestPacedLaunch: a chain bomb leaves chainLaunchGap after the previous one, or later so that
// it explodes chainHitGap after the last landing.
func TestPacedLaunch(t *testing.T) {
	if got := pacedLaunch(0.3, 1.5, 1.0); math.Abs(got-0.4) > 1e-9 {
		t.Fatalf("no fall in the way: %v, want 0.4", got)
	}
	if got := pacedLaunch(0.3, 1.5, 2.4); math.Abs(got-(2.4+chainHitGap-1.5)) > 1e-9 {
		t.Fatalf("waiting for a landing at 2.4: %v", got)
	}
}

// TestBombChainKeepsFloor: seven bombs (a 220% terrain bonus on cannon_ball's 52 px crater) dig
// through 400 px of ground but stop chainFloorMargin above the bottom of the map; nobody falls
// out of it.
func TestBombChainKeepsFloor(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 415})
	m.State.Players["p1"].SkillRank = 4
	m.State.Players["p1"].TerrainDamageBonus = 220
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
