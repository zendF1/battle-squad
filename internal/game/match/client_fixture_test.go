package match

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"testing"

	"battle-squad/internal/game/gamedata"
)

type fixtureOp struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
}

type fixtureLanding struct {
	X      float64 `json:"x"`
	StartY float64 `json:"startY"`
	Y      float64 `json:"y"`
}

type fixtureWalk struct {
	StartX  float64 `json:"startX"`
	StartY  float64 `json:"startY"`
	TargetX float64 `json:"targetX"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

// fixtureShot is SimulateProjectile against terrain only. The initial velocity is recorded,
// not the angle, so the client parity test avoids Go-vs-C differences in cos/sin.
// X, Y is the shot origin: shotOriginAt(FeetX, FeetY), the shooter's body centre
// (feet − (0, ShotOriginLift), or lower under a low ceiling).
type fixtureShot struct {
	FeetX     float64 `json:"feetX"`
	FeetY     float64 `json:"feetY"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	VX        float64 `json:"vx"`
	VY        float64 `json:"vy"`
	WindForce float64 `json:"windForce"`
	Hit       bool    `json:"hit"`
	HitX      float64 `json:"hitX"`
	HitY      float64 `json:"hitY"`
}

type fixtureMoveStep struct {
	TargetX float64 `json:"targetX"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Energy  int     `json:"energy"`
}

// fixtureMoveSeq is a series of Move commands run through the real processMove.
type fixtureMoveSeq struct {
	StartX float64           `json:"startX"`
	StartY float64           `json:"startY"`
	Energy int               `json:"energy"`
	Steps  []fixtureMoveStep `json:"steps"`
}

// TestExportClientFixture writes parity cases for the Godot client's port of snapshot decoding,
// DestroyCircle, GetLandingY, WalkTo, the shot integration and processMove. It runs only when
// CLIENT_FIXTURE_OUT names the output file.
func TestExportClientFixture(t *testing.T) {
	out := os.Getenv("CLIENT_FIXTURE_OUT")
	if out == "" {
		t.Skip("set CLIENT_FIXTURE_OUT to export the client terrain fixture")
	}

	const cols, rows, cell = 40, 24, 16
	rng := rand.New(rand.NewSource(7))

	// Rolling destructible ground on an indestructible bedrock row, plus floating blocks.
	tiles := make([][]int, rows)
	for r := range tiles {
		tiles[r] = make([]int, cols)
	}
	ground := 14
	for c := 0; c < cols; c++ {
		ground += rng.Intn(3) - 1
		if ground < 8 {
			ground = 8
		}
		if ground > 20 {
			ground = 20
		}
		for r := ground; r < rows-1; r++ {
			tiles[r][c] = 1
		}
		tiles[rows-1][c] = 2
		if rng.Intn(6) == 0 {
			tiles[4+rng.Intn(3)][c] = 1
		}
	}
	bricks := map[int]gamedata.BrickTypeConfig{
		1: {BrickTypeID: 1, Destructible: true},
		2: {BrickTypeID: 2, Destructible: false},
	}
	terrain := NewTerrainWithBricks(gamedata.MapConfig{
		MapID: "client_fixture", GridWidth: cols, GridHeight: rows, CellSize: cell, Tiles: tiles,
	}, bricks)
	before := terrain.Snapshot()

	var ops []fixtureOp
	for i := 0; i < 12; i++ {
		x := rng.Float64() * float64(terrain.Width)
		ops = append(ops, fixtureOp{X: x, Y: terrain.GetLandingY(x, 0) + rng.Float64()*20 - 10, Radius: 15 + rng.Float64()*30})
	}
	ops = append(ops,
		fixtureOp{X: 2, Y: terrain.GetLandingY(2, 0), Radius: 30},               // clipped at the left edge
		fixtureOp{X: 320, Y: float64(terrain.Height - 8), Radius: 40},           // hits indestructible bedrock
		fixtureOp{X: 100.5, Y: terrain.GetLandingY(100, 0) + 0.5, Radius: 20.5}, // half-pixel rounding
	)
	for _, op := range ops {
		terrain.DestroyCircle(op.X, op.Y, op.Radius)
	}
	after := terrain.Snapshot()

	var landings []fixtureLanding
	for i := 0; i < 40; i++ {
		x := rng.Float64()*float64(terrain.Width+20) - 10
		startY := rng.Float64() * float64(terrain.Height)
		landings = append(landings, fixtureLanding{X: x, StartY: startY, Y: terrain.GetLandingY(x, startY)})
	}

	var walks []fixtureWalk
	for len(walks) < 40 {
		sx := float64(rng.Intn(terrain.Width))
		sy := terrain.GetLandingY(sx, 0)
		if sy >= float64(terrain.Height) {
			continue
		}
		tx := sx + float64(rng.Intn(401)-200)
		x, y := terrain.WalkTo(sx, sy, tx)
		walks = append(walks, fixtureWalk{StartX: sx, StartY: sy, TargetX: tx, X: x, Y: y})
	}

	// Shots use the gamedata.DefaultPhysics() the client preview assumes.
	savedPhysics := gamedata.Physics
	gamedata.Physics = nil
	defer func() { gamedata.Physics = savedPhysics }()
	windScale, speed := physics().WindScale, physics().ProjectileSpeedMultiplier
	weapon := gamedata.WeaponConfig{ExplosionRadius: 20, ProjectileWeight: 1, WindInfluence: 1}
	var shots []fixtureShot
	loweredOrigins := 0 // shots whose origin sits below the body centre (low ceiling or embedded feet)
	addShot := func(feet Vector2, angle, power float64, wind WindState) {
		origin := shotOriginAt(terrain, feet)
		if origin.Y != feet.Y-ShotOriginLift {
			loweredOrigins++
		}
		r := SimulateProjectile("shooter", 1, origin, angle, power, weapon, wind,
			terrain, map[string]*BattlePlayerState{}, 0)
		rad := angle * math.Pi / 180.0
		shot := fixtureShot{
			FeetX: feet.X, FeetY: feet.Y,
			X: origin.X, Y: origin.Y,
			VX: power * speed * math.Cos(rad), VY: -power * speed * math.Sin(rad),
			WindForce: float64(wind.Direction) * wind.Power * windScale * weapon.WindInfluence,
		}
		if r.ExplosionPoint != nil {
			shot.Hit, shot.HitX, shot.HitY = true, r.ExplosionPoint.X, r.ExplosionPoint.Y
		}
		shots = append(shots, shot)
	}
	for len(shots) < 60 {
		w := walks[len(shots)%len(walks)]
		angle := rng.Float64() * 180
		power := 10 + rng.Float64()*90
		wind := WindState{Direction: rng.Intn(3) - 1, Power: rng.Float64() * 4}
		addShot(Vector2{X: w.StartX, Y: w.StartY}, angle, power, wind)
	}
	// Lowered origins (no rng, so the move sequences below are unchanged): feet 12 px under a
	// terrain overhang, where the origin is the free pixel just below it, and feet embedded in
	// the ground, where the origin is the feet.
	for x, lowCeilings := 0, 0; x < terrain.Width && lowCeilings < 3; x += 7 {
		for y := 1; y < terrain.Height-13; y++ {
			if terrain.IsSolid(float64(x), float64(y-1)) && !terrain.IsSolid(float64(x), float64(y)) &&
				!terrain.IsSolid(float64(x), float64(y+12)) {
				addShot(Vector2{X: float64(x), Y: float64(y + 12)}, 30+float64(lowCeilings)*60, 40, WindState{})
				lowCeilings++
				break
			}
		}
	}
	for _, x := range []float64{150, 450} {
		addShot(Vector2{X: x, Y: terrain.GetLandingY(x, 0) + 8}, 60, 40, WindState{})
	}

	// Move sequences through the real processMove, with direction changes and energy limits.
	m, _ := newFlowMatch(t)
	m.Terrain = terrain
	mover := m.State.Players[m.State.CurrentPlayerID]
	var moves []fixtureMoveSeq
	for len(moves) < 6 {
		w := walks[rng.Intn(len(walks))]
		seq := fixtureMoveSeq{StartX: w.StartX, StartY: w.StartY, Energy: 60 + rng.Intn(41)}
		mover.Position = Vector2{X: w.StartX, Y: w.StartY}
		mover.MoveEnergy = seq.Energy
		for i := 0; i < 12; i++ {
			tx := mover.Position.X + float64(rng.Intn(51)-25)
			if i == 10 {
				tx = mover.Position.X + float64(rng.Intn(601)-300) // beyond the energy budget
			}
			m.processMove(context.Background(), nil, MoveAction{TargetX: tx})
			seq.Steps = append(seq.Steps, fixtureMoveStep{TargetX: tx, X: mover.Position.X, Y: mover.Position.Y, Energy: mover.MoveEnergy})
		}
		moves = append(moves, seq)
	}

	data, err := json.MarshalIndent(map[string]any{
		"before":   before,
		"ops":      ops,
		"after":    after,
		"landings": landings,
		"walks":    walks,
		"shots":    shots,
		"moves":    moves,
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%dx%d, %d ops, %d landings, %d walks, %d shots (%d lowered origins), %d move sequences)",
		out, terrain.Width, terrain.Height, len(ops), len(landings), len(walks), len(shots), loweredOrigins, len(moves))
}
