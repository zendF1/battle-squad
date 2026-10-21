package match

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// skillPlayer is one player of flatSkillMatch.
type skillPlayer struct {
	id   string
	team int
	char string
	x    float64
}

// flatSkillMatch is a match on identityMatch's flat ground (400 px thick, surface at
// identitySurface) with the configured game data, base stats, no wind, the first player to
// play and every client drained.
func flatSkillMatch(t *testing.T, specs ...skillPlayer) (*Match, map[string]*ws.Client) {
	t.Helper()
	useConfigGameData(t)
	tiles := make([][]int, 56)
	for r := range tiles {
		tiles[r] = make([]int, 100)
		if r >= 31 {
			for c := range tiles[r] {
				tiles[r][c] = 1
			}
		}
	}
	var players []*BattlePlayerState
	clients := map[string]*ws.Client{}
	for _, sp := range specs {
		b := battlestats.Base(sp.char)
		players = append(players, &BattlePlayerState{PlayerID: sp.id, TeamID: sp.team, CharacterID: sp.char, HP: b.HP, MaxHP: b.HP,
			Defense: b.Defense, MaxMoveEnergy: b.TurnMoveEnergy(), Position: Vector2{X: sp.x}, IsAlive: true, StatusEffects: []StatusEffect{}})
		clients[sp.id] = &ws.Client{PlayerID: sp.id, Send: make(chan ws.Message, 512)}
	}
	conns := map[string]*ws.Client{}
	for id, c := range clients {
		conns[id] = c
	}
	m := NewMatch("m-skill", "r1", "pvp_2v2", "skill_test", players, conns, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, used map[string][]string) (map[string]RewardResult, error) {
		return map[string]RewardResult{}, nil
	}
	m.Terrain = NewTerrainWithBricks(gamedata.MapConfig{
		MapID: "skill_test", GridWidth: 100, GridHeight: 56, CellSize: botTestCell, Tiles: tiles,
	}, nil)
	for _, p := range m.State.Players {
		p.Position.Y = m.Terrain.SpawnY(p.Position.X, 0)
	}
	order := []string{}
	for _, sp := range specs {
		order = append(order, sp.id)
	}
	m.State.TurnOrder = order
	m.State.CurrentPlayerID = specs[0].id
	m.startTurn(context.Background())
	m.State.Wind = WindState{}
	for _, c := range clients {
		drain(c)
	}
	return m, clients
}

// fireSkill fires p1's skill at full energy and returns the ShotResolved.
func fireSkill(t *testing.T, m *Match, clients map[string]*ws.Client, shooter string, angle, power float64) ShotResolved {
	t.Helper()
	m.State.Players[shooter].SkillEnergy = 100
	send(m, clients[shooter], "Shoot", ShootAction{Angle: angle, Power: power, ActionMode: "skill"})
	s := shotResolved(t, clients[shooter])
	assertTimeline(t, s)
	if s.ActionMode != "skill" || m.State.Players[shooter].SkillEnergy != 0 || s.SkillEnergy != 0 {
		t.Fatalf("skill not fired: %+v, energy %d", s, m.State.Players[shooter].SkillEnergy)
	}
	return s
}

func totalDamage(s ShotResolved, playerID string) (total int) {
	for _, e := range eventsOf(s, "damage", playerID) {
		total += e.Amount
	}
	return total
}

func hasStatus(s ShotResolved, playerID, effectID string) bool {
	for _, e := range eventsOf(s, "status", playerID) {
		if e.EffectID == effectID {
			return true
		}
	}
	return false
}

func TestWeaponSkillRank(t *testing.T) {
	for id, want := range map[string]int{
		"": 0, "rookie_weapon_lv10": 1, "tanko_weapon_lv20": 2, "spark_weapon_lv30": 3, "flora_weapon_lv40": 4,
		"normal_weapon_lv10": 1, "normal_weapon_lv40": 4, "crafted_weapon_silver": 2, "crafted_weapon_gold": 3,
		"crafted_weapon_titan": 4, "crafted_weapon_diamond": 4, "mystery": 0, "rookie_weapon_lv50": 0,
	} {
		if got := WeaponSkillRank(id); got != want {
			t.Errorf("WeaponSkillRank(%q) = %d, want %d", id, got, want)
		}
	}
	// NewMatch derives it from the weapon item; it is in the players payload
	p := &BattlePlayerState{PlayerID: "a", TeamID: 1, WeaponItemID: "crafted_weapon_gold", IsAlive: true}
	m := NewMatch("m", "r", "pvp_1v1", "none", []*BattlePlayerState{p, {PlayerID: "b", TeamID: 2, IsAlive: true}}, map[string]*ws.Client{}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	raw, _ := json.Marshal(m.State.Players["a"])
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if m.State.Players["a"].SkillRank != 3 || out["skillRank"] != 3.0 || out["shield"] != 0.0 {
		t.Fatalf("skill rank %d, payload %v", m.State.Players["a"].SkillRank, out)
	}
}

func TestSkillCapClampsPerTarget(t *testing.T) {
	a := &BattlePlayerState{PlayerID: "a"}
	b := &BattlePlayerState{PlayerID: "b"}
	c := newSkillCap(func(*BattlePlayerState) int { return 100 })
	if c.clamp(a, 60) != 60 || c.clamp(a, 60) != 40 || c.clamp(a, 60) != 0 || c.clamp(b, 70) != 70 {
		t.Fatal("cap does not limit the total per target")
	}
	var none *skillCap
	if none.clamp(a, 999) != 999 {
		t.Fatal("nil cap limits")
	}
	if got := capOfBase(1200)(&BattlePlayerState{Defense: 600}); got != 600 {
		t.Fatalf("cap of 1200 against defense 600 = %d, want 600", got)
	}
}

func TestShieldAbsorbsDamageFirst(t *testing.T) {
	p := &BattlePlayerState{HP: 1000, Shield: 300, IsAlive: true}
	if hurtPlayer(p, 200) || p.Shield != 100 || p.HP != 1000 {
		t.Fatalf("shield %d hp %d", p.Shield, p.HP)
	}
	if hurtPlayer(p, 300) || p.Shield != 0 || p.HP != 800 {
		t.Fatalf("shield %d hp %d", p.Shield, p.HP)
	}
	// A shot: the damage event carries the shield after the hit
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 415})
	target := m.State.Players["p2"]
	target.Shield = 100
	send(m, clients["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	s := shotResolved(t, clients["p1"])
	d := eventsOf(s, "damage", "p2")
	if len(d) == 0 || d[0].Shield == nil || *d[0].Shield != 0 || target.HP != 1500-(d[0].Amount-100) {
		t.Fatalf("damage %+v, hp %d", d, target.HP)
	}
}

// startTurnOf makes id the current player and starts its turn, returning the messages sent.
func startTurnOf(m *Match, clients map[string]*ws.Client, id string) []ws.Message {
	for _, c := range clients {
		drain(c)
	}
	m.State.CurrentPlayerID = id
	m.startTurn(context.Background())
	var msgs []ws.Message
	c := clients[id]
	for {
		select {
		case msg := <-c.Send:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

func messagesOf(msgs []ws.Message, event string) []json.RawMessage {
	var out []json.RawMessage
	for _, m := range msgs {
		if m.Event == event {
			out = append(out, m.Data)
		}
	}
	return out
}

// TestBurnTicksAtTurnStart: 10% of max HP, shield first, sent as PlayerDamaged type burn.
func TestBurnTicksAtTurnStart(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 1300})
	p := m.State.Players["p2"]
	p.Shield = 50
	p.StatusEffects = []StatusEffect{{EffectID: "burn", DurationTurn: 1}}
	msgs := startTurnOf(m, clients, "p2")
	dmg := messagesOf(msgs, "PlayerDamaged")
	if len(dmg) != 1 {
		t.Fatalf("PlayerDamaged %d", len(dmg))
	}
	var rows []map[string]any
	_ = json.Unmarshal(dmg[0], &rows)
	if rows[0]["type"] != "burn" || rows[0]["damage"] != 150.0 || rows[0]["shield"] != 0.0 || rows[0]["hp"] != 1400.0 {
		t.Fatalf("burn payload %v", rows)
	}
	if p.HP != 1400 || p.Shield != 0 || m.State.CurrentPlayerID != "p2" {
		t.Fatalf("hp %d shield %d current %s", p.HP, p.Shield, m.State.CurrentPlayerID)
	}
	// It lasts one turn: gone after p2's turn ends
	m.endTurn(context.Background())
	if HasEffect(p, "burn") {
		t.Fatal("burn still on after the target's turn")
	}
}

func TestBurnCanKillAndEndTheMatch(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 1300})
	p := m.State.Players["p2"]
	p.HP = 100
	p.StatusEffects = []StatusEffect{{EffectID: "burn", DurationTurn: 1}}
	msgs := startTurnOf(m, clients, "p2")
	if p.IsAlive || m.State.Status != "ended" || len(messagesOf(msgs, "MatchEnded")) != 1 {
		t.Fatalf("alive %v status %s", p.IsAlive, m.State.Status)
	}
}

func TestBurnKillPassesTheTurn(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 300}, skillPlayer{"p2", 2, "rookie", 700},
		skillPlayer{"p3", 1, "rookie", 1000}, skillPlayer{"p4", 2, "rookie", 1300})
	p := m.State.Players["p2"]
	p.HP = 10
	p.StatusEffects = []StatusEffect{{EffectID: "burn", DurationTurn: 1}}
	startTurnOf(m, clients, "p2")
	if p.IsAlive || m.State.Status != "in_progress" || m.State.CurrentPlayerID != "p3" {
		t.Fatalf("alive %v status %s current %s", p.IsAlive, m.State.Status, m.State.CurrentPlayerID)
	}
}

// TestRootBlocksMoveAndTeleportNotShooting
func TestRootBlocksMoveAndTeleportNotShooting(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 1300})
	p := m.State.Players["p2"]
	p.Items = []string{"teleport"}
	p.StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}}
	msgs := startTurnOf(m, clients, "p2")
	var started map[string]any
	_ = json.Unmarshal(messagesOf(msgs, "TurnStarted")[0], &started)
	if p.MoveEnergy != 0 || started["moveEnergy"] != 0.0 || started["skipped"] != nil {
		t.Fatalf("rooted move energy %d, TurnStarted %v", p.MoveEnergy, started)
	}
	pos := p.Position
	send(m, clients["p2"], "Move", MoveAction{Direction: "left", TargetX: pos.X - 50})
	item := "teleport"
	send(m, clients["p2"], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "item", ItemID: &item})
	if p.Position != pos || len(p.Items) != 1 || m.resolving {
		t.Fatalf("rooted player moved or teleported: %v items %v", p.Position, p.Items)
	}
	send(m, clients["p2"], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "weapon"})
	if p.ShotsFired != 1 || !m.resolving {
		t.Fatal("rooted player could not shoot")
	}
}

// TestStunAndFreezeSkipTheTurn: no move, shot or item; TurnStarted says skipped; the server
// ends the turn after skipTurnDelay.
func TestStunAndFreezeSkipTheTurn(t *testing.T) {
	for _, effect := range []string{"stun", "freeze"} {
		m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 1300})
		p := m.State.Players["p2"]
		p.Items = []string{"medkit"}
		p.HP = 500
		p.SkillEnergy = 100
		p.StatusEffects = []StatusEffect{{EffectID: effect, DurationTurn: 1}}
		msgs := startTurnOf(m, clients, "p2")
		var started map[string]any
		_ = json.Unmarshal(messagesOf(msgs, "TurnStarted")[0], &started)
		if started["skipped"] != effect || !m.resolving {
			t.Fatalf("%s: TurnStarted %v resolving %v", effect, started, m.resolving)
		}
		m.resolving = false // the checks below must be refused for the effect, not the lock
		pos := p.Position
		send(m, clients["p2"], "Move", MoveAction{Direction: "left", TargetX: pos.X - 50})
		send(m, clients["p2"], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "weapon"})
		send(m, clients["p2"], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "skill"})
		send(m, clients["p2"], "UseItem", UseItemAction{ItemID: "medkit"})
		if p.Position != pos || p.ShotsFired != 0 || p.HP != 500 || len(p.Items) != 1 {
			t.Fatalf("%s: acted while skipped", effect)
		}
		m.resolving = true
		select {
		case ev := <-m.Events:
			m.handleEvent(ev)
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: turn never auto-ended", effect)
		}
		if m.State.CurrentPlayerID != "p1" || HasEffect(p, effect) {
			t.Fatalf("%s: current %s effects %+v", effect, m.State.CurrentPlayerID, p.StatusEffects)
		}
	}
}

// testDoubleHit is a test skill: two projectiles straight down at the shooter's feet.
const testDoubleHit = "test_double_hit"

func init() {
	registerSkill(testDoubleHit, func(m *Match, s *skillShot) {
		for i := 0; i < 2; i++ {
			resolveSingleProjectile(m, s)
		}
	})
}

// TestHitEnergyOncePerEnemyAction: one action hitting an enemy several times gives it +10
// once; capped at 100; the shooter itself and dead players get nothing.
func TestHitEnergyOncePerEnemyAction(t *testing.T) {
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 415})
	gamedata.Data.Skills["triple_shot"] = gamedata.SkillConfig{SkillID: "triple_shot", EffectType: testDoubleHit, DamageMultiplier: 0.2}
	enemy := m.State.Players["p2"]
	enemy.SkillEnergy = 40
	s := fireSkill(t, m, clients, "p1", 90, 10)
	if hits := len(eventsOf(s, "damage", "p2")); hits < 2 {
		t.Fatalf("enemy hit %d times; the test needs several", hits)
	}
	if enemy.SkillEnergy != 50 || s.SkillEnergies["p2"] != 50 || s.SkillEnergies["p1"] != 0 {
		t.Fatalf("enemy energy %d, payload %v", enemy.SkillEnergy, s.SkillEnergies)
	}

	m2, clients2 := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 415})
	m2.State.Players["p2"].SkillEnergy = 95
	send(m2, clients2["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	s2 := shotResolved(t, clients2["p1"])
	if m2.State.Players["p2"].SkillEnergy != 100 || s2.SkillEnergies["p2"] != 100 {
		t.Fatalf("capped energy %d", m2.State.Players["p2"].SkillEnergy)
	}

	// Self damage (falling into the own crater) and a killed enemy give nothing
	m3, clients3 := flatSkillMatch(t, skillPlayer{"p1", 1, "tanko", 400}, skillPlayer{"p2", 2, "rookie", 415})
	m3.State.Players["p2"].HP = 1
	m3.State.Players["p2"].SkillEnergy = 30
	m3.State.Players["p1"].SkillEnergy = 30
	send(m3, clients3["p1"], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	shotResolved(t, clients3["p1"])
	if m3.State.Players["p2"].SkillEnergy != 30 || m3.State.Players["p1"].SkillEnergy != 30 {
		t.Fatalf("energy dead %d self %d", m3.State.Players["p2"].SkillEnergy, m3.State.Players["p1"].SkillEnergy)
	}
}

// TestCleanseOnlyBadEffects
func TestCleanseOnlyBadEffects(t *testing.T) {
	p := &BattlePlayerState{StatusEffects: []StatusEffect{{EffectID: "burn"}, {EffectID: "power_shot"}, {EffectID: "freeze"},
		{EffectID: "drill_bomb"}, {EffectID: "root"}, {EffectID: "net"}, {EffectID: "stun"}}}
	removed := Cleanse(p)
	if len(removed) != 5 || len(p.StatusEffects) != 2 || p.StatusEffects[0].EffectID != "power_shot" || p.StatusEffects[1].EffectID != "drill_bomb" {
		t.Fatalf("removed %v kept %+v", removed, p.StatusEffects)
	}
}

func botSkillMatch(t *testing.T, specs ...skillPlayer) (*Match, *BattlePlayerState) {
	m, _ := flatSkillMatch(t, specs...)
	bot := m.State.Players[specs[0].id]
	bot.IsBot = true
	bot.SkillEnergy = 100
	m.BotTier = perfectAim
	return m, bot
}
