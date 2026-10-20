package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
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

// (Tanko's bomb chain lobs instead: TestBotBombChainLobs.)
func TestBotRainAimedLikeItsBestShot(t *testing.T) {
	for _, char := range []string{"rookie"} {
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

// TestRainColumnsFocus: the columns crowd the flare (about half within ± spread / 4 for rainFocus
// 2), stay within the spread and the map, and the outermost slices still reach out.
func TestRainColumnsFocus(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		n, spread := rainCount(rank), rainSpread(rank)
		xs := rainColumns(rand.New(rand.NewSource(int64(rank))), n, 800, spread, 1600)
		inner, far := 0, 0.0
		for _, x := range xs {
			d := math.Abs(x - 800)
			if d > spread {
				t.Fatalf("rank %d: column %v outside ± %v", rank, x, spread)
			}
			if d <= spread/4 {
				inner++
			}
			far = math.Max(far, d)
		}
		// the outermost slice is |v| >= 1 - 2/n
		if inner < n/2-1 || inner > n/2+1 || far < spread*math.Pow(1-2/float64(n), rainFocus) {
			t.Fatalf("rank %d: %d of %d columns within ± %v (want %d ± 1), farthest %v", rank, inner, n, spread/4, n/2, far)
		}
	}
	edge := rainColumns(rand.New(rand.NewSource(1)), 14, 20, 150, 1600)
	for _, x := range edge {
		if x < 2 {
			t.Fatalf("column %v off the map", x)
		}
	}
}

// rainMapMatch is a match on the real grassland_valley map: Rookie (team 1) with weapon at
// x 450 and the enemies (base stats, no weapon), no wind, Rookie to play; id seeds the rain
// columns.
func rainMapMatch(t *testing.T, id, weapon string, enemies ...skillPlayer) (*Match, map[string]*ws.Client) {
	t.Helper()
	var players []*BattlePlayerState
	clients := map[string]*ws.Client{}
	add := func(pid, char, w string, team int, x float64) {
		b := battlestats.Base(char)
		players = append(players, &BattlePlayerState{PlayerID: pid, DisplayName: pid, TeamID: team, CharacterID: char,
			WeaponItemID: w, HP: b.HP, MaxHP: b.HP, Defense: b.Defense, MaxMoveEnergy: b.TurnMoveEnergy(),
			Position: Vector2{X: x, Y: 300}, IsAlive: true, StatusEffects: []StatusEffect{}})
		clients[pid] = &ws.Client{PlayerID: pid, Send: make(chan ws.Message, 512)}
	}
	add("rookie", "rookie", weapon, 1, 450)
	for _, e := range enemies {
		add(e.id, e.char, "", e.team, e.x)
	}
	conns := map[string]*ws.Client{}
	for k, c := range clients {
		conns[k] = c
	}
	m := NewMatch(id, "r", "pvp_2v2", "grassland_valley", players, conns, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.State.CurrentPlayerID = "rookie"
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	return m, clients
}

// TestMissileRainOnRealMap plays the rain on grassland_valley, the flare aimed like the bot's
// best shot at a Tanko 600 px away with a second one 100 px behind it, over many turns (rain
// seeds): the aimed target takes ~1 × base at rank 0 and ~1.7 × base at rank 4 (cap 2 ×), the
// one behind still takes some, both burn, and the whole skill replays in under ~4.5 s.
func TestMissileRainOnRealMap(t *testing.T) {
	useConfigGameData(t)
	const runs = 20
	base := float64(gamedata.Data.Weapons["basic_rocket"].Damage)
	for _, c := range []struct {
		rank           int
		weapon         string
		minHit, maxHit float64 // mean damage to the aimed target, × base
	}{{0, "", 0.75, 1.3}, {4, "rookie_weapon_lv40", 1.4, 2.0}} {
		var aimed, behind, burned int
		longest := 0.0
		for k := 0; k < runs; k++ {
			m, cl := rainMapMatch(t, fmt.Sprintf("rain-%d", k), c.weapon, skillPlayer{"e1", 2, "tanko", 1050}, skillPlayer{"e2", 2, "tanko", 1150})
			p := m.State.Players["rookie"]
			if p.SkillRank != c.rank {
				t.Fatalf("rank %d, want %d", p.SkillRank, c.rank)
			}
			w, _ := botWeapon(p)
			best := NewBotBrain(perfectAim).bestShot(m, p, p.Position, w)
			s := fireSkill(t, m, cl, "rookie", best.angle, best.power)
			aimed += totalDamage(s, "e1")
			behind += totalDamage(s, "e2")
			for _, id := range []string{"e1", "e2"} {
				if hasStatus(s, id, "burn") {
					burned++
				}
			}
			if limit := capOfBase(rainCap * base)(m.State.Players["e1"]); totalDamage(s, "e1") > limit {
				t.Fatalf("rank %d: %d damage over the cap %d", c.rank, totalDamage(s, "e1"), limit)
			}
			longest = math.Max(longest, s.Duration)
		}
		mean := float64(aimed) / runs / base
		t.Logf("rank %d: aimed %.2f × base, behind %.2f × base, %d burns, longest %.2f s", c.rank, mean, float64(behind)/runs/base, burned, longest)
		if mean < c.minHit || mean > c.maxHit {
			t.Fatalf("rank %d: aimed target took %.2f × base on average, want %.2f..%.2f", c.rank, mean, c.minHit, c.maxHit)
		}
		if c.rank == 4 && (behind == 0 || burned <= runs) {
			t.Fatalf("rank 4: the enemy behind took %d, %d burns", behind, burned)
		}
		if longest > 4.5 {
			t.Fatalf("rank %d: the rain lasts %.2f s", c.rank, longest)
		}
	}
}

// TestExportRainFixture writes a rank 4 missile rain on grassland_valley (Rookie with a dragon
// bazooka at 450, a Tanko at 1050 and a Spark at 1150, the flare aimed like the bot's best
// shot) and the burn tick of the Tanko's next turn (the PlayerDamaged rows) as the Godot
// client's rain fixture (tests/fixtures/skill_rain.json: {"rain": case, "burnTick": rows}).
// It runs only when RAIN_FIXTURE_OUT names the output file.
func TestExportRainFixture(t *testing.T) {
	out := os.Getenv("RAIN_FIXTURE_OUT")
	if out == "" {
		t.Skip("set RAIN_FIXTURE_OUT to export the client rain fixture")
	}
	useConfigGameData(t)
	m, cl := rainMapMatch(t, "fixture", "rookie_weapon_lv40", skillPlayer{"e1", 2, "tanko", 1050}, skillPlayer{"e2", 2, "spark", 1150})
	players := map[string]*BattlePlayerState{}
	for id, p := range m.State.Players {
		cp := *p
		cp.StatusEffects = append([]StatusEffect{}, p.StatusEffects...)
		players[id] = &cp
	}
	c := skillFixtureCase{MapID: m.State.MapID, Shooter: "rookie", Players: players, Terrain: m.Terrain.Snapshot()}
	p := m.State.Players["rookie"]
	w, _ := botWeapon(p)
	best := NewBotBrain(perfectAim).bestShot(m, p, p.Position, w)
	p.SkillEnergy = 100
	send(m, cl["rookie"], "Shoot", ShootAction{Angle: best.angle, Power: best.power, ActionMode: "skill"})
	for {
		msg := <-cl["rookie"].Send
		if msg.Event == "ShotResolved" {
			c.Shot = msg.Data
			c.After = m.Terrain.Snapshot()
			break
		}
	}
	if !HasEffect(m.State.Players["e1"], "burn") {
		t.Fatal("the Tanko does not burn: pick another seed")
	}
	ticks := messagesOf(startTurnOf(m, cl, "e1"), "PlayerDamaged")
	if len(ticks) != 1 {
		t.Fatalf("%d PlayerDamaged at the Tanko's turn start", len(ticks))
	}
	data, err := json.MarshalIndent(map[string]any{"rain": c, "burnTick": ticks[0]}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	var s ShotResolved
	_ = json.Unmarshal(c.Shot, &s)
	t.Logf("rain: %d projectiles, %d events, %.2f s; Tanko %d, Spark %d damage; burn tick %s",
		len(s.Projectiles), len(s.Events), s.Duration, totalDamage(s, "e1"), totalDamage(s, "e2"), ticks[0])
}
