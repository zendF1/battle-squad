package match

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/ws"
)

// bloomMatch: Flora and a Rookie teammate against a Rookie.
func bloomMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	return flatSkillMatch(t, skillPlayer{"p1", 1, "flora", 400}, skillPlayer{"e1", 2, "rookie", 1300},
		skillPlayer{"a1", 1, "rookie", 700})
}

// TestBloomHealsShieldsAndCleanses: Flora 40%, teammate 30% of max HP (rank 0); the overflow
// becomes shield; bad effects removed, the caster's shot buffs kept; enemies untouched.
func TestBloomHealsShieldsAndCleanses(t *testing.T) {
	m, clients := bloomMatch(t)
	flora, ally, enemy := m.State.Players["p1"], m.State.Players["a1"], m.State.Players["e1"]
	flora.HP = 1000 // of 1900: +760, no overflow
	ally.HP = 1450  // of 1500: +450 → 50 HP, 400 shield
	enemy.HP = 500
	flora.StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}, {EffectID: "power_shot", DurationTurn: 1}}
	ally.StatusEffects = []StatusEffect{{EffectID: "burn", DurationTurn: 1}, {EffectID: "stun", DurationTurn: 1}, {EffectID: "net", DurationTurn: 1}}
	s := fireSkill(t, m, clients, "p1", 90, 50)

	if flora.HP != 1760 || flora.Shield != 0 || ally.HP != 1500 || ally.Shield != 400 || enemy.HP != 500 {
		t.Fatalf("flora %d/%d ally %d/%d enemy %d", flora.HP, flora.Shield, ally.HP, ally.Shield, enemy.HP)
	}
	if h := eventsOf(s, "heal", "a1"); len(h) != 1 || h[0].Amount != 50 || h[0].T != bloomSeedTime {
		t.Fatalf("ally heal %+v", h)
	}
	if sh := eventsOf(s, "shield", "a1"); len(sh) != 1 || sh[0].Amount != 400 || *sh[0].Shield != 400 {
		t.Fatalf("ally shield %+v", sh)
	}
	if len(eventsOf(s, "shield", "p1")) != 0 || len(eventsOf(s, "heal", "e1")) != 0 {
		t.Fatal("unexpected shield / heal")
	}
	if c := eventsOf(s, "cleanse", "a1"); len(c) != 1 || len(c[0].EffectIDs) != 3 || len(ally.StatusEffects) != 0 {
		t.Fatalf("ally cleanse %+v, left %+v", c, ally.StatusEffects)
	}
	if len(flora.StatusEffects) != 1 || flora.StatusEffects[0].EffectID != "power_shot" {
		t.Fatalf("flora effects %+v, want only power_shot kept", flora.StatusEffects)
	}
	if len(s.Projectiles) != 2 {
		t.Fatalf("%d seeds, want one per living ally", len(s.Projectiles))
	}
	for _, r := range s.Projectiles {
		p := m.State.Players[r.HitPlayerID]
		end := r.Path[len(r.Path)-1]
		if r.Kind != "seed" || r.ExplosionPoint != nil || end.Time != bloomSeedTime ||
			end.Position != (Vector2{X: p.Position.X, Y: p.Position.Y - ShotOriginLift}) {
			t.Fatalf("seed %+v ends %+v", r, end)
		}
	}
	if !m.resolving {
		t.Fatal("bloom did not end the turn")
	}
}

func TestBloomShieldCappedAtHalfMaxHP(t *testing.T) {
	m, clients := bloomMatch(t)
	ally := m.State.Players["a1"]
	m.State.Players["e1"].IsAlive = true
	ally.Shield = 600 // + 450 → capped at 750
	s := fireSkill(t, m, clients, "p1", 90, 50)
	if ally.Shield != 750 || ally.HP != 1500 {
		t.Fatalf("ally shield %d hp %d, want 750 cap", ally.Shield, ally.HP)
	}
	if sh := eventsOf(s, "shield", "a1"); len(sh) != 1 || sh[0].Amount != 150 {
		t.Fatalf("shield events %+v", sh)
	}
	// Rank 4: Flora 60%, teammate 50%
	m2, clients2 := bloomMatch(t)
	m2.State.Players["p1"].SkillRank = 4
	m2.State.Players["p1"].HP = 100
	m2.State.Players["a1"].HP = 100
	fireSkill(t, m2, clients2, "p1", 90, 50)
	if hp := m2.State.Players["p1"].HP; hp != 100+1140 {
		t.Fatalf("flora rank 4 hp %d, want +60%% = 1240", hp)
	}
	if hp := m2.State.Players["a1"].HP; hp != 100+750 {
		t.Fatalf("ally rank 4 hp %d, want +50%% = 850", hp)
	}
}

func TestBotBloomWhenHurtOrAfflicted(t *testing.T) {
	m, bot := botSkillMatch(t, skillPlayer{"b", 1, "flora", 300}, skillPlayer{"e1", 2, "rookie", 900},
		skillPlayer{"a1", 1, "rookie", 600})
	brain := NewBotBrain(perfectAim)
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode == "skill" {
		t.Fatal("bloom at full HP")
	}
	m.State.Players["a1"].StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}}
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode != "skill" {
		t.Fatal("no bloom for an afflicted ally")
	}
	m.State.Players["a1"].StatusEffects = nil
	bot.HP = bot.MaxHP / 2
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode != "skill" {
		t.Fatal("no bloom at half HP")
	}
	bot.SkillEnergy = 80
	if act := brain.DecideAction(m, bot, botTurnState{}).(ShootAction); act.ActionMode == "skill" {
		t.Fatal("skill without full energy")
	}
}

func TestBloomPercentPerRank(t *testing.T) {
	for rank := 0; rank <= 4; rank++ {
		if bloomSelfPercent(defBloom, rank) != 40+5*rank || bloomAllyPercent(defBloom, rank) != 30+5*rank {
			t.Errorf("bloom rank %d: self %d ally %d", rank, bloomSelfPercent(defBloom, rank), bloomAllyPercent(defBloom, rank))
		}
	}
}

// TestBloomSeedFlight: every seed rises straight above Flora out of the top of the map, crosses
// over to its ally up there (out of view), falls straight down onto the ally and lands exactly
// on its body at bloomSeedTime; seen on the map (y ≥ 0) a seed is only ever above Flora or above
// its ally. The replay lasts bloomLinger after the landing.
func TestBloomSeedFlight(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "flora", 200}, skillPlayer{"e1", 2, "rookie", 800},
		skillPlayer{"a1", 1, "rookie", 1400})
	flora := m.State.Players["p1"]
	origin := shotOrigin(m.Terrain, flora)
	s := fireSkill(t, m, clients, "p1", NoAimAngle, NoAimPower)
	if math.Abs(s.Duration-(bloomSeedTime+bloomLinger)) > 1e-9 {
		t.Fatalf("duration %v, want %v", s.Duration, bloomSeedTime+bloomLinger)
	}
	for _, r := range s.Projectiles {
		ally := m.State.Players[r.HitPlayerID]
		to := Vector2{X: ally.Position.X, Y: ally.Position.Y - ShotOriginLift}
		if r.Path[0].Position != origin || r.Path[0].Time != 0 {
			t.Fatalf("seed for %s starts %+v, want Flora's gun %+v", r.HitPlayerID, r.Path[0], origin)
		}
		top := r.Path[0].Position.Y
		for i, st := range r.Path {
			if i > 0 && st.Time <= r.Path[i-1].Time {
				t.Fatalf("seed path times not increasing at %d: %+v", i, r.Path)
			}
			top = math.Min(top, st.Position.Y)
			if st.Position.Y >= 0 && st.Position.X != origin.X && st.Position.X != to.X {
				t.Fatalf("seed for %s seen at %+v, off the lines above Flora and the ally", r.HitPlayerID, st)
			}
			if st.Time > bloomRiseTime+bloomCrossTime && (st.Position.X != to.X || st.Velocity.Y <= 0) {
				t.Fatalf("seed for %s not falling straight down at %+v", r.HitPlayerID, st)
			}
		}
		if top != bloomSkyY {
			t.Fatalf("seed for %s tops at %v, want %v above the map", r.HitPlayerID, top, bloomSkyY)
		}
		end := r.Path[len(r.Path)-1]
		if end.Position != to || end.Time != bloomSeedTime || end.Velocity.Y < 300 {
			t.Fatalf("seed for %s lands %+v, want %+v at %v still falling", r.HitPlayerID, end, to, bloomSeedTime)
		}
		// The recorded velocities are the path's derivative (the client interpolates with them),
		// except where the seed starts falling, above the map.
		fall := bloomRiseTime + bloomCrossTime
		for i := 1; i < len(r.Path); i++ {
			a, b := r.Path[i-1], r.Path[i]
			if a.Time < fall+1e-9 && b.Time > fall-1e-9 {
				continue
			}
			mid := (a.Velocity.Y + b.Velocity.Y) / 2 * (b.Time - a.Time)
			if d := b.Position.Y - a.Position.Y; math.Abs(d-mid) > 0.08*math.Abs(d)+2 {
				t.Fatalf("seed step %d moves %v px, velocities say %v", i, d, mid)
			}
		}
	}
}

// TestBloomOnRealMap: rank 4 (dragon seed launcher) on grassland_valley, the teammate across
// the map: Flora heals 60 %, the teammate 50 %, its overflow becomes shield (capped), both are
// cleansed, and the enemy between them gets nothing.
func TestBloomOnRealMap(t *testing.T) {
	c := bloomFixture(t)
	var s ShotResolved
	if err := json.Unmarshal(c.Shot, &s); err != nil {
		t.Fatal(err)
	}
	flora, ally := c.Players["flora"], c.Players["a1"]
	// Flora: 60 % of 1900 = 1140 for the 1045 HP she misses, 95 shield
	gap := flora.MaxHP - flora.HP
	if h := eventsOf(s, "heal", "flora"); len(h) != 1 || h[0].Amount != gap || gap >= flora.MaxHP*60/100 {
		t.Fatalf("flora heal %+v, want the %d HP missing", h, gap)
	}
	if sh := eventsOf(s, "shield", "flora"); len(sh) != 1 || sh[0].Amount != flora.MaxHP*60/100-gap {
		t.Fatalf("flora shield %+v", sh)
	}
	gap = ally.MaxHP - ally.HP
	if h := eventsOf(s, "heal", "a1"); len(h) != 1 || h[0].Amount != gap {
		t.Fatalf("ally heal %+v, want the %d HP missing", h, gap)
	}
	if sh := eventsOf(s, "shield", "a1"); len(sh) != 1 || sh[0].Amount != ally.MaxHP*50/100-gap {
		t.Fatalf("ally shield %+v", sh)
	}
	if len(eventsOf(s, "cleanse", "flora")) != 1 || len(eventsOf(s, "cleanse", "a1")) != 1 || len(eventsOf(s, "heal", "e1")) != 0 {
		t.Fatalf("cleanse / enemy events %+v", s.Events)
	}
	for _, r := range s.Projectiles {
		for _, st := range r.Path {
			if st.Position.X < 0 || st.Position.X > 1600 {
				t.Fatalf("seed off the map sides at %+v", st)
			}
		}
	}
}

// bloomFixture fires Flora's bloom at rank 4 on grassland_valley: Flora (45 % HP, rooted) at
// x 200, an enemy at 800 and a Rookie teammate across the map at 1400 (90 % HP, burned and
// rooted).
func bloomFixture(t *testing.T) skillFixtureCase {
	useConfigGameData(t)
	type spec struct {
		id, char, weapon string
		team             int
		x, hp            float64
		effects          []StatusEffect
	}
	specs := []spec{
		{"flora", "flora", "flora_weapon_lv40", 1, 200, 0.45, []StatusEffect{{EffectID: "root", DurationTurn: 1}}},
		{"e1", "tanko", "", 2, 800, 1, nil},
		{"a1", "rookie", "rookie_weapon_lv20", 1, 1400, 0.9, []StatusEffect{{EffectID: "burn", DurationTurn: 1}, {EffectID: "root", DurationTurn: 1}}},
	}
	var players []*BattlePlayerState
	clients := map[string]*ws.Client{}
	for _, s := range specs {
		b := battlestats.Base(s.char)
		players = append(players, &BattlePlayerState{PlayerID: s.id, DisplayName: s.id, TeamID: s.team, CharacterID: s.char,
			WeaponItemID: s.weapon, HP: int(float64(b.HP) * s.hp), MaxHP: b.HP, Defense: b.Defense, MaxMoveEnergy: b.TurnMoveEnergy(),
			Position: Vector2{X: s.x, Y: 300}, IsAlive: true, StatusEffects: append([]StatusEffect{}, s.effects...)})
		clients[s.id] = &ws.Client{PlayerID: s.id, Send: make(chan ws.Message, 512)}
	}
	conns := map[string]*ws.Client{}
	for id, c := range clients {
		conns[id] = c
	}
	m := NewMatch("bloom-fixture", "r", "pvp_2v2", "grassland_valley", players, conns, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.State.CurrentPlayerID = "flora"
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	// startTurn reset the effects' move energy only; the HP and effects stay as given
	before := map[string]*BattlePlayerState{}
	for id, p := range m.State.Players {
		cp := *p
		cp.StatusEffects = append([]StatusEffect{}, p.StatusEffects...)
		before[id] = &cp
	}
	c := skillFixtureCase{MapID: m.State.MapID, Shooter: "flora", Players: before, Terrain: m.Terrain.Snapshot()}
	m.State.Players["flora"].SkillEnergy = 100
	send(m, clients["flora"], "Shoot", ShootAction{Angle: NoAimAngle, Power: NoAimPower, ActionMode: "skill"})
	for msg := range clients["flora"].Send {
		if msg.Event == "ShotResolved" {
			c.Shot = msg.Data
			c.After = m.Terrain.Snapshot()
			break
		}
	}
	return c
}

// TestExportBloomFixture writes bloomFixture as the Godot client's bloom fixture
// (tests/fixtures/skill_bloom.json: its replay test and preview). It runs only when
// BLOOM_FIXTURE_OUT names the output file.
func TestExportBloomFixture(t *testing.T) {
	out := os.Getenv("BLOOM_FIXTURE_OUT")
	if out == "" {
		t.Skip("set BLOOM_FIXTURE_OUT to export the client bloom fixture")
	}
	data, err := json.MarshalIndent(map[string]skillFixtureCase{"bloom": bloomFixture(t)}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
