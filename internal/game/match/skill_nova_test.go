package match

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// ── Tide's water orb: one bounce ─────────────────────────────────────────────────────────────

// waterOrb is Tide's configured weapon (bounces 1).
func waterOrb(t *testing.T, m *Match) gamedata.WeaponConfig {
	t.Helper()
	w, ok := m.gd.Data.Weapons[m.gd.Data.Characters["tide"].WeaponID]
	if !ok || w.Bounces != 1 || w.WeaponID != "water_orb" {
		t.Fatalf("tide's weapon %+v", w)
	}
	return w
}

// On flat ground the orb bounces once off the surface (its normal (0, −1): it leaves with
// −restitution × its vertical and friction × its horizontal speed), flies on and explodes at its
// next contact; the bounce is in bounces[] and twice in the path (arrival, departure).
func TestWaterOrbBouncesOnceThenExplodes(t *testing.T) {
	m, _ := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"f", 2, "rookie", 1500})
	orb := waterOrb(t, m)
	tide := m.State.Players["w"]
	r := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, orb, WindState{}, m.Terrain, m.State.Players, 0)
	if len(r.Bounces) != 1 || r.ExplosionPoint == nil || r.HitPlayerID != "" {
		t.Fatalf("bounces %+v, explosion %v, hit %q", r.Bounces, r.ExplosionPoint, r.HitPlayerID)
	}
	b := r.Bounces[0]
	if math.Abs(b.Position.Y-identitySurface) > 3 || m.Terrain.IsSolid(b.Position.X, b.Position.Y) {
		t.Fatalf("bounce at %v, the surface is at y %d", b.Position, identitySurface)
	}
	if end := flightEnd(r); end <= b.T || r.ExplosionPoint.X <= b.Position.X+5 {
		t.Fatalf("explodes at %v (t %.2f) before / at the bounce %+v", *r.ExplosionPoint, end, b)
	}
	// the path: the bounce twice at its time and point, with the arrival then the departure velocity
	idx := -1
	for i := 0; i+1 < len(r.Path); i++ {
		if r.Path[i].Time == b.T && r.Path[i+1].Time == b.T {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("the path has no double sample at the bounce %+v", b)
	}
	in, out := r.Path[idx].Velocity, r.Path[idx+1].Velocity
	sr := m.gd.Rules.Physics
	if in.Y <= 0 || math.Abs(out.Y+sr.BounceRestitution*in.Y) > 1e-6 || math.Abs(out.X-sr.BounceFriction*in.X) > 1e-6 {
		t.Fatalf("bounce velocity %v → %v (restitution %v, friction %v)", in, out, sr.BounceRestitution, sr.BounceFriction)
	}
	t.Logf("bounce at %.0f, %.0f (t %.2f): %v → %v; explodes at %.0f, %.0f (t %.2f)", b.Position.X, b.Position.Y, b.T, in, out,
		r.ExplosionPoint.X, r.ExplosionPoint.Y, flightEnd(r))
	for i := 1; i < len(r.Path); i++ {
		if r.Path[i].Time < r.Path[i-1].Time {
			t.Fatalf("path times go back at %d", i)
		}
	}

	// the same shot of a weapon without bounces explodes at the bounce point
	plain := orb
	plain.Bounces = 0
	p := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, plain, WindState{}, m.Terrain, m.State.Players, 0)
	if len(p.Bounces) != 0 || p.ExplosionPoint == nil || math.Abs(p.ExplosionPoint.X-b.Position.X) > 3 {
		t.Fatalf("no-bounce shot explodes at %v, the orb bounced at %v", p.ExplosionPoint, b.Position)
	}
}

// A unit hit before the first terrain contact explodes the orb at once: no bounce.
func TestWaterOrbDirectHitExplodes(t *testing.T) {
	m, _ := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"f", 2, "rookie", 1500})
	orb := waterOrb(t, m)
	tide, f := m.State.Players["w"], m.State.Players["f"]
	plain := orb
	plain.Bounces = 0
	ground := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, plain, WindState{}, m.Terrain, nil, 0)
	f.Position.X = ground.ExplosionPoint.X // standing where it would bounce
	r := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, orb, WindState{}, m.Terrain, m.State.Players, 0)
	if r.HitPlayerID != "f" || len(r.Bounces) != 0 {
		t.Fatalf("hit %q, bounces %+v", r.HitPlayerID, r.Bounces)
	}
}

// After its bounce the orb still explodes on a unit it meets (a direct hit).
func TestWaterOrbHitsAfterTheBounce(t *testing.T) {
	m, _ := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"f", 2, "rookie", 1500})
	orb := waterOrb(t, m)
	tide, f := m.State.Players["w"], m.State.Players["f"]
	free := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, orb, WindState{}, m.Terrain, nil, 0)
	b := free.Bounces[0]
	f.Position.X = free.ExplosionPoint.X // where the hop after the bounce comes down
	if f.Position.X-b.Position.X < 60 {
		t.Fatalf("hop %v → %v", b.Position, *free.ExplosionPoint)
	}
	r := SimulateProjectile("w", 1, shotOrigin(m.Terrain, tide), 60, 45, orb, WindState{}, m.Terrain, m.State.Players, 0)
	if len(r.Bounces) != 1 || r.HitPlayerID != "f" {
		t.Fatalf("bounces %+v, hit %q", r.Bounces, r.HitPlayerID)
	}
}

// ShotResolved reports the bounce (projectiles[].bounces) for the orb only; a teleport fired
// with Tide's gun never bounces.
func TestWaterOrbBounceInShotResolved(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"f", 2, "rookie", 1500})
	send(m, clients["w"], "Shoot", ShootAction{Angle: 60, Power: 45, ActionMode: "weapon"})
	var raw []byte
	for {
		msg := <-clients["w"].Send
		if msg.Event == "ShotResolved" {
			raw = msg.Data
			break
		}
	}
	var s ShotResolved
	_ = json.Unmarshal(raw, &s)
	if len(s.Projectiles) != 1 || len(s.Projectiles[0].Bounces) != 1 || !strings.Contains(string(raw), `"bounces":[{"t":`) {
		t.Fatalf("ShotResolved %s", raw)
	}
	assertTimeline(t, s)
	if ex := eventsOf(s, "explode", ""); len(ex) != 1 || ex[0].T <= s.Projectiles[0].Bounces[0].T {
		t.Fatalf("explode %+v after the bounce %+v", ex, s.Projectiles[0].Bounces)
	}

	// a rookie shell: no bounces field
	m2, c2 := flatSkillMatch(t, skillPlayer{"f", 1, "rookie", 200}, skillPlayer{"w", 2, "tide", 1500})
	send(m2, c2["f"], "Shoot", ShootAction{Angle: 60, Power: 45, ActionMode: "weapon"})
	for {
		msg := <-c2["f"].Send
		if msg.Event == "ShotResolved" {
			if strings.Contains(string(msg.Data), `"bounces"`) {
				t.Fatalf("rookie shot %s", msg.Data)
			}
			break
		}
	}
	if teleportWeapon(waterOrb(t, m)).Bounces != 0 {
		t.Fatal("teleport bounces")
	}
}

// The surface normal on flat ground points straight up; off a wall it points sideways.
func TestSurfaceNormal(t *testing.T) {
	m, _ := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 200}, skillPlayer{"f", 2, "rookie", 1500})
	n := surfaceNormal(m.Terrain, Vector2{X: 700, Y: identitySurface}, Vector2{X: 100, Y: 300})
	if math.Abs(n.X) > 1e-9 || math.Abs(n.Y+1) > 1e-9 {
		t.Fatalf("flat normal %v", n)
	}
	for x := 900; x < 960; x++ { // a wall from x 900
		for y := 300; y < identitySurface; y++ {
			setBit(m.Terrain.Mask, x, y, m.Terrain.Width)
		}
	}
	n = surfaceNormal(m.Terrain, Vector2{X: 900, Y: 400}, Vector2{X: 300, Y: 0})
	if n.X > -0.95 {
		t.Fatalf("wall normal %v", n)
	}
	if v := bounceVelocity(Vector2{X: 300, Y: 0}, n, 0.5, 1); v.X >= 0 {
		t.Fatalf("off the wall %v", v)
	}
}

// ── Tide's snow nova ─────────────────────────────────────────────────────────────────────────

var defNova = gamedata.DefaultNovaTuning()

var placeholderRe = regexp.MustCompile(`\{(\w+)\}`)

// The nova's damage factor: 1.3 (+ 0.1 per rank) at the centre, linear down to 0.4 at the
// radius (500 px), nothing beyond.
func TestNovaFalloff(t *testing.T) {
	for _, c := range []struct{ rank, d, want float64 }{
		{0, 0, 1.3}, {0, 500, 0.4}, {0, 250, 0.85}, {0, 100, 1.3 - 0.9*0.2}, {4, 0, 1.7}, {4, 500, 0.4}, {4, 250, 1.05},
		{0, 500.01, 0}, {0, 800, 0},
	} {
		if got := novaFactor(defNova, int(c.rank), c.d); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("rank %v at %v px: %v, want %v", c.rank, c.d, got, c.want)
		}
	}
	if defNova.Radius != 500 || defNova.WaveSpeed != 1400 || defNova.FreezeTurns != 1 || defNova.EdgeFactor != 0.4 {
		t.Fatalf("defaults %+v", defNova)
	}
}

// The nova at Tide's body centre: every enemy within the radius takes the falloff × base ×
// element (reduced by defense) and is frozen when the ring reaches it (castTime + d /
// waveSpeed); the ally, Tide herself and the enemy beyond the radius are untouched; no crater.
func TestNovaDamagesAndFreezesEnemiesInside(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 400}, skillPlayer{"a", 1, "flora", 470},
		skillPlayer{"e1", 2, "rookie", 460}, skillPlayer{"e2", 2, "spark", 800}, skillPlayer{"e3", 2, "tanko", 1000})
	for _, p := range m.State.Players {
		p.CritChance = 0
	}
	tide := m.State.Players["w"]
	version := m.Terrain.Version
	hp := map[string]int{}
	for id, p := range m.State.Players {
		hp[id] = p.HP
	}
	s := fireSkill(t, m, clients, "w", 90, 50)
	if s.SkillID != "snow_nova" || len(s.Projectiles) != 0 {
		t.Fatalf("shot %+v", s)
	}
	lift := shotOriginLift(m.Terrain)
	centre := bodyCentre(tide, lift)
	novas := eventsOf(s, "nova", "w")
	if len(novas) != 1 || *novas[0].Position != centre || novas[0].Radius != 500 || novas[0].T != defNova.CastTime ||
		math.Abs(novas[0].Duration-500.0/1400) > 1e-9 {
		t.Fatalf("nova events %+v", novas)
	}
	skill := m.gd.Data.Skills["snow_nova"]
	weapon := m.gd.Data.Weapons["water_orb"]
	base := skillBaseDamage(tide, skill, weapon, m.gd.PowerShotFactor())
	k := m.gd.Physics.DefenseKOrDefault()
	times := map[string]float64{}
	for _, id := range []string{"e1", "e2"} {
		p := m.State.Players[id]
		d := blastDistance(p, centre, m.gd.Physics.PlayerHitRadius, lift)
		ef, effect := m.elementFactor(tide, p)
		want := int(math.Round(reduceByDefense(novaFactor(defNova, 0, d)*base*ef, p.Defense, k)))
		dmg := eventsOf(s, "damage", id)
		wantT := defNova.CastTime + d/defNova.WaveSpeed
		if len(dmg) != 1 || dmg[0].Amount != want || dmg[0].Cause != CauseNova || math.Abs(dmg[0].T-wantT) > 1e-9 ||
			dmg[0].ElementEffect != effect || hp[id]-p.HP != want {
			t.Fatalf("%s at %.0f px: %+v, want %d at %.3f s (%q)", id, d, dmg, want, wantT, effect)
		}
		st := eventsOf(s, "status", id)
		if len(st) != 1 || st[0].EffectID != "freeze" || st[0].Turns != 1 || st[0].T != dmg[0].T || !HasEffect(p, "freeze") {
			t.Fatalf("%s freeze %+v", id, st)
		}
		times[id] = dmg[0].T
	}
	if times["e1"] >= times["e2"] {
		t.Fatalf("the nearer enemy is hit later: %v", times)
	}
	if ef, _ := m.elementFactor(tide, m.State.Players["e1"]); ef != 1.1 {
		t.Fatalf("water on fire: %v", ef)
	}
	for _, id := range []string{"w", "a", "e3"} {
		if len(eventsOf(s, "damage", id)) != 0 || len(eventsOf(s, "status", id)) != 0 || m.State.Players[id].HP != hp[id] {
			t.Fatalf("%s touched by the nova", id)
		}
	}
	if m.Terrain.Version != version || len(eventsOf(s, "explode", "")) != 0 || len(eventsOf(s, "fall", "")) != 0 {
		t.Fatal("the nova carved or moved something")
	}
}

// The centre factor grows 0.1 per gun rank.
func TestNovaRankScaling(t *testing.T) {
	hit := func(rank int) int {
		m, clients := flatSkillMatch(t, skillPlayer{"w", 1, "tide", 400}, skillPlayer{"e", 2, "spark", 500})
		m.State.Players["w"].SkillRank = rank
		m.State.Players["w"].CritChance = 0
		return totalDamage(fireSkill(t, m, clients, "w", 90, 50), "e")
	}
	r0, r4 := hit(0), hit(4)
	// spark's body edge is 76 px from Tide's centre: 1.3 − 0.9 × 76/500 vs 1.7 − 1.3 × 76/500
	f0, f4 := novaFactor(defNova, 0, 76), novaFactor(defNova, 4, 76)
	if r0 <= 0 || math.Abs(float64(r4)/float64(r0)-f4/f0) > 0.01 {
		t.Fatalf("rank 0 %d, rank 4 %d: ratio %.3f, want %.3f", r0, r4, float64(r4)/float64(r0), f4/f0)
	}
}

// Boss mode: the nova reaches the flying witch and her snowmen (the boss through her minion
// shield), and freezes them.
func TestNovaHitsFlyingBossAndMinions(t *testing.T) {
	loadConfigs(t)
	m, watch := bossMatch(t, "ice_witch", 1)
	witch := m.bossPlayer()
	if !witch.Flying {
		t.Fatal("the witch flies")
	}
	tide := m.State.Players["p0"]
	tide.CharacterID, tide.Element, tide.CritChance = "tide", "water", 0
	lift := shotOriginLift(m.Terrain)
	wc := bodyCentre(witch, lift)
	tide.Position = Vector2{X: wc.X - 250, Y: wc.Y + lift} // her body centre level with the witch's, 250 px off
	var snow *BattlePlayerState
	for _, p := range m.playersByID() {
		if p.Role == RoleMinion && p.IsAlive {
			if snow == nil {
				snow = p
				p.Position = Vector2{X: tide.Position.X + 120, Y: tide.Position.Y}
				p.HP, p.MaxHP = 1e5, 1e5
			} else {
				p.Position = Vector2{X: tide.Position.X - 2000, Y: tide.Position.Y} // far away
			}
		}
	}
	witch.HP = witch.MaxHP
	tide.SkillEnergy = 100
	m.State.CurrentPlayerID = tide.PlayerID
	drainMsgs(watch)
	m.shootSkill(context.Background(), tide, ShootAction{Angle: 90, Power: 50, ActionMode: "skill"}, "snow_nova", m.gd.Data.Weapons["water_orb"])
	s := shotOf(t, watch)
	for _, p := range []*BattlePlayerState{witch, snow} {
		var dmg, st []ShotEvent
		for _, e := range s.Events {
			if e.PlayerID == p.PlayerID && e.Type == "damage" {
				dmg = append(dmg, e)
			}
			if e.PlayerID == p.PlayerID && e.Type == "status" && e.EffectID == "freeze" {
				st = append(st, e)
			}
		}
		if len(dmg) != 1 || dmg[0].Amount <= 0 || len(st) != 1 || !HasEffect(p, "freeze") {
			t.Fatalf("%s: damage %+v, freeze %+v", p.PlayerID, dmg, st)
		}
	}
	if tide.BossDamage <= 0 || tide.MinionDamage <= 0 {
		t.Fatalf("credited boss %d, minions %d", tide.BossDamage, tide.MinionDamage)
	}
}

// The bot casts the nova when two enemies are inside, or one near enough that the nova beats
// its shot; never when none is.
func TestBotCastsNova(t *testing.T) {
	cases := []struct {
		name  string
		foes  []skillPlayer
		skill bool
	}{
		{"two inside", []skillPlayer{{"e1", 2, "spark", 650}, {"e2", 2, "rookie", 750}}, true},
		{"one close", []skillPlayer{{"e1", 2, "spark", 470}}, true},
		{"none inside", []skillPlayer{{"e1", 2, "spark", 1300}}, false},
	}
	for _, c := range cases {
		m, bot := botSkillMatch(t, append([]skillPlayer{{"b", 1, "tide", 400}}, c.foes...)...)
		act, ok := NewBotBrain(perfectAim).DecideAction(m, bot, botTurnState{}).(ShootAction)
		if !ok || (act.ActionMode == "skill") != c.skill {
			t.Fatalf("%s: action %+v", c.name, act)
		}
		if c.skill && (act.Angle != m.gd.Rules.Aim.NoAimAngle || act.Power != m.gd.Rules.Aim.NoAimPower) {
			t.Fatalf("%s: nova sent with %+v", c.name, act)
		}
	}
}

// GET /gamedata's skill rules of the nova: no aim, the description's numbers per rank.
func TestNovaSkillRules(t *testing.T) {
	useConfigGameData(t)
	gd := gamedata.Current()
	sk, ok := gd.Data.Skills["snow_nova"]
	if !ok || sk.EffectType != gamedata.EffectNova || sk.CharacterID != "tide" || sk.CooldownTurn != 4 || sk.StatusEffectID != "freeze" {
		t.Fatalf("skill %+v", sk)
	}
	r := SkillRulesOf(gd, sk)
	if r.AimKind != AimNone || r.Params["radius"] != 500 || r.Ranks[0]["centrePct"] != 130 || r.Ranks[4]["centrePct"] != 170 ||
		r.Ranks[0]["edgePct"] != 40 || r.Ranks[0]["freezeTurns"] != 1 {
		t.Fatalf("rules %+v", r)
	}
	for _, m := range placeholderRe.FindAllStringSubmatch(sk.DescriptionTemplate, -1) {
		if _, ok := r.Ranks[0][m[1]]; !ok {
			t.Fatalf("template placeholder {%s} has no value", m[1])
		}
	}
}

// Tide (water, controller) and Tanko's earth rework (Địa chấn liên hoàn: the same bomb chain).
func TestTideAndTankoConfig(t *testing.T) {
	useConfigGameData(t)
	gd := gamedata.Current()
	c := gd.Data.Characters["tide"]
	if c.Element != "water" || c.Role != "controller" || c.RoleName != "Khống chế" || c.GunKind != "water_gun" || c.SortOrder != 5 ||
		c.HP != 1400 || c.Damage != 500 || c.Mobility != 110 || c.Defense != 45 || c.SkillPower != 70 || c.TerrainDamage != 16 ||
		c.Difficulty != 2 || c.WeaponID != "water_orb" || c.SkillID != "snow_nova" {
		t.Fatalf("tide %+v", c)
	}
	rookie := gd.Data.Weapons["basic_rocket"]
	if w := gd.Data.Weapons["water_orb"]; w.Bounces != 1 || w.ExplosionRadius >= rookie.ExplosionRadius || w.TerrainDamage >= rookie.TerrainDamage {
		t.Fatalf("water orb %+v", w)
	}
	for id, w := range gd.Data.Weapons {
		if id != "water_orb" && w.Bounces != 0 {
			t.Fatalf("%s bounces", id)
		}
	}
	hb := gd.Data.Skills["heavy_bomb"]
	if hb.Name != "Địa chấn liên hoàn" || hb.EffectType != gamedata.EffectBombChain || hb.StatusEffectID != "root" ||
		gamedata.ChainTuningOf(hb) != gamedata.DefaultChainTuning() {
		t.Fatalf("tanko skill %+v", hb)
	}
	for _, ph := range []string{"{count}", "{countPerRank}", "{eachPct}", "{capPct}"} {
		if !strings.Contains(hb.DescriptionTemplate, ph) {
			t.Fatalf("tanko template lost %s: %s", ph, hb.DescriptionTemplate)
		}
	}
	if strings.Contains(hb.Description, "bom") || !strings.Contains(hb.Description, "đá") {
		t.Fatalf("tanko description %s", hb.Description)
	}
}
