package lobby

import (
	"encoding/json"
	"testing"

	"battle-squad/internal/game/ws"
)

func newTestLobby(t *testing.T, status string, members ...string) (*LobbyHub, *LobbyRoom, map[string]*ws.Client) {
	t.Helper()
	hub := NewLobbyHub(nil, nil, "test")
	state := LobbyState{LobbyID: "l1", HostPlayerID: members[0], Status: status}
	l := NewLobbyRoom("l1", state, hub)
	clients := map[string]*ws.Client{}
	for _, id := range members {
		l.State.Members = append(l.State.Members, LobbyMember{PlayerID: id})
		c := &ws.Client{PlayerID: id, LobbyID: "l1", Send: make(chan ws.Message, 16)}
		clients[id] = c
		l.Clients[id] = c
	}
	hub.lobbies["l1"] = l
	return hub, l, clients
}

func drain(c *ws.Client) []string {
	var out []string
	for {
		select {
		case m := <-c.Send:
			out = append(out, m.Event)
		default:
			return out
		}
	}
}

// MatchFound reaches every lobby client with the payload, hands each to the room
// and moves the lobby to in_match; ReturnToLobby opens it again.
func TestNotifyMatchFoundAndReturn(t *testing.T) {
	hub, l, clients := newTestLobby(t, "in_queue", "p1", "p2")
	joined := map[string]bool{}
	payload := map[string]string{"roomId": "r1"}
	if err := hub.Notify("l1", "MatchFound", payload, func(c *ws.Client) { joined[c.PlayerID] = true; c.RoomID = "r1" }); err != nil {
		t.Fatal(err)
	}
	for id, c := range clients {
		msg := <-c.Send
		var got map[string]string
		if msg.Event != "MatchFound" || json.Unmarshal(msg.Data, &got) != nil || got["roomId"] != "r1" {
			t.Fatalf("%s got %s %s", id, msg.Event, msg.Data)
		}
		if ev := drain(c); len(ev) != 1 || ev[0] != "LobbyUpdated" {
			t.Fatalf("%s then %v", id, ev)
		}
		if !joined[id] {
			t.Fatalf("%s not joined", id)
		}
	}
	if l.State.Status != "in_match" {
		t.Fatalf("status %s", l.State.Status)
	}

	if err := hub.Notify("l1", "ReturnToLobby", map[string]string{"lobbyId": "l1"}, nil); err != nil {
		t.Fatal(err)
	}
	if ev := drain(clients["p1"]); len(ev) != 2 || ev[0] != "ReturnToLobby" || ev[1] != "LobbyUpdated" || l.State.Status != "preparing" {
		t.Fatalf("return: %v status %s", ev, l.State.Status)
	}
	// A lobby no longer waiting for that match ignores its ReturnToLobby
	l.State.Status = "in_queue"
	if err := hub.Notify("l1", "ReturnToLobby", map[string]string{"lobbyId": "l1"}, nil); err != nil {
		t.Fatal(err)
	}
	if ev := drain(clients["p1"]); len(ev) != 0 || l.State.Status != "in_queue" {
		t.Fatalf("stale return: %v status %s", ev, l.State.Status)
	}
	if err := hub.Notify("nope", "MatchFound", payload, nil); err == nil {
		t.Fatal("unknown lobby notified")
	}
}

// A client busy in another room is not moved into the battle room.
func TestNotifySkipsClientsInARoom(t *testing.T) {
	hub, _, clients := newTestLobby(t, "in_queue", "p1")
	clients["p1"].RoomID = "other"
	called := false
	if err := hub.Notify("l1", "MatchFound", map[string]string{}, func(c *ws.Client) { called = true }); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("joined a client already in a room")
	}
}

func TestReuseForHost(t *testing.T) {
	_, l, clients := newTestLobby(t, "preparing", "p1", "p2")
	if !l.reuseForHost(clients["p1"]) || l.reuseForHost(clients["p2"]) {
		t.Fatal("preparing lobby: host reuses, member does not")
	}
	l.State.Status = "in_queue"
	if l.reuseForHost(clients["p1"]) {
		t.Fatal("queued lobby reused")
	}
	l.State.Status = "in_match"
	if l.reuseForHost(clients["p1"]) {
		t.Fatal("party lobby in a match reused")
	}

	_, solo, sc := newTestLobby(t, "in_match", "p1")
	sc["p1"].RoomID = "r1"
	if solo.reuseForHost(sc["p1"]) {
		t.Fatal("reused while still in the match")
	}
	sc["p1"].RoomID = ""
	if !solo.reuseForHost(sc["p1"]) || solo.State.Status != "preparing" {
		t.Fatalf("solo who left the match: status %s", solo.State.Status)
	}
}
