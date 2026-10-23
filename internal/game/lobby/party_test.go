package lobby

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/ranked"
)

// stubStore is the DB of the party tests: player p<N> is "Name<N>" at level N with
// rating ratings[id] (1000 by default), playing rookie.
type stubStore struct {
	ratings map[string]int
	items   []string // every member's saved items (nil: none)
}

func (s stubStore) rating(id string) int {
	if r, ok := s.ratings[id]; ok {
		return r
	}
	return 1000
}

func (s stubStore) Member(_ context.Context, id string) LobbyMember {
	r, tier, div := s.Rating(context.Background(), id)
	items := append([]string{}, s.items...)
	return LobbyMember{PlayerID: id, DisplayName: s.DisplayName(context.Background(), id), CharacterID: "rookie", Items: items,
		Rating: r, Tier: tier, Division: div, Level: 7, Gun: MemberGun{Kind: "bazooka", Tier: gunTierBasic}}
}

func (s stubStore) Look(context.Context, string, string) (int, MemberGun) {
	return 7, MemberGun{Kind: "bazooka", Tier: gunTierBasic}
}

func (s stubStore) Rating(_ context.Context, id string) (int, string, int) {
	r := s.rating(id)
	tier, div := ranked.TierAndDivision(r)
	return r, tier, div
}

func (s stubStore) DisplayName(_ context.Context, id string) string { return "Name_" + id }

func (s stubStore) Players(_ context.Context, ids []string) (map[string]playerCard, error) {
	out := map[string]playerCard{}
	for _, id := range ids {
		r, tier, div := s.Rating(context.Background(), id)
		out[id] = playerCard{OnlinePlayer{PlayerID: id, DisplayName: s.DisplayName(context.Background(), id), Level: 7, Tier: tier,
			Division: div, CharacterID: "rookie"}, r}
	}
	return out, nil
}

type party struct {
	t   *testing.T
	hub *LobbyHub
	h   *WSHandler
}

func newParty(t *testing.T, ttl time.Duration) *party {
	hub := NewLobbyHub(nil, nil, "test")
	hub.store = stubStore{ratings: map[string]int{}}
	hub.inviteTTL = ttl
	return &party{t: t, hub: hub, h: &WSHandler{hub: hub}}
}

// online connects a player.
func (p *party) online(id string) *ws.Client {
	c := &ws.Client{PlayerID: id, Send: make(chan ws.Message, 64)}
	p.hub.SetOnline(c)
	return c
}

func (p *party) send(c *ws.Client, event string, data any) {
	raw, _ := json.Marshal(data)
	p.h.HandleLobbyMessage(context.Background(), c, ws.Message{Event: event, Data: raw})
}

// next is the client's next message other than LobbyUpdated when skipUpdates.
func (p *party) next(c *ws.Client, skipUpdates bool) ws.Message {
	p.t.Helper()
	for {
		select {
		case m := <-c.Send:
			if skipUpdates && m.Event == "LobbyUpdated" {
				continue
			}
			return m
		case <-time.After(2 * time.Second):
			p.t.Fatalf("%s: no message", c.PlayerID)
		}
	}
}

// expect waits for the event (skipping others) and decodes it into out.
func (p *party) expect(c *ws.Client, event string, out any) {
	p.t.Helper()
	for {
		m := p.next(c, false)
		if m.Event == "LobbyError" && event != "LobbyError" {
			p.t.Fatalf("%s: waiting for %s, got %s", c.PlayerID, event, m.Data)
		}
		if m.Event == event {
			if out != nil {
				if err := json.Unmarshal(m.Data, out); err != nil {
					p.t.Fatal(err)
				}
			}
			return
		}
	}
}

// errorCode is the client's next LobbyError code.
func (p *party) errorCode(c *ws.Client) string {
	p.t.Helper()
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	p.expect(c, "LobbyError", &e)
	return e.Error.Code
}

// quiet reports no message for a moment.
func (p *party) quiet(c *ws.Client) {
	p.t.Helper()
	select {
	case m := <-c.Send:
		p.t.Fatalf("%s: unexpected %s %s", c.PlayerID, m.Event, m.Data)
	case <-time.After(30 * time.Millisecond):
	}
}

// host creates a lobby for c and returns its state.
func (p *party) host(c *ws.Client) LobbyState {
	p.t.Helper()
	p.send(c, "CreateLobby", struct{}{})
	var st LobbyState
	p.expect(c, "LobbyUpdated", &st)
	return st
}

// invite sends InviteToLobby from host to invitee and returns the invitee's LobbyInvite.
func (p *party) invite(host, invitee *ws.Client) LobbyInvitePayload {
	p.t.Helper()
	p.send(host, "InviteToLobby", InvitePayload{PlayerID: invitee.PlayerID})
	var sent InviteSentPayload
	p.expect(host, "InviteSent", &sent)
	if sent.PlayerID != invitee.PlayerID || sent.ExpiresInSec < 1 {
		p.t.Fatalf("InviteSent %+v", sent)
	}
	var inv LobbyInvitePayload
	p.expect(invitee, "LobbyInvite", &inv)
	return inv
}

// Invite → accept: the invitee (who hosted a lobby alone) leaves it and joins, not
// ready; both get LobbyUpdated with 2 members; the other invite is cancelled.
func TestInviteAccept(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b, c := p.online("pa"), p.online("pb"), p.online("pc")
	st := p.host(a)
	own := p.host(b)

	inv := p.invite(a, b)
	if inv.LobbyID != st.LobbyID || inv.Mode != "ranked_2v2" || inv.InviteID == "" || inv.ExpiresInSec != 60 ||
		inv.From != (OnlinePlayer{PlayerID: "pa", DisplayName: "Name_pa", Level: 7, Tier: "silver", Division: 3, CharacterID: "rookie", Status: StatusIdle}) {
		t.Fatalf("LobbyInvite %+v", inv)
	}
	invC := p.invite(a, c)

	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	var got LobbyState
	p.expect(b, "LobbyUpdated", &got)
	if got.LobbyID != st.LobbyID || len(got.Members) != 2 || got.Members[1].PlayerID != "pb" || got.Members[1].Ready ||
		!got.Members[0].Ready || got.Members[1].Level != 7 || got.Members[1].Gun != (MemberGun{"bazooka", "basic"}) {
		t.Fatalf("member view %+v", got)
	}
	p.expect(a, "LobbyUpdated", &got)
	if len(got.Members) != 2 {
		t.Fatalf("host view %+v", got)
	}
	if b.LobbyID != st.LobbyID {
		t.Fatalf("invitee LobbyID %q", b.LobbyID)
	}
	if _, err := p.hub.FindLobby(own.LobbyID); err == nil {
		time.Sleep(50 * time.Millisecond) // its goroutine unregisters it
		if _, err := p.hub.FindLobby(own.LobbyID); err == nil {
			t.Fatal("the invitee's own lobby is still there")
		}
	}

	var exp InviteExpiredPayload
	p.expect(c, "InviteExpired", &exp)
	if exp.InviteID != invC.InviteID {
		t.Fatalf("cancelled invite %+v", exp)
	}
	p.send(c, "RespondInvite", RespondInvitePayload{InviteID: invC.InviteID, Accept: true})
	if code := p.errorCode(c); code != "INVITE_EXPIRED" {
		t.Fatalf("accept cancelled invite: %s", code)
	}
	if p.hub.Status(b) != StatusInLobby || p.hub.Status(a) != StatusInLobby || p.hub.Status(c) != StatusIdle {
		t.Fatalf("statuses %s %s %s", p.hub.Status(a), p.hub.Status(b), p.hub.Status(c))
	}
}

// Decline: the inviter gets InviteDeclined; the invite is gone.
func TestInviteDecline(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b := p.online("pa"), p.online("pb")
	p.host(a)
	inv := p.invite(a, b)

	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID})
	var d InviteDeclinedPayload
	p.expect(a, "InviteDeclined", &d)
	if d != (InviteDeclinedPayload{PlayerID: "pb", DisplayName: "Name_pb"}) {
		t.Fatalf("declined %+v", d)
	}
	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	if code := p.errorCode(b); code != "INVITE_EXPIRED" {
		t.Fatalf("accept declined invite: %s", code)
	}
	// declined, the player can be invited again
	p.invite(a, b)
}

// Expiry: InviteExpired {playerId} to the inviter, {inviteId} to the invitee.
func TestInviteExpires(t *testing.T) {
	p := newParty(t, 40*time.Millisecond)
	a, b := p.online("pa"), p.online("pb")
	p.host(a)
	inv := p.invite(a, b)
	if inv.ExpiresInSec != 1 {
		t.Fatalf("expiresInSec %d", inv.ExpiresInSec)
	}

	var e InviteExpiredPayload
	p.expect(a, "InviteExpired", &e)
	if e != (InviteExpiredPayload{PlayerID: "pb"}) {
		t.Fatalf("inviter %+v", e)
	}
	var e2 InviteExpiredPayload
	p.expect(b, "InviteExpired", &e2)
	if e2 != (InviteExpiredPayload{InviteID: inv.InviteID}) {
		t.Fatalf("invitee %+v", e2)
	}
	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	if code := p.errorCode(b); code != "INVITE_EXPIRED" {
		t.Fatalf("accept expired invite: %s", code)
	}
}

// The InviteToLobby checks: no lobby, offline, busy (in a room, in a full lobby, in
// the queue), pending, self, full, not preparing.
func TestInviteChecks(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b, c, d, e := p.online("pa"), p.online("pb"), p.online("pc"), p.online("pd"), p.online("pe")
	code := func(from *ws.Client, to string) string {
		t.Helper()
		p.send(from, "InviteToLobby", InvitePayload{PlayerID: to})
		return p.errorCode(from)
	}
	if got := code(a, "pb"); got != "LOBBY_REQUIRED" {
		t.Fatalf("no lobby: %s", got)
	}
	st := p.host(a)
	if got := code(a, "nobody"); got != "PLAYER_OFFLINE" {
		t.Fatalf("offline: %s", got)
	}
	b.RoomID = "r1"
	if got := code(a, "pb"); got != "PLAYER_BUSY" {
		t.Fatalf("in a room: %s", got)
	}
	b.RoomID = ""

	// c and d in a full lobby, e queued alone
	p.host(c)
	inv := p.invite(c, d)
	p.send(d, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	p.expect(d, "LobbyUpdated", nil)
	if got := code(a, "pd"); got != "PLAYER_BUSY" {
		t.Fatalf("in a full lobby: %s", got)
	}
	el := p.host(e)
	lob, _ := p.hub.FindLobby(el.LobbyID)
	lob.mu.Lock()
	lob.State.Status = "in_queue"
	lob.mu.Unlock()
	if got := code(a, "pe"); got != "PLAYER_BUSY" {
		t.Fatalf("queued: %s", got)
	}

	p.invite(a, b)
	if got := code(a, "pb"); got != "INVITE_PENDING" {
		t.Fatalf("pending: %s", got)
	}
	if got := code(a, "pa"); got != "INVALID_PAYLOAD" {
		t.Fatalf("self: %s", got)
	}
	if got := code(c, "pb"); got != "LOBBY_FULL" {
		t.Fatalf("full: %s", got)
	}
	al, _ := p.hub.FindLobby(st.LobbyID)
	al.mu.Lock()
	al.State.Status = "in_match"
	al.mu.Unlock()
	if got := code(a, "pe"); got != "INVALID_STATUS" {
		t.Fatalf("in a match: %s", got)
	}
	al.mu.Lock()
	al.State.Status = "preparing"
	al.mu.Unlock()

	// accepting while in a room
	f := p.online("pf")
	invF := p.invite(a, f)
	f.RoomID = "r2"
	p.send(f, "RespondInvite", RespondInvitePayload{InviteID: invF.InviteID, Accept: true})
	if got := p.errorCode(f); got != "IN_ROOM" {
		t.Fatalf("accept in a room: %s", got)
	}
}

// Kick: host only; the member gets LobbyKicked and leaves.
func TestKickMember(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b := p.online("pa"), p.online("pb")
	st := p.host(a)
	inv := p.invite(a, b)
	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	p.expect(b, "LobbyUpdated", nil)
	p.expect(a, "LobbyUpdated", nil)

	p.send(b, "KickMember", KickMemberPayload{PlayerID: "pa"})
	if got := p.errorCode(b); got != "NOT_HOST" {
		t.Fatalf("member kicks: %s", got)
	}
	p.send(a, "KickMember", KickMemberPayload{PlayerID: "pa"})
	if got := p.errorCode(a); got != "INVALID_PAYLOAD" {
		t.Fatalf("host kicks self: %s", got)
	}
	p.send(a, "KickMember", KickMemberPayload{PlayerID: "pb"})
	var k LobbyKickedPayload
	p.expect(b, "LobbyKicked", &k)
	if k.LobbyID != st.LobbyID || b.LobbyID != "" {
		t.Fatalf("kicked %+v, LobbyID %q", k, b.LobbyID)
	}
	var got LobbyState
	p.expect(a, "LobbyUpdated", &got)
	if len(got.Members) != 1 || got.Members[0].PlayerID != "pa" {
		t.Fatalf("after kick %+v", got)
	}
	p.quiet(b)
	if p.hub.Status(b) != StatusIdle {
		t.Fatal("kicked member not idle")
	}
}

// SetReady: a member's ready gates StartQueue; the host is always ready.
func TestReadyGatesStartQueue(t *testing.T) {
	p := newParty(t, time.Minute)
	a, b := p.online("pa"), p.online("pb")
	p.host(a)
	p.send(a, "StartQueue", struct{}{})
	if got := p.errorCode(a); got != "QUEUE_FAILED" { // alone: no ready check (no matchmaker here)
		t.Fatalf("solo start: %s", got)
	}
	inv := p.invite(a, b)
	p.send(b, "RespondInvite", RespondInvitePayload{InviteID: inv.InviteID, Accept: true})
	p.expect(b, "LobbyUpdated", nil)
	p.expect(a, "LobbyUpdated", nil)

	p.send(a, "StartQueue", struct{}{})
	if got := p.errorCode(a); got != "MEMBER_NOT_READY" {
		t.Fatalf("member not ready: %s", got)
	}
	p.send(a, "SetReady", SetReadyPayload{Ready: true})
	if got := p.errorCode(a); got != "INVALID_PAYLOAD" {
		t.Fatalf("host ready: %s", got)
	}
	p.send(b, "SetReady", SetReadyPayload{Ready: true})
	var st LobbyState
	p.expect(a, "LobbyUpdated", &st)
	if !st.Members[1].Ready {
		t.Fatalf("ready %+v", st.Members)
	}
	p.send(a, "StartQueue", struct{}{})
	if got := p.errorCode(a); got != "QUEUE_FAILED" {
		t.Fatalf("ready start: %s", got)
	}
	p.send(b, "SetReady", SetReadyPayload{Ready: false})
	p.expect(a, "LobbyUpdated", &st)
	if st.Members[1].Ready {
		t.Fatalf("unready %+v", st.Members)
	}
}

// ListOnlinePlayers: the others online, filtered by name or id, idle first then by
// rating distance, at most 30; the newest connection counts.
func TestListOnlinePlayers(t *testing.T) {
	p := newParty(t, time.Minute)
	ratings := p.hub.store.(stubStore).ratings
	me := p.online("me")
	ratings["me"] = 1200
	for i, r := range []int{1300, 1190, 900} {
		ratings[fmt.Sprintf("p%d", i)] = r
	}
	p0, _, p2 := p.online("p0"), p.online("p1"), p.online("p2")
	p0.RoomID = "r1"
	list := func(q string) []OnlinePlayer {
		t.Helper()
		p.send(me, "ListOnlinePlayers", ListOnlinePlayersPayload{Query: q})
		var out OnlinePlayersPayload
		p.expect(me, "OnlinePlayers", &out)
		return out.Players
	}
	got := list("")
	if len(got) != 3 || got[0].PlayerID != "p1" || got[1].PlayerID != "p2" || got[2].PlayerID != "p0" ||
		got[2].Status != StatusInMatch || got[0].Status != StatusIdle || got[0].DisplayName != "Name_p1" || got[0].Level != 7 {
		t.Fatalf("list %+v", got)
	}
	if got := list("NAME_P2"); len(got) != 1 || got[0].PlayerID != "p2" {
		t.Fatalf("by name %+v", got)
	}
	if got := list("p0"); len(got) != 1 || got[0].PlayerID != "p0" {
		t.Fatalf("by id %+v", got)
	}
	if got := list("zzz"); len(got) != 0 {
		t.Fatalf("no match %+v", got)
	}

	// a second connection replaces the first; the old one closing keeps it online
	newer := &ws.Client{PlayerID: "p2", Send: make(chan ws.Message, 4)}
	p.hub.SetOnline(newer)
	p.hub.SetOffline(p2)
	if p.hub.OnlineClient("p2") != newer {
		t.Fatal("newest connection lost")
	}
	p.hub.SetOffline(newer)
	if len(list("")) != 2 {
		t.Fatal("offline player listed")
	}

	for i := 0; i < 40; i++ {
		p.online(fmt.Sprintf("x%02d", i))
	}
	if got := list(""); len(got) != maxOnlineListed {
		t.Fatalf("cap: %d", len(got))
	}
}

// gunOf matches the client's EquipmentModel.gun.
func TestGunOf(t *testing.T) {
	snap := gamedata.NewSnapshot(&gamedata.GameData{Characters: map[string]gamedata.CharacterConfig{
		"rookie": {GunKind: "bazooka"}, "tanko": {GunKind: "cannon"}, "odd": {GunKind: "nope"},
	}})
	snap.GunKinds = []gamedata.GunKindConfig{{ID: "bazooka"}, {ID: "cannon"}, {ID: "shock_rifle"}, {ID: "seed_launcher"}, {ID: "blaster"}}
	snap.TypeWords = []gamedata.TypeWordConfig{{Word: "bazooka", GunKind: "bazooka"}, {Word: "shock rifle", GunKind: "shock_rifle"},
		{Word: "blaster", GunKind: "blaster"}, {Word: "armor", GunKind: ""}}
	snap.EquipTiers = []gamedata.EquipmentTierConfig{{Tier: "iron", Kind: "normal", IDSuffix: "lv10"}, {Tier: "mithril", Kind: "normal", IDSuffix: "lv30"},
		{Tier: "silver", Kind: "crafted"}, {Tier: "gold", Kind: "crafted"}}
	cases := []struct {
		char string
		w    *wornWeapon
		want MemberGun
	}{
		{"tanko", nil, MemberGun{"cannon", "basic"}},
		{"odd", nil, MemberGun{"bazooka", "basic"}}, // unknown gun kind: the default character's
		{"rookie", &wornWeapon{ItemID: "rookie_weapon_lv30", Name: "Mithril Bazooka", Category: "normal", CharacterID: "rookie"}, MemberGun{"bazooka", "mithril"}},
		{"tanko", &wornWeapon{ItemID: "tanko_weapon_lv10", Name: "Whatever", Category: "normal", CharacterID: "tanko"}, MemberGun{"cannon", "iron"}},
		{"rookie", &wornWeapon{ItemID: "crafted_weapon_gold", Name: "Gold Blaster", Category: "crafted", Tier: "gold"}, MemberGun{"blaster", "gold"}},
		{"rookie", &wornWeapon{ItemID: "normal_weapon_lv10", Name: "Iron Shock Rifle", Category: "normal"}, MemberGun{"shock_rifle", "iron"}},
		{"rookie", &wornWeapon{ItemID: "crafted_weapon_x", Name: "X", Category: "crafted"}, MemberGun{"blaster", "silver"}},
		{"rookie", ptr(weaponFromID(snap, "tanko_weapon_lv30")), MemberGun{"cannon", "mithril"}},
		{"rookie", ptr(weaponFromID(snap, "crafted_weapon_gold")), MemberGun{"blaster", "gold"}},
	}
	for _, c := range cases {
		if got := gunOf(snap, c.char, c.w); got != c.want {
			t.Errorf("%s %+v: %+v, want %+v", c.char, c.w, got, c.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
