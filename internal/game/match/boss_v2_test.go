package match

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// asTurnOf makes it p's turn, as startTurn would leave it (no timers).
func asTurnOf(m *Match, p *BattlePlayerState) {
	m.State.CurrentPlayerID = p.PlayerID
	m.resolving = false
	m.turnSeq++ // a new turn (a leaper's leapt dates from its own turn)
	p.MoveEnergy = m.turnMoveEnergy(p)
	if p.Role == RoleMinion {
		p.MoveEnergy = m.minionMoveEnergy(p)
	}
}

// blastDamage is what a minion blast of pct % with radius deals p (target mode) from point.
func blastDamage(m *Match, p *BattlePlayerState, base int, point Vector2, pct, radius float64) int {
	d := blastEdgeDistance(p, point, m.gd.Physics.PlayerHitRadius, shotOriginLift(m.Terrain))
	if d > radius {
		return 0
	}
	return int(math.Round(float64(base) * pct / 100 * (1 - 0.5*d/radius)))
}

// A drone flies toward the nearest party member (UnitFlew, at most flyRange px, over the ground)
// and, ending its move within detonateRange, detonates: a ShotResolved self_destruct with the
// blast (explode with blastRadius, damage cause self_destruct = 50 % of each target's max HP,
// falling off to half at the radius, no defense) and its die.
func TestBomberFliesAndDetonates(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()

	t.Run("approach", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 1)
		drone := m.State.Players["minion_drone_1"]
		p0 := m.State.Players["p0"]
		from := drone.Position
		asTurnOf(m, drone)
		drainMsgs(watch)
		m.playMoverTurn(ctx, drone)
		var fl UnitFlew
		if err := json.Unmarshal(lastEvent(t, drainMsgs(watch), "UnitFlew"), &fl); err != nil {
			t.Fatal(err)
		}
		moved := math.Hypot(drone.Position.X-from.X, drone.Position.Y-from.Y)
		if fl.PlayerID != drone.PlayerID || fl.From != from || fl.To != drone.Position || moved > 270+61 || moved < 200 || // ≤ flyRange 270 (+ the hover lift), 3 × v2's
			math.Abs(fl.Duration-moved/220) > 0.01 || drone.Position.X >= from.X || !drone.IsAlive {
			t.Fatalf("flight %+v moved %.0f to %v (p0 at %v)", fl, moved, drone.Position, p0.Position)
		}
		ground := m.Terrain.SpawnY(drone.Position.X, drone.Position.Y)
		if ground < float64(m.Terrain.Height) && math.Abs(ground-60-drone.Position.Y) > 1 {
			t.Fatalf("drone at %v, ground %v: not 60 px over it", drone.Position, ground)
		}
	})

	t.Run("detonation", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 2)
		drone := m.State.Players["minion_drone_1"]
		p0, p1 := m.State.Players["p0"], m.State.Players["p1"]
		p1.Position = Vector2{X: p0.Position.X - 70, Y: m.Terrain.SpawnY(p0.Position.X-70, p0.Position.Y-30)}
		drone.Position = Vector2{X: p0.Position.X + 80, Y: p0.Position.Y - 60}
		for _, p := range []*BattlePlayerState{p0, p1} {
			p.Shield, p.HP = 0, p.MaxHP
		}
		asTurnOf(m, drone)
		drainMsgs(watch)
		m.playMoverTurn(ctx, drone)
		point := drone.Position
		if math.Hypot(point.X-p0.Position.X, point.Y-p0.Position.Y) > 70 {
			t.Fatalf("drone ended at %v, p0 at %v", point, p0.Position)
		}
		s := shotOf(t, watch)
		if s.ShooterID != drone.PlayerID || s.ActionMode != ActionSelfDestruct || len(s.Projectiles) != 0 || drone.IsAlive {
			t.Fatalf("shot %+v alive %v", s, drone.IsAlive)
		}
		want0 := blastDamage(m, p0, p0.MaxHP, point, 50, 80)
		want1 := blastDamage(m, p1, p1.MaxHP, point, 50, 80)
		if want0 < p0.MaxHP*45/100-1 || want1 <= 0 || float64(want1)/float64(p1.MaxHP) >= float64(want0)/float64(p0.MaxHP) {
			t.Fatalf("expected %d / %d", want0, want1)
		}
		got := map[string]int{}
		explode, die := false, false
		for _, e := range s.Events {
			switch {
			case e.Type == "explode" && e.PlayerID == drone.PlayerID:
				explode = e.BlastRadius == 80 && e.Radius == 20 && *e.Position == point
			case e.Type == "damage" && e.Cause == CauseSelfDestruct:
				got[e.PlayerID] = e.Amount
			case e.Type == "die" && e.PlayerID == drone.PlayerID:
				die = e.Cause == CauseSelfDestruct
			}
		}
		if !explode || !die || got["p0"] != want0 || got["p1"] != want1 || p0.MaxHP-p0.HP != want0 {
			t.Fatalf("explode %v die %v damage %v want %d / %d", explode, die, got, want0, want1)
		}
	})

	t.Run("self", func(t *testing.T) {
		m, watch := bossMatch(t, "steel_robot", 1)
		m.boss.cfg.Minions[0].ExplodeDamageOf = gamedata.ExplodeOfSelf
		drone := m.State.Players["minion_drone_1"]
		p0 := m.State.Players["p0"]
		p0.Shield, p0.HP = 0, p0.MaxHP
		drone.Position = Vector2{X: p0.Position.X + 40, Y: p0.Position.Y - 60}
		asTurnOf(m, drone)
		drainMsgs(watch)
		m.playMoverTurn(ctx, drone)
		want := blastDamage(m, p0, drone.MaxHP, drone.Position, 50, 80) // 50 % of the drone's 650
		if s := shotOf(t, watch); s.ActionMode != ActionSelfDestruct || p0.MaxHP-p0.HP != want || want <= 0 || want > 325 {
			t.Fatalf("self blast: lost %d, want %d", p0.MaxHP-p0.HP, want)
		}
	})
}

// A crab walks toward the nearest party member on the continuous mound arena, at most walkRange
// px a turn, never off the map, and detonates once it ends a walk next to them.
func TestCrabWalksToPlayer(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()
	m, watch := bossMatch(t, "lava_crab", 1)
	crab := m.State.Players["minion_crab_1"]
	p0 := m.State.Players["p0"]
	p0.HP, p0.MaxHP = 100000, 100000
	turns := 0
	for ; turns < 40 && m.detonateTarget(crab) == nil; turns++ {
		asTurnOf(m, crab)
		before := crab.Position
		m.minionMove(ctx, crab)
		if crab.Position.Y >= float64(m.Terrain.Height) || math.Abs(crab.Position.X-before.X) > 241 {
			t.Fatalf("turn %d: %v → %v", turns, before, crab.Position)
		}
		if crab.Position == before {
			t.Fatalf("turn %d: stuck at %v (p0 at %v)", turns, before, p0.Position)
		}
		m.Terrain.CarveBowl(crab.Position.X+40, crab.Position.Y, 30) // the party shelling the ground in front of it
	}
	if m.detonateTarget(crab) != p0 || turns < 2 {
		t.Fatalf("after %d turns crab at %v, p0 at %v", turns, crab.Position, p0.Position)
	}
	drainMsgs(watch)
	m.selfDestruct(ctx, crab)
	if s := shotOf(t, watch); s.ActionMode != ActionSelfDestruct || p0.HP >= p0.MaxHP {
		t.Fatalf("crab detonation %+v", s.Events)
	}
}

// The mound arena's ground is continuous: after many random craters every column still has
// ground (the hard core), and a walk across it never drops off the map.
func TestCrabArenaHasNoHoles(t *testing.T) {
	loadConfigs(t)
	gd := gamedata.Current()
	cfg := gd.Data.Maps["arena_lava_crab_mound"]
	tr := NewTerrainFor(gd, cfg)
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 800; i++ {
		x := rng.Float64() * float64(tr.Width)
		y := tr.GetLandingY(x, 0) + rng.Float64()*60 - 10
		tr.CarveBowl(x, y, 10+rng.Float64()*50)
	}
	for x := 0; x < tr.Width; x++ {
		if tr.GetLandingY(float64(x), 0) >= float64(tr.Height) {
			t.Fatalf("column %d has no ground", x)
		}
	}
	for _, from := range []float64{20, float64(tr.Width) - 20} {
		start := Vector2{X: from, Y: tr.GetLandingY(from, 0)}
		if _, ok := walkChecked(tr, start, float64(tr.Width)-from); !ok {
			t.Fatalf("a walk from %v falls off the map", start)
		}
	}
}

// Every crab spawn on the mound arena (initial and summoned) stands on the ground with its body
// at least 40 px clear of the crab boss's as the client draws them (~50 px half-width per
// bodySize: the boss's 3.2 → ~158 px); the initial crabs land there.
func TestCrabSpawnsClearOfBoss(t *testing.T) {
	loadConfigs(t)
	const drawnHalfWidth = 158.0 / 3.2
	m, _ := bossMatch(t, "lava_crab", 1)
	boss := m.State.Players[BossPlayerID("lava_crab")]
	mc, _ := m.boss.cfg.Minion("crab")
	clear := func(x float64) float64 {
		return math.Abs(x-boss.Position.X) - drawnHalfWidth*(boss.BodySize+mc.BodySize)
	}
	if len(m.boss.spawns) != 4 {
		t.Fatalf("spawns %v", m.boss.spawns)
	}
	for _, sp := range m.boss.spawns {
		if gap := clear(sp.X); gap < 40 || !m.Terrain.CanStandAt(sp.X, sp.Y) {
			t.Fatalf("spawn %v: %.0f px from the boss's body (boss at %v), stands %v", sp, gap, boss.Position, m.Terrain.CanStandAt(sp.X, sp.Y))
		}
	}
	for _, id := range m.boss.minions {
		p := m.State.Players[id]
		if gap := clear(p.Position.X); gap < 40 || p.Position.Y != m.Terrain.SpawnY(p.Position.X, p.Position.Y) {
			t.Fatalf("%s at %v: %.0f px from the boss's body", id, p.Position, gap)
		}
	}
}

// The witch hovers: each turn she flies (UnitFlew) to a point of her band at least minMove px
// away, at flySpeed; a crater under her does not move her.
func TestWitchHovers(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "ice_witch", 4)
	witch := m.State.Players["boss_ice_witch"]
	h := m.boss.cfg.Hover.Normalized()
	in := func(p Vector2) bool { return p.X >= h.MinX && p.X <= h.MaxX && p.Y >= h.MinY && p.Y <= h.MaxY }
	if !witch.Flying || !in(witch.Position) {
		t.Fatalf("witch starts at %v", witch.Position)
	}
	for i := 0; i < 12; i++ {
		m.State.TurnIndex = i
		before := witch.Position
		drainMsgs(watch)
		dur := m.bossHover(witch)
		d := math.Hypot(witch.Position.X-before.X, witch.Position.Y-before.Y)
		var fl UnitFlew
		_ = json.Unmarshal(lastEvent(t, drainMsgs(watch), "UnitFlew"), &fl)
		if d < h.MinMove || !in(witch.Position) || math.Abs(dur-d/h.FlySpeed) > 0.01 || fl.To != witch.Position || fl.From != before {
			t.Fatalf("hover %d: %v → %v (%.0f px, %.2f s) %+v", i, before, witch.Position, d, dur, fl)
		}
	}
	pos := witch.Position
	ground := m.Terrain.GetLandingY(pos.X, pos.Y)
	if ground < float64(m.Terrain.Height) {
		m.Terrain.CarveBowl(pos.X, ground, 80)
	}
	tl := m.newTimeline()
	m.settleFalls(m.State.Players["p0"], 0, tl)
	if witch.Position != pos || !witch.IsAlive {
		t.Fatalf("the witch fell: %v → %v", pos, witch.Position)
	}
}

// A snowman's death blast (explodeOnDeath) follows its die in the same ShotResolved, hits the
// party and the other minions (not the boss) and chains: a snowman it kills blasts too.
func TestSnowmanDeathBlastChains(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "ice_witch", 1)
	s1, s2 := m.State.Players["minion_snowman_1"], m.State.Players["minion_snowman_2"]
	p0 := m.State.Players["p0"]
	p0.Shield, p0.HP = 0, p0.MaxHP
	s2.Position = Vector2{X: s1.Position.X + 50, Y: m.Terrain.SpawnY(s1.Position.X+50, s1.Position.Y-20)}
	p0.Position = Vector2{X: s1.Position.X - 50, Y: m.Terrain.SpawnY(s1.Position.X-50, s1.Position.Y-20)}
	s2.HP = 10
	bossHP := m.State.Players["boss_ice_witch"].HP

	tl := m.newTimeline()
	r := &ProjectileResult{ProjectileID: "x", HitPlayerID: s1.PlayerID, ExplosionPoint: vecPtr(bodyCentre(s1, 20)), ExplosionRadius: 10,
		Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
	m.explodeProjectile(p0, r, 1e6, tl)
	drainMsgs(watch)
	m.broadcastShot(&ShotResolved{ShooterID: p0.PlayerID, ActionMode: "weapon", Projectiles: []*ProjectileResult{r}}, tl)
	s := shotOf(t, watch)

	var order []string
	var versions []uint64
	blastOn := map[string]int{}
	for _, e := range s.Events {
		if e.Type == "explode" {
			versions = append(versions, e.TerrainVersion)
		}
		switch {
		case e.Type == "die":
			order = append(order, "die:"+e.PlayerID)
		case e.Type == "explode" && e.PlayerID != "":
			order = append(order, "blast:"+e.PlayerID)
			if e.BlastRadius != 140 {
				t.Fatalf("blast radius %v", e.BlastRadius)
			}
		case e.Type == "damage" && e.Cause == CauseDeathBlast:
			blastOn[e.PlayerID] += e.Amount
		}
	}
	want := []string{"die:" + s1.PlayerID, "blast:" + s1.PlayerID, "die:" + s2.PlayerID, "blast:" + s2.PlayerID}
	if len(order) != 4 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] || order[3] != want[3] {
		t.Fatalf("order %v, want %v", order, want)
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Fatalf("terrain versions out of order %v", versions)
		}
	}
	first := blastDamage(m, p0, p0.MaxHP, s1.Position, 15, 140)
	if blastOn["p0"] < first || blastOn[s2.PlayerID] <= 0 || s2.IsAlive || m.State.Players["boss_ice_witch"].HP != bossHP ||
		p0.MinionDamage < 10 || blastOn["boss_ice_witch"] != 0 {
		t.Fatalf("blast damage %v (p0 first %d), p0 minion damage %d", blastOn, first, p0.MinionDamage)
	}
}

// On the pillar arena the party spawns on distinct random pillars and the snowmen on free ones.
func TestPillarSpawnsDistinct(t *testing.T) {
	loadConfigs(t)
	gd := gamedata.Current()
	cfg := gd.Data.Maps["arena_ice_witch_pillars"]
	pillars := gamedata.ArenaPillars(cfg)
	if len(pillars) != 9 {
		t.Fatalf("%d pillars", len(pillars))
	}
	seen := map[float64]bool{}
	for seed := int64(0); seed < 40; seed++ {
		sps := gamedata.BossPartySpawns(cfg, 4, rand.New(rand.NewSource(seed)).Intn)
		xs := map[float64]bool{}
		for _, sp := range sps {
			if xs[sp.X] || sp.Set != gamedata.SpawnSetPillar {
				t.Fatalf("seed %d: %v", seed, sps)
			}
			xs[sp.X] = true
			seen[sp.X] = true
		}
	}
	if len(seen) < 8 {
		t.Fatalf("only %d pillars ever used", len(seen))
	}
	m, _ := bossMatch(t, "ice_witch", 4)
	xs := map[float64]string{}
	for _, p := range m.State.Players {
		if p.Flying {
			continue
		}
		for x, other := range xs {
			if math.Abs(x-p.Position.X) < 80 {
				t.Fatalf("%s and %s share a pillar", p.PlayerID, other)
			}
		}
		xs[p.Position.X] = p.PlayerID
		if !m.Terrain.CanStandAt(p.Position.X, p.Position.Y) {
			t.Fatalf("%s does not stand at %v", p.PlayerID, p.Position)
		}
	}
}

// The witch escapes after her 30th turn.
func TestWitchTurnLimit30(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()
	m, watch := bossMatch(t, "ice_witch", 1)
	if m.boss.cfg.TurnLimit != 30 || m.State.Boss.TurnLimit != 30 {
		t.Fatalf("turn limit %d", m.boss.cfg.TurnLimit)
	}
	m.startTurn(ctx)
	for turns := 0; m.State.Status == "in_progress" && turns < 400; turns++ {
		m.endTurn(ctx)
	}
	e := matchEnded(t, watch)
	if m.boss.round != 30 || string(e["result"]) != `"boss_escaped"` {
		t.Fatalf("round %d result %s", m.boss.round, e["result"])
	}
}

// A fresh bomber needs at least 2 of its own turns (at its boss v3 range, 3 × v2's) to come within
// detonateRange of the nearest party spawn from any point it appears at.
func TestBomberApproachTakesTwoTurns(t *testing.T) {
	loadConfigs(t)
	gd := gamedata.Current()
	for _, b := range gamedata.DefaultBosses() {
		cfg := gd.Data.Maps[b.MapID]
		party, boss, spawns := gamedata.BossSpawns(cfg)
		if pillars := gamedata.ArenaPillars(cfg); len(pillars) > 0 {
			party = pillars
		}
		for _, mc := range b.Minions {
			mc = mc.Normalized()
			if mc.Behavior != gamedata.MinionBomber {
				continue // a shooter never closes in
			}
			for _, sp := range nearestFirst(spawns, boss) {
				best := math.MaxFloat64
				for _, ps := range party {
					d := math.Abs(ps.X - sp.X) // a walk covers the horizontal distance
					if mc.Movement == gamedata.MinionMoveFly {
						d = math.Hypot(ps.X-sp.X, ps.Y-mc.HoverHeight-sp.Y)
					}
					best = math.Min(best, d)
				}
				step := mc.WalkRange
				if mc.Movement == gamedata.MinionMoveFly {
					step = mc.FlyRange
				}
				if turns := math.Ceil((best - mc.DetonateRange) / step); turns < 2 {
					t.Fatalf("%s %s at %v: %.0f px from the party, %v turns", b.BossID, mc.MinionID, sp, best, turns)
				}
			}
		}
	}
}

// A summon is due only from summon.firstRound on, when the previous wave (the initial minions
// first) is dead and more than cooldownRounds rounds after the last summon; it never goes past
// maxAlive (3 for the robot).
func TestBossSummonWaits(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 4)
	due := func() bool { _, ok := m.summonDue(); return ok }
	m.boss.round = 10
	if due() {
		t.Fatal("due while the initial drones live")
	}
	killPlayer(m.State.Players["minion_drone_1"])
	if due() {
		t.Fatal("due while a drone of the wave lives")
	}
	killPlayer(m.State.Players["minion_drone_2"])
	m.boss.round = 1 // before firstRound 2
	if due() {
		t.Fatal("due before firstRound")
	}
	m.boss.round = 2
	if !due() {
		t.Fatal("no summon once the wave is dead")
	}
	if _, ok := m.bossFreeSummon(m.bossPlayer()); !ok || m.aliveMinions() != 3 || len(m.boss.wave) != 3 || due() || m.boss.lastSummon != 2 {
		t.Fatalf("after a summon: %d alive, wave %v", m.aliveMinions(), m.boss.wave)
	}
	// summoned by the boss: the free spawn nearest it first
	boss := m.bossPlayer()
	first := m.State.Players[m.boss.wave[0]]
	for _, sp := range m.boss.spawns[1:] {
		if math.Abs(sp.X-boss.Position.X) < math.Abs(first.Position.X-boss.Position.X)-1 {
			t.Fatalf("summoned at %v, %v is nearer the boss", first.Position, sp)
		}
	}
	// the wave dies at once: the cooldown (3 rounds) still holds
	for _, id := range m.boss.wave {
		killPlayer(m.State.Players[id])
	}
	for r, want := range map[int]bool{3: false, 5: false, 6: true} {
		m.boss.round = r
		if due() != want {
			t.Fatalf("round %d: due %v", r, !want)
		}
	}
}

// pumpUntil runs the match loop's events (timers) until want was sent or timeout; it returns the
// events sent with the time each was seen, from start.
func pumpUntil(t *testing.T, m *Match, c *ws.Client, want string, timeout time.Duration) []timedMsg {
	t.Helper()
	start := time.Now()
	var out []timedMsg
	deadline := time.After(timeout)
	for {
		for _, msg := range drainMsgs(c) {
			out = append(out, timedMsg{msg.Event, time.Since(start).Seconds()})
			if msg.Event == want {
				return out
			}
		}
		select {
		case ev := <-m.Events:
			m.handleEvent(ev)
		case <-deadline:
			t.Fatalf("no %s within %v: %v", want, timeout, out)
			return out
		case <-time.After(10 * time.Millisecond):
		}
	}
}

type timedMsg struct {
	event string
	at    float64
}

func at(msgs []timedMsg, event string) float64 {
	for _, m := range msgs {
		if m.event == event {
			return m.at
		}
	}
	return -1
}

// The timed turns: a drone flies ~0.6 s after TurnStarted, aims 0.4 s after landing and shoots
// 0.4 s later; the witch flies ~0.6 s after TurnStarted and shows her skill 0.4 s after landing.
func TestMoverAndHoverTiming(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()

	m, watch := bossMatch(t, "steel_robot", 1)
	drone := m.State.Players["minion_drone_1"]
	m.State.CurrentPlayerID = drone.PlayerID
	drainMsgs(watch)
	m.startTurn(ctx)
	msgs := pumpUntil(t, m, watch, "ShotResolved", 6*time.Second)
	ts, fly, aim, shot := at(msgs, "TurnStarted"), at(msgs, "UnitFlew"), at(msgs, "PlayerAiming"), at(msgs, "ShotResolved")
	if ts < 0 || fly-ts < 0.5 || fly-ts > 0.9 || aim < fly || shot-aim < 0.3 || shot-aim > 0.7 {
		t.Fatalf("drone turn timing %v", msgs)
	}

	m2, watch2 := bossMatch(t, "ice_witch", 1)
	witch := m2.State.Players["boss_ice_witch"]
	m2.State.CurrentPlayerID = witch.PlayerID
	drainMsgs(watch2)
	m2.startTurn(ctx)
	msgs = pumpUntil(t, m2, watch2, "ActionSelected", 8*time.Second)
	ts, fly, sel := at(msgs, "TurnStarted"), at(msgs, "UnitFlew"), at(msgs, "ActionSelected")
	if ts < 0 || fly-ts < 0.5 || fly-ts > 0.9 || sel-fly < 1.0 { // ≥ 200 px at 220 px/s, then 0.4 s
		t.Fatalf("witch turn timing %v", msgs)
	}
}
