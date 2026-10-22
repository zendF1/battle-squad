package lobby

import (
	"context"
	"sort"
	"strings"

	"battle-squad/internal/game/ws"
)

// Presence: the players connected to this game node, playerId → their newest
// connection (registered on connect, removed when that connection closes). Lobbies
// live on one node too, so this is single-node only: a player connected to another
// game node is offline here.

// maxOnlineListed caps ListOnlinePlayers.
const maxOnlineListed = 30

// SetOnline registers the client as its player's connection (the newest wins).
func (h *LobbyHub) SetOnline(c *ws.Client) {
	h.onlineMu.Lock()
	h.online[c.PlayerID] = c
	h.onlineMu.Unlock()
}

// SetOffline forgets the client, unless a newer connection of its player replaced it.
func (h *LobbyHub) SetOffline(c *ws.Client) {
	h.onlineMu.Lock()
	if h.online[c.PlayerID] == c {
		delete(h.online, c.PlayerID)
	}
	h.onlineMu.Unlock()
}

// OnlineClient is the player's connection on this node, nil when offline.
func (h *LobbyHub) OnlineClient(playerID string) *ws.Client {
	h.onlineMu.RLock()
	defer h.onlineMu.RUnlock()
	return h.online[playerID]
}

// Status is the client's player status: in_match in a room, in_lobby in a lobby
// with 2 members or one in the queue, idle otherwise (a host alone in a preparing
// lobby is idle). Never call it holding a lobby's lock.
func (h *LobbyHub) Status(c *ws.Client) string {
	status, _ := h.presenceOf(c)
	return status
}

// presenceOf is the client's Status and the character its lobby member plays ("" when
// it is in no lobby).
func (h *LobbyHub) presenceOf(c *ws.Client) (string, string) {
	if c.LobbyID == "" {
		if c.RoomID != "" {
			return StatusInMatch, ""
		}
		return StatusIdle, ""
	}
	l, err := h.FindLobby(c.LobbyID)
	if err != nil {
		if c.RoomID != "" {
			return StatusInMatch, ""
		}
		return StatusIdle, ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	characterID := ""
	for _, m := range l.State.Members {
		if m.PlayerID == c.PlayerID {
			characterID = m.CharacterID
		}
	}
	switch {
	case c.RoomID != "":
		return StatusInMatch, characterID
	case len(l.State.Members) >= 2 || l.State.Status == "in_queue":
		return StatusInLobby, characterID
	}
	return StatusIdle, characterID
}

// ListOnline is ListOnlinePlayers: the players online here but the caller whose
// display name contains query (case-insensitive) or whose id is query, idle first,
// then the closest rating to the caller's; at most maxOnlineListed.
func (h *LobbyHub) ListOnline(ctx context.Context, caller *ws.Client, query string) ([]OnlinePlayer, error) {
	h.onlineMu.RLock()
	clients := make([]*ws.Client, 0, len(h.online))
	ids := make([]string, 0, len(h.online))
	for id, c := range h.online {
		if id != caller.PlayerID {
			clients = append(clients, c)
			ids = append(ids, id)
		}
	}
	h.onlineMu.RUnlock()

	cards, err := h.store.Players(ctx, ids)
	if err != nil {
		return nil, err
	}
	myRating, _, _ := h.store.Rating(ctx, caller.PlayerID)

	query = strings.TrimSpace(query)
	lower := strings.ToLower(query)
	type listed struct {
		OnlinePlayer
		dist int
	}
	rows := make([]listed, 0, len(clients))
	for _, c := range clients {
		card, ok := cards[c.PlayerID]
		if !ok {
			continue
		}
		if query != "" && c.PlayerID != query && !strings.Contains(strings.ToLower(card.DisplayName), lower) {
			continue
		}
		status, characterID := h.presenceOf(c)
		card.Status = status
		if characterID != "" {
			card.CharacterID = characterID
		}
		dist := card.Rating - myRating
		if dist < 0 {
			dist = -dist
		}
		rows = append(rows, listed{card.OnlinePlayer, dist})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.Status == StatusIdle) != (b.Status == StatusIdle) {
			return a.Status == StatusIdle
		}
		if a.dist != b.dist {
			return a.dist < b.dist
		}
		if a.DisplayName != b.DisplayName {
			return a.DisplayName < b.DisplayName
		}
		return a.PlayerID < b.PlayerID
	})
	out := make([]OnlinePlayer, 0, min(len(rows), maxOnlineListed))
	for _, r := range rows[:min(len(rows), maxOnlineListed)] {
		out = append(out, r.OnlinePlayer)
	}
	return out, nil
}
