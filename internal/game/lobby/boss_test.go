package lobby

import (
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
)

// A boss lobby holds its mode's party size (4): four players join through invites, a fifth
// invite is LOBBY_FULL; StartQueue enqueues in the boss queue; a 4-player lobby cannot switch
// to a 2v2 queue (PARTY_TOO_LARGE), and a 2v2 lobby still stops at 2.
func TestBossLobbyPartySize(t *testing.T) {
	gamedata.UseForTest(t, gamedata.Current().WithBosses(gamedata.DefaultBosses()).WithModes([]gamedata.GameModeConfig{
		{ModeID: "boss_steel_robot", Kind: "lobby", Queue: "boss:steel_robot", PartySize: 4, Enabled: true},
		{ModeID: "ranked", Kind: "lobby", Queue: matchmaker.RankedQueue, PartySize: 2, Enabled: true},
	}))
	p := newParty(t, time.Minute)
	q := &fakeQueuer{}
	p.hub.matchmaker = q
	host := p.online("ph")
	p.send(host, "CreateLobby", CreateLobbyPayload{Queue: "boss:steel_robot"})
	var st LobbyState
	p.expect(host, "LobbyUpdated", &st)
	if st.Queue != "boss:steel_robot" {
		t.Fatalf("lobby %+v", st)
	}
	for _, id := range []string{"p1", "p2", "p3"} {
		c := p.online(id)
		inv := p.invite(host, c)
		if inv.Mode != "boss:steel_robot" {
			t.Fatalf("invite mode %q", inv.Mode)
		}
		p.send(c, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
		p.expect(c, "LobbyUpdated", &st)
		p.send(c, "SetReady", SetReadyPayload{Ready: true})
	}
	if len(st.Members) != 4 {
		t.Fatalf("%d members", len(st.Members))
	}
	fifth := p.online("p5")
	p.send(host, "InviteToLobby", InvitePayload{PlayerID: fifth.PlayerID})
	if code := p.errorCode(host); code != "LOBBY_FULL" {
		t.Fatalf("fifth invite: %s", code)
	}
	p.send(host, "SetLobbyQueue", SetLobbyQueuePayload{Queue: matchmaker.RankedQueue})
	if code := p.errorCode(host); code != "PARTY_TOO_LARGE" {
		t.Fatalf("4 players to ranked: %s", code)
	}
	p.send(host, "StartQueue", struct{}{})
	p.expect(host, "QueueStarted", nil)
	if got := q.enqueued(); len(got) != 1 || got[0] != "boss:steel_robot" {
		t.Fatalf("enqueued %v", got)
	}

	// a 2v2 lobby still holds 2
	h2, a, b := p.online("qh"), p.online("qa"), p.online("qb")
	p.host(h2)
	inv := p.invite(h2, a)
	p.send(a, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	p.expect(a, "LobbyUpdated", nil)
	p.send(h2, "InviteToLobby", InvitePayload{PlayerID: b.PlayerID})
	if code := p.errorCode(h2); code != "LOBBY_FULL" {
		t.Fatalf("third in ranked: %s", code)
	}
}
