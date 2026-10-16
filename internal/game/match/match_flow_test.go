package match

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// newFlowMatch builds a 1v1 match on the fallback terrain with one connected
// client per player, and starts the first turn.
func newFlowMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	t.Helper()
	setFlowGameData()

	players := []*BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 200, Y: 300}, IsAlive: true},
		{PlayerID: "p2", TeamID: 2, CharacterID: "c1", HP: 1500, MaxHP: 1500, Position: Vector2{X: 1400, Y: 300}, IsAlive: true},
	}
	clients := map[string]*ws.Client{
		"p1": {PlayerID: "p1", Send: make(chan ws.Message, 256)},
		"p2": {PlayerID: "p2", Send: make(chan ws.Message, 256)},
	}
	m := NewMatch("m1", "r1", "pvp_1v1", "none", players, map[string]*ws.Client{"p1": clients["p1"], "p2": clients["p2"]}, nil, nil, nil, nil)
	t.Cleanup(m.Stop)
	m.startTurn(context.Background())
	return m, clients
}

// setFlowGameData installs the minimal game data the flow matches use.
func setFlowGameData() {
	gamedata.Data = &gamedata.GameData{
		Maps: map[string]gamedata.MapConfig{},
		Characters: map[string]gamedata.CharacterConfig{
			"c1": {CharacterID: "c1", WeaponID: "w1"},
		},
		Weapons: map[string]gamedata.WeaponConfig{
			"w1": {WeaponID: "w1", Damage: 240, ExplosionRadius: 30, ProjectileWeight: 1, WindInfluence: 1},
		},
		Skills: map[string]gamedata.SkillConfig{},
		Items:  map[string]gamedata.ItemConfig{},
	}
}

func (m *Match) other(playerID string) string {
	for id := range m.State.Players {
		if id != playerID {
			return id
		}
	}
	return ""
}

// send delivers a client message to the match loop handler, bypassing the rate limit.
func send(m *Match, c *ws.Client, event string, data any) {
	delete(m.lastAction, c.PlayerID)
	raw, _ := json.Marshal(data)
	m.handleEvent(matchEvent{client: c, msg: ws.Message{Event: event, Data: raw}, ctx: context.Background()})
}

func TestClientCannotSendInternalEvents(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	opp := clients[m.other(cur)]

	send(m, opp, "__delayed_end_turn", nil)
	send(m, opp, "__teleport_complete", map[string]any{"playerID": opp.PlayerID, "targetX": 10.0, "targetY": 10.0})

	if m.State.CurrentPlayerID != cur {
		t.Fatalf("turn changed by client-sent internal event")
	}
	if pos := m.State.Players[opp.PlayerID].Position; pos.X == 10 && pos.Y == 10 {
		t.Fatalf("player teleported by client-sent internal event")
	}
}

func TestShotLocksTurnAndStaleEndTurnIsIgnored(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	c := clients[cur]

	send(m, c, "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	if m.State.Players[cur].ShotsFired != 1 || !m.resolving {
		t.Fatalf("first shot not resolved: shots=%d resolving=%v", m.State.Players[cur].ShotsFired, m.resolving)
	}

	send(m, c, "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	send(m, c, "EndTurn", nil)
	if m.State.Players[cur].ShotsFired != 1 {
		t.Fatalf("second shot accepted while resolving")
	}
	if m.State.CurrentPlayerID != cur {
		t.Fatalf("EndTurn accepted while resolving")
	}

	var ev matchEvent
	select {
	case ev = <-m.Events:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled end turn never arrived")
	}

	// Turn moved on before the callback ran: it must not end the new turn
	m.endTurn(context.Background())
	next := m.State.CurrentPlayerID
	m.handleEvent(ev)
	if m.State.CurrentPlayerID != next {
		t.Fatalf("stale callback ended the next player's turn")
	}
}

func TestShotPowerIsBounded(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID

	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 1e9, ActionMode: "weapon"})
	if m.State.Players[cur].ShotsFired != 0 {
		t.Fatalf("out-of-range power was simulated")
	}
}

func TestShotModifierItemUsedViaShoot(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.Items = []string{"power_shot", "medkit"}

	item := "power_shot"
	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "item", ItemID: &item})

	if p.ShotsFired != 1 {
		t.Fatalf("shot with item not fired")
	}
	if len(p.Items) != 1 || p.Items[0] != "medkit" {
		t.Fatalf("items = %v, want [medkit]", p.Items)
	}
	if len(p.UsedItems) != 1 || p.UsedItems[0] != "power_shot" {
		t.Fatalf("used items = %v, want [power_shot]", p.UsedItems)
	}
}

func TestImmediateItemRejectedViaShoot(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.Items = []string{"medkit"}

	item := "medkit"
	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "item", ItemID: &item})

	if p.ShotsFired != 0 || len(p.Items) != 1 || len(p.UsedItems) != 0 {
		t.Fatalf("medkit via Shoot was accepted: shots=%d items=%v used=%v", p.ShotsFired, p.Items, p.UsedItems)
	}
}

func TestNetAndFreezeLastThroughTargetTurn(t *testing.T) {
	m, _ := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	target := m.State.Players[m.other(cur)]
	ctx := context.Background()

	ApplyStatusEffect(target, StatusEffect{EffectID: "net", TargetPlayerID: target.PlayerID, DurationTurn: 1})
	m.endTurn(ctx) // target's turn
	if target.MoveEnergy != netMoveEnergyCap || !HasEffect(target, "net") {
		t.Fatalf("net not active on target turn: energy=%d", target.MoveEnergy)
	}
	m.endTurn(ctx)
	m.endTurn(ctx) // target's next turn
	if target.MoveEnergy != 100 || HasEffect(target, "net") {
		t.Fatalf("net still active a turn later: energy=%d", target.MoveEnergy)
	}

	m.endTurn(ctx) // shooter's turn
	ApplyStatusEffect(target, StatusEffect{EffectID: "freeze", TargetPlayerID: target.PlayerID, DurationTurn: 1})
	m.endTurn(ctx) // target's turn
	if target.MoveEnergy != 0 || !HasEffect(target, "freeze") {
		t.Fatalf("freeze not active on target turn: energy=%d", target.MoveEnergy)
	}
}

func TestWindStopperExpires(t *testing.T) {
	m, _ := newFlowMatch(t)
	ctx := context.Background()
	cur := m.State.CurrentPlayerID
	m.State.Players[cur].Items = []string{"wind_stopper"}

	if err := ApplyImmediateItem(ctx, &m.State, cur, "wind_stopper", nil, m.Terrain); err != nil {
		t.Fatal(err)
	}
	m.endTurn(ctx)
	if len(m.State.ActiveEffects) != 1 || m.State.Wind.Power != 0 {
		t.Fatalf("wind stopper should still hold on the next turn: effects=%v wind=%v", m.State.ActiveEffects, m.State.Wind)
	}
	m.endTurn(ctx)
	if len(m.State.ActiveEffects) != 0 {
		t.Fatalf("wind stopper never expired: %v", m.State.ActiveEffects)
	}
}

func TestTurnStartedCarriesSkillEnergy(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	c := clients[cur]
	for len(c.Send) > 0 {
		<-c.Send
	}

	m.endTurn(context.Background())

	for len(c.Send) > 0 {
		msg := <-c.Send
		if msg.Event != "TurnStarted" {
			continue
		}
		var data struct {
			SkillEnergy map[string]int `json:"skillEnergy"`
		}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.SkillEnergy[cur] != 20 {
			t.Fatalf("skillEnergy[%s] = %d, want 20", cur, data.SkillEnergy[cur])
		}
		return
	}
	t.Fatal("no TurnStarted broadcast")
}

func TestUseItemRejectsTeleport(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.Items = []string{"teleport"}
	start := p.Position

	send(m, clients[cur], "UseItem", UseItemAction{ItemID: "teleport", TargetPosition: &Vector2{X: 800, Y: 100}})

	if p.Position != start || len(p.Items) != 1 || len(p.UsedItems) != 0 {
		t.Fatalf("direct teleport accepted: pos=%v items=%v", p.Position, p.Items)
	}
}

func TestFrozenPlayerCannotMove(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	ApplyStatusEffect(p, StatusEffect{EffectID: "freeze", TargetPlayerID: cur, DurationTurn: 1})
	p.MoveEnergy = 100 // even with energy left
	start := p.Position

	send(m, clients[cur], "Move", MoveAction{TargetX: start.X + 40})

	if p.Position != start {
		t.Fatalf("frozen player moved from %v to %v", start, p.Position)
	}
}

func TestIceSlideRequiresMovement(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	m.Terrain.Zones = append(m.Terrain.Zones, TerrainZone{Type: "ice", MinX: 0, MaxX: float64(m.Terrain.Width), MinY: 0, MaxY: float64(m.Terrain.Height)})
	p.MoveEnergy = 0
	start := p.Position

	for i := 0; i < 3; i++ {
		send(m, clients[cur], "Move", MoveAction{TargetX: p.Position.X})
	}

	if p.Position != start {
		t.Fatalf("slid on ice without moving: %v → %v", start, p.Position)
	}
}
