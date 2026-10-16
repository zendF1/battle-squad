package match

import (
	"context"
	"math"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
)

var perfectAim = matchmaker.BotTierConfig{HitChance: 100, UseItemChance: 1, MovementSmart: 1}

const botTestCell = 16

// newBotMatch builds a 1v1 on flat ground (surface at y=496) with the bot at
// botX and the enemy at enemyX. wall, if set, is a solid column of cells
// [col, col+2] from row top down to the ground.
func newBotMatch(t *testing.T, botX, enemyX float64, wall *[2]int) (*Match, *BattlePlayerState, *BattlePlayerState) {
	t.Helper()
	gamedata.Data = &gamedata.GameData{
		Maps: map[string]gamedata.MapConfig{},
		Characters: map[string]gamedata.CharacterConfig{
			"c1": {CharacterID: "c1", WeaponID: "w1"},
		},
		Weapons: map[string]gamedata.WeaponConfig{
			"w1": {WeaponID: "w1", Damage: 240, ExplosionRadius: 30, ProjectileWeight: 1, WindInfluence: 1},
		},
		Skills: map[string]gamedata.SkillConfig{},
		Items:  map[string]gamedata.ItemConfig{},
	}

	tiles := make([][]int, 56)
	for r := range tiles {
		tiles[r] = make([]int, 100)
		for c := range tiles[r] {
			if r >= 31 || (wall != nil && r >= wall[1] && c >= wall[0] && c < wall[0]+3) {
				tiles[r][c] = 1
			}
		}
	}
	terrain := NewTerrainWithBricks(gamedata.MapConfig{
		MapID: "bot_test", GridWidth: 100, GridHeight: 56, CellSize: botTestCell, Tiles: tiles,
	}, nil)

	bot := &BattlePlayerState{PlayerID: "bot_1", TeamID: 1, CharacterID: "c1", HP: 1500, MaxHP: 1500,
		MoveEnergy: 100, Position: Vector2{X: botX, Y: 0}, IsAlive: true, IsBot: true}
	enemy := &BattlePlayerState{PlayerID: "p2", TeamID: 2, CharacterID: "c1", HP: 1500, MaxHP: 1500,
		Position: Vector2{X: enemyX, Y: 0}, IsAlive: true}
	m := NewMatch("m1", "r1", "pvp_1v1", "bot_test", []*BattlePlayerState{bot, enemy},
		map[string]*ws.Client{}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.Terrain = terrain
	for _, p := range m.State.Players {
		p.Position.Y = terrain.SpawnY(p.Position.X, 0)
	}
	m.BotTier = perfectAim
	m.State.CurrentPlayerID = bot.PlayerID
	return m, bot, enemy
}

// assertShotLands fires act from the bot and checks it explodes within the blast radius of target.
func assertShotLands(t *testing.T, m *Match, bot, target *BattlePlayerState, act ShootAction) {
	t.Helper()
	weapon := gamedata.Data.Weapons["w1"]
	r := SimulateProjectile(bot.PlayerID, bot.TeamID, bot.Position, act.Angle, act.Power, weapon,
		m.State.Wind, m.Terrain, m.State.Players, 0)
	if r.ExplosionPoint == nil {
		t.Fatalf("shot %.0f°/%.0f never exploded", act.Angle, act.Power)
	}
	d := math.Hypot(r.ExplosionPoint.X-target.Position.X, r.ExplosionPoint.Y-target.Position.Y)
	if d >= float64(weapon.ExplosionRadius) {
		t.Fatalf("shot %.0f°/%.0f exploded at %+v, %.0fpx from target %+v", act.Angle, act.Power, *r.ExplosionPoint, d, target.Position)
	}
}

func decideShot(t *testing.T, m *Match, bot *BattlePlayerState) ShootAction {
	t.Helper()
	action := NewBotBrain(m.BotTier).DecideAction(m, bot, botTurnState{})
	act, ok := action.(ShootAction)
	if !ok {
		t.Fatalf("expected ShootAction, got %#v", action)
	}
	return act
}

func TestBotHitsTargetAtSameHeight(t *testing.T) {
	for _, tc := range []struct {
		name         string
		botX, enemyX float64
	}{
		{"right 600px", 400, 1000},
		{"left 600px", 1000, 400},
		{"right 1100px", 200, 1300},
		{"right 150px", 700, 850},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, bot, enemy := newBotMatch(t, tc.botX, tc.enemyX, nil)
			assertShotLands(t, m, bot, enemy, decideShot(t, m, bot))
		})
	}
}

func TestBotCompensatesForWind(t *testing.T) {
	for _, dir := range []int{-1, 1} {
		m, bot, enemy := newBotMatch(t, 400, 1000, nil)
		m.State.Wind = WindState{Direction: dir, Power: 4}
		assertShotLands(t, m, bot, enemy, decideShot(t, m, bot))
	}
}

func TestBotShootsOverWall(t *testing.T) {
	// 48px wide, 176px tall wall halfway between the players
	m, bot, enemy := newBotMatch(t, 400, 1000, &[2]int{43, 20})
	assertShotLands(t, m, bot, enemy, decideShot(t, m, bot))
}

func TestBotMissRollFiresHarmlessNearbyShot(t *testing.T) {
	for _, tc := range []struct {
		name         string
		botX, enemyX float64
	}{
		{"600px", 400, 1000},
		{"150px", 700, 850},
		{"1100px", 200, 1300},
		{"600px left", 1000, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, bot, enemy := newBotMatch(t, tc.botX, tc.enemyX, nil)
			m.BotTier.HitChance = 0
			brain := NewBotBrain(m.BotTier)
			weapon := gamedata.Data.Weapons["w1"]
			for i := 0; i < 20; i++ {
				act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction)
				if act.Angle < botMinAngle || act.Angle > botMaxAngle || act.Power < botMinPower || act.Power > maxShotPower {
					t.Fatalf("shot out of range: %.1f°/%.1f", act.Angle, act.Power)
				}
				s := brain.evalShot(m, bot, bot.Position, act.Angle, act.Power, weapon)
				if s.damage > 0 || s.allyRisk > 0 {
					t.Fatalf("miss roll fired %.0f°/%.0f: damage=%.0f allyRisk=%d", act.Angle, act.Power, s.damage, s.allyRisk)
				}
				if s.miss > float64(weapon.ExplosionRadius)+botNearMissSlack {
					t.Fatalf("miss landed %.0fpx from target %+v, want a near miss", s.miss, enemy.Position)
				}
			}
		})
	}
}

func TestBotHitRollThreshold(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 400, 1000, nil)
	weapon := gamedata.Data.Weapons["w1"]
	for _, tc := range []struct {
		chance  float64
		roll    int
		wantHit bool
	}{
		{60, 599, true},
		{60, 600, false},
		{99, 989, true},
		{99, 990, false},
		{100, 999, true},
		{0, 0, false},
	} {
		brain := NewBotBrain(matchmaker.BotTierConfig{HitChance: tc.chance})
		brain.roll = func() int { return tc.roll }
		act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction)
		hit := brain.evalShot(m, bot, bot.Position, act.Angle, act.Power, weapon).damage > 0
		if hit != tc.wantHit {
			t.Errorf("hitChance=%v roll=%d: hit=%v, want %v (target %+v)", tc.chance, tc.roll, hit, tc.wantHit, enemy.Position)
		}
	}
}

func TestCryptoRollRange(t *testing.T) {
	seen := map[bool]bool{}
	for i := 0; i < 5000; i++ {
		n := cryptoRoll()
		if n < 0 || n > 999 {
			t.Fatalf("roll %d outside [0, 999]", n)
		}
		seen[n < 500] = true
	}
	if !seen[true] || !seen[false] {
		t.Fatal("rolls never covered both halves of the range")
	}
}

func TestBotMovesWhenEveryShotIsBlocked(t *testing.T) {
	// Bot sits in a cave closed toward the enemy and open to its left
	m, bot, _ := newBotMatch(t, 600, 1200, nil)
	solid := func(x0, x1, y0, y1 int) {
		for x := x0; x < x1; x++ {
			for y := y0; y < y1; y++ {
				setBit(m.Terrain.Mask, x, y, m.Terrain.Width)
			}
		}
	}
	solid(500, 720, 380, 400) // roof
	solid(700, 720, 400, 496) // right wall
	action := NewBotBrain(m.BotTier).DecideAction(m, bot, botTurnState{})
	act, ok := action.(MoveAction)
	if !ok {
		t.Fatalf("expected MoveAction, got %#v", action)
	}
	if act.TargetX >= bot.Position.X {
		t.Fatalf("expected move left toward open sky, got targetX=%.0f from x=%.0f", act.TargetX, bot.Position.X)
	}
}

func TestBotShootsAfterMedkit(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	bot.HP = 300
	bot.Items = []string{"medkit"}
	ctx := context.Background()

	m.botStep(ctx, botTurnState{}, 0)
	if bot.HP != 750 || len(bot.Items) != 0 {
		t.Fatalf("medkit not used: hp=%d items=%v", bot.HP, bot.Items)
	}
	if m.resolving || m.State.CurrentPlayerID != bot.PlayerID {
		t.Fatalf("medkit must not end the bot's turn")
	}

	select {
	case ev := <-m.Events:
		m.handleEvent(ev)
	case <-time.After(botFollowUpDelay + time.Second):
		t.Fatal("bot did not schedule a follow-up action after medkit")
	}
	if !m.resolving || bot.ShotsFired != 1 {
		t.Fatalf("bot did not shoot after medkit: resolving=%v shots=%d", m.resolving, bot.ShotsFired)
	}
}

func BenchmarkBotDecideShot(b *testing.B) {
	t := &testing.T{}
	m, bot, _ := newBotMatch(t, 400, 1000, &[2]int{43, 20})
	brain := NewBotBrain(m.BotTier)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		brain.DecideAction(m, bot, botTurnState{})
	}
}

// carve clears terrain in [x0, x1) × [y0, y1).
func carve(tr *Terrain, x0, x1, y0, y1 int) {
	for x := x0; x < x1; x++ {
		for y := y0; y < y1; y++ {
			clearBit(tr.Mask, x, y, tr.Width)
		}
	}
}

func TestBotNeverWalksOffTheMap(t *testing.T) {
	// Bot in a cave closed toward the enemy; left of the cave is a bottomless void
	m, bot, _ := newBotMatch(t, 600, 1200, nil)
	for x := 500; x < 720; x++ {
		for y := 380; y < 400; y++ {
			setBit(m.Terrain.Mask, x, y, m.Terrain.Width) // roof
		}
	}
	for x := 700; x < 720; x++ {
		for y := 400; y < 496; y++ {
			setBit(m.Terrain.Mask, x, y, m.Terrain.Width) // right wall
		}
	}
	carve(m.Terrain, 0, 470, 0, m.Terrain.Height)
	bot.MoveEnergy = 300

	for i := 0; i < 20; i++ {
		action := NewBotBrain(m.BotTier).DecideAction(m, bot, botTurnState{})
		mv, ok := action.(MoveAction)
		if !ok {
			continue
		}
		x, y := m.Terrain.WalkTo(bot.Position.X, bot.Position.Y, mv.TargetX)
		if y >= float64(m.Terrain.Height) || mv.TargetX < 470 {
			t.Fatalf("bot walked off the map: targetX=%.0f ends at (%.0f, %.0f)", mv.TargetX, x, y)
		}
	}
}

func TestPredictMoveRejectsFallingOffTheMap(t *testing.T) {
	m, bot, _ := newBotMatch(t, 600, 1200, nil)
	carve(m.Terrain, 400, 480, 0, m.Terrain.Height) // bottomless pit, ground continues past it

	if _, ok := predictMove(m.Terrain, bot.Position, 350); ok {
		t.Fatal("walk across a bottomless pit accepted")
	}
	if pos, ok := predictMove(m.Terrain, bot.Position, 500); !ok || pos.Y >= float64(m.Terrain.Height) {
		t.Fatalf("walk short of the pit rejected: %+v ok=%v", pos, ok)
	}
}

func TestPredictMoveMatchesProcessMoveOnIce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		targetX float64
		ok      bool
	}{
		{"slide on ground", 700, true},
		{"slide into pit", 840, false}, // ends 10px short of the pit, slides 50px into it
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, bot, _ := newBotMatch(t, 600, 1300, nil)
			m.Terrain.Zones = append(m.Terrain.Zones, TerrainZone{Type: "ice", MinX: 0, MaxX: float64(m.Terrain.Width), MinY: 0, MaxY: float64(m.Terrain.Height)})
			carve(m.Terrain, 850, 950, 0, m.Terrain.Height)

			pos, ok := predictMove(m.Terrain, bot.Position, tc.targetX)
			m.processMove(context.Background(), nil, MoveAction{TargetX: tc.targetX})
			if ok != tc.ok {
				t.Fatalf("predictMove ok=%v, want %v (bot ended at %+v)", ok, tc.ok, bot.Position)
			}
			if !ok {
				if bot.Position.Y < float64(m.Terrain.Height) {
					t.Fatalf("predictMove rejected a move processMove survived: %+v", bot.Position)
				}
				return
			}
			if pos != bot.Position {
				t.Fatalf("predictMove=%+v, processMove ended at %+v", pos, bot.Position)
			}
		})
	}
}

// botTurnEvents runs one bot step with a client watching the match and
// returns the events it was sent.
func botTurnEvents(t *testing.T, m *Match, bot *BattlePlayerState) []string {
	t.Helper()
	c := &ws.Client{PlayerID: "p2", Send: make(chan ws.Message, 256)}
	m.Clients["p2"] = c
	m.botStep(context.Background(), botTurnState{}, 0)
	var events []string
	for {
		select {
		case msg := <-c.Send:
			events = append(events, msg.Event)
		default:
			return events
		}
	}
}

func TestIdleBotEndsTurnWithoutShooting(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 400, 1000, nil)
	bot.IsIdle = true

	events := botTurnEvents(t, m, bot)
	if bot.ShotsFired != 0 || m.resolving {
		t.Fatalf("idle bot shot: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}
	if m.State.CurrentPlayerID != enemy.PlayerID {
		t.Fatalf("idle bot did not end its turn, current=%s", m.State.CurrentPlayerID)
	}
	for _, ev := range events {
		if ev == "ShotResolved" || ev == "PlayerMoved" {
			t.Fatalf("idle bot sent %s", ev)
		}
	}
}

func TestAIBotShootsOnItsTurn(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	m.BotTier = DefaultBotTier
	bot.DisplayName = "Target Bot" // the name no longer makes a bot idle

	events := botTurnEvents(t, m, bot)
	if bot.ShotsFired != 1 || !m.resolving {
		t.Fatalf("AI bot did not shoot: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}
	shot := false
	for _, ev := range events {
		shot = shot || ev == "ShotResolved"
	}
	if !shot {
		t.Fatalf("no ShotResolved broadcast, got %v", events)
	}
}
