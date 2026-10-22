package match

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/mapgen"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/game/ws"
)

// loadConfigs installs configs/*.yaml (maps.yaml and bricks.yaml included) as the game data.
func loadConfigs(t *testing.T) {
	t.Helper()
	prevData, prevBricks := gamedata.Data, gamedata.BrickTypes
	t.Cleanup(func() { gamedata.Data, gamedata.BrickTypes = prevData, prevBricks })
	_, self, _, _ := runtime.Caller(0)
	if err := gamedata.LoadGameData(filepath.Join(filepath.Dir(self), "..", "..", "..", "configs")); err != nil {
		t.Fatal(err)
	}
}

// The brick shapes fill their cells as drawn: slopes are 45° staircases a player can walk
// (at most 1 px up per px), a 45° band has no holes, planks are 6 px thick.
func TestBrickShapesFillCells(t *testing.T) {
	bricks := mapgen.BrickTypeMap()
	fill := func(rows []string) *Terrain {
		g := make([][]int, len(rows))
		for r, row := range rows {
			g[r] = make([]int, len(row))
			for c, ch := range row {
				switch ch {
				case '/':
					g[r][c] = mapgen.BrickID(mapgen.Soft, mapgen.SlopeR)
				case '\\':
					g[r][c] = mapgen.BrickID(mapgen.Soft, mapgen.SlopeL)
				case 'b':
					g[r][c] = mapgen.BrickID(mapgen.Hard, mapgen.CeilR)
				case 'd':
					g[r][c] = mapgen.BrickID(mapgen.Hard, mapgen.CeilL)
				case '-':
					g[r][c] = mapgen.BrickID(mapgen.Hard, mapgen.Plank)
				case '#':
					g[r][c] = mapgen.BrickDirt
				}
			}
		}
		return NewTerrainWithBricks(gamedata.MapConfig{MapID: "shapes", GridWidth: len(rows[0]), GridHeight: len(rows), CellSize: 16, Tiles: g}, bricks)
	}
	count := func(tr *Terrain, x0, y0, x1, y1 int) int {
		n := 0
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if tr.IsSolid(float64(x), float64(y)) {
					n++
				}
			}
		}
		return n
	}

	tr := fill([]string{"/\\-", "##."})
	if n := count(tr, 0, 0, 16, 16); n != 136 { // 1 + 2 + ... + 16
		t.Fatalf("slope up-right: %d px, want 136", n)
	}
	if n := count(tr, 16, 0, 32, 16); n != 120 { // 0 + 1 + ... + 15
		t.Fatalf("slope up-left: %d px, want 120", n)
	}
	if n := count(tr, 32, 0, 48, 16); n != 96 {
		t.Fatalf("plank: %d px, want 16 x 6", n)
	}
	if tr.IsDestructible(32, 0) || !tr.IsDestructible(8, 15) {
		t.Fatal("plank should be hard, the slope soft")
	}
	// A player walks up the slope from the left and down the other side.
	if x, y := tr.WalkTo(0, tr.SpawnY(0, 0), 31); x != 31 || y > 17 {
		t.Fatalf("walk over the slopes stopped at (%v, %v)", x, y)
	}

	// Bands: every column 16 px solid, no holes.
	band := fill([]string{"\\../", "b..d"})
	for x := 0; x < 64; x++ {
		if x >= 16 && x < 48 {
			continue
		}
		if n := count(band, x, 0, x+1, 32); n != 16 {
			t.Fatalf("band column %d: %d px solid, want 16", x, n)
		}
	}
}

// spawnDepthOK reports whether a player standing at (x, surface) has at least 60 px of solid
// ground under it or hard ground within 60 px.
func spawnDepthOK(tr *Terrain, x int, surface int) bool {
	for y := surface; y < surface+60; y++ {
		if !tr.IsSolid(float64(x), float64(y)) {
			return false
		}
		if !tr.IsDestructible(x, y) {
			return true
		}
	}
	return true
}

// Every generated map loads from configs, validates with its brick types (as the admin map
// editor checks it on save), has 2 spawns per team on its own half, each landing on its
// platform with headroom and solid footing, and no floating single cell.
func TestGeneratedMapsAreSound(t *testing.T) {
	loadConfigs(t)
	if len(mapgen.Defs) != 16 {
		t.Fatalf("%d generated maps, want 16", len(mapgen.Defs))
	}
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			cfg, ok := gamedata.Data.Maps[d.ID]
			if !ok {
				t.Fatal("not in configs/maps.yaml")
			}
			if err := gamedata.ValidateMap(cfg, gamedata.BrickTypes); err != nil {
				t.Fatal(err)
			}
			if !gamedata.KnownMapTheme(cfg.Theme) || cfg.Theme != d.Theme || cfg.Name != d.Name {
				t.Fatalf("theme %q name %q", cfg.Theme, cfg.Name)
			}
			mapW, mapH := cfg.PixelSize()
			if mapW != 2400 || mapH != 1120 || cfg.Width != mapW || cfg.Height != mapH {
				t.Fatalf("size %dx%d (width %d height %d)", mapW, mapH, cfg.Width, cfg.Height)
			}
			if r := cfg.DefaultWindPowerRange; len(r) != 2 || r[1] > 4 {
				t.Fatalf("wind %v", r)
			}
			if len(cfg.SpawnPoints) != 4 {
				t.Fatalf("%d spawns, want 4", len(cfg.SpawnPoints))
			}
			team1, team2 := cfg.TeamSpawns()
			tr := NewTerrain(cfg)
			for i, sp := range append(append([]gamedata.SpawnPoint{}, team1...), team2...) {
				if left := sp.X < float64(mapW)/2; left != (i < 2) {
					t.Errorf("spawn %d (%v,%v) on the wrong half", i, sp.X, sp.Y)
				}
				if !tr.CanStandAt(sp.X, sp.Y) {
					t.Errorf("spawn %d (%v,%v): no ground or headroom", i, sp.X, sp.Y)
					continue
				}
				surface := tr.SpawnY(sp.X, sp.Y)
				want := float64(d.Spawns[i%2][1] * mapgen.CellSize)
				if math.Abs(surface-want) > 16 {
					t.Errorf("spawn %d lands at y=%v, its platform is at %v", i, surface, want)
				}
				if !spawnDepthOK(tr, int(sp.X), int(surface)) {
					t.Errorf("spawn %d (%v,%v): less than 60 px of ground and nothing hard under it", i, sp.X, sp.Y)
				}
			}
			for i := 0; i < 2; i++ {
				a, b := cfg.SpawnPoints[i], cfg.SpawnPoints[i+2]
				if a.X != float64(mapW)-b.X || a.Y != b.Y {
					t.Errorf("spawns %d and %d are not mirrored: %v %v", i, i+2, a, b)
				}
			}
			if math.Abs(cfg.SpawnPoints[0].X-cfg.SpawnPoints[1].X) < 48 {
				t.Errorf("team 1's spawns overlap")
			}
			// connected groups of cells (sharing an edge) are at least 2 cells
			seen := map[[2]int]bool{}
			for r := range cfg.Tiles {
				for c := range cfg.Tiles[r] {
					if cfg.Tiles[r][c] == 0 || seen[[2]int{r, c}] {
						continue
					}
					n, stack := 0, [][2]int{{r, c}}
					seen[[2]int{r, c}] = true
					for len(stack) > 0 {
						p := stack[len(stack)-1]
						stack = stack[:len(stack)-1]
						n++
						for _, dd := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
							q := [2]int{p[0] + dd[0], p[1] + dd[1]}
							if q[0] < 0 || q[0] >= len(cfg.Tiles) || q[1] < 0 || q[1] >= len(cfg.Tiles[0]) || seen[q] || cfg.Tiles[q[0]][q[1]] == 0 {
								continue
							}
							seen[q] = true
							stack = append(stack, q)
						}
					}
					if n < 2 {
						t.Errorf("floating single cell at row %d col %d", r, c)
					}
				}
			}
		})
	}
}

// mapMatch is a bot match on a configs map: rookie, tanko, spark and flora (one per spawn, 2v2
// or the first two 1v1), each a bot of tier.
func mapMatch(t *testing.T, mapID string, players int, tier matchmaker.BotTierConfig) *Match {
	t.Helper()
	cfg := gamedata.Data.Maps[mapID]
	team1, team2 := cfg.TeamSpawns()
	chars := []string{"rookie", "tanko", "spark", "flora"}
	var ps []*BattlePlayerState
	for i := 0; i < players; i++ {
		team, sp := 1, team1[(i/2)%len(team1)]
		if i%2 == 1 {
			team, sp = 2, team2[(i/2)%len(team2)]
		}
		ch := gamedata.Data.Characters[chars[i]]
		ps = append(ps, &BattlePlayerState{PlayerID: fmt.Sprintf("bot_%d", i), TeamID: team, CharacterID: ch.CharacterID,
			HP: ch.HP, MaxHP: ch.HP, Defense: ch.Defense, Position: Vector2{X: sp.X, Y: sp.Y}, IsAlive: true, IsBot: true})
	}
	m := NewMatch("m_"+mapID, "r1", "ranked_2v2", mapID, ps, map[string]*ws.Client{}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.BotTier = tier
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		return nil, nil
	}
	return m
}

// On every generated map the sides are not sealed off (no wind): every spawn has a damaging
// shot at some enemy spawn and can be hit from one, and the 1v1 spawns can hit each other.
func TestGeneratedMapsHaveShotsBetweenSides(t *testing.T) {
	loadConfigs(t)
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			cfg := gamedata.Data.Maps[d.ID]
			team1, team2 := cfg.TeamSpawns()
			canHit := func(from, to gamedata.SpawnPoint) bool {
				m := mapMatch(t, d.ID, 2, perfectAim)
				var shooter, target *BattlePlayerState
				for _, p := range m.State.Players {
					if p.TeamID == 1 {
						shooter = p
					} else {
						target = p
					}
				}
				shooter.Position = Vector2{X: from.X, Y: m.Terrain.SpawnY(from.X, from.Y)}
				target.Position = Vector2{X: to.X, Y: m.Terrain.SpawnY(to.X, to.Y)}
				weapon, _ := botWeapon(shooter)
				return NewBotBrain(perfectAim).bestShot(m, shooter, shooter.Position, weapon).damage > 0
			}
			hits := map[[2]int]bool{} // [from, to] spawn indexes 0..3 (team 1: 0, 1)
			all := append(append([]gamedata.SpawnPoint{}, team1...), team2...)
			for i := 0; i < 2; i++ {
				for j := 2; j < 4; j++ {
					hits[[2]int{i, j}] = canHit(all[i], all[j])
					hits[[2]int{j, i}] = canHit(all[j], all[i])
				}
			}
			if !hits[[2]int{0, 2}] || !hits[[2]int{2, 0}] {
				t.Errorf("the 1v1 spawns cannot hit each other: %v", hits)
			}
			for i := range all {
				out, in := false, false
				for k, ok := range hits {
					out = out || (ok && k[0] == i)
					in = in || (ok && k[1] == i)
				}
				if !out || !in {
					t.Errorf("spawn %d (%v,%v): hits an enemy %v, is hit %v", i, all[i].X, all[i].Y, out, in)
				}
			}
		})
	}
}

// reachShot looks for an angle / power in the bot's ranges whose shell (no wind, on the map's
// fresh terrain) from a player standing at from hits a player standing at to: it meets the
// body (PlayerHitRadius round the body centre) or explodes that close to it.
func reachShot(tr *Terrain, from, to Vector2) (angle, power float64, ok bool) {
	target := &BattlePlayerState{PlayerID: "target", TeamID: 2, IsAlive: true, Position: to}
	players := map[string]*BattlePlayerState{"target": target}
	body := Vector2{X: to.X, Y: to.Y - ShotOriginLift}
	weapon := gamedata.WeaponConfig{ProjectileWeight: 1, WindInfluence: 1}
	lo, hi := botMinAngle, 90.0
	if to.X < from.X {
		lo, hi = 90, botMaxAngle
	}
	origin := shotOriginAt(tr, from)
	for a := lo; a <= hi; a++ {
		for p := botMinPower; p <= maxShotPower; p++ {
			r := SimulateProjectile("shooter", 1, origin, a, p, weapon, WindState{}, tr, players, 0)
			if r.HitPlayerID == "target" {
				return a, p, true
			}
			if e := r.ExplosionPoint; e != nil && math.Hypot(e.X-body.X, e.Y-body.Y) <= physics().PlayerHitRadius {
				return a, p, true
			}
		}
	}
	return 0, 0, false
}

// Every spawn reaches every enemy spawn: with no wind some angle / power hits a player standing
// there, on the real terrain (a 2400 px map is about as wide as a full-power shot carries).
func TestGeneratedMapsEveryEnemyReachable(t *testing.T) {
	loadConfigs(t)
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			t.Parallel()
			cfg := gamedata.Data.Maps[d.ID]
			tr := NewTerrain(cfg)
			team1, team2 := cfg.TeamSpawns()
			feet := func(sp gamedata.SpawnPoint) Vector2 { return Vector2{X: sp.X, Y: tr.SpawnY(sp.X, sp.Y)} }
			for _, pair := range [][2][]gamedata.SpawnPoint{{team1, team2}, {team2, team1}} {
				for i, a := range pair[0] {
					for j, b := range pair[1] {
						from, to := feet(a), feet(b)
						angle, power, ok := reachShot(tr, from, to)
						if !ok {
							t.Errorf("(%v,%v) cannot hit the enemy at (%v,%v), %.0f px away", from.X, from.Y, to.X, to.Y, math.Abs(to.X-from.X))
							continue
						}
						t.Logf("spawn %d -> enemy %d: %.0f px, angle %.0f power %.0f", i, j, math.Abs(to.X-from.X), angle, power)
					}
				}
			}
		})
	}
}

// minSpawnFloor is the walkable ground every spawn needs round it (12 cells).
const minSpawnFloor = 192

// walkSpan is how far a player standing at feet can walk each way with the move rules (WalkTo,
// one pixel at a time) before a wall stops it or the ground ends (a drop of more than 4 px in
// one pixel: off the edge).
func walkSpan(tr *Terrain, feet Vector2) (left, right float64) {
	walk := func(dir float64) float64 {
		x, y := feet.X, feet.Y
		for {
			nx, ny := tr.WalkTo(x, y, x+dir)
			if nx == x || ny-y > 4 || ny >= float64(tr.Height) {
				return math.Abs(x - feet.X)
			}
			x, y = nx, ny
		}
	}
	return walk(-1), walk(1)
}

// Every spawn stands on wide ground: at least minSpawnFloor px of continuous walkable surface
// round it.
func TestGeneratedMapsSpawnsHaveWideFloor(t *testing.T) {
	loadConfigs(t)
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			cfg := gamedata.Data.Maps[d.ID]
			tr := NewTerrain(cfg)
			for i, sp := range cfg.SpawnPoints {
				feet := Vector2{X: sp.X, Y: tr.SpawnY(sp.X, sp.Y)}
				l, r := walkSpan(tr, feet)
				if l+r < minSpawnFloor {
					t.Errorf("spawn %d (%v,%v): %.0f px of floor (%.0f left, %.0f right), want %d", i, sp.X, sp.Y, l+r, l, r, minSpawnFloor)
				}
				if i < 2 {
					t.Logf("spawn %d: %.0f px of floor (%.0f left, %.0f right)", i, l+r, l, r)
				}
			}
		})
	}
}

// tankoMaxCrater is the biggest crater on the battle maps (no fragile bricks there): Tanko's
// cannon ball with the terrain damage bonus at its cap. Skill shells carve no more: the bomb
// chain's bombs carve the weapon's bowl, the missile rain's missiles rainCrater × Rookie's.
func tankoMaxCrater(t *testing.T) float64 {
	t.Helper()
	w, ok := gamedata.Data.Weapons["cannon_ball"]
	if !ok {
		t.Fatal("no cannon_ball")
	}
	return craterRadius(&BattlePlayerState{TerrainDamageBonus: 1000}, weaponCraterRadius(w))
}

// shotsToFallOut digs at a player standing at feet: each shot explodes 3 px into the ground
// under its feet (where a shell landing at the feet explodes) with the given crater, and the
// player drops onto what is left. It returns how many shots it takes until the player falls
// out of the map (limit + 1 when it is still standing after limit shots).
func shotsToFallOut(tr *Terrain, feet Vector2, crater float64, limit int) int {
	tr = cloneTerrain(tr)
	y := feet.Y
	for n := 1; n <= limit; n++ {
		tr.CarveBowl(feet.X, y+3, crater)
		y = tr.GetLandingY(feet.X, y)
		if y >= float64(tr.Height) {
			return n
		}
	}
	return limit + 1
}

// minShotsToFallOut is how many of the biggest craters at the same spot it takes, at least,
// before a player standing on a spawn can fall out of the map.
const minShotsToFallOut = 3

// The ground holds against Tanko: a player on a spawn falls out of the map no sooner than the
// minShotsToFallOut-th max-bonus cannon ball at its feet, and one such crater anywhere on a
// spawn's floor does not cut the platform through (it still has ground under the crater).
func TestGeneratedMapsGroundHoldsTankoCraters(t *testing.T) {
	loadConfigs(t)
	crater := tankoMaxCrater(t)
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			cfg := gamedata.Data.Maps[d.ID]
			tr := NewTerrain(cfg)
			for i, sp := range cfg.SpawnPoints {
				feet := Vector2{X: sp.X, Y: tr.SpawnY(sp.X, sp.Y)}
				n := shotsToFallOut(tr, feet, crater, 60)
				if n < minShotsToFallOut {
					t.Errorf("spawn %d (%v,%v): out of the map after %d craters of %.0f px", i, sp.X, sp.Y, n, crater)
				}
				if i < 2 {
					shots := fmt.Sprint(n)
					if n > 60 {
						shots = "never (hard ground under it)"
					}
					t.Logf("spawn %d: out after %s", i, shots)
				}
				// one crater anywhere on the floor (but its last 32 px) leaves ground under it
				l, r := walkSpan(tr, feet)
				for x := feet.X - l + 32; x <= feet.X+r-32; x += 8 {
					fy := tr.SpawnY(x, feet.Y-48)
					c := cloneTerrain(tr)
					c.CarveBowl(x, fy+3, crater)
					if c.GetLandingY(x, fy) >= float64(c.Height) || !c.IsSolid(x, c.GetLandingY(x, fy)) {
						t.Errorf("spawn %d: one crater at x=%v cuts its floor through", i, x)
						break
					}
					if land := c.GetLandingY(x, fy); land-fy > 2*crater {
						t.Errorf("spawn %d: one crater at x=%v drops a player %.0f px (through the platform)", i, x, land-fy)
						break
					}
				}
			}
		})
	}
}

// A shell that bursts on hard (indestructible) ground carves nothing there, and players near it
// still take the full splash damage.
func TestExplosionOnHardGroundStillSplashes(t *testing.T) {
	loadConfigs(t)
	m := mapMatch(t, "escape", 2, perfectAim) // every brick of escape is hard
	var shooter, target *BattlePlayerState
	for _, p := range m.State.Players {
		if p.TeamID == 1 {
			shooter = p
		} else {
			target = p
		}
	}
	shooter.TerrainDamageBonus = 1000
	feet := target.Position
	feet.Y = m.Terrain.SpawnY(feet.X, feet.Y)
	target.Position = feet
	before := cloneTerrain(m.Terrain)
	at := Vector2{X: feet.X + 20, Y: feet.Y + 3} // in the crate, 20 px from the target
	r := &ProjectileResult{ProjectileID: "p", ExplosionPoint: &at, ExplosionRadius: 60, CraterRadius: 52}
	hp := target.HP
	hit := m.explodeProjectile(shooter, r, 420, &shotTimeline{})
	for i := range before.Mask {
		if before.Mask[i] != m.Terrain.Mask[i] {
			t.Fatal("hard ground carved")
		}
	}
	want := CalculateExplosionDamage(feet, at, 420, 60, target.Defense)
	if len(hit) != 1 || want <= 0 || hp-target.HP != want {
		t.Fatalf("splash on hard ground: %d hit, hp %d -> %d, want -%d", len(hit), hp, target.HP, want)
	}
}

// playBotMatch runs a bot-only match turn by turn on the test goroutine, as botStep does
// (medkit, move, shoot; the turn ends after the shot without waiting for the replay), and
// returns the number of turns played.
func playBotMatch(t *testing.T, m *Match, maxTurns int) int {
	t.Helper()
	ctx := context.Background()
	m.startTurn(ctx)
	turns := 0
	for ; turns < maxTurns && m.State.Status == "in_progress"; turns++ {
		seq := m.turnSeq
		var ts botTurnState
		for step := 0; step < botMaxSteps && m.turnSeq == seq && !m.resolving && m.State.Status == "in_progress"; step++ {
			bot := m.State.Players[m.State.CurrentPlayerID]
			if HasEffect(bot, "freeze") {
				break
			}
			switch act := NewBotBrain(m.BotTier).DecideAction(m, bot, ts).(type) {
			case UseItemAction:
				m.processUseItem(ctx, nil, act)
				ts.usedItem = true
			case MoveAction:
				m.processMove(ctx, nil, act)
				ts.moved = true
			case ShootAction:
				m.selectBotAction(bot, act)
				m.processShoot(ctx, nil, act)
				step = botMaxSteps
			}
		}
		if m.State.Status == "in_progress" && m.turnSeq == seq {
			m.endTurn(ctx)
		}
	}
	return turns
}

// Bots play a whole 2v2 on every generated map and the match ends (nobody stuck forever).
func TestBotsFinishMatchOnEveryGeneratedMap(t *testing.T) {
	if testing.Short() {
		t.Skip("bot matches on 16 maps")
	}
	loadConfigs(t)
	tier := matchmaker.BotTierConfig{HitChance: 70, UseItemChance: 0, MovementSmart: 1}
	for _, d := range mapgen.Defs {
		t.Run(d.ID, func(t *testing.T) {
			t.Parallel()
			m := mapMatch(t, d.ID, 4, tier)
			turns := playBotMatch(t, m, 240)
			if m.State.Status != "ended" {
				alive := ""
				for _, p := range m.State.Players {
					alive += fmt.Sprintf(" %s(team %d) hp %d at (%.0f,%.0f);", p.PlayerID, p.TeamID, p.HP, p.Position.X, p.Position.Y)
				}
				t.Fatalf("not over after %d turns:%s", turns, alive)
			}
			t.Logf("%s: over in %d turns", d.ID, turns)
		})
	}
}

// MatchStarted / MatchStateSync carry the map's name and theme next to its id.
func TestMatchSnapshotCarriesMapNameAndTheme(t *testing.T) {
	loadConfigs(t)
	m := mapMatch(t, "ice_bridge", 2, perfectAim)
	var snap struct {
		MapID    string `json:"mapId"`
		MapName  string `json:"mapName"`
		MapTheme string `json:"mapTheme"`
	}
	if err := json.Unmarshal(m.snapshotPayload(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.MapID != "ice_bridge" || snap.MapName != "Cầu băng" || snap.MapTheme != "ice" {
		t.Fatalf("snapshot %+v", snap)
	}
	if err := json.Unmarshal(m.syncPayload("bot_0"), &snap); err != nil || snap.MapTheme != "ice" {
		t.Fatalf("sync %+v %v", snap, err)
	}
	legacy := mapMatch(t, "grassland_valley", 2, perfectAim)
	if legacy.State.MapName != "Thung lũng cỏ" || legacy.State.MapTheme != "grassland" {
		t.Fatalf("grassland_valley %q %q", legacy.State.MapName, legacy.State.MapTheme)
	}
}
