package match

import (
	"context"
	"encoding/json"
	"time"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/ws"
)

// selectBroadcastInterval is the shortest time between two ActionSelected broadcasts for one
// player: SelectAction changes inside it are coalesced into one broadcast at its end.
const selectBroadcastInterval = 100 * time.Millisecond

// SelectAction is what the current player armed in the HUD (client → server): the weapon,
// the skill or a shot item. It commits nothing; Shoot still carries the actual choice.
type SelectAction struct {
	ActionMode string  `json:"actionMode"` // weapon, skill, item
	ItemID     *string `json:"itemId,omitempty"`
}

// ActionSelected tells every player what the current player has armed, so each client shows
// that player's use badge before the shot (server → client). SkillID is set for skill, ItemID
// for item; "weapon" clears the badge.
type ActionSelected struct {
	PlayerID   string `json:"playerId"`
	ActionMode string `json:"actionMode"`
	SkillID    string `json:"skillId,omitempty"`
	ItemID     string `json:"itemId,omitempty"`
}

// armed reports whether a is a skill or item selection (shows a badge), not the weapon.
func (a ActionSelected) armed() bool {
	return a.ActionMode == "skill" || a.ActionMode == "item"
}

// sameSelection reports whether a and b arm the same thing (two weapon selections are equal).
func sameSelection(a, b ActionSelected) bool {
	if !a.armed() && !b.armed() {
		return true
	}
	return a == b
}

// isShotItem reports whether itemID is used by firing a shot (Shoot with actionMode item): a
// shot modifier or the teleport. Instant items (medkit, wind_stopper...) go through UseItem.
func isShotItem(itemID string) bool {
	return IsShotModifierItem(itemID) || itemID == "teleport"
}

// validSelection checks sel for player and returns it as an ActionSelected: the weapon always
// (unless the player cannot act), the skill at full skill energy with a configured skill, a shot
// item the player still holds (the teleport only while it can move). false for anything else.
func validSelection(player *BattlePlayerState, sel SelectAction) (ActionSelected, bool) {
	out := ActionSelected{PlayerID: player.PlayerID, ActionMode: sel.ActionMode}
	if !player.IsAlive || skipsTurn(player) != "" {
		return out, false
	}
	switch sel.ActionMode {
	case "weapon":
		return out, true
	case "skill":
		if player.SkillEnergy < skillEnergyMax || gamedata.Data == nil {
			return out, false
		}
		char, ok := gamedata.Data.Characters[player.CharacterID]
		if !ok {
			return out, false
		}
		if _, ok := gamedata.Data.Skills[char.SkillID]; !ok {
			return out, false
		}
		out.SkillID = char.SkillID
		return out, true
	case "item":
		if sel.ItemID == nil || !isShotItem(*sel.ItemID) || !hasItem(player, *sel.ItemID) {
			return out, false
		}
		if *sel.ItemID == "teleport" && !canMove(player) {
			return out, false
		}
		out.ItemID = *sel.ItemID
		return out, true
	}
	return out, false
}

func hasItem(player *BattlePlayerState, itemID string) bool {
	for _, it := range player.Items {
		if it == itemID {
			return true
		}
	}
	return false
}

// processSelectAction records what the current player armed and broadcasts it (coalesced, see
// queueSelectionBroadcast). Ignored from anyone else, while a shot resolves, or when invalid.
func (m *Match) processSelectAction(ctx context.Context, client *ws.Client, sel SelectAction) {
	if client == nil || client.PlayerID != m.State.CurrentPlayerID || m.resolving || m.State.Status != "in_progress" {
		return
	}
	player := m.State.Players[m.State.CurrentPlayerID]
	if player == nil {
		return
	}
	next, ok := validSelection(player, sel)
	if !ok || sameSelection(next, m.selection) {
		return
	}
	m.selection = next
	m.queueSelectionBroadcast(ctx)
}

// queueSelectionBroadcast broadcasts the current selection now when the player's last
// ActionSelected is at least selectBroadcastInterval old, otherwise once at the end of that
// interval (one pending broadcast at a time, carrying the latest selection). Nothing is sent
// when the selection is back to what was last broadcast this turn.
func (m *Match) queueSelectionBroadcast(ctx context.Context) {
	if m.selectFlushPending {
		return
	}
	wait := selectBroadcastInterval - time.Since(m.selectSentAt[m.selection.PlayerID])
	if wait <= 0 {
		m.flushSelection()
		return
	}
	m.selectFlushPending = true
	m.runOnTurnAfter(ctx, wait, func(ctx context.Context) {
		m.selectFlushPending = false
		if !m.resolving {
			m.flushSelection()
		}
	})
}

// flushSelection broadcasts the current selection if it differs from the last one sent.
func (m *Match) flushSelection() {
	if sameSelection(m.selection, m.selectionSent) {
		return
	}
	m.broadcastSelection(m.selection)
}

// broadcastSelection sends ActionSelected for sel to every player.
func (m *Match) broadcastSelection(sel ActionSelected) {
	if !sel.armed() {
		sel = ActionSelected{PlayerID: sel.PlayerID, ActionMode: "weapon"}
	}
	m.selectionSent = sel
	m.selectSentAt[sel.PlayerID] = time.Now()
	payload, _ := json.Marshal(sel)
	m.broadcast(ws.Message{Event: "ActionSelected", Data: payload})
	m.el.Log("ActionSelected", sel.PlayerID, sel)
}

// resetSelection forgets the turn's selection without a broadcast: TurnStarted and
// ShotResolved already tell clients the badge has moved on.
func (m *Match) resetSelection() {
	m.selection = ActionSelected{}
	m.selectionSent = ActionSelected{}
	m.selectFlushPending = false
}

// selectBotAction broadcasts ActionSelected right away for a bot about to aim a skill or a
// shot item, so its badge shows during the aim. A weapon shot sends nothing.
func (m *Match) selectBotAction(bot *BattlePlayerState, act ShootAction) {
	if act.ActionMode != "skill" && act.ActionMode != "item" {
		return
	}
	sel, ok := validSelection(bot, SelectAction{ActionMode: act.ActionMode, ItemID: act.ItemID})
	if !ok {
		return
	}
	m.selection = sel
	m.broadcastSelection(sel)
}

// syncSelection is the current player's armed skill or item for MatchStateSync, nil for the
// weapon.
func (m *Match) syncSelection() *ActionSelected {
	if !m.selection.armed() || m.selection.PlayerID != m.State.CurrentPlayerID {
		return nil
	}
	sel := m.selection
	return &sel
}
