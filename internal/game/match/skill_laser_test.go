package match

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
	"time"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
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
	// Timeline: the gun charges, then the beam sweeps out; each hit when its front gets there.
	if beams[0].T != laserChargeTime || d1[0].T != laserHitTime(400) || d2[0].T != laserHitTime(800) || d2[0].T > beams[0].T+beams[0].Duration {
		t.Fatalf("timing: beam %v, hits %v %v", beams[0].T, d1[0].T, d2[0].T)
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

// grasslandLaserMatch: the laser's real-map scene on grassland_valley: Spark (dragon shock
// rifle, rank 4) on the right hill at 1450, a Rookie at 900 on the slope and a Tanko at 700 in the valley
// past the 1150 hill, all on the ground, no wind, Spark to play.
func grasslandLaserMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	t.Helper()
	useConfigGameData(t)
	type spec struct {
		id, char, weapon string
		team             int
		x                float64
	}
	var players []*BattlePlayerState
	clients := map[string]*ws.Client{}
	for _, s := range []spec{{"spark", "spark", "spark_weapon_lv40", 1, 1450}, {"e1", "rookie", "", 2, 900}, {"e2", "tanko", "", 2, 700}} {
		b := battlestats.Base(s.char)
		players = append(players, &BattlePlayerState{PlayerID: s.id, DisplayName: s.id, TeamID: s.team, CharacterID: s.char,
			WeaponItemID: s.weapon, HP: b.HP, MaxHP: b.HP, Defense: b.Defense, MaxMoveEnergy: b.TurnMoveEnergy(),
			Position: Vector2{X: s.x, Y: 300}, IsAlive: true, StatusEffects: []StatusEffect{}})
		clients[s.id] = &ws.Client{PlayerID: s.id, Send: make(chan ws.Message, 512)}
	}
	conns := map[string]*ws.Client{}
	for id, c := range clients {
		conns[id] = c
	}
	m := NewMatch("laser-fixture", "r", "pvp_2v2", "grassland_valley", players, conns, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.State.CurrentPlayerID = "spark"
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	return m, clients
}

// TestLaserOnGrassland: on the real map the best line (the bot's) goes down through the hill
// and hits both enemies, the Rookie first in full, the Tanko ×0.8; the hill stays; both are
// stunned and the Tanko's next turn is skipped (TurnStarted skipped stun).
func TestLaserOnGrassland(t *testing.T) {
	m, clients := grasslandLaserMatch(t)
	spark := m.State.Players["spark"]
	if spark.SkillRank != 4 {
		t.Fatalf("rank %d", spark.SkillRank)
	}
	angle, hits := bestLaser(m, spark)
	if hits != 2 {
		t.Fatalf("best line hits %d", hits)
	}
	origin := shotOrigin(m.Terrain, spark)
	end := beamEnd(origin, angleDir(angle), m.Terrain.Width, m.Terrain.Height)
	inside := 0
	for k := 0; k <= 200; k++ {
		x, y := origin.X+(end.X-origin.X)*float64(k)/200, origin.Y+(end.Y-origin.Y)*float64(k)/200
		if x > 800 && m.Terrain.IsSolid(x, y) {
			inside++
		}
	}
	if inside < 20 {
		t.Fatalf("the line does not go through the hill (%d samples inside)", inside)
	}
	version := m.Terrain.Version
	s := fireSkill(t, m, clients, "spark", angle, 50)
	d1, d2 := eventsOf(s, "damage", "e1"), eventsOf(s, "damage", "e2")
	if len(d1) != 1 || len(d2) != 1 || d2[0].T <= d1[0].T {
		t.Fatalf("hits e1 %+v e2 %+v", d1, d2)
	}
	weapon, _ := botWeapon(spark)
	base := skillBaseDamage(spark, gamedata.Data.Skills["shock_field"], weapon) * laserFirst(4)
	if want := int(math.Round(ReduceByDefense(base, m.State.Players["e1"].Defense))); d1[0].Amount != want && !d1[0].Critical {
		t.Fatalf("first %d, want %d", d1[0].Amount, want)
	}
	if want := int(math.Round(ReduceByDefense(base*laserDecay, m.State.Players["e2"].Defense))); d2[0].Amount != want && !d2[0].Critical {
		t.Fatalf("second %d, want %d", d2[0].Amount, want)
	}
	t.Logf("grassland laser at %.1f°: e1 %d, e2 %d, %.2f s", angle, d1[0].Amount, d2[0].Amount, s.Duration)
	if m.Terrain.Version != version {
		t.Fatal("the laser carved the hill")
	}
	if !hasStatus(s, "e1", "stun") || !hasStatus(s, "e2", "stun") {
		t.Fatal("not stunned")
	}
	started := nextTurnStarted(t, m, clients["spark"])
	if (started["currentPlayerId"] != "e1" && started["currentPlayerId"] != "e2") || started["skipped"] != "stun" {
		t.Fatalf("the stunned enemy's turn is not skipped: %v", started)
	}
}

// nextTurnStarted runs the match's next scheduled event(s) until a TurnStarted reaches c and
// returns it.
func nextTurnStarted(t *testing.T, m *Match, c *ws.Client) map[string]any {
	t.Helper()
	deadline := time.After(6 * time.Second)
	for {
		select {
		case msg := <-c.Send:
			if msg.Event == "TurnStarted" {
				var started map[string]any
				_ = json.Unmarshal(msg.Data, &started)
				return started
			}
		case ev := <-m.Events:
			m.handleEvent(ev)
		case <-deadline:
			t.Fatal("no TurnStarted")
		}
	}
}

// TestExportLaserFixture writes the grassland laser scene (two enemies on the line through
// the hill) as the Godot client's fixture tests/fixtures/skill_laser.json: the players and
// terrain before, the ShotResolved, the terrain after and the stunned enemy's TurnStarted.
// It runs only when SKILL_LASER_FIXTURE_OUT names the output file.
func TestExportLaserFixture(t *testing.T) {
	out := os.Getenv("SKILL_LASER_FIXTURE_OUT")
	if out == "" {
		t.Skip("set SKILL_LASER_FIXTURE_OUT to export the client laser fixture")
	}
	m, clients := grasslandLaserMatch(t)
	angle, _ := bestLaser(m, m.State.Players["spark"])
	players := map[string]*BattlePlayerState{}
	for id, p := range m.State.Players {
		cp := *p
		cp.StatusEffects = append([]StatusEffect{}, p.StatusEffects...)
		players[id] = &cp
	}
	c := struct {
		skillFixtureCase
		Angle       float64         `json:"angle"`
		TurnStarted json.RawMessage `json:"turnStarted"`
	}{skillFixtureCase: skillFixtureCase{MapID: m.State.MapID, Shooter: "spark", Players: players, Terrain: m.Terrain.Snapshot()}, Angle: angle}
	m.State.Players["spark"].SkillEnergy = 100
	send(m, clients["spark"], "Shoot", ShootAction{Angle: angle, Power: 50, ActionMode: "skill"})
	for c.Shot == nil {
		if msg := <-clients["spark"].Send; msg.Event == "ShotResolved" {
			c.Shot = msg.Data
			c.After = m.Terrain.Snapshot()
		}
	}
	started := nextTurnStarted(t, m, clients["spark"])
	c.TurnStarted, _ = json.Marshal(started)
	data, err := json.MarshalIndent(map[string]any{"laser": c}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("laser fixture: angle %.2f, next turn %v", angle, started)
}
