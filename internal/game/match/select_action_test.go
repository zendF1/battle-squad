package match

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"battle-squad/internal/game/ws"
)

// selectMatch is a 1v1 rookie match on flat ground; p1 plays, holds a drill bomb, a teleport
// and a medkit, and has full skill energy.
func selectMatch(t *testing.T) (*Match, map[string]*ws.Client) {
	t.Helper()
	m, clients := flatSkillMatch(t, skillPlayer{"p1", 1, "rookie", 400}, skillPlayer{"p2", 2, "rookie", 1300})
	p1 := m.State.Players["p1"]
	p1.SkillEnergy = 100
	p1.Items = []string{"drill_bomb", "teleport", "medkit"}
	return m, clients
}

// selectedOf decodes every ActionSelected in msgs.
func selectedOf(t *testing.T, msgs []ws.Message) []ActionSelected {
	t.Helper()
	var out []ActionSelected
	for _, msg := range msgs {
		if msg.Event != "ActionSelected" {
			continue
		}
		var a ActionSelected
		if err := json.Unmarshal(msg.Data, &a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func strp(s string) *string { return &s }

// waitSelectFlush lets the pending coalesced broadcast run on the match loop.
func waitSelectFlush(t *testing.T, m *Match) {
	t.Helper()
	select {
	case ev := <-m.Events:
		m.handleEvent(ev)
	case <-time.After(selectBroadcastInterval + time.Second):
		t.Fatal("coalesced ActionSelected never flushed")
	}
}

func TestSelectActionBroadcastsToEveryPlayer(t *testing.T) {
	m, clients := selectMatch(t)
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "skill"})
	for id, c := range clients {
		got := selectedOf(t, drainSent(c))
		want := ActionSelected{PlayerID: "p1", ActionMode: "skill", SkillID: "triple_shot"}
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s got %+v, want %+v", id, got, want)
		}
	}

	m.selectSentAt["p1"] = time.Time{} // outside the coalescing window
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 1 || got[0] != (ActionSelected{PlayerID: "p1", ActionMode: "item", ItemID: "drill_bomb"}) {
		t.Fatalf("item selection %+v", got)
	}

	m.selectSentAt["p1"] = time.Time{}
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "weapon"})
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 1 || got[0] != (ActionSelected{PlayerID: "p1", ActionMode: "weapon"}) {
		t.Fatalf("weapon selection %+v", got)
	}
	if m.syncSelection() != nil {
		t.Fatalf("weapon left a selection: %+v", m.selection)
	}
}

func TestSelectActionIgnored(t *testing.T) {
	cases := []struct {
		name  string
		from  string
		sel   SelectAction
		setup func(m *Match)
	}{
		{"not the current player", "p2", SelectAction{ActionMode: "skill"}, func(m *Match) { m.State.Players["p2"].SkillEnergy = 100 }},
		{"skill energy below 100", "p1", SelectAction{ActionMode: "skill"}, func(m *Match) { m.State.Players["p1"].SkillEnergy = 90 }},
		{"stunned", "p1", SelectAction{ActionMode: "skill"}, func(m *Match) {
			m.State.Players["p1"].StatusEffects = []StatusEffect{{EffectID: "stun", DurationTurn: 1}}
		}},
		{"frozen", "p1", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")}, func(m *Match) {
			m.State.Players["p1"].StatusEffects = []StatusEffect{{EffectID: "freeze", DurationTurn: 1}}
		}},
		{"item not held", "p1", SelectAction{ActionMode: "item", ItemID: strp("power_shot")}, nil},
		{"instant item", "p1", SelectAction{ActionMode: "item", ItemID: strp("medkit")}, nil},
		{"item without id", "p1", SelectAction{ActionMode: "item"}, nil},
		{"teleport while rooted", "p1", SelectAction{ActionMode: "item", ItemID: strp("teleport")}, func(m *Match) {
			m.State.Players["p1"].StatusEffects = []StatusEffect{{EffectID: "root", DurationTurn: 1}}
		}},
		{"unknown mode", "p1", SelectAction{ActionMode: "dance"}, nil},
		{"weapon with nothing armed", "p1", SelectAction{ActionMode: "weapon"}, nil},
		{"shot resolving", "p1", SelectAction{ActionMode: "skill"}, func(m *Match) { m.resolving = true }},
		{"dead", "p1", SelectAction{ActionMode: "skill"}, func(m *Match) { m.State.Players["p1"].IsAlive = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, clients := selectMatch(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			send(m, clients[tc.from], "SelectAction", tc.sel)
			if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 0 {
				t.Fatalf("broadcast %+v", got)
			}
			if m.syncSelection() != nil || m.selectFlushPending {
				t.Fatalf("selection recorded: %+v", m.selection)
			}
		})
	}
}

func TestSelectActionAcceptsTeleport(t *testing.T) {
	m, clients := selectMatch(t)
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("teleport")})
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 1 || got[0].ItemID != "teleport" {
		t.Fatalf("teleport selection %+v", got)
	}
}

func TestSelectActionDoesNotRateLimitTheShot(t *testing.T) {
	m, clients := selectMatch(t)
	delete(m.lastAction, "p1")
	raw, _ := json.Marshal(SelectAction{ActionMode: "skill"})
	m.handleEvent(matchEvent{client: clients["p1"], msg: ws.Message{Event: "SelectAction", Data: raw}, ctx: context.Background()})
	raw, _ = json.Marshal(ShootAction{Angle: 60, Power: 30, ActionMode: "skill"})
	m.handleEvent(matchEvent{client: clients["p1"], msg: ws.Message{Event: "Shoot", Data: raw}, ctx: context.Background()})
	if !m.resolving {
		t.Fatal("Shoot right after SelectAction was dropped")
	}
}

func TestSelectActionCoalesces(t *testing.T) {
	m, clients := selectMatch(t)
	c := clients["p1"]

	// Inside the window: only the latest selection goes out, once, at its end
	send(m, c, "SelectAction", SelectAction{ActionMode: "skill"})
	start := time.Now()
	send(m, c, "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("teleport")})
	send(m, c, "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 1 || got[0].ActionMode != "skill" {
		t.Fatalf("first selection %+v, want one skill broadcast", got)
	}
	waitSelectFlush(t, m)
	if waited := time.Since(start); waited < selectBroadcastInterval-20*time.Millisecond {
		t.Fatalf("flushed after %v, before the %v window", waited, selectBroadcastInterval)
	}
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 1 || got[0].ItemID != "drill_bomb" {
		t.Fatalf("coalesced %+v, want one drill_bomb broadcast", got)
	}

	// Back to what was last sent inside the window: nothing goes out
	send(m, c, "SelectAction", SelectAction{ActionMode: "weapon"})
	send(m, c, "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	waitSelectFlush(t, m)
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 0 {
		t.Fatalf("unchanged selection broadcast %+v", got)
	}

	// The same selection twice: no broadcast
	m.selectSentAt["p1"] = time.Time{}
	send(m, c, "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	if got := selectedOf(t, drainSent(clients["p2"])); len(got) != 0 || m.selectFlushPending {
		t.Fatalf("repeated selection broadcast %+v", got)
	}
}

func TestSelectionClearedByShotAndTurnEnd(t *testing.T) {
	m, clients := selectMatch(t)
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "skill"})
	send(m, clients["p1"], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "skill"})
	if !m.resolving || m.syncSelection() != nil {
		t.Fatalf("shot left the selection %+v (resolving %v)", m.selection, m.resolving)
	}

	m2, clients2 := selectMatch(t)
	send(m2, clients2["p1"], "SelectAction", SelectAction{ActionMode: "skill"})
	send(m2, clients2["p1"], "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")}) // coalesced, pending
	m2.endTurn(context.Background())
	if m2.syncSelection() != nil || m2.selectionSent.armed() || m2.selectFlushPending {
		t.Fatalf("turn end left the selection %+v", m2.selection)
	}
	drain(clients2["p2"])
	// The old turn's pending flush does not broadcast in the new one
	waitSelectFlush(t, m2)
	if got := selectedOf(t, drainSent(clients2["p2"])); len(got) != 0 {
		t.Fatalf("broadcast after the turn ended %+v", got)
	}
}

// syncSelected sends Reconnect for c and returns the selectedAction of its MatchStateSync.
func syncSelected(t *testing.T, m *Match, c *ws.Client) *ActionSelected {
	t.Helper()
	drain(c)
	send(m, c, "Reconnect", nil)
	for _, msg := range drainSent(c) {
		if msg.Event != "MatchStateSync" {
			continue
		}
		var out struct {
			SelectedAction *ActionSelected `json:"selectedAction"`
		}
		if err := json.Unmarshal(msg.Data, &out); err != nil {
			t.Fatal(err)
		}
		return out.SelectedAction
	}
	t.Fatal("no MatchStateSync")
	return nil
}

func TestMatchStateSyncSelectedAction(t *testing.T) {
	m, clients := selectMatch(t)
	if got := syncSelected(t, m, clients["p2"]); got != nil {
		t.Fatalf("selectedAction %+v with nothing armed", got)
	}
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "skill"})
	// The latest selection, even while its broadcast is still coalesced
	send(m, clients["p1"], "SelectAction", SelectAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	for _, id := range []string{"p1", "p2"} {
		got := syncSelected(t, m, clients[id])
		if got == nil || *got != (ActionSelected{PlayerID: "p1", ActionMode: "item", ItemID: "drill_bomb"}) {
			t.Fatalf("%s sync selectedAction %+v", id, got)
		}
	}
}

func TestBotSkillSelectedBeforeAiming(t *testing.T) {
	m, bot := botSkillMatch(t, skillPlayer{"b", 1, "rookie", 300}, skillPlayer{"e1", 2, "rookie", 900})
	c := m.Clients["e1"]
	drain(c)
	m.botStep(context.Background(), botTurnState{}, 0)
	msgs := drainSent(c)
	var events []string
	for _, msg := range msgs {
		events = append(events, msg.Event)
	}
	if len(events) != 2 || events[0] != "ActionSelected" || events[1] != "PlayerAiming" {
		t.Fatalf("bot skill turn sent %v, want ActionSelected then PlayerAiming", events)
	}
	if got := selectedOf(t, msgs); got[0] != (ActionSelected{PlayerID: bot.PlayerID, ActionMode: "skill", SkillID: "triple_shot"}) {
		t.Fatalf("bot selection %+v", got[0])
	}
	if sel := m.syncSelection(); sel == nil || sel.ActionMode != "skill" {
		t.Fatalf("bot selection not in sync: %+v", sel)
	}

	// The aimed skill fires and clears the selection
	select {
	case ev := <-m.Events:
		m.handleEvent(ev)
	case <-time.After(botAimMaxDelay + time.Second):
		t.Fatal("bot aimed but never fired")
	}
	if !m.resolving || m.syncSelection() != nil {
		t.Fatalf("bot shot left the selection %+v", m.selection)
	}
}

func TestBotWeaponShotSelectsNothing(t *testing.T) {
	m, bot, _ := newBotMatch(t, 400, 1000, nil)
	c := watchMatch(m)
	m.botStep(context.Background(), botTurnState{}, 0)
	if got := selectedOf(t, drainSent(c)); len(got) != 0 {
		t.Fatalf("weapon shot selected %+v", got)
	}

	// A bot shot item selects the item
	bot.Items = []string{"drill_bomb"}
	m.selectBotAction(bot, ShootAction{ActionMode: "item", ItemID: strp("drill_bomb")})
	if got := selectedOf(t, drainSent(c)); len(got) != 1 || got[0].ItemID != "drill_bomb" {
		t.Fatalf("bot item selection %+v", got)
	}
}
