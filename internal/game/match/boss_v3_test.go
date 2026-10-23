package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// killWave kills the boss's living minions (its wave is then dead).
func killWave(m *Match) {
	for _, p := range m.State.Players {
		if p.Role == RoleMinion && p.IsAlive {
			killPlayer(p)
		}
	}
}

// shotEventsOf are the events of type typ of shot s.
func shotEventsOf(s ShotResolved, typ string) []ShotEvent {
	var out []ShotEvent
	for _, e := range s.Events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}

// msgsBetween are the event names in msgs.
func names(msgs []timedMsg) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.event
	}
	return out
}

// The boss's turn with a wave due: TurnStarted, the free summon (ShotResolved actionMode summon,
// summon events only, the shield up), then its attack as usual (ActionSelected, the skill's
// ShotResolved), in the same turn.
func TestFreeSummonThenAttack(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()
	m, watch := bossMatch(t, "steel_robot", 2)
	killWave(m)
	m.syncBossShield()
	boss := m.bossPlayer()
	m.boss.round = 1 // the boss's turn starts round 2 (firstRound)
	m.State.CurrentPlayerID = boss.PlayerID
	drainMsgs(watch)
	m.startTurn(ctx)
	msgs := pumpUntil(t, m, watch, "ActionSelected", 8*time.Second)
	msgs = append(msgs, pumpUntil(t, m, watch, "ShotResolved", 8*time.Second)...)
	got := names(msgs)
	want := []string{"TurnStarted", "ShotResolved", "BossShieldChanged", "ActionSelected", "ShotResolved"}
	j := 0
	for _, n := range got {
		if j < len(want) && n == want[j] {
			j++
		}
	}
	if j != len(want) || m.State.CurrentPlayerID != boss.PlayerID {
		t.Fatalf("boss turn %v (current %s)", got, m.State.CurrentPlayerID)
	}
	if m.aliveMinions() != 3 || m.boss.lastSummon != 2 || !*m.State.BossShielded {
		t.Fatalf("alive %d last summon %d", m.aliveMinions(), m.boss.lastSummon)
	}
	if at(msgs, "ActionSelected")-at(msgs, "ShotResolved") < 1.0 { // the summon plays (≥ 1.2 s) before the attack shows
		t.Fatalf("timing %v", msgs)
	}
}

// A frozen (or stunned) boss still summons a due wave, then loses its attack: no ActionSelected,
// the next turn starts.
func TestFrozenBossSummons(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()
	m, watch := bossMatch(t, "lava_crab", 2)
	killWave(m)
	boss := m.bossPlayer()
	ApplyStatusEffect(m.gd, boss, StatusEffect{EffectID: "freeze", TargetPlayerID: boss.PlayerID, DurationTurn: 1})
	m.boss.round = 1
	m.State.CurrentPlayerID = boss.PlayerID
	drainMsgs(watch)
	m.startTurn(ctx)
	first := pumpUntil(t, m, watch, "ShotResolved", 6*time.Second)
	next := pumpUntil(t, m, watch, "TurnStarted", 8*time.Second)
	all := append(first, next...)
	if at(all, "ActionSelected") >= 0 || m.State.CurrentPlayerID == boss.PlayerID || m.aliveMinions() != 4 {
		t.Fatalf("frozen boss turn %v, current %s, %d minions", names(all), m.State.CurrentPlayerID, m.aliveMinions())
	}
}

// Wave sizes: the robot summons 3 drones, the crab 4 crabs, the witch 5 snowmen (= maxAlive),
// each standing (or hovering) where it can.
func TestBossWaveSizes(t *testing.T) {
	loadConfigs(t)
	for id, want := range map[string]int{"steel_robot": 3, "lava_crab": 4, "ice_witch": 5} {
		m, watch := bossMatch(t, id, 4)
		killWave(m)
		m.boss.round = 2
		drainMsgs(watch)
		if _, ok := m.bossFreeSummon(m.bossPlayer()); !ok {
			t.Fatalf("%s: no summon", id)
		}
		s := shotOf(t, watch)
		if n := len(shotEventsOf(s, "summon")); n != want || m.aliveMinions() != want || m.boss.cfg.Waves.MaxAlive != want {
			t.Fatalf("%s: %d summoned, %d alive", id, n, m.aliveMinions())
		}
		for _, e := range shotEventsOf(s, "summon") {
			p := m.State.Players[e.PlayerID]
			if !p.Flying && !m.Terrain.CanStandAt(p.Position.X, p.Position.Y) {
				t.Fatalf("%s: %s does not stand at %v", id, p.PlayerID, p.Position)
			}
		}
	}
}

// The witch's frost beam: 20 beams at the same t from her body centre, 18° apart, each to the
// map's bounds; a party member at her body is hit once (not 20 times) and stunned; one off every
// beam is not hit.
func TestWitchRadialBeams(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "ice_witch", 2)
	witch := m.bossPlayer()
	lift := shotOriginLift(m.Terrain)
	origin := bodyCentre(witch, lift)
	p0, p1 := m.State.Players["p0"], m.State.Players["p1"]
	p0.Position = Vector2{X: origin.X, Y: origin.Y + lift} // its body centre on hers
	p0.Shield, p0.HP = 0, p0.MaxHP
	m.boss.statusRollFix = func() float64 { return 0 } // every status roll lands
	bossCast(t, m, "frost_beam")
	s := shotOf(t, watch)
	beams := shotEventsOf(s, "beam")
	if len(beams) != 20 {
		t.Fatalf("%d beams", len(beams))
	}
	w, h := float64(m.Terrain.Width), float64(m.Terrain.Height)
	var angles []float64
	for _, b := range beams {
		if b.T != beams[0].T || *b.From != origin || b.PlayerID != witch.PlayerID || b.Duration <= 0 {
			t.Fatalf("beam %+v", b)
		}
		edge := math.Min(math.Min(math.Abs(b.To.X), math.Abs(b.To.X-w)), math.Min(math.Abs(b.To.Y), math.Abs(b.To.Y-h)))
		if edge > 0.01 {
			t.Fatalf("beam ends at %v, not the map bounds", b.To)
		}
		angles = append(angles, math.Mod(math.Atan2(b.To.Y-origin.Y, b.To.X-origin.X)*180/math.Pi+360, 360))
	}
	sort.Float64s(angles)
	for i := range angles {
		next := angles[(i+1)%20]
		if i == 19 {
			next += 360
		}
		if math.Abs(next-angles[i]-18) > 0.01 {
			t.Fatalf("angles %v", angles)
		}
	}
	damaged := map[string]int{}
	for _, e := range shotEventsOf(s, "damage") {
		damaged[e.PlayerID]++
	}
	if damaged["p0"] != 1 || !HasEffect(p0, "stun") {
		t.Fatalf("p0 damage events %d, effects %+v", damaged["p0"], p0.StatusEffects)
	}
	// p1 is hit exactly when its body is within reach of a beam, and then stunned
	reach := m.boss.cfg.Skills[1].HalfWidth + m.gd.Physics.PlayerHitRadius
	c := bodyCentre(p1, lift)
	onBeam := false
	for _, b := range beams {
		dx, dy := b.To.X-origin.X, b.To.Y-origin.Y
		l := math.Hypot(dx, dy)
		if math.Abs((c.X-origin.X)*dy/l-(c.Y-origin.Y)*dx/l) <= reach && (c.X-origin.X)*dx+(c.Y-origin.Y)*dy >= 0 {
			onBeam = true
		}
	}
	if onBeam != (damaged["p1"] == 1) || (onBeam && !HasEffect(p1, "stun")) || damaged["p1"] > 1 {
		t.Fatalf("p1 on a beam %v, damage events %d", onBeam, damaged["p1"])
	}
}

// The blizzard rains over the whole map: 40 shells, at least one in every 120 px column, and a
// party member hit is frozen.
func TestBlizzardCoversTheMap(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "ice_witch", 4)
	m.boss.statusRollFix = func() float64 { return 0 } // every status roll lands
	bossCast(t, m, "blizzard")
	s := shotOf(t, watch)
	if len(s.Projectiles) != 40 {
		t.Fatalf("%d shells", len(s.Projectiles))
	}
	cols := map[int]bool{}
	witch := m.bossPlayer()
	centre := bodyCentre(witch, shotOriginLift(m.Terrain))
	for _, pr := range s.Projectiles {
		cols[int(pr.Path[len(pr.Path)-1].Position.X)/120] = true
		// an ice shard leaves the ring around the witch (bodySize × 30 px), then falls on its column
		first := pr.Path[0]
		if pr.Kind != "ice_shard" || math.Abs(math.Hypot(first.Position.X-centre.X, first.Position.Y-centre.Y)-witch.BodySize*30) > 0.5 ||
			pr.LaunchDelay != first.Time || pr.Path[rainArcSamples].Position.Y != rainArcTopY {
			t.Fatalf("shard %s kind %s starts at %v (witch centre %v), launch %v", pr.ProjectileID, pr.Kind, first, centre, pr.LaunchDelay)
		}
		for i := 1; i < len(pr.Path); i++ {
			if pr.Path[i].Time < pr.Path[i-1].Time {
				t.Fatalf("shard path times %v", pr.Path)
			}
		}
	}
	for c := 0; c < m.Terrain.Width/120; c++ {
		if !cols[c] {
			t.Fatalf("column %d (x %d..%d) gets no shell", c, c*120, c*120+120)
		}
	}
	rain := shotEventsOf(s, "rain")
	if len(rain) != 1 || rain[0].Radius != float64(m.Terrain.Width)/2 || rain[0].Position.X != float64(m.Terrain.Width)/2 {
		t.Fatalf("rain %+v", rain)
	}
	hit := map[string]bool{}
	for _, e := range shotEventsOf(s, "damage") {
		hit[e.PlayerID] = true
	}
	for id := range hit {
		if p := m.State.Players[id]; p.IsAlive && !HasEffect(p, "freeze") {
			t.Fatalf("%s hit but not frozen", id)
		}
	}
}

// Boss statuses land by chance (052, statusChance 0.5): the status skills carry 0.5 and 1.3 × their
// v3 damage; each party member a cast hits rolls once (a rain's many shells too); a roll under
// the chance lands the status, one at or over it does not (the damage stays); unfixed, the rolls
// are seeded by the match and turn (the same turn rolls the same) and land about half the time.
func TestBossStatusChance(t *testing.T) {
	loadConfigs(t)
	v3 := map[string]float64{"fire_bombs": 0.35, "ice_spikes": 0.25, "frost_beam": 0.8, "blizzard": 0.15}
	for _, b := range gamedata.DefaultBosses() {
		for _, s := range b.Skills {
			if s.Status == "" {
				continue
			}
			if s.StatusChance != 0.5 || math.Abs(s.Damage-1.3*v3[s.SkillID]) > 1e-9 {
				t.Fatalf("%s: chance %v damage %v", s.SkillID, s.StatusChance, s.Damage)
			}
		}
	}
	if (gamedata.BossSkill{}).StatusChanceOrDefault() != 0.5 {
		t.Fatal("default chance")
	}

	// the frost beam on p0 at the witch's body: a roll of 0.49 stuns, 0.5 does not
	beamOnP0 := func(roll func() float64, turn int) (*Match, ShotResolved) {
		m, watch := bossMatch(t, "ice_witch", 2)
		lift := shotOriginLift(m.Terrain)
		origin := bodyCentre(m.bossPlayer(), lift)
		p0 := m.State.Players["p0"]
		p0.Position = Vector2{X: origin.X, Y: origin.Y + lift}
		p0.Shield, p0.HP = 0, p0.MaxHP
		m.State.TurnIndex = turn
		m.boss.statusRollFix = roll
		bossCast(t, m, "frost_beam")
		return m, shotOf(t, watch)
	}
	k := gamedata.DefenseK()
	for roll, stunned := range map[float64]bool{0.49: true, 0.5: false} {
		m, s := beamOnP0(func() float64 { return roll }, 1)
		p0 := m.State.Players["p0"]
		ef, _ := m.elementFactor(m.bossPlayer(), p0) // the witch (water) beats rookie (fire)
		want := int(math.Round(reduceByDefense(1.04*780*ef, p0.Defense, k)))
		if HasEffect(p0, "stun") != stunned || (len(shotEventsOf(s, "status")) > 0) != stunned || p0.MaxHP-p0.HP != want {
			t.Fatalf("roll %v: stunned %v, status events %d, lost %d want %d", roll, HasEffect(p0, "stun"),
				len(shotEventsOf(s, "status")), p0.MaxHP-p0.HP, want)
		}
	}

	// a blizzard over the party: one roll per member hit, however many shards hit it
	m, watch := bossMatch(t, "ice_witch", 4)
	for _, p := range m.State.Players {
		if p.Role == "" {
			p.Shield, p.HP = 0, p.MaxHP
		}
	}
	rolls := 0
	m.boss.statusRollFix = func() float64 { rolls++; return 0.9 }
	bossCast(t, m, "blizzard")
	s := shotOf(t, watch)
	hit := map[string]bool{}
	for _, e := range shotEventsOf(s, "damage") {
		if m.State.Players[e.PlayerID].Role == "" {
			hit[e.PlayerID] = true
		}
	}
	if len(hit) == 0 || rolls != len(hit) || len(shotEventsOf(s, "status")) != 0 {
		t.Fatalf("%d members hit, %d rolls, %d status events", len(hit), rolls, len(shotEventsOf(s, "status")))
	}

	// seeded: the same match and turn roll the same; over many turns about half land
	landed := 0
	for turn := 0; turn < 60; turn++ {
		a, _ := beamOnP0(nil, turn)
		b, _ := beamOnP0(nil, turn)
		sa, sb := HasEffect(a.State.Players["p0"], "stun"), HasEffect(b.State.Players["p0"], "stun")
		if sa != sb {
			t.Fatalf("turn %d rolled differently", turn)
		}
		if sa {
			landed++
		}
	}
	if landed < 18 || landed > 42 {
		t.Fatalf("%d / 60 stuns landed", landed)
	}
}

// The robot's laser sweep: 5 horizontal beams across the whole map width, one at each party
// member's body height; each party member is hit once by it.
func TestRobotLaserSweep(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "steel_robot", 4)
	m.State.Players["p3"].Position = Vector2{X: 1200, Y: m.Terrain.SpawnY(1200, 600)}
	for _, p := range m.State.Players {
		if p.Role == "" {
			p.Shield, p.HP = 0, p.MaxHP
		}
	}
	bossCast(t, m, "laser_beam")
	s := shotOf(t, watch)
	beams := shotEventsOf(s, "beam")
	if len(beams) != 5 {
		t.Fatalf("%d beams", len(beams))
	}
	w := float64(m.Terrain.Width)
	ys := map[float64]bool{}
	for _, b := range beams {
		if b.From.Y != b.To.Y || math.Abs(b.From.X-b.To.X) != w || b.T != beams[0].T {
			t.Fatalf("beam %v → %v", b.From, b.To)
		}
		ys[b.From.Y] = true
	}
	lift := shotOriginLift(m.Terrain)
	damaged := map[string]int{}
	for _, e := range shotEventsOf(s, "damage") {
		damaged[e.PlayerID]++
	}
	for i := 0; i < 4; i++ {
		p := m.State.Players[fmt.Sprintf("p%d", i)]
		if !ys[bodyCentre(p, lift).Y] || damaged[p.PlayerID] != 1 {
			t.Fatalf("%s at %v: beams %v, damage events %d", p.PlayerID, p.Position, ys, damaged[p.PlayerID])
		}
	}
}

// The crab's claw slam: an explode at the crab, then explodes stepping 150 px along the ground
// both ways out to both map edges, each later than the one before it; every party member on the
// ground takes at least 40 % of the slam's damage.
func TestCrabShockwave(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "lava_crab", 4)
	crab := m.bossPlayer()
	for _, p := range m.State.Players {
		if p.Role == "" {
			p.Shield, p.HP = 0, p.MaxHP
		}
	}
	bossCast(t, m, "claw_slam")
	s := shotOf(t, watch)
	ex := shotEventsOf(s, "explode")
	w := float64(m.Terrain.Width)
	minX, maxX := w, 0.0
	byX := map[float64]float64{}
	for _, e := range ex {
		minX, maxX = math.Min(minX, e.Position.X), math.Max(maxX, e.Position.X)
		byX[e.Position.X] = e.T
	}
	if ex[0].Position.X != crab.Position.X || ex[0].BlastRadius != 120 || minX > 2 || maxX < w-2 || len(ex) < 16 {
		t.Fatalf("explodes from %v to %v (%d), first %+v", minX, maxX, len(ex), ex[0])
	}
	for x, tt := range byX {
		d := math.Abs(x - crab.Position.X)
		if d > 0 && d < 1199 && math.Abs(tt-(bossChargeTime+0.08*math.Round(d/150))) > 1e-9 && x != 2 && x != w-2 {
			t.Fatalf("explode at %v: t %v", x, tt)
		}
	}
	k := m.gd.Physics.DefenseKOrDefault()
	full := 0.6 * 720.0
	for _, e := range shotEventsOf(s, "damage") {
		p := m.State.Players[e.PlayerID]
		if least := int(math.Round(reduceByDefense(0.4*full, p.Defense, k))); e.Amount < least {
			t.Fatalf("%s took %d < %d", p.PlayerID, e.Amount, least)
		}
	}
	if len(shotEventsOf(s, "damage")) < 4 {
		t.Fatalf("damage %v", shotEventsOf(s, "damage"))
	}
}

// replayShot plays shot s the way the clients do: the events in order, each explode's carve
// counted (the terrain version + 1), and at the first explode whose terrainVersion is not the
// count the client takes its terrain for drifted, asks for a MatchStateSync and drops the rest
// of the replay. It returns the HP each player was shown (from the damage events played, the
// start HP otherwise) and whether the whole shot played.
func replayShot(s ShotResolved, version uint64, hp map[string]int) (map[string]int, bool) {
	shown := map[string]int{}
	for id, v := range hp {
		shown[id] = v
	}
	for _, e := range s.Events {
		switch {
		case e.Type == "explode" && e.TerrainVersion > 0:
			version++
			if e.TerrainVersion != version {
				return shown, false
			}
		case e.Type == "damage" && e.HP != nil:
			shown[e.PlayerID] = *e.HP
		}
	}
	return shown, true
}

// The crab's claw slam hurts the whole party on the mound: every grounded member loses
// damage × the boss damage × max(minFactor, 1 − distance / the farther edge's) through its
// defense (a damage event each, cause explosion), and the clients play all of it: the steps'
// terrain versions rise in play order (both sides interleaved), so no client drops the replay
// (and its damage) for a drifted terrain.
func TestCrabShockwaveDamagesParty(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "lava_crab", 4)
	crab := m.bossPlayer()
	start := map[string]int{}
	for _, p := range m.State.Players {
		if p.Role == "" {
			p.Shield, p.HP = 0, p.MaxHP
			if !m.Terrain.CanStandAt(p.Position.X, p.Position.Y) {
				t.Fatalf("%s is not grounded at %v", p.PlayerID, p.Position)
			}
		}
		start[p.PlayerID] = p.HP
	}
	skill, _ := m.boss.cfg.Skill("claw_slam")
	version := m.Terrain.Version
	x0, w := crab.Position.X, float64(m.Terrain.Width)
	bossCast(t, m, "claw_slam")
	s := shotOf(t, watch)
	k := m.gd.Physics.DefenseKOrDefault()
	hits := map[string]int{}
	for _, e := range shotEventsOf(s, "damage") {
		if e.Cause == "explosion" {
			hits[e.PlayerID] += e.Amount
		}
	}
	fell := map[string]int{}
	for _, e := range shotEventsOf(s, "damage") {
		if e.Cause == "fall" {
			fell[e.PlayerID] += e.Amount
		}
	}
	shown, whole := replayShot(s, version, start)
	if !whole {
		t.Errorf("the clients drop the replay at a terrain version out of play order")
	}
	for i := 0; i < 4; i++ {
		p := m.State.Players[fmt.Sprintf("p%d", i)]
		f := math.Max(skill.MinFactor, 1-math.Abs(p.Position.X-x0)/math.Max(x0, w-x0))
		ef, _ := m.elementFactor(crab, p) // the crab (fire) beats spark (metal)
		want := int(math.Round(reduceByDefense(skill.Damage*float64(m.boss.cfg.Damage)*f*ef, p.Defense, k)))
		if want <= 0 || hits[p.PlayerID] != want || p.MaxHP-p.HP != want+fell[p.PlayerID] {
			t.Errorf("%s at %v: hit %d, lost %d, want %d (+ fall %d)", p.PlayerID, p.Position, hits[p.PlayerID], p.MaxHP-p.HP, want, fell[p.PlayerID])
		}
		if shown[p.PlayerID] != p.HP {
			t.Errorf("%s: the clients show %d HP, the server has %d", p.PlayerID, shown[p.PlayerID], p.HP)
		}
	}
}

// The witch's sky ice islands: 12 islets of hard ice under snow, nothing below them (a fall off
// an islet is a death out of the map), the party on distinct random islets, the hover band
// above the islets and below the HUD (minY ≥ 320).
func TestWitchIslandsArena(t *testing.T) {
	loadConfigs(t)
	gd := gamedata.Current()
	cfg := gd.Data.Maps["arena_ice_witch_islands"]
	islets := gamedata.ArenaPillars(cfg)
	if len(islets) != 12 {
		t.Fatalf("%d islets", len(islets))
	}
	tr := NewTerrainFor(gd, cfg)
	// nothing connects them to the bottom: the lowest 140 px are open sky in every column
	for x := 0; x < tr.Width; x++ {
		for y := tr.Height - 140; y < tr.Height; y++ {
			if tr.IsSolid(float64(x), float64(y)) {
				t.Fatalf("solid at %d,%d", x, y)
			}
		}
	}
	tops := map[float64]bool{}
	for _, sp := range islets {
		y := tr.SpawnY(sp.X, sp.Y)
		if !tr.CanStandAt(sp.X, y) || tr.IsDestructible(int(sp.X), int(y)+40) || !tr.IsDestructible(int(sp.X), int(y)+2) {
			t.Fatalf("islet %v: stands %v, hard core / snow cap", sp, tr.CanStandAt(sp.X, y))
		}
		tops[y] = true
	}
	if len(tops) < 8 {
		t.Fatalf("only %d islet heights", len(tops))
	}
	for seed := int64(0); seed < 30; seed++ {
		sps := gamedata.BossPartySpawns(cfg, 4, rand.New(rand.NewSource(seed)).Intn)
		seen := map[gamedata.SpawnPoint]bool{}
		for _, sp := range sps {
			if seen[sp] {
				t.Fatalf("seed %d: %v", seed, sps)
			}
			seen[sp] = true
		}
	}
	m, _ := bossMatch(t, "ice_witch", 4)
	if m.State.MapID != "arena_ice_witch_islands" {
		t.Fatalf("witch arena %s", m.State.MapID)
	}
	var units []*BattlePlayerState
	for _, p := range m.playersByID() {
		if !p.Flying {
			units = append(units, p)
		}
	}
	for i, p := range units {
		for _, q := range units[i+1:] {
			if math.Hypot(p.Position.X-q.Position.X, p.Position.Y-q.Position.Y) < 80 {
				t.Fatalf("%s and %s share an islet", p.PlayerID, q.PlayerID)
			}
		}
	}
	h := m.boss.cfg.Hover
	highest := float64(tr.Height)
	for y := range tops {
		highest = math.Min(highest, y)
	}
	if h == nil || h.MinY < 320 || h.MaxY+60 > highest {
		t.Fatalf("hover band %+v, highest islet top %v", h, highest)
	}
	// a fall off an islet is a death
	p0 := m.State.Players["p0"]
	p0.Position = Vector2{X: islets[0].X - 120, Y: tr.SpawnY(islets[0].X, islets[0].Y) - 10}
	if m.Terrain.GetLandingY(p0.Position.X, p0.Position.Y) < float64(m.Terrain.Height) {
		t.Fatalf("ground under %v", p0.Position)
	}
	tl := m.newTimeline()
	m.settleFalls(m.bossPlayer(), 0, tl)
	if p0.IsAlive {
		t.Fatal("p0 survived a fall off the islet")
	}
}

// A snowman leaps (UnitJumped, an arc to firm ground on the nearest party member's islet within
// detonateRange of them), leapt shows on its state and it does not shoot or blow up that turn;
// on its next turn it detonates when the player is still near, not when they moved away.
func TestSnowmanLeapsThenDetonates(t *testing.T) {
	loadConfigs(t)
	ctx := context.Background()
	setup := func(t *testing.T) (*Match, *ws.Client, *BattlePlayerState, *BattlePlayerState) {
		m, watch := bossMatch(t, "ice_witch", 1)
		p0 := m.State.Players["p0"]
		p0.Shield, p0.HP, p0.MaxHP = 0, 100000, 100000
		s := m.State.Players["minion_snowman_1"]
		if math.Hypot(s.Position.X-p0.Position.X, s.Position.Y-p0.Position.Y) < 200 {
			t.Fatalf("snowman starts next to p0: %v %v", s.Position, p0.Position)
		}
		asTurnOf(m, s)
		drainMsgs(watch)
		m.playMoverTurn(ctx, s)
		msgs := drainMsgs(watch)
		var j UnitJumped
		if err := json.Unmarshal(lastEvent(t, msgs, "UnitJumped"), &j); err != nil {
			t.Fatal(err)
		}
		d := math.Hypot(s.Position.X-p0.Position.X, s.Position.Y-p0.Position.Y)
		if j.PlayerID != s.PlayerID || j.To != s.Position || j.Duration < 0.6 || j.Height < 80 || d > 70 || d < 20 ||
			math.Abs(s.Position.Y-p0.Position.Y) > 40 || !m.Terrain.CanStandAt(s.Position.X, s.Position.Y) || !m.firmGround(s.Position.X, s.Position.Y) ||
			!s.Leapt || hasEvent(msgs, "ShotResolved") || hasEvent(msgs, "PlayerAiming") || !s.IsAlive {
			t.Fatalf("leap %+v → %v (p0 %v, %.0f px), leapt %v, events %v", j, s.Position, p0.Position, d, s.Leapt, msgs)
		}
		var snap struct {
			Players map[string]struct {
				Leapt bool `json:"leapt"`
			} `json:"players"`
		}
		_ = json.Unmarshal(m.snapshotPayload(), &snap)
		if !snap.Players[s.PlayerID].Leapt {
			t.Fatal("leapt not in the snapshot")
		}
		m.leaperTurnEnded(s) // its own turn ends: leapt stays
		if !s.Leapt {
			t.Fatal("leapt cleared on the turn of the leap")
		}
		return m, watch, s, p0
	}

	t.Run("detonates", func(t *testing.T) {
		m, watch, s, p0 := setup(t)
		asTurnOf(m, s)
		m.playMoverTurn(ctx, s)
		shot := shotOf(t, watch)
		if shot.ActionMode != ActionSelfDestruct || shot.ShooterID != s.PlayerID || s.IsAlive || p0.HP >= p0.MaxHP {
			t.Fatalf("no detonation: %+v alive %v", shot, s.IsAlive)
		}
	})

	t.Run("moved away", func(t *testing.T) {
		m, watch, s, p0 := setup(t)
		// p0 walks off to another islet
		far := gamedata.ArenaPillars(m.gd.Data.Maps[m.State.MapID])
		for _, sp := range far {
			if math.Abs(sp.X-p0.Position.X) > 400 {
				p0.Position = Vector2{X: sp.X, Y: m.Terrain.SpawnY(sp.X, sp.Y)}
				break
			}
		}
		asTurnOf(m, s)
		drainMsgs(watch)
		m.playMoverTurn(ctx, s)
		msgs := drainMsgs(watch)
		for _, msg := range msgs {
			if msg.Event == "ShotResolved" {
				var sr ShotResolved
				_ = json.Unmarshal(msg.Data, &sr)
				if sr.ActionMode == ActionSelfDestruct {
					t.Fatal("detonated with nobody near")
				}
			}
		}
		if !s.IsAlive || !hasEvent(msgs, "UnitJumped") || !s.Leapt {
			t.Fatalf("after p0 moved: alive %v, events %v", s.IsAlive, msgs)
		}
	})

	t.Run("leapt clears", func(t *testing.T) {
		m, _, s, _ := setup(t)
		m.turnSeq++ // its next turn, frozen: nothing happens, the turn ends
		m.leaperTurnEnded(s)
		if s.Leapt {
			t.Fatal("leapt kept after its next turn")
		}
	})
}

// Every boss rain has its own projectile kind (steel_shell, magma_bomb, ice_spike, ice_shard)
// and a minion's weapon shot carries its config's (a snowman's snowball).
func TestBossProjectileKinds(t *testing.T) {
	loadConfigs(t)
	for _, c := range []struct{ boss, skill, kind string }{
		{"steel_robot", "shell_barrage", "steel_shell"}, {"lava_crab", "fire_bombs", "magma_bomb"},
		{"ice_witch", "ice_spikes", "ice_spike"}, {"ice_witch", "blizzard", "ice_shard"},
	} {
		m, watch := bossMatch(t, c.boss, 2)
		bossCast(t, m, c.skill)
		s := shotOf(t, watch)
		for _, pr := range s.Projectiles {
			if pr.Kind != c.kind {
				t.Fatalf("%s %s: kind %q", c.boss, c.skill, pr.Kind)
			}
		}
	}
	m, watch := bossMatch(t, "ice_witch", 1)
	s := m.State.Players["minion_snowman_1"]
	p0 := m.State.Players["p0"]
	// p0 within detonateRange but no leap before: the snowman shoots
	p0.Position = Vector2{X: s.Position.X + 40, Y: m.Terrain.SpawnY(s.Position.X+40, s.Position.Y-20)}
	asTurnOf(m, s)
	drainMsgs(watch)
	m.playMoverTurn(context.Background(), s)
	shot := shotOf(t, watch)
	if shot.ActionMode != "weapon" || len(shot.Projectiles) == 0 || shot.Projectiles[0].Kind != "snowball" || s.Leapt {
		t.Fatalf("snowman shot %+v", shot)
	}
}

// A party bot next to a leapt snowman walks out of its detonateRange (never off its islet) before
// it shoots.
func TestBotDodgesLeapedSnowman(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "ice_witch", 1)
	p0 := m.State.Players["p0"]
	s := m.State.Players["minion_snowman_1"]
	asTurnOf(m, s)
	m.leapUnit(s, Vector2{X: p0.Position.X + 40, Y: p0.Position.Y})
	asTurnOf(m, p0)
	act, ok := NewBotBrain(perfectAim).DecideAction(m, p0, botTurnState{}).(MoveAction)
	if !ok {
		t.Fatal("the bot does not move away")
	}
	pos, safe := predictMove(m.Terrain, p0.Position, act.TargetX)
	if !safe || math.Hypot(pos.X-s.Position.X, pos.Y-s.Position.Y) <= 70 {
		t.Fatalf("walks to %v (safe %v), snowman at %v", pos, safe, s.Position)
	}
}
