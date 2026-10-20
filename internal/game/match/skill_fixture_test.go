package match

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/ws"
)

// skillFixtureCase is one skill fired on a real map: the players and terrain before the shot
// and the ShotResolved the server sent, and the terrain after it.
type skillFixtureCase struct {
	MapID   string                        `json:"mapId"`
	Shooter string                        `json:"shooter"`
	Players map[string]*BattlePlayerState `json:"players"`
	Terrain TerrainSnapshot               `json:"terrain"`
	After   TerrainSnapshot               `json:"terrainAfter"` // after the shot's craters
	Shot    json.RawMessage               `json:"shot"`
}

// TestExportSkillFixture writes the four skills at rank 4 (dragon guns) on grassland_valley as
// the Godot client's timeline fixture (tests/fixtures/skill_shots.json; also used by its
// preview). It runs only when SKILL_FIXTURE_OUT names the output file.
func TestExportSkillFixture(t *testing.T) {
	out := os.Getenv("SKILL_FIXTURE_OUT")
	if out == "" {
		t.Skip("set SKILL_FIXTURE_OUT to export the client skill fixture")
	}
	useConfigGameData(t)
	type spec struct {
		id, char, weapon string
		team             int
		x                float64
		hp               float64 // share of max HP
		effects          []StatusEffect
	}
	build := func(specs ...spec) (*Match, map[string]*ws.Client) {
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
		m := NewMatch("fixture", "r", "pvp_2v2", "grassland_valley", players, conns, nil, nil, nil, nil)
		t.Cleanup(m.Stop)
		m.processRewards = func(context.Context, map[string]*PlayerStats, map[string][]string) (map[string]RewardResult, error) {
			return map[string]RewardResult{}, nil
		}
		m.State.CurrentPlayerID = specs[0].id
		m.startTurn(context.Background())
		m.State.Wind = WindState{}
		for _, c := range clients {
			drain(c)
		}
		return m, clients
	}
	fire := func(m *Match, clients map[string]*ws.Client, shooter string, angle, power float64) skillFixtureCase {
		players := map[string]*BattlePlayerState{}
		for id, p := range m.State.Players {
			cp := *p
			cp.StatusEffects = append([]StatusEffect{}, p.StatusEffects...)
			players[id] = &cp
		}
		c := skillFixtureCase{MapID: m.State.MapID, Shooter: shooter, Players: players, Terrain: m.Terrain.Snapshot()}
		m.State.Players[shooter].SkillEnergy = 100
		send(m, clients[shooter], "Shoot", ShootAction{Angle: angle, Power: power, ActionMode: "skill"})
		for {
			msg := <-clients[shooter].Send
			if msg.Event == "ShotResolved" {
				c.Shot = msg.Data
				c.After = m.Terrain.Snapshot()
				return c
			}
		}
	}
	aim := func(m *Match, shooter string) (float64, float64) {
		p := m.State.Players[shooter]
		w, _ := botWeapon(p)
		best := NewBotBrain(perfectAim).bestShot(m, p, p.Position, w)
		return best.angle, best.power
	}

	cases := map[string]skillFixtureCase{}

	m, cl := build(spec{"rookie", "rookie", "rookie_weapon_lv40", 1, 450, 1, nil},
		spec{"e1", "tanko", "", 2, 1050, 1, nil}, spec{"e2", "spark", "", 2, 1150, 1, nil})
	a, pw := aim(m, "rookie")
	cases["rain"] = fire(m, cl, "rookie", a, pw)

	m, cl = build(spec{"tanko", "tanko", "tanko_weapon_lv40", 1, 450, 1, nil},
		spec{"e1", "rookie", "", 2, 1050, 1, nil})
	a, pw = aim(m, "tanko")
	cases["chain"] = fire(m, cl, "tanko", a, pw)

	m, cl = build(spec{"spark", "spark", "spark_weapon_lv40", 1, 250, 1, nil},
		spec{"e1", "rookie", "", 2, 800, 1, nil}, spec{"e2", "tanko", "", 2, 1350, 1, nil})
	angle, _ := bestLaser(m, m.State.Players["spark"])
	cases["laser"] = fire(m, cl, "spark", angle, 50)

	m, cl = build(spec{"flora", "flora", "flora_weapon_lv40", 1, 600, 0.45, []StatusEffect{{EffectID: "root", DurationTurn: 1}}},
		spec{"e1", "rookie", "", 2, 1300, 1, nil},
		spec{"a1", "rookie", "rookie_weapon_lv20", 1, 880, 0.9, []StatusEffect{{EffectID: "burn", DurationTurn: 1}}})
	cases["bloom"] = fire(m, cl, "flora", NoAimAngle, NoAimPower)

	data, err := json.MarshalIndent(cases, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for k, c := range cases {
		var s ShotResolved
		_ = json.Unmarshal(c.Shot, &s)
		t.Logf("%s: %d projectiles, %d events, %.2f s", k, len(s.Projectiles), len(s.Events), s.Duration)
	}
}
