package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// touchingSnowmen is the witch's match with its first two snowmen standing side by side on
// level ground, their bodies touching (centres 2 × hitRadius × bodySize apart), every other
// minion dead, and p0 (no crit) as the shooter.
func touchingSnowmen(t *testing.T) (*Match, *BattlePlayerState, *BattlePlayerState, *BattlePlayerState) {
	t.Helper()
	loadConfigs(t)
	m, _ := bossMatch(t, "ice_witch", 1)
	s1, s2 := m.State.Players["minion_snowman_1"], m.State.Players["minion_snowman_2"]
	for _, p := range m.State.Players {
		if p.Role == RoleMinion && p != s1 && p != s2 {
			killPlayer(p)
		}
	}
	gap := 2 * m.gd.Physics.PlayerHitRadius * bodyScale(s1)
	s2.Position = Vector2{X: s1.Position.X + gap, Y: s1.Position.Y}
	for _, s := range []*BattlePlayerState{s1, s2} {
		s.HP, s.MaxHP, s.Shield = 1e6, 1e6, 0
	}
	p0 := m.State.Players["p0"]
	p0.CritChance = 0
	return m, p0, s1, s2
}

// splash fires a blast of radius at point (hitting hitID directly) from shooter and returns the
// damage events of the shot by unit.
func splash(t *testing.T, m *Match, shooter *BattlePlayerState, point Vector2, hitID string, radius, base float64) map[string]int {
	t.Helper()
	tl := m.newTimeline()
	r := &ProjectileResult{ProjectileID: "x", HitPlayerID: hitID, ExplosionPoint: vecPtr(point), ExplosionRadius: radius,
		Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
	m.explodeProjectile(shooter, r, base, tl)
	out := map[string]int{}
	for _, e := range tl.events {
		if e.Type == "damage" {
			out[e.PlayerID] += e.Amount
		}
	}
	return out
}

// A shell bursting on the top of a snowman also hurts the snowman touching it: the blast
// measures to the edge of each body (the circle the shell hits), not to the feet.
func TestSplashHitsTouchingSnowmen(t *testing.T) {
	m, p0, s1, s2 := touchingSnowmen(t)
	r := m.gd.Physics.PlayerHitRadius
	top := bodyCentre(s1, shotOriginLift(m.Terrain))
	top.Y -= r * bodyScale(s1)
	got := splash(t, m, p0, top, s1.PlayerID, 40, 480)
	if got[s1.PlayerID] <= 0 || got[s2.PlayerID] <= 0 {
		t.Fatalf("shell on a snowman's head: damage %v, both touching snowmen must be hurt", got)
	}
	if got[s2.PlayerID] >= got[s1.PlayerID] {
		t.Fatalf("the neighbour %d takes more than the snowman struck %d", got[s2.PlayerID], got[s1.PlayerID])
	}
}

// A blast between two touching snowmen (no direct hit) hurts both by the same amount.
func TestSplashBetweenSnowmenIsEqual(t *testing.T) {
	m, p0, s1, s2 := touchingSnowmen(t)
	lift := shotOriginLift(m.Terrain)
	c1, c2 := bodyCentre(s1, lift), bodyCentre(s2, lift)
	for _, dy := range []float64{0, -25} { // where the bodies touch, and above that
		mid := Vector2{X: (c1.X + c2.X) / 2, Y: c1.Y + dy}
		got := splash(t, m, p0, mid, "", 40, 480)
		if got[s1.PlayerID] <= 0 || got[s1.PlayerID] != got[s2.PlayerID] {
			t.Fatalf("blast between the snowmen (dy %v): damage %v, want equal and > 0", dy, got)
		}
	}
	// the shell striking one of them still deals it the full (direct) damage
	got := splash(t, m, p0, Vector2{X: c1.X + 1, Y: c1.Y}, s1.PlayerID, 40, 480)
	full := explosionDamageAt(0, 480, 40, s1.Defense, m.gd.Physics.DefenseKOrDefault())
	if got[s1.PlayerID] != full || got[s2.PlayerID] <= 0 {
		t.Fatalf("direct hit: damage %v, want %d on %s and the neighbour hurt", got, full, s1.PlayerID)
	}
}

// Player vs player: two enemies touching both take the splash, the same amount at the same
// distance from the blast; a blast farther than the radius from a body deals nothing.
func TestPlayerSplashMeasuresToTheBody(t *testing.T) {
	loadConfigs(t)
	m := mapMatch(t, "escape", 4, perfectAim)
	var shooter *BattlePlayerState
	var foes []*BattlePlayerState
	for _, id := range []string{"bot_0", "bot_1", "bot_2", "bot_3"} {
		p := m.State.Players[id]
		if p.TeamID == 1 && shooter == nil {
			shooter = p
		} else if p.TeamID == 2 {
			foes = append(foes, p)
		}
	}
	shooter.CritChance = 0
	a, b := foes[0], foes[1]
	r, lift := m.gd.Physics.PlayerHitRadius, shotOriginLift(m.Terrain)
	b.Position = Vector2{X: a.Position.X + 2*r, Y: a.Position.Y}
	b.Defense = a.Defense
	head := Vector2{X: a.Position.X, Y: a.Position.Y - lift - r}
	got := splash(t, m, shooter, head, a.PlayerID, 40, 480)
	if got[a.PlayerID] <= 0 || got[b.PlayerID] <= 0 {
		t.Fatalf("shell on a player's head: damage %v, the enemy touching it must be hurt", got)
	}
	mid := Vector2{X: a.Position.X + r, Y: a.Position.Y - lift}
	got = splash(t, m, shooter, mid, "", 40, 480)
	if got[a.PlayerID] <= 0 || got[a.PlayerID] != got[b.PlayerID] {
		t.Fatalf("blast between two players: damage %v, want equal and > 0", got)
	}
	far := Vector2{X: a.Position.X - r - 41, Y: a.Position.Y - lift}
	if got = splash(t, m, shooter, far, "", 40, 480); got[a.PlayerID] != 0 {
		t.Fatalf("blast 41 px from the body edge, radius 40: damage %v", got)
	}
	// 20 px from the body edge: half the base (before defense)
	at := Vector2{X: a.Position.X - r - 20, Y: a.Position.Y - lift}
	want := explosionDamageAt(20, 480, 40, a.Defense, m.gd.Physics.DefenseKOrDefault())
	if got = splash(t, m, shooter, at, "", 40, 480); math.Abs(float64(got[a.PlayerID]-want)) > 1 {
		t.Fatalf("blast 20 px from the body edge: %d, want %d", got[a.PlayerID], want)
	}
}

// The boss's body is measured the same way: a blast just outside the edge of its (big) body
// hurts it as much as the same blast just outside a player's.
func TestBossSplashMeasuresToTheBody(t *testing.T) {
	loadConfigs(t)
	m, _ := bossMatch(t, "steel_robot", 1)
	killWave(m) // the shield down
	boss, p0 := m.State.Players["boss_steel_robot"], m.State.Players["p0"]
	p0.CritChance = 0
	r, lift := m.gd.Physics.PlayerHitRadius, shotOriginLift(m.Terrain)
	c := bodyCentre(boss, lift)
	at := Vector2{X: c.X - r*bodyScale(boss) - 20, Y: c.Y}
	got := splash(t, m, p0, at, "", 40, 480)
	ef, effect := m.elementFactor(p0, boss) // rookie (fire) beats the robot (metal): + bossStrongPct
	if effect != ElementStrong || ef != 1.2 {
		t.Fatalf("rookie on the robot: element %v %q", ef, effect)
	}
	want := explosionDamageAt(20, 480*ef, 40, boss.Defense, m.gd.Physics.DefenseKOrDefault())
	if math.Abs(float64(got[boss.PlayerID]-want)) > 1 {
		t.Fatalf("blast 20 px from the boss's body edge: %d, want %d", got[boss.PlayerID], want)
	}
}

// The snowman's death blast (killed by a shot) deals deathBlastDamageFactor 0.5 × what it did
// before (15 % of the target's max HP at the body edge); its detonation (self_destruct) keeps
// the full 15 %. Both reach the client as damage events of the ShotResolved.
func TestSnowmanDeathBlastHalfOfDetonation(t *testing.T) {
	m, p0, s1, s2 := touchingSnowmen(t)
	mc, _ := m.minionCfg(s1)
	if mc.DeathBlastDamageFactor != 0.5 || mc.ExplodeDamagePct != 15 {
		t.Fatalf("snowman config %+v", mc)
	}
	killPlayer(s2)
	lift := shotOriginLift(m.Terrain)
	p0.Position = Vector2{X: s1.Position.X - 60, Y: s1.Position.Y}
	p0At, s1At := p0.Position, s1.Position
	p0.Shield, p0.HP, p0.MaxHP = 0, 5000, 5000
	d := blastDistance(p0, s1.Position, m.gd.Physics.PlayerHitRadius, lift)
	ef, _ := m.elementFactor(s1, p0) // the witch's snowman (water) beats rookie (fire): + bossTakenPct
	full := int(math.Round(5000 * 15.0 / 100 * (1 - 0.5*d/140) * ef))
	half := int(math.Round(5000 * 15.0 * 0.5 / 100 * (1 - 0.5*d/140) * ef))

	// shot dead: the death blast
	s1.HP, s1.MaxHP = 10, 900
	tl := m.newTimeline()
	r := &ProjectileResult{ProjectileID: "x", HitPlayerID: s1.PlayerID, ExplosionPoint: vecPtr(bodyCentre(s1, lift)), ExplosionRadius: 10,
		Path: []ProjectileStep{{Time: 0}, {Time: 1}}}
	m.explodeProjectile(p0, r, 1e4, tl)
	m.deathBlasts(p0, tl)
	got := 0
	for _, e := range tl.events {
		if e.Type == "damage" && e.Cause == CauseDeathBlast && e.PlayerID == p0.PlayerID {
			got += e.Amount
		}
	}
	if s1.IsAlive || got != half || half == 0 || math.Abs(float64(2*half-full)) > 1 {
		t.Fatalf("death blast on p0 %d, want %d (half of the detonation's %d)", got, half, full)
	}

	// the detonation from the same place (the death blast's craters may have dropped p0): the full 15 %
	p0.HP, p0.Position, s1.Position = 5000, p0At, s1At
	s1.IsAlive, s1.HP = true, 900
	tl = m.newTimeline()
	m.minionBlast(s1, mc, s1, blastTime, CauseSelfDestruct, false, tl)
	got = 0
	for _, e := range tl.events {
		if e.Type == "damage" && e.Cause == CauseSelfDestruct && e.PlayerID == p0.PlayerID {
			got += e.Amount
		}
	}
	if got != full {
		t.Fatalf("detonation on p0 %d, want %d", got, full)
	}

	// a minion without the field (the drone): its death blast is its full detonation
	if dm := (gamedata.MinionConfig{}).Normalized(); dm.DeathBlastDamageFactor != 1 {
		t.Fatalf("default deathBlastDamageFactor %v", dm.DeathBlastDamageFactor)
	}
}
