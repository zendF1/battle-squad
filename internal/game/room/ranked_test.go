package room

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database"
)

func rankedEntry(lobby string, ratings map[string]int, order ...string) matchmaker.QueueEntry {
	e := matchmaker.QueueEntry{
		EntryID: "e_" + lobby, LobbyID: lobby, PlayerIDs: order, PlayerRatings: ratings,
		PlayerChars: map[string]string{}, PlayerItems: map[string][]string{}, PlayerNames: map[string]string{},
	}
	for _, id := range order {
		e.PlayerChars[id] = "spark"
		e.PlayerItems[id] = []string{"medkit"}
		e.PlayerNames[id] = "Q_" + id
	}
	return e
}

func botIDs() func() string {
	n := 0
	return func() string {
		n++
		return fmt.Sprintf("bot_000%d", n)
	}
}

// A solo with a bot against two bots: the MatchFound payload, the room players
// and the Elo team ratings (a bot counts as the humans' average rating).
func TestRankedRosterOneHuman(t *testing.T) {
	result := matchmaker.MatchResult{
		Teams: [2]matchmaker.MatchTeam{
			{Entries: []matchmaker.QueueEntry{rankedEntry("l1", map[string]int{"p1": 1050}, "p1")}, Bots: 1},
			{Bots: 2},
		},
		MapID: "grassland_valley", HasBot: true,
	}
	snap := gamedata.UseYAML(t, configsDir())
	gamedata.UseForTest(t, snap.WithData(&gamedata.GameData{Maps: map[string]gamedata.MapConfig{
		"grassland_valley": {MapID: "grassland_valley", Name: "Thung lũng cỏ"},
	}, Characters: snap.Data.Characters, Weapons: snap.Data.Weapons, Items: snap.Data.Items}))
	profiles := map[string]rankedProfile{"p1": {DisplayName: "Smoke", Level: 40, TitleID: "first_blood"}}
	// the middle of every draw: level 40 + 0, the middle free character
	bots := newBotOutfitter(gamedata.Current(), testBotWeapons(), func(n int) int { return n / 2 })
	ro := buildRankedRoster(result, profiles, botIDs(), bots)
	ro.Found.RoomID, ro.Found.MatchID = "r1", "m1"

	raw, err := json.Marshal(ro.Found)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"roomId":"r1","matchId":"m1","mapId":"grassland_valley","mapName":"Thung lũng cỏ","mapTheme":"grassland","mode":"ranked_2v2","teams":[` +
		`{"teamId":1,"players":[` +
		`{"playerId":"p1","displayName":"Smoke","characterId":"spark","isBot":false,"level":40,"rating":1050,"tier":"silver","division":3,"titleId":"first_blood"},` +
		`{"playerId":"bot_0001","displayName":"Bot 0001","characterId":"spark","isBot":true,"level":40,"rating":1050,"tier":"silver","division":3,"titleId":""}]},` +
		`{"teamId":2,"players":[` +
		`{"playerId":"bot_0002","displayName":"Bot 0002","characterId":"tanko","isBot":true,"level":40,"rating":1050,"tier":"silver","division":3,"titleId":""},` +
		`{"playerId":"bot_0003","displayName":"Bot 0003","characterId":"rookie","isBot":true,"level":40,"rating":1050,"tier":"silver","division":3,"titleId":""}]}]}`
	if string(raw) != want {
		t.Fatalf("MatchFound\n got %s\nwant %s", raw, want)
	}

	if len(ro.Players) != 4 || ro.Players[0].PlayerID != "p1" || !ro.Players[0].IsHost || ro.Players[0].TeamID != 1 ||
		!reflect.DeepEqual(ro.Players[0].Items, []string{"medkit"}) || ro.Players[3].TeamID != 2 {
		t.Fatalf("players %+v", ro.Players)
	}
	if !reflect.DeepEqual(ro.LobbyMapping, map[string]string{"p1": "l1"}) {
		t.Fatalf("lobbies %v", ro.LobbyMapping)
	}
	if ro.BotRating != 1050 || ro.TeamRatings[1] != 1050 || ro.TeamRatings[2] != 1050 {
		t.Fatalf("ratings bot %d teams %v", ro.BotRating, ro.TeamRatings)
	}
	// each bot's room entry carries its loadout: medkit, teleport and one random item
	for _, p := range ro.Players[1:] {
		l, ok := ro.Bots[p.PlayerID]
		if !ok || p.CharacterID != l.CharacterID || !reflect.DeepEqual(p.Items, l.Items) || len(p.Items) != 3 ||
			p.Items[0] != "medkit" || p.Items[1] != "teleport" {
			t.Fatalf("bot %+v loadout %+v", p, l)
		}
		if want := l.CharacterID + "_weapon_lv40"; l.Look.WeaponItemID != want && l.Look.WeaponItemID != "crafted_weapon_gold" {
			t.Fatalf("bot %s gun %q, want %s or crafted_weapon_gold", p.PlayerID, l.Look.WeaponItemID, want)
		}
	}
}

// Four humans from three lobbies: teams in MatchResult order, no bots, team
// averages of the players' own ratings; a missing profile keeps the queue name
// and level 1.
func TestRankedRosterFullMatch(t *testing.T) {
	result := matchmaker.MatchResult{
		Teams: [2]matchmaker.MatchTeam{
			{Entries: []matchmaker.QueueEntry{rankedEntry("lp", map[string]int{"a": 1200, "b": 1000}, "a", "b")}},
			{Entries: []matchmaker.QueueEntry{
				rankedEntry("lc", map[string]int{"c": 1150}, "c"),
				rankedEntry("ld", map[string]int{"d": 1010}, "d"),
			}},
		},
		MapID: "m",
	}
	ro := buildRankedRoster(result, map[string]rankedProfile{"a": {DisplayName: "A", Level: 5}}, botIDs(), nil)
	if ro.TeamRatings[1] != 1100 || ro.TeamRatings[2] != 1080 {
		t.Fatalf("team ratings %v", ro.TeamRatings)
	}
	var ids []string
	for _, p := range ro.Players {
		ids = append(ids, fmt.Sprintf("%s/%d", p.PlayerID, p.TeamID))
	}
	if !reflect.DeepEqual(ids, []string{"a/1", "b/1", "c/2", "d/2"}) {
		t.Fatalf("players %v", ids)
	}
	b := ro.Found.Teams[0].Players[1]
	if b.DisplayName != "Q_b" || b.Level != 1 || b.Tier != "silver" || b.Division != 3 || b.IsBot {
		t.Fatalf("card %+v", b)
	}
	if a := ro.Found.Teams[0].Players[0]; a.Tier != "gold" || a.Division != 3 || a.Level != 5 {
		t.Fatalf("card %+v", a)
	}
	if !reflect.DeepEqual(ro.LobbyMapping, map[string]string{"a": "lp", "b": "lp", "c": "lc", "d": "ld"}) {
		t.Fatalf("lobbies %v", ro.LobbyMapping)
	}
}

// Ranked reserves every human's items like a custom room; a player whose items
// cannot be reserved plays without them, the others keep theirs.
func TestReserveRankedItems(t *testing.T) {
	pool := newReservePool(t)
	setLoadoutGameData(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO inventory_items (player_id, item_id, quantity) VALUES ('p1', 'medkit', 2), ('p2', 'medkit', 0)`); err != nil {
		t.Fatal(err)
	}
	r := &Room{ID: "r1", db: &database.PostgresDB{Pool: pool}, State: RoomState{Players: []RoomPlayer{
		{PlayerID: "p1", Items: []string{"medkit"}},
		{PlayerID: "p2", Items: []string{"medkit"}},
		{PlayerID: "bot_1", Items: []string{}},
	}}}
	r.reserveRankedItems("m1")

	if !reflect.DeepEqual(r.State.Players[0].Items, []string{"medkit"}) || len(r.State.Players[1].Items) != 0 {
		t.Fatalf("items p1 %v p2 %v", r.State.Players[0].Items, r.State.Players[1].Items)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE match_id = 'm1' AND player_id = 'p1' AND status = 'reserved'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("p1 reservations %d (%v)", n, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE player_id <> 'p1'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("other reservations %d (%v)", n, err)
	}
}
