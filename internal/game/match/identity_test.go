package match

import (
	"context"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// Character identities (configs/*.yaml): Rookie balanced, Tanko digs, Spark hits hardest,
// Flora lasts longest. These tests run on the real config files.

var identityCharacters = []string{"rookie", "tanko", "spark", "flora"}

// identitySurface is the ground height of identityMatch's flat terrain (rows 31.. of 16 px).
const identitySurface = 31 * botTestCell

// useConfigGameData loads configs/*.yaml as the game data for the test.
func useConfigGameData(t *testing.T) {
	t.Helper()
	prev := gamedata.Data
	_, self, _, _ := runtime.Caller(0)
	if err := gamedata.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gamedata.Data = prev })
}

// identityMatch is a 1v1 on flat ground 400 px thick (surface at identitySurface): the
// shooter (p1, team 1) at shooterX and the target (p2, team 2) at targetX, both with their
// character's base HP and defense, no wind, p1 to play.
func identityMatch(t *testing.T, shooterChar string, shooterX float64, targetChar string, targetX float64) (*Match, map[string]*ws.Client) {
	t.Helper()
	tiles := make([][]int, 56)
	for r := range tiles {
		tiles[r] = make([]int, 100)
		if r >= 31 {
			for c := range tiles[r] {
				tiles[r][c] = 1
			}
		}
	}
	player := func(id string, team int, char string, x float64) *BattlePlayerState {
		b := battlestats.Base(char)
		return &BattlePlayerState{PlayerID: id, TeamID: team, CharacterID: char, HP: b.HP, MaxHP: b.HP, Defense: b.Defense,
			MaxMoveEnergy: b.TurnMoveEnergy(), Position: Vector2{X: x, Y: identitySurface}, IsAlive: true}
	}
	clients := map[string]*ws.Client{
		"p1": {PlayerID: "p1", Send: make(chan ws.Message, 256)},
		"p2": {PlayerID: "p2", Send: make(chan ws.Message, 256)},
	}
	m := NewMatch("m1", "r1", "pvp_1v1", "identity_test",
		[]*BattlePlayerState{player("p1", 1, shooterChar, shooterX), player("p2", 2, targetChar, targetX)},
		map[string]*ws.Client{"p1": clients["p1"], "p2": clients["p2"]}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.Terrain = NewTerrainWithBricks(gamedata.MapConfig{
		MapID: "identity_test", GridWidth: 100, GridHeight: 56, CellSize: botTestCell, Tiles: tiles,
	}, nil)
	for _, p := range m.State.Players {
		p.Position.Y = m.Terrain.SpawnY(p.Position.X, 0) // NewMatch placed them on its own terrain
	}
	m.State.CurrentPlayerID = "p1"
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	return m, clients
}

// TestCraterDepthByCharacter: a normal shot into flat ground. The crater radius is the
// weapon's terrain damage × 0.8 px, so Tanko (130) digs far deeper than the others.
func TestCraterDepthByCharacter(t *testing.T) {
	useConfigGameData(t)
	depth := map[string]float64{}
	for _, id := range identityCharacters {
		// Straight up at low power: the shot falls back onto the ground at the shooter's feet
		// (no self hit), far from the target.
		m, clients := identityMatch(t, id, 400, "rookie", 1300)
		send(m, clients["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
		s := shotResolved(t, clients["p1"])
		ex := eventsOf(s, "explode", "")
		if len(ex) != 1 {
			t.Fatalf("%s: %d explode events", id, len(ex))
		}
		p := *ex[0].Position
		depth[id] = m.Terrain.GetLandingY(p.X, 0) - identitySurface
		w := gamedata.Data.Weapons[gamedata.Data.Characters[id].WeaponID]
		t.Logf("%-6s blast %3d px, terrain damage %3d, crater radius %5.1f px, depth below the surface %5.1f px",
			id, w.ExplosionRadius, w.TerrainDamage, ex[0].Radius, depth[id])
		if ex[0].Radius != float64(w.TerrainDamage)*craterPxPerTerrainDamage {
			t.Fatalf("%s: crater radius %v, want terrain damage %d × %v", id, ex[0].Radius, w.TerrainDamage, craterPxPerTerrainDamage)
		}
		if s.Projectiles[0].ExplosionRadius != float64(w.ExplosionRadius) {
			t.Fatalf("%s: blast radius %v, want %d", id, s.Projectiles[0].ExplosionRadius, w.ExplosionRadius)
		}
	}
	if math.Abs(depth["rookie"]-40) > 2 {
		t.Fatalf("Rookie crater %v px deep, want about 40 (his 40 px blast, as before)", depth["rookie"])
	}
	for _, id := range []string{"rookie", "spark", "flora"} {
		if depth["tanko"] < 2*depth[id] {
			t.Fatalf("Tanko digs %v px, not clearly deeper than %s's %v", depth["tanko"], id, depth[id])
		}
	}
}

// fallOf returns target's fall distance and fall damage in the shot (0, 0 without a fall).
func fallOf(t *testing.T, s ShotResolved, target string) (dist float64, damage int) {
	t.Helper()
	for _, e := range eventsOf(s, "fall", target) {
		dist += e.To.Y - e.From.Y
	}
	for _, e := range eventsOf(s, "damage", target) {
		if e.Cause == "fall" {
			damage += e.Amount
		}
	}
	return dist, damage
}

// TestDiggingBombDropsTargetIntoFallDamage: Tanko's heavy_bomb on a target hit at the centre
// carves a crater deep enough (radius 260 px) that the target falls more than the fall damage
// threshold, while his normal shot (104 px) only drops it without fall damage.
func TestDiggingBombDropsTargetIntoFallDamage(t *testing.T) {
	useConfigGameData(t)
	for _, tc := range []struct {
		mode         string
		wantFallHurt bool
	}{{"weapon", false}, {"skill", true}} {
		// The shot falls straight back down at x 400 and hits the target standing 15 px away.
		m, clients := identityMatch(t, "tanko", 400, "rookie", 415)
		m.State.Players["p1"].SkillEnergy = 100
		send(m, clients["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: tc.mode})
		s := shotResolved(t, clients["p1"])
		if len(s.Projectiles) != 1 || s.Projectiles[0].HitPlayerID != "p2" {
			t.Fatalf("%s: shot did not hit the target directly: %+v", tc.mode, s.Projectiles)
		}
		ex := eventsOf(s, "explode", "")[0]
		var blast int
		for _, e := range eventsOf(s, "damage", "p2") {
			if e.Cause == "explosion" {
				blast += e.Amount
			}
		}
		dist, fall := fallOf(t, s, "p2")
		t.Logf("Tanko %-6s blast %5.1f px, crater %5.1f px: target takes %d explosion damage, falls %5.1f px, %d fall damage",
			tc.mode, s.Projectiles[0].ExplosionRadius, ex.Radius, blast, dist, fall)
		if dist <= 0 {
			t.Fatalf("%s: target did not fall into the crater", tc.mode)
		}
		if got := fall > 0; got != tc.wantFallHurt {
			t.Fatalf("%s: fall %v px, fall damage %d, want fall damage %v", tc.mode, dist, fall, tc.wantFallHurt)
		}
		if tc.mode == "skill" {
			w := gamedata.Data.Weapons["cannon_ball"]
			if s.Projectiles[0].ExplosionRadius != float64(w.ExplosionRadius)*heavyBombBlastScale ||
				ex.Radius != float64(w.TerrainDamage)*craterPxPerTerrainDamage*heavyBombCraterScale {
				t.Fatalf("heavy bomb blast %v crater %v", s.Projectiles[0].ExplosionRadius, ex.Radius)
			}
			want := int(math.Round(ReduceByDefense(float64(w.Damage)*gamedata.Data.Skills["heavy_bomb"].DamageMultiplier, 50)))
			if blast != want {
				t.Fatalf("heavy bomb explosion damage %d, want %d", blast, want)
			}
		}
	}
}

// TestHealingBloomHeals650 with the configured skill.
func TestHealingBloomHeals650(t *testing.T) {
	useConfigGameData(t)
	m, clients := identityMatch(t, "flora", 400, "rookie", 1300)
	p := m.State.Players["p1"]
	p.SkillEnergy = 100
	p.HP = 1000
	send(m, clients["p1"], "Shoot", ShootAction{Angle: 60, Power: 20, ActionMode: "skill"})
	s := shotResolved(t, clients["p1"])
	if h := eventsOf(s, "heal", "p1"); len(h) != 1 || h[0].Amount != 650 || p.HP != 1650 {
		t.Fatalf("heal %+v, hp %d; want 650", h, p.HP)
	}
}

// TestShockFieldHitsHarderAndNets: Spark's skill deals 1.3× his shot and nets the target.
func TestShockFieldHitsHarderAndNets(t *testing.T) {
	useConfigGameData(t)
	hit := func(mode string) (int, ShotResolved) {
		m, clients := identityMatch(t, "spark", 400, "rookie", 415)
		m.State.Players["p1"].SkillEnergy = 100
		send(m, clients["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: mode})
		s := shotResolved(t, clients["p1"])
		d := eventsOf(s, "damage", "p2")
		if len(d) == 0 {
			t.Fatalf("%s did not hit", mode)
		}
		return d[0].Amount, s
	}
	normal, _ := hit("weapon")
	skill, s := hit("skill")
	if math.Abs(float64(skill)-1.3*float64(normal)) > 1 {
		t.Fatalf("shock field %d, want 1.3 × %d", skill, normal)
	}
	if st := eventsOf(s, "status", "p2"); len(st) != 1 || st[0].EffectID != "net" {
		t.Fatalf("status %+v, want net", st)
	}
}

// TestMoveEnergyPerCharacter: each character refills its own base move energy (plus bonus) at
// the start of its turn, and a move is limited to 2 px per energy.
func TestMoveEnergyPerCharacter(t *testing.T) {
	useConfigGameData(t)
	want := map[string]int{"rookie": 100, "tanko": 80, "spark": 120, "flora": 100}
	for _, id := range identityCharacters {
		m, clients := identityMatch(t, id, 400, "rookie", 1300)
		p := m.State.Players["p1"]
		if p.MoveEnergy != want[id] || p.MaxMoveEnergy != want[id] {
			t.Fatalf("%s: move energy %d / max %d, want %d", id, p.MoveEnergy, p.MaxMoveEnergy, want[id])
		}
		send(m, clients["p1"], "Move", MoveAction{Direction: "right", TargetX: 1000})
		if moved := p.Position.X - 400; moved != float64(2*want[id]) || p.MoveEnergy != 0 {
			t.Fatalf("%s: moved %v px, energy left %d; want %d px", id, moved, p.MoveEnergy, 2*want[id])
		}
	}

	// A state without MaxMoveEnergy (an older snapshot) falls back to the config + bonus
	m, _ := identityMatch(t, "rookie", 400, "spark", 1300)
	next := m.State.Players["p2"]
	next.MaxMoveEnergy, next.MoveEnergyBonus = 0, 7
	m.endTurn(t.Context())
	if m.State.CurrentPlayerID != "p2" || next.MoveEnergy != 127 || next.MaxMoveEnergy != 127 {
		t.Fatalf("current %s energy %d max %d, want p2 with 120 + 7", m.State.CurrentPlayerID, next.MoveEnergy, next.MaxMoveEnergy)
	}
}

// TestBotAimsWithEveryCharacter: the bot's shot scoring finds a hit with each character's
// weapon (radius, weight, wind) at short and long range, with and without wind.
func TestBotAimsWithEveryCharacter(t *testing.T) {
	useConfigGameData(t)
	for _, id := range identityCharacters {
		for _, tc := range []struct {
			enemyX float64
			wind   WindState
		}{{1000, WindState{}}, {700, WindState{Direction: 1, Power: 3}}, {1300, WindState{Direction: -1, Power: 2}}} {
			m, _ := identityMatch(t, id, 400, "rookie", tc.enemyX)
			m.BotTier = perfectAim
			m.State.Wind = tc.wind
			bot := m.State.Players["p1"]
			bot.IsBot = true
			act := decideShot(t, m, bot)
			weapon := gamedata.Data.Weapons[gamedata.Data.Characters[id].WeaponID]
			r := SimulateProjectile(bot.PlayerID, bot.TeamID, shotOrigin(m.Terrain, bot), act.Angle, act.Power, weapon,
				m.State.Wind, m.Terrain, m.State.Players, 0)
			target := m.State.Players["p2"]
			if r.ExplosionPoint == nil {
				t.Fatalf("%s vs x %v: shot never exploded", id, tc.enemyX)
			}
			if d := math.Hypot(r.ExplosionPoint.X-target.Position.X, r.ExplosionPoint.Y-target.Position.Y); d >= float64(weapon.ExplosionRadius) {
				t.Fatalf("%s vs x %v wind %+v: shot %.0f°/%.0f landed %.0f px from the target", id, tc.enemyX, tc.wind, act.Angle, act.Power, d)
			}
		}
	}
}

// hitsToKill is how many direct hits (no wind, no crit) attacker's shot needs to kill
// defender, with their base stats plus the given flat bonuses.
func hitsToKill(attacker, defender string, damageBonus, hpBonus, defenseBonus int) int {
	w := gamedata.Data.Weapons[gamedata.Data.Characters[attacker].WeaponID]
	d := battlestats.Base(defender)
	hit := CalculateExplosionDamage(Vector2{}, Vector2{}, float64(w.Damage+damageBonus), float64(w.ExplosionRadius), d.Defense+defenseBonus)
	return int(math.Ceil(float64(d.HP+hpBonus) / float64(hit)))
}

// CheckIdentityMatrix checks a hits-to-kill matrix (attacker → defender → hits): Spark kills
// fastest, Flora survives longest, Rookie sits in between; with inRange, every matchup takes
// 3 to 8 hits.
func checkIdentityMatrix(t *testing.T, hits map[string]map[string]int, inRange bool) {
	t.Helper()
	t.Logf("%-8s %7s %7s %7s %7s", "atk\\def", "rookie", "tanko", "spark", "flora")
	for _, a := range identityCharacters {
		t.Logf("%-8s %7d %7d %7d %7d", a, hits[a]["rookie"], hits[a]["tanko"], hits[a]["spark"], hits[a]["flora"])
	}
	row := map[string]int{}
	col := map[string]int{}
	for _, a := range identityCharacters {
		for _, d := range identityCharacters {
			h := hits[a][d]
			row[a] += h
			col[d] += h
			if inRange && (h < 3 || h > 8) {
				t.Errorf("%s kills %s in %d hits, want 3..8", a, d, h)
			}
		}
	}
	for _, d := range identityCharacters {
		for _, a := range identityCharacters {
			if hits["spark"][d] > hits[a][d] {
				t.Errorf("%s kills %s in %d, faster than Spark's %d", a, d, hits[a][d], hits["spark"][d])
			}
			if hits[a]["flora"] < hits[a][d] {
				t.Errorf("%s lasts %d hits from %s, longer than Flora's %d", d, hits[a][d], a, hits[a]["flora"])
			}
		}
	}
	for _, id := range identityCharacters {
		if id != "spark" && row["spark"] >= row[id] {
			t.Errorf("Spark needs %d hits over all matchups, not fewer than %s's %d", row["spark"], id, row[id])
		}
		if id != "flora" && col["flora"] <= col[id] {
			t.Errorf("Flora lasts %d hits over all matchups, not more than %s's %d", col["flora"], id, col[id])
		}
	}
	if !(row["spark"] < row["rookie"] && row["rookie"] < row["flora"]) {
		t.Errorf("Rookie's offence %d hits is not between Spark's %d and Flora's %d", row["rookie"], row["spark"], row["flora"])
	}
	if !(col["spark"] < col["rookie"] && col["rookie"] < col["flora"]) {
		t.Errorf("Rookie's durability %d hits is not between Spark's %d and Flora's %d", col["rookie"], col["spark"], col["flora"])
	}
}

// TestIdentityHitsToKillNoGear: the matrix with base stats only (the DB version with gear is
// room.TestCharacterHitsToKillMatrix).
func TestIdentityHitsToKillNoGear(t *testing.T) {
	useConfigGameData(t)
	hits := map[string]map[string]int{}
	for _, a := range identityCharacters {
		hits[a] = map[string]int{}
		for _, d := range identityCharacters {
			hits[a][d] = hitsToKill(a, d, 0, 0, 0)
		}
	}
	checkIdentityMatrix(t, hits, true)
}
