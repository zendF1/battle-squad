package match

import (
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
