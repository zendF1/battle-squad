package match

import (
	"context"
	"encoding/json"
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
	useData(t, &gamedata.GameData{
		Maps: map[string]gamedata.MapConfig{},
		Characters: map[string]gamedata.CharacterConfig{
			"c1": {CharacterID: "c1", WeaponID: "w1"},
		},
		Weapons: map[string]gamedata.WeaponConfig{
			"w1": {WeaponID: "w1", Damage: 240, ExplosionRadius: 30, ProjectileWeight: 1, WindInfluence: 1},
		},
		Skills: map[string]gamedata.SkillConfig{},
		Items:  map[string]gamedata.ItemConfig{},
	})

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
	weapon := gamedata.Current().Data.Weapons["w1"]
	r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOrigin(m.Terrain, bot), act.Angle, act.Power, weapon,
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
			weapon := gamedata.Current().Data.Weapons["w1"]
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
	weapon := gamedata.Current().Data.Weapons["w1"]
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
		m.handleEvent(ev) // aims
	case <-time.After(botFollowUpDelay + time.Second):
		t.Fatal("bot did not schedule a follow-up action after medkit")
	}
	select {
	case ev := <-m.Events:
		m.handleEvent(ev) // fires
	case <-time.After(botAimMaxDelay + time.Second):
		t.Fatal("bot did not shoot after aiming")
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

// watchMatch connects a client for p2 and returns it.
func watchMatch(m *Match) *ws.Client {
	c := &ws.Client{PlayerID: "p2", Send: make(chan ws.Message, 256)}
	m.Clients["p2"] = c
	return c
}

// drainSent returns the messages sent to c so far.
func drainSent(c *ws.Client) []ws.Message {
	var msgs []ws.Message
	for {
		select {
		case msg := <-c.Send:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// botTurnEvents runs one bot step with a client watching the match, runs the
// delayed shot after an aim, and returns the events the client was sent.
func botTurnEvents(t *testing.T, m *Match, bot *BattlePlayerState) []string {
	t.Helper()
	c := watchMatch(m)
	m.botStep(context.Background(), botTurnState{}, 0)
	var events []string
	for _, msg := range drainSent(c) {
		events = append(events, msg.Event)
	}
	if len(events) > 0 && events[len(events)-1] == "PlayerAiming" {
		select {
		case ev := <-m.Events:
			m.handleEvent(ev)
		case <-time.After(botAimMaxDelay + time.Second):
			t.Fatal("bot aimed but never fired")
		}
		for _, msg := range drainSent(c) {
			events = append(events, msg.Event)
		}
	}
	return events
}

// aimingOf decodes the single PlayerAiming in msgs.
func aimingOf(t *testing.T, msgs []ws.Message) PlayerAiming {
	t.Helper()
	var found []PlayerAiming
	for _, msg := range msgs {
		if msg.Event == "ShotResolved" {
			t.Fatalf("shot fired with the aim, before the aim delay")
		}
		if msg.Event == "PlayerAiming" {
			var a PlayerAiming
			if err := json.Unmarshal(msg.Data, &a); err != nil {
				t.Fatal(err)
			}
			found = append(found, a)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one PlayerAiming, got %d in %d messages", len(found), len(msgs))
	}
	return found[0]
}

func TestBotAimsBeforeShooting(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	c := watchMatch(m)

	start := time.Now()
	m.botStep(context.Background(), botTurnState{}, 0)
	aim := aimingOf(t, drainSent(c))
	if aim.PlayerID != bot.PlayerID {
		t.Fatalf("aiming playerId=%q, want %q", aim.PlayerID, bot.PlayerID)
	}
	if d := time.Duration(aim.DurationMs) * time.Millisecond; d < botAimMinDelay || d > botAimMaxDelay {
		t.Fatalf("durationMs=%d outside [%v, %v]", aim.DurationMs, botAimMinDelay, botAimMaxDelay)
	}
	if bot.ShotsFired != 0 || m.resolving {
		t.Fatalf("bot fired while aiming: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}

	var ev matchEvent
	select {
	case ev = <-m.Events:
	case <-time.After(botAimMaxDelay + time.Second):
		t.Fatal("aimed shot never scheduled")
	}
	// runOnTurnAfter timers are not early; allow a little scheduling slack
	if waited := time.Since(start); waited < time.Duration(aim.DurationMs)*time.Millisecond-50*time.Millisecond {
		t.Fatalf("shot arrived after %v, before durationMs=%d", waited, aim.DurationMs)
	}
	m.handleEvent(ev)
	if bot.ShotsFired != 1 || !m.resolving {
		t.Fatalf("bot did not fire after aiming: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}

	var shot ShotResolved
	for _, msg := range drainSent(c) {
		if msg.Event == "ShotResolved" {
			if err := json.Unmarshal(msg.Data, &shot); err != nil {
				t.Fatal(err)
			}
		}
	}
	if shot.ShooterID != bot.PlayerID || len(shot.Projectiles) == 0 {
		t.Fatalf("no ShotResolved from the bot after aiming: %+v", shot)
	}
}

// loggedEvents drains the match event log (not started in unit tests).
func loggedEvents(m *Match) []eventLogEntry {
	var entries []eventLogEntry
	for {
		select {
		case e := <-m.el.events:
			entries = append(entries, e)
		default:
			return entries
		}
	}
}

func TestBotAimAngleIsTheFiredAngle(t *testing.T) {
	// A miss roll fires a nearby angle, not the best one: the aim must show it
	for _, chance := range []float64{100, 0} {
		m, bot, _ := newBotMatch(t, 400, 1000, nil)
		m.BotTier.HitChance = chance
		c := watchMatch(m)

		m.botStep(context.Background(), botTurnState{}, 0)
		aim := aimingOf(t, drainSent(c))
		m.handleEvent(<-m.Events)
		if bot.ShotsFired != 1 {
			t.Fatalf("hitChance=%v: bot did not fire", chance)
		}

		var logged []string
		var fired *ShootAction
		for _, e := range loggedEvents(m) {
			logged = append(logged, e.eventType)
			if e.eventType == "Shoot" {
				fired = &ShootAction{}
				if err := json.Unmarshal(e.data, fired); err != nil {
					t.Fatal(err)
				}
			}
		}
		if len(logged) < 2 || logged[0] != "PlayerAiming" || logged[1] != "Shoot" {
			t.Fatalf("hitChance=%v: event log %v, want PlayerAiming then Shoot", chance, logged)
		}
		if fired.Angle != aim.Angle {
			t.Fatalf("hitChance=%v: aimed %.2f°, fired %.2f°", chance, aim.Angle, fired.Angle)
		}
	}
}

func TestTurnChangeCancelsAimedShot(t *testing.T) {
	m, bot, enemy := newBotMatch(t, 400, 1000, nil)
	c := watchMatch(m)

	m.botStep(context.Background(), botTurnState{}, 0)
	aimingOf(t, drainSent(c))
	m.endTurn(context.Background()) // e.g. the turn timer ran out while aiming

	select {
	case ev := <-m.Events:
		m.handleEvent(ev)
	case <-time.After(botAimMaxDelay + time.Second):
		t.Fatal("aimed shot callback never ran")
	}
	if bot.ShotsFired != 0 || m.resolving {
		t.Fatalf("aimed shot fired after the turn changed: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}
	if m.State.CurrentPlayerID != enemy.PlayerID {
		t.Fatalf("stale aimed shot changed the turn: current=%s", m.State.CurrentPlayerID)
	}
}

func TestBotDeadDuringAimDoesNotShoot(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	watchMatch(m)

	m.botStep(context.Background(), botTurnState{}, 0)
	bot.IsAlive = false
	m.handleEvent(<-m.Events)
	if bot.ShotsFired != 0 || m.resolving {
		t.Fatalf("dead bot fired its aimed shot: shots=%d resolving=%v", bot.ShotsFired, m.resolving)
	}
}

func TestRandomDelayRange(t *testing.T) {
	for i := 0; i < 1000; i++ {
		if d := randomDelay(botAimMinDelay, botAimMaxDelay); d < botAimMinDelay || d > botAimMaxDelay {
			t.Fatalf("aim delay %v outside [%v, %v]", d, botAimMinDelay, botAimMaxDelay)
		}
		if d := randomDelay(botTurnStartMinDelay, botTurnStartMaxDelay); d < botTurnStartMinDelay || d > botTurnStartMaxDelay {
			t.Fatalf("turn start delay %v outside [%v, %v]", d, botTurnStartMinDelay, botTurnStartMaxDelay)
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
