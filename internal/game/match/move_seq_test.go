package match

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"battle-squad/internal/game/ws"
)

func nextPlayerMoved(t *testing.T, c *ws.Client) map[string]any {
	t.Helper()
	select {
	case msg := <-c.Send:
		if msg.Event != "PlayerMoved" {
			t.Fatalf("got %s, want PlayerMoved", msg.Event)
		}
		var data map[string]any
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			t.Fatal(err)
		}
		return data
	case <-time.After(time.Second):
		t.Fatal("no PlayerMoved")
		return nil
	}
}

func TestPlayerMovedEchoesMoveSeq(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.Position.Y = m.Terrain.GetLandingY(p.Position.X, 0)
	p.MoveEnergy = 100
	watcher := clients[m.other(cur)]
	drain(watcher)

	send(m, clients[cur], "Move", MoveAction{TargetX: p.Position.X + 20, Seq: 7})
	if got := nextPlayerMoved(t, watcher)["seq"]; got != float64(7) {
		t.Fatalf("seq = %v, want 7", got)
	}

	send(m, clients[cur], "Move", MoveAction{TargetX: p.Position.X + 20})
	if data := nextPlayerMoved(t, watcher); data["seq"] != nil {
		t.Fatalf("seq = %v, want absent for a Move without seq", data["seq"])
	}
}

// reconnectLastMoveSeq sends Reconnect for c and returns the lastMoveSeq of its MatchStateSync.
func reconnectLastMoveSeq(t *testing.T, m *Match, c *ws.Client) any {
	t.Helper()
	drain(c)
	send(m, c, "Reconnect", nil)
	select {
	case msg := <-c.Send:
		if msg.Event != "MatchStateSync" {
			t.Fatalf("got %s, want MatchStateSync", msg.Event)
		}
		var data map[string]any
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			t.Fatal(err)
		}
		if _, ok := data["lastMoveSeq"]; !ok {
			t.Fatal("MatchStateSync without lastMoveSeq")
		}
		return data["lastMoveSeq"]
	case <-time.After(time.Second):
		t.Fatal("no MatchStateSync")
		return nil
	}
}

func TestMatchStateSyncLastMoveSeq(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	opp := m.other(cur)
	p := m.State.Players[cur]
	p.Position.Y = m.Terrain.GetLandingY(p.Position.X, 0)
	p.MoveEnergy = 100

	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(0) {
		t.Fatalf("before any Move: lastMoveSeq = %v, want 0", got)
	}

	send(m, clients[cur], "Move", MoveAction{TargetX: p.Position.X + 20, Seq: 3})
	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(3) {
		t.Fatalf("after an applied Move: lastMoveSeq = %v, want 3", got)
	}

	// Dropped by the rate limit (no lastAction reset): no PlayerMoved, but the seq counts.
	send(m, clients[cur], "Move", MoveAction{TargetX: p.Position.X + 20, Seq: 4})
	drain(clients[cur])
	x := p.Position.X
	raw, _ := json.Marshal(MoveAction{TargetX: x + 20, Seq: 5})
	m.handleEvent(matchEvent{client: clients[cur], msg: ws.Message{Event: "Move", Data: raw}, ctx: context.Background()})
	if p.Position.X != x {
		t.Fatal("rate-limited Move was applied")
	}
	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(5) {
		t.Fatalf("after a rate-limited Move: lastMoveSeq = %v, want 5", got)
	}

	// Dropped while the turn's shot resolves: counts too.
	m.resolving = true
	send(m, clients[cur], "Move", MoveAction{TargetX: x + 20, Seq: 6})
	m.resolving = false
	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(6) {
		t.Fatalf("after a Move dropped while resolving: lastMoveSeq = %v, want 6", got)
	}

	// A Move without seq leaves the record; another player's Move is not recorded.
	send(m, clients[cur], "Move", MoveAction{TargetX: x})
	send(m, clients[opp], "Move", MoveAction{TargetX: x, Seq: 9})
	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(6) {
		t.Fatalf("lastMoveSeq = %v, want 6", got)
	}
	if got := reconnectLastMoveSeq(t, m, clients[opp]); got != float64(0) {
		t.Fatalf("other player's lastMoveSeq = %v, want 0", got)
	}

	m.endTurn(context.Background())
	if got := reconnectLastMoveSeq(t, m, clients[cur]); got != float64(0) {
		t.Fatalf("after the turn ended: lastMoveSeq = %v, want 0", got)
	}
}
