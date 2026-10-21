package match

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// chainFixtureMatch is a grassland_valley match with Tanko (dragon cannon, rank 4) at x 450
// and the given enemies (character, x), no wind, Tanko's turn.
func chainFixtureMatch(t *testing.T, enemies ...skillPlayer) (*Match, map[string]*ws.Client) {
	t.Helper()
	useConfigGameData(t)
	specs := append([]skillPlayer{{"tanko", 1, "tanko", 450}}, enemies...)
	var players []*BattlePlayerState
	clients := map[string]*ws.Client{}
	for _, s := range specs {
		b := battlestats.Base(s.char)
		weapon := ""
		if s.id == "tanko" {
			weapon = "tanko_weapon_lv40"
		}
		players = append(players, &BattlePlayerState{PlayerID: s.id, DisplayName: s.id, TeamID: s.team, CharacterID: s.char,
			WeaponItemID: weapon, HP: b.HP, MaxHP: b.HP, Defense: b.Defense, MaxMoveEnergy: b.TurnMoveEnergy(),
			Position: Vector2{X: s.x, Y: 300}, IsAlive: true, StatusEffects: []StatusEffect{}})
		clients[s.id] = &ws.Client{PlayerID: s.id, Send: make(chan ws.Message, 512)}
	}
	conns := map[string]*ws.Client{}
	for id, c := range clients {
		conns[id] = c
	}
	m := NewMatch("chain-fixture", "r", "pvp_2v2", "grassland_valley", players, conns, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.State.CurrentPlayerID = "tanko"
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	return m, clients
}

// fireChainFixture fires Tanko's bomb chain at angle / power, checks the replay is coherent and
// returns the fixture case.
func fireChainFixture(t *testing.T, m *Match, clients map[string]*ws.Client, angle, power float64) skillFixtureCase {
	t.Helper()
	players := map[string]*BattlePlayerState{}
	for id, q := range m.State.Players {
		cp := *q
		cp.StatusEffects = append([]StatusEffect{}, q.StatusEffects...)
		players[id] = &cp
	}
	c := skillFixtureCase{MapID: m.State.MapID, Shooter: "tanko", Players: players, Terrain: m.Terrain.Snapshot()}
	s := fireSkill(t, m, clients, "tanko", angle, power)
	c.Shot, _ = json.Marshal(s)
	c.After = m.Terrain.Snapshot()
	assertChainCoherent(t, s)
	return c
}

// chainAims are Tanko's flat shot (the bot's best normal shot, low arc) and its lobbed shot
// (lobShot) at the enemies, with perfect aim.
func chainAims(m *Match) (flat, lob shotPlan) {
	p := m.State.Players["tanko"]
	w, _ := botWeapon(p)
	b := NewBotBrain(perfectAim)
	return b.bestShot(m, p, p.Position, w), b.lobShot(m, p, w)
}

// chainHits is how many bombs hit each player and the damage they took.
func chainHits(s ShotResolved) (hits, damage map[string]int) {
	hits, damage = map[string]int{}, map[string]int{}
	for _, e := range eventsOf(s, "damage", "") {
		if e.Cause == "explosion" {
			hits[e.PlayerID]++
		}
		damage[e.PlayerID] += e.Amount
	}
	return hits, damage
}

// TestBombChainOnRealMap: rank 4 on grassland_valley at a typical distance (600 px). Every bomb
// flies the shot's own arc: the flat shot hits the target with its first bomb and the rest fly
// on through the crater; the lobbed one comes down steeply, digs one shaft and hits the
// target again and again up to the per-target cap. Every bomb explodes (none flies past
// MaxFlightSeconds), every replay is coherent (assertChainCoherent) and nobody leaves the
// map. With BOMBCHAIN_FIXTURE_OUT set it writes the Godot client's fixture
// (tests/fixtures/skill_bombchain.json: "flat", "lob" one target, "pair" two enemies side by
// side, lobbed).
func TestBombChainOnRealMap(t *testing.T) {
	cases := map[string]skillFixtureCase{}
	m, cl := chainFixtureMatch(t, skillPlayer{"e1", 2, "rookie", 1050})
	flat, lob := chainAims(m)
	if math.Abs(lob.angle-90) > chainBotLobSpan || math.Abs(flat.angle-90) <= chainBotLobSpan {
		t.Fatalf("flat shot at %v°, lob at %v°", flat.angle, lob.angle)
	}
	cases["flat"] = fireChainFixture(t, m, cl, flat.angle, flat.power)
	m, cl = chainFixtureMatch(t, skillPlayer{"e1", 2, "rookie", 1050})
	cases["lob"] = fireChainFixture(t, m, cl, lob.angle, lob.power)
	m, cl = chainFixtureMatch(t, skillPlayer{"e1", 2, "rookie", 1000}, skillPlayer{"e2", 2, "spark", 1070})
	_, lob2 := chainAims(m)
	cases["pair"] = fireChainFixture(t, m, cl, lob2.angle, lob2.power)
	hits, damage := map[string]map[string]int{}, map[string]map[string]int{}
	for _, k := range []string{"flat", "lob", "pair"} {
		var s ShotResolved
		_ = json.Unmarshal(cases[k].Shot, &s)
		for _, e := range s.Events {
			if e.Cause == "out_of_map" {
				t.Fatalf("%s: out of map %+v", k, e)
			}
		}
		assertSameArc(t, k, s)
		for i, r := range s.Projectiles {
			if r.ExplosionPoint == nil {
				t.Fatalf("%s: bomb %d vanishes after %.2f s of flight", k, i, flightEnd(r)-r.LaunchDelay)
			}
		}
		h, d := chainHits(s)
		hits[k], damage[k] = h, d
		t.Logf("%s: %d bombs, hits %v, damage %v, %.2f s", k, len(s.Projectiles), h, d, s.Duration)
	}
	if hits["lob"]["e1"] < 5 || hits["lob"]["e1"] <= hits["flat"]["e1"] {
		t.Fatalf("lobbed chain hits %d times, flat %d", hits["lob"]["e1"], hits["flat"]["e1"])
	}
	// The lob reaches the per-target cap (1.8 × base at rank 4, after the Rookie's defense).
	tanko := cases["lob"].Players["tanko"]
	w, _ := botWeapon(tanko)
	limit := capOfBase(chainCap(4) * skillBaseDamage(tanko, gamedata.Data.Skills["heavy_bomb"], w))(cases["lob"].Players["e1"])
	if d := damage["lob"]["e1"]; d > limit || d < limit-1 {
		t.Fatalf("lobbed chain deals %d, want the cap %d", d, limit)
	}
	if out := os.Getenv("BOMBCHAIN_FIXTURE_OUT"); out != "" {
		data, err := json.MarshalIndent(cases, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestBotBombChainLobs: the bot fires its chain on a steep arc when one reaches the enemy.
func TestBotBombChainLobs(t *testing.T) {
	m, _ := chainFixtureMatch(t, skillPlayer{"e1", 2, "rookie", 1050})
	p := m.State.Players["tanko"]
	w, _ := botWeapon(p)
	angle, _, ok := botBombChain(NewBotBrain(perfectAim), m, p, w)
	if !ok || math.Abs(angle-90) > chainBotLobSpan {
		t.Fatalf("bot chain at %v° (ok %v), want a lob", angle, ok)
	}
}

// assertSameArc: nothing steers a bomb: each one flies the first bomb's arc (the same positions
// at the same time since its launch) until one of them explodes.
func assertSameArc(t *testing.T, name string, s ShotResolved) {
	t.Helper()
	first := s.Projectiles[0]
	for i, r := range s.Projectiles[1:] {
		n := min(len(r.Path), len(first.Path)) - 1
		for k := 0; k < n; k++ {
			a, b := first.Path[k], r.Path[k]
			if math.Hypot(a.Position.X-b.Position.X, a.Position.Y-b.Position.Y) > 1e-6 || math.Abs(b.Time-r.LaunchDelay-a.Time) > 1e-6 {
				t.Fatalf("%s: bomb %d leaves the arc at step %d: %+v vs %+v", name, i+1, k, b, a)
			}
		}
	}
}
