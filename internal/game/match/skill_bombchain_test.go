package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// TestBombChain: rank 2 → 5 bombs on one trajectory at least chainLaunchGap (0.1 s) apart,
// every crater the weapon's own bowl; fired straight up they come down the same
// column, the later ones exploding deeper; the total split evenly, one target capped at
// chainCap × base; the enemy rooted; its falls play one after another.
func TestBombChain(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 426})
	m.State.Players["p1"].SkillRank = 2
	m.State.Players["p1"].TerrainDamageBonus = MaxTerrainDamageBonus // a bowl wide enough to reach under the target
	enemy := m.State.Players["p2"]
	s := fireSkill(t, m, clients, "p1", 90, 10)
	if len(s.Projectiles) != 4 {
		t.Fatalf("%d bombs, want 4", len(s.Projectiles))
	}
	w := gamedata.Current().Data.Weapons["cannon_ball"]
	start := s.Projectiles[0].Path[0].Position
	lastY := -1.0
	for i, r := range s.Projectiles {
		if r.Kind != "bomb" || r.Path[0].Position != start || r.Path[0].Time != r.LaunchDelay ||
			(i == 0 && r.LaunchDelay != 0) || (i > 0 && r.LaunchDelay < s.Projectiles[i-1].LaunchDelay+chainLaunchGap-1e-9) {
			t.Fatalf("bomb %d: kind %q start %v launch %v", i, r.Kind, r.Path[0].Position, r.LaunchDelay)
		}
		if r.ExplosionRadius != float64(w.ExplosionRadius)*chainBlast || r.CraterRadius != weaponCraterRadius(gamedata.Current(), w) {
			t.Fatalf("bomb %d blast %v crater %v", i, r.ExplosionRadius, r.CraterRadius)
		}
		if r.ExplosionPoint == nil || r.ExplosionPoint.Y < lastY {
			t.Fatalf("bomb %d explodes at %v, above the previous %v", i, r.ExplosionPoint, lastY)
		}
		lastY = r.ExplosionPoint.Y
	}
	for _, e := range eventsOf(s, "explode", "") {
		if e.Crater != CraterBowl || e.Radius != craterRadius(m.State.Players["p1"], weaponCraterRadius(gamedata.Current(), w), MaxTerrainDamageBonus) {
			t.Fatalf("explode %+v: want the weapon's bowl", e)
		}
	}
	limit := chainCap(defChain, 2) * float64(w.Damage)
	got := totalDamage(s, "p2")
	var blast int
	for _, e := range eventsOf(s, "damage", "p2") {
		if e.Cause == "explosion" {
			blast += e.Amount
		}
	}
	t.Logf("rank 2 chain: %d explosion damage (cap %.0f before defense), %d in all, enemy hp %d, at y %v",
		blast, limit, got, enemy.HP, enemy.Position.Y)
	if blast <= 0 || blast > testCapOfBase(limit)(enemy) {
		t.Fatalf("chain explosion damage %d, cap %d", blast, testCapOfBase(limit)(enemy))
	}
	if first := eventsOf(s, "damage", "p2")[0]; first.Amount > int(math.Round(ReduceByDefense(chainEach(defChain, 2)*float64(w.Damage), enemy.Defense)))+1 {
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
	// The bombs land beside the target (26 px: its body is missed) and drop it into their bowls,
	// one after another as the shaft deepens
	if len(eventsOf(s, "fall", "p2")) < 1 {
		t.Fatal("the enemy never fell into the hole")
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
		if want := fallDuration(f.To.Y-f.From.Y, testGravity()) / chainFallSpeed; math.Abs(f.Duration-want) > 1e-9 {
			t.Fatalf("fall of %v px takes %v, want %v", f.To.Y-f.From.Y, f.Duration, want)
		}
	}
}

// TestPacedLaunch: a chain bomb leaves chainLaunchGap after the previous one, or later so that
// it explodes chainHitGap after the last landing.
func TestPacedLaunch(t *testing.T) {
	if got := pacedLaunch(defChain, 0.3, 1.5, 1.0); math.Abs(got-0.4) > 1e-9 {
		t.Fatalf("no fall in the way: %v, want 0.4", got)
	}
	if got := pacedLaunch(defChain, 0.3, 1.5, 2.4); math.Abs(got-(2.4+chainHitGap-1.5)) > 1e-9 {
		t.Fatalf("waiting for a landing at 2.4: %v", got)
	}
}

// TestBombChainDigsToTheBottom: nothing caps how deep the chain digs. Six bombs (rank 4, the
// terrain bonus at its cap: cannon_ball's 18 px crater grown to 27 px bowls, ~20 px deep each)
// fired straight up dig through 96 px of ground, the bottom row of the map included; the shell
// then explodes on the floor under the map (IsSolid) and the players standing over the open
// column fall out of the map (out_of_map), today's rule for a column with no ground left.
func TestBombChainDigsToTheBottom(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 426})
	for y := 0; y < m.Terrain.Height-96; y++ {
		for x := 0; x < m.Terrain.Width; x++ {
			clearBit(m.Terrain.Mask, x, y, m.Terrain.Width)
		}
	}
	for _, p := range m.State.Players {
		p.Position.Y = m.Terrain.SpawnY(p.Position.X, 0)
	}
	m.State.Players["p1"].SkillRank = 4
	m.State.Players["p1"].TerrainDamageBonus = 220 // capped at MaxTerrainDamageBonus
	s := fireSkill(t, m, clients, "p1", 90, 10)
	deepest := 0.0
	for _, e := range eventsOf(s, "explode", "") {
		if e.Crater != CraterBowl {
			t.Fatalf("explode crater %q", e.Crater)
		}
		deepest = math.Max(deepest, e.Position.Y)
	}
	bottom := m.Terrain.Height - 1
	if m.Terrain.IsSolid(400, float64(bottom)) {
		t.Fatalf("the bottom row under the shaft is kept (deepest explosion at %v of %d)", deepest, m.Terrain.Height)
	}
	if !m.Terrain.IsSolid(400, float64(m.Terrain.Height)) {
		t.Fatal("below the map is no longer solid floor")
	}
	out := map[string]bool{}
	for _, e := range eventsOf(s, "die", "") {
		if e.Cause == "out_of_map" {
			out[e.PlayerID] = true
		}
	}
	t.Logf("deepest explosion y %.0f (map %d px), out of the map: %v", deepest, m.Terrain.Height, out)
	if !out["p2"] || m.State.Players["p2"].IsAlive {
		t.Fatalf("the enemy over the open column did not fall out: %v", out)
	}
	if !out["p1"] {
		t.Fatalf("the shooter standing on the shaft did not fall out too: %v", out)
	}
	if got := m.State.Players["p1"].KillCount; got != 1 {
		t.Fatalf("shooter kills = %d, want 1 (the enemy; its own fall is no kill)", got)
	}
}

func TestChainCountsPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if got, want := chainCount(defChain, rank), 2+rank; got != want {
			t.Errorf("chain count rank %d = %d, want %d", rank, got, want)
		}
		if got, want := chainTotal(defChain, rank), 1.8+0.15*float64(rank); math.Abs(got-want) > 1e-9 {
			t.Errorf("chain total rank %d = %v, want %v", rank, got, want)
		}
		if got, want := chainCap(defChain, rank), 1.2+0.15*float64(rank); math.Abs(got-want) > 1e-9 {
			t.Errorf("chain cap rank %d = %v, want %v", rank, got, want)
		}
	}
}
