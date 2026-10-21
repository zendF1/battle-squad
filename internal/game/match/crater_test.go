package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/battlestats"
)

// slopeTerrain is a 1600×896 map of destructible ground under y = 600 - tan(deg)·(x - 800)
// (deg > 0 rises to the right).
func slopeTerrain(deg float64) *Terrain {
	const w, h = 1600, 896
	t := &Terrain{Width: w, Height: h, Mask: make([]uint64, maskSize(w, h)), DestructibleMask: make([]uint64, maskSize(w, h))}
	k := math.Tan(deg * math.Pi / 180)
	for x := 0; x < w; x++ {
		top := int(math.Round(600 - k*float64(x-800)))
		for y := max(top, 0); y < h; y++ {
			setBit(t.Mask, x, y, w)
			setBit(t.DestructibleMask, x, y, w)
		}
	}
	return t
}

func solidTerrain(w, h int) *Terrain {
	t := &Terrain{Width: w, Height: h, Mask: make([]uint64, maskSize(w, h)), DestructibleMask: make([]uint64, maskSize(w, h))}
	for i := range t.Mask {
		t.Mask[i], t.DestructibleMask[i] = ^uint64(0), ^uint64(0)
	}
	return t
}

func cloneTerrain(t *Terrain) *Terrain {
	c := *t
	c.Mask = append([]uint64{}, t.Mask...)
	c.DestructibleMask = append([]uint64{}, t.DestructibleMask...)
	return &c
}

// maxSurfaceStep is the largest height change (px) of the top surface between neighbouring
// columns in [x0, x1].
func maxSurfaceStep(t *Terrain, x0, x1 int) int {
	worst := 0
	prev := int(t.GetLandingY(float64(x0), 0))
	for x := x0 + 1; x <= x1; x++ {
		y := int(t.GetLandingY(float64(x), 0))
		worst = max(worst, abs(y-prev))
		prev = y
	}
	return worst
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestCarveBowlShape: dx² + (r/k)·dy <= r² (k = bowlDepth) under a flat top bowlCeiling·r
// above the centre.
func TestCarveBowlShape(t *testing.T) {
	if bowlDepth != 0.72 {
		t.Fatalf("bowlDepth %v: the cases below are for 0.72", bowlDepth)
	}
	tr := solidTerrain(400, 300)
	if !tr.CarveBowl(200, 150, 20) || tr.Version != 1 {
		t.Fatal("nothing carved or the version not bumped")
	}
	cases := []struct {
		x, y   float64
		carved bool
	}{
		{200, 164, true}, {200, 165, false}, // the floor k·r = 14.4 px under the centre
		{220, 150, true}, {221, 150, false}, // r to each side at the centre's height
		{210, 160, true}, {210, 161, false}, // 100 + 27.8·10 = 378 <= 400; 100 + 27.8·11 = 406
		{200, 120, true}, {200, 119, false}, // the flat top 1.5·r above
		{235, 120, true}, {236, 120, false}, // the floor meets the top r·√(1 + 1.5/k) = 35.1 px out
		{230, 140, false}, // 900 - 278 = 622 > 400: outside the floor
	}
	for _, c := range cases {
		if solid := tr.IsSolid(c.x, c.y); solid == c.carved {
			t.Errorf("(%v, %v) carved %v, want %v", c.x, c.y, !solid, c.carved)
		}
	}

	// The bottom row of the map can be carved (nothing keeps a floor); a zero radius carves
	// nothing but is still an operation
	tr = solidTerrain(100, 100)
	tr.CarveBowl(50, 95, 20)
	if tr.IsSolid(50, 99) || !tr.IsSolid(50, 100) {
		t.Fatal("the bottom row kept, or below the map not solid")
	}
	if tr.CarveBowl(90, 50, 0) || tr.Version != 2 || !tr.IsSolid(90, 50) {
		t.Fatal("a zero radius carved, or did not count as an operation")
	}
	// Carve dispatches on the shape
	tr = solidTerrain(100, 100)
	tr.Carve(CraterCircle, 50, 50, 10)
	if tr.IsSolid(50, 59) || tr.IsSolid(50, 41) {
		t.Fatal("circle not carved")
	}
	tr = solidTerrain(100, 100)
	tr.Carve("", 50, 50, 10)
	if tr.IsSolid(50, 57) || !tr.IsSolid(50, 58) {
		t.Fatal("the default shape is not the bowl")
	}
}

// TestBowlWallsClimbable: every crater (rain 12 px, Rookie 20, Flora 24, Rookie on fragile 40,
// Tanko 52, Tanko on fragile 104) has no pixel step over 2 px on flat ground (walls at most
// atan(2·bowlDepth) = 55°) and none over 3 px, the climb of a player, on 30° slopes (the uphill
// wall ~65°: a 2.1:1 wall cannot stay under 2 px a column), where the circle it replaces had
// a wall at the rim.
func TestBowlWallsClimbable(t *testing.T) {
	for _, deg := range []float64{0, 30, -30} {
		for _, r := range []float64{12, 20, 24, 40, 52, 104} {
			bowl, circle := slopeTerrain(deg), slopeTerrain(deg)
			x := 800.0
			y := bowl.GetLandingY(x, 0) + 3 // the shell explodes one sample inside the ground
			bowl.CarveBowl(x, y, r)
			circle.DestroyCircle(x, y, r)
			x0, x1 := int(x-3*r), int(x+3*r)
			bs, cs := maxSurfaceStep(bowl, x0, x1), maxSurfaceStep(circle, x0, x1)
			depth := bowl.GetLandingY(x, 0) - (y - 3)
			t.Logf("slope %3.0f° crater %3.0f px: bowl %3.0f px deep under the impact column, steepest step %d px (circle: %3.0f deep, %3d px)",
				deg, r, depth, bs, circle.GetLandingY(x, 0)-(y-3), cs)
			if limit := wallStepLimit(deg); bs > limit {
				t.Errorf("slope %v° crater %v: a %d px step (limit %d)", deg, r, bs, limit)
			}
			if deg == 0 && cs <= maxClimbStep {
				t.Errorf("crater %v: the circle's rim is climbable (%d px): the test proves nothing", r, cs)
			}
		}
	}
}

// maxClimbStep is WalkTo's maxClimbPerStep.
const maxClimbStep = 3

// wallStepLimit is the largest pixel step a bowl may leave on a slope of deg degrees: 2 on flat
// ground, the 3 px a player climbs on a slope.
func wallStepLimit(deg float64) int {
	if deg == 0 {
		return 2
	}
	return maxClimbStep
}

// TestWalkOutOfCrater: Rookie and Tanko fire straight up on flat ground and on 30° and 35°
// slopes (rising either way); the shell falls back at their feet, they sink into their own crater and
// then walk out of it to the left and to the right, each way on one turn's move energy
// (processMove: 1 energy per 2 px). In the circle crater the same shot used to carve they are
// stuck.
func TestWalkOutOfCrater(t *testing.T) {
	useConfigGameData(t)
	for _, char := range []string{"rookie", "tanko"} {
		for _, deg := range []float64{0, 30, -30, 35, -35} {
			m, clients := identityMatch(t, char, 800, "rookie", 1500)
			m.Terrain = slopeTerrain(deg)
			for _, p := range m.State.Players {
				p.Position.Y = m.Terrain.SpawnY(p.Position.X, 0)
			}
			shooter := m.State.Players["p1"]
			before := cloneTerrain(m.Terrain)
			start := shooter.Position

			send(m, clients["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
			s := shotResolved(t, clients["p1"])
			ex := eventsOf(s, "explode", "")
			if len(ex) != 1 || ex[0].Crater != CraterBowl {
				t.Fatalf("%s %v°: explode events %+v", char, deg, ex)
			}
			p, r := *ex[0].Position, ex[0].Radius
			if math.Abs(p.X-start.X) > r/2 {
				t.Fatalf("%s %v°: the shell came down at %v, not at the feet %v", char, deg, p.X, start.X)
			}
			fell, fallDamage := fallOf(t, s, "p1")
			if fell <= 0 || fallDamage != 0 {
				t.Fatalf("%s %v°: fell %v px, %d fall damage", char, deg, fell, fallDamage)
			}

			// The circle the same explosion used to carve, for comparison
			old := cloneTerrain(before)
			old.DestroyCircle(p.X, p.Y, r)
			oldFeet := Vector2{X: start.X, Y: old.GetLandingY(start.X, start.Y)}

			energy := battlestats.Base(char).TurnMoveEnergy()
			reach := float64(energy * 2)
			for _, dir := range []float64{-1, 1} {
				x, y := m.Terrain.WalkTo(shooter.Position.X, shooter.Position.Y, shooter.Position.X+dir*reach)
				out := math.Abs(x-p.X) > 2*r && y == before.GetLandingY(x, 0)
				ox, _ := old.WalkTo(oldFeet.X, oldFeet.Y, oldFeet.X+dir*reach)
				t.Logf("%-6s slope %3.0f° crater %2.0f px, dir %+2.0f: fell %4.1f px (circle %4.1f), walked %5.1f of %3.0f px, out %v (circle: walked %5.1f)",
					char, deg, r, dir, fell, oldFeet.Y-start.Y, math.Abs(x-shooter.Position.X), reach, out, math.Abs(ox-oldFeet.X))
				if x != shooter.Position.X+dir*reach || !out {
					t.Errorf("%s slope %v° dir %v: stuck at (%v, %v)", char, deg, dir, x, y)
				}
			}
			if deg == 0 {
				lx, _ := old.WalkTo(oldFeet.X, oldFeet.Y, oldFeet.X-reach)
				rx, _ := old.WalkTo(oldFeet.X, oldFeet.Y, oldFeet.X+reach)
				if math.Abs(lx-p.X) > r && math.Abs(rx-p.X) > r {
					t.Errorf("%s: the circle crater lets the player out too: the comparison proves nothing", char)
				}
			}
			if st := maxSurfaceStep(m.Terrain, int(p.X-3*r), int(p.X+3*r)); st > wallStepLimit(deg) {
				t.Errorf("%s slope %v°: a %d px step", char, deg, st)
			}
		}
	}
}
