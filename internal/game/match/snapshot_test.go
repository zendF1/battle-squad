package match

import (
	"context"
	"encoding/json"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// snapshotData is a one-character game data on a flat 1600 × 896 map.
func snapshotData() *gamedata.GameData {
	tiles := make([][]int, 56)
	for r := range tiles {
		tiles[r] = make([]int, 100)
		if r >= 31 {
			for c := range tiles[r] {
				tiles[r][c] = 1
			}
		}
	}
	return &gamedata.GameData{
		Maps: map[string]gamedata.MapConfig{"flat": {MapID: "flat", Name: "Flat", GridWidth: 100, GridHeight: 56, CellSize: 16, Tiles: tiles,
			DefaultWindPowerRange: []float64{0, 0}}},
		Characters: map[string]gamedata.CharacterConfig{"c1": {CharacterID: "c1", HP: 1000, WeaponID: "w1", SkillID: "s1"}},
		Weapons:    map[string]gamedata.WeaponConfig{"w1": {WeaponID: "w1", Damage: 300, ExplosionRadius: 40, TerrainDamage: 20, ProjectileWeight: 1, WindInfluence: 1}},
		Skills:     map[string]gamedata.SkillConfig{"s1": {SkillID: "s1", CharacterID: "c1", EffectType: "missile_rain"}},
		Items:      map[string]gamedata.ItemConfig{},
	}
}

func snapshotMatch(t *testing.T, gd *gamedata.Snapshot, id string) (*Match, *ws.Client) {
	t.Helper()
	c := &ws.Client{PlayerID: "p1", Send: make(chan ws.Message, 256)}
	players := []*BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "c1", HP: 1000, MaxHP: 1000, Position: Vector2{X: 400, Y: 0}, IsAlive: true},
		{PlayerID: "p2", TeamID: 2, CharacterID: "c1", HP: 1000, MaxHP: 1000, Position: Vector2{X: 1200, Y: 0}, IsAlive: true},
	}
	m := NewMatchWithData(gd, id, "r1", "pvp_1v1", "flat", players, map[string]*ws.Client{"p1": c}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.State.TurnOrder = []string{"p1", "p2"}
	m.State.CurrentPlayerID = "p1"
	return m, c
}

// A match plays with the game data it was created with for its whole life: a reload that
// publishes other game data (another turn length, gravity, item rule, version) changes only the
// matches created after it. MatchStarted / MatchStateSync carry the match's own version and
// TurnStarted its turn length.
func TestMatchKeepsItsGameDataThroughAReload(t *testing.T) {
	a := gamedata.NewSnapshot(snapshotData())
	a.Version = "aaaaaaaaaaaaaaaa"
	gamedata.UseForTest(t, a)
	m, c := snapshotMatch(t, gamedata.Current(), "m_a")

	// the reload: turn 30 s, heavier gravity, 50 skill energy per turn, another version
	b := gamedata.NewSnapshot(snapshotData())
	b.Physics.TurnTimeSeconds, b.Physics.Gravity = 30, 900
	b.Rules.SkillEnergy.PerTurn = 50
	b.Version = "bbbbbbbbbbbbbbbb"
	gamedata.UseForTest(t, b)

	ctx := context.Background()
	m.startTurn(ctx)
	var started map[string]any
	for _, msg := range drainSent(c) {
		if msg.Event == "TurnStarted" {
			_ = json.Unmarshal(msg.Data, &started)
		}
	}
	if m.State.TurnTimeLeft != 20 || started["turnTimeLeft"] != float64(20) {
		t.Fatalf("turn time %d, TurnStarted %v", m.State.TurnTimeLeft, started["turnTimeLeft"])
	}
	var snap struct {
		GamedataVersion string `json:"gamedataVersion"`
	}
	_ = json.Unmarshal(m.snapshotPayload(), &snap)
	if snap.GamedataVersion != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("MatchStarted gamedataVersion %q", snap.GamedataVersion)
	}
	_ = json.Unmarshal(m.syncPayload("p1"), &snap)
	if snap.GamedataVersion != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("MatchStateSync gamedataVersion %q", snap.GamedataVersion)
	}
	m.endTurn(ctx)
	if e := m.State.Players["p1"].SkillEnergy; e != 20 {
		t.Fatalf("skill energy per turn %d, want the match's 20", e)
	}

	// the same shot in both matches: the old one flies under the old gravity
	shot := func(m *Match) *ProjectileResult {
		p := m.State.Players["p1"]
		w := m.gd.Data.Weapons["w1"]
		return SimulateProjectile("p1", 1, shotOrigin(m.Terrain, p), 60, 70, w, WindState{}, m.Terrain, m.State.Players, 0)
	}
	n, _ := snapshotMatch(t, gamedata.Current(), "m_b")
	n.startTurn(ctx)
	if n.State.TurnTimeLeft != 30 || n.GameData().Version != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("new match turn %d version %q", n.State.TurnTimeLeft, n.GameData().Version)
	}
	if fa, fb := flightEnd(shot(m)), flightEnd(shot(n)); fa <= fb {
		t.Fatalf("flight %v under gravity 445 should outlast %v under 900", fa, fb)
	}
}
