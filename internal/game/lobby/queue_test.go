package lobby

import (
	"context"
	"sync"
	"testing"
	"time"

	"battle-squad/internal/game/matchmaker"
)

// fakeQueuer records the queue each StartQueue enqueues a lobby in.
type fakeQueuer struct {
	mu     sync.Mutex
	queues []string
	items  []map[string][]string // each enqueued lobby's playerItems
}

func (f *fakeQueuer) EnqueueLobby(_ context.Context, queue, lobbyID string, playerIDs []string, _ map[string]int,
	_ map[string]string, items map[string][]string, _ map[string]string) (*matchmaker.QueueEntry, error) {
	if !matchmaker.KnownQueue(queue) {
		return nil, matchmaker.ErrUnknownQueue
	}
	f.mu.Lock()
	f.queues = append(f.queues, queue)
	f.items = append(f.items, items)
	f.mu.Unlock()
	return &matchmaker.QueueEntry{EntryID: "e_" + lobbyID, Queue: queue, LobbyID: lobbyID, PlayerIDs: playerIDs}, nil
}

func (f *fakeQueuer) CancelQueue(context.Context, string) (*matchmaker.QueueEntry, error) {
	return nil, nil
}

func (f *fakeQueuer) GetConfig() matchmaker.MatchmakingConfig {
	return matchmaker.DefaultMatchmakingConfig()
}

func (f *fakeQueuer) enqueued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.queues...)
}

// CreateLobby {queue?}: no queue is ranked_2v2, casual_2v2 is kept, an unknown queue is
// INVALID_QUEUE; the host's CreateLobby on its open lobby switches the queue.
func TestCreateLobbyQueue(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b, c := p.online("pa"), p.online("pb"), p.online("pc")

	if st := p.host(a); st.Queue != matchmaker.RankedQueue {
		t.Fatalf("default queue %q", st.Queue)
	}
	p.send(b, "CreateLobby", CreateLobbyPayload{Queue: matchmaker.CasualQueue})
	var st LobbyState
	p.expect(b, "LobbyUpdated", &st)
	if st.Queue != matchmaker.CasualQueue {
		t.Fatalf("casual lobby %+v", st)
	}
	p.send(c, "CreateLobby", CreateLobbyPayload{Queue: "boss_4p"})
	if code := p.errorCode(c); code != "INVALID_QUEUE" {
		t.Fatalf("unknown queue: %s", code)
	}
	if c.LobbyID != "" {
		t.Fatalf("a lobby was made for an unknown queue: %q", c.LobbyID)
	}

	// The host's lobby is reused, switched to the asked queue
	p.send(b, "CreateLobby", CreateLobbyPayload{Queue: matchmaker.RankedQueue})
	var again LobbyState
	p.expect(b, "LobbyUpdated", &again)
	if again.LobbyID != st.LobbyID || again.Queue != matchmaker.RankedQueue {
		t.Fatalf("reused lobby %+v (was %s)", again, st.LobbyID)
	}
}

// SetLobbyQueue {queue}: host only, while preparing, a queue the matchmaker runs; everyone
// gets LobbyUpdated; the invite names the lobby's queue and StartQueue enqueues in it.
func TestSetLobbyQueue(t *testing.T) {
	p := newParty(t, time.Minute)
	q := &fakeQueuer{}
	p.hub.matchmaker = q
	a, b := p.online("pa"), p.online("pb")
	p.host(a)

	p.send(a, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.CasualQueue})
	var st LobbyState
	p.expect(a, "LobbyUpdated", &st)
	if st.Queue != matchmaker.CasualQueue {
		t.Fatalf("after SetLobbyQueue %+v", st)
	}
	inv := p.invite(a, b)
	if inv.Mode != matchmaker.CasualQueue {
		t.Fatalf("LobbyInvite mode %q", inv.Mode)
	}
	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	p.expect(b, "LobbyUpdated", &st)
	if st.Queue != matchmaker.CasualQueue || len(st.Members) != 2 {
		t.Fatalf("member view %+v", st)
	}
	p.expect(a, "LobbyUpdated", nil)

	p.send(b, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.RankedQueue})
	if code := p.errorCode(b); code != "NOT_HOST" {
		t.Fatalf("member SetLobbyQueue: %s", code)
	}
	p.send(a, "SetLobbyQueue", SetLobbyQueuePayload{Queue: "boss_4p"})
	if code := p.errorCode(a); code != "INVALID_QUEUE" {
		t.Fatalf("unknown queue: %s", code)
	}
	p.send(a, "SetLobbyQueue", struct{}{})
	if code := p.errorCode(a); code != "INVALID_PAYLOAD" {
		t.Fatalf("no queue: %s", code)
	}

	// Ranked and back: both members see each change
	p.send(a, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.RankedQueue})
	p.expect(b, "LobbyUpdated", &st)
	if st.Queue != matchmaker.RankedQueue {
		t.Fatalf("member after ranked %+v", st)
	}
	p.send(a, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.CasualQueue})
	p.expect(b, "LobbyUpdated", &st)
	if st.Queue != matchmaker.CasualQueue {
		t.Fatalf("member after casual %+v", st)
	}

	p.send(b, "SetReady", SetReadyPayload{Ready: true})
	p.send(a, "StartQueue", struct{}{})
	p.expect(a, "QueueStarted", nil)
	if got := q.enqueued(); len(got) != 1 || got[0] != matchmaker.CasualQueue {
		t.Fatalf("enqueued in %v", got)
	}
	p.send(a, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.RankedQueue})
	if code := p.errorCode(a); code != "INVALID_STATUS" {
		t.Fatalf("SetLobbyQueue in the queue: %s", code)
	}
}
