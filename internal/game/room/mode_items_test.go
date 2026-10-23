package room

import (
	"context"
	"errors"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database"
)

// setModeItemGameData is setLoadoutGameData with 7 items, rules.items maxLoadout 3 and the boss
// lobby mode of boss:lava_crab taking 6 items (config_game_modes max_items, 052).
func setModeItemGameData(t *testing.T) []string {
	t.Helper()
	ids := []string{"medkit", "teleport", "power_shot", "air_strike", "drill_bomb", "freeze_bomb", "spider_net"}
	items := map[string]gamedata.ItemConfig{}
	for _, id := range ids {
		items[id] = gamedata.ItemConfig{ItemID: id, MaxUsePerMatch: 1}
	}
	six := 6
	rules := gamedata.DefaultRules()
	rules.Items.MaxLoadout = 3
	gamedata.UseForTest(t, gamedata.Current().WithData(&gamedata.GameData{Items: items, Characters: map[string]gamedata.CharacterConfig{
		"rookie": {CharacterID: "rookie"}}}).WithRules(rules).WithModes([]gamedata.GameModeConfig{
		{ModeID: "boss_lava_crab", Kind: "lobby", Queue: "boss:lava_crab", PartySize: 4, Enabled: true, MaxItems: &six},
		{ModeID: "ranked", Kind: "lobby", Queue: matchmaker.RankedQueue, PartySize: 2, Enabled: true},
	}))
	return ids
}

// A boss queue takes its mode's 6 items, every other mode (and quick play) the global 3; a
// loadout over the limit is a LoadoutError ("at most N items"); TrimLoadout keeps the first N.
func TestLoadoutItemLimitPerMode(t *testing.T) {
	ids := setModeItemGameData(t)
	own := &fakeOwnership{items: map[string]bool{}}
	for _, id := range ids {
		own.items[id] = true
	}
	ctx := context.Background()
	for _, tc := range []struct {
		mode    string
		n       int
		wantErr string
	}{
		{"boss:lava_crab", 6, ""},
		{"boss:lava_crab", 7, "at most 6 items"},
		{matchmaker.RankedQueue, 3, ""},
		{matchmaker.RankedQueue, 4, "at most 3 items"},
		{"", 4, "at most 3 items"},
	} {
		err := validateLoadout(ctx, own, "p1", "rookie", ids[:tc.n], tc.mode, false)
		var le *LoadoutError
		switch {
		case tc.wantErr == "" && err != nil:
			t.Fatalf("%s %d items: %v", tc.mode, tc.n, err)
		case tc.wantErr != "" && (!errors.As(err, &le) || !strings.Contains(le.Reason, tc.wantErr)):
			t.Fatalf("%s %d items: %v, want %q", tc.mode, tc.n, err, tc.wantErr)
		}
	}
	if MaxLoadoutItems("boss:lava_crab") != 6 || MaxLoadoutItems(matchmaker.CasualQueue) != 3 {
		t.Fatalf("limits %d %d", MaxLoadoutItems("boss:lava_crab"), MaxLoadoutItems(matchmaker.CasualQueue))
	}
	if got := TrimLoadout(ids[:6], matchmaker.RankedQueue); len(got) != 3 || got[2] != ids[2] {
		t.Fatalf("trim %v", got)
	}
	if got := TrimLoadout(ids[:6], "boss:lava_crab"); len(got) != 6 {
		t.Fatalf("boss trim %v", got)
	}
}

// A matchmade roster keeps a boss party's 6 items and cuts a 2v2 player's to 3.
func TestRosterItemsFollowMode(t *testing.T) {
	ids := setModeItemGameData(t)
	entry := func(id string) matchmaker.QueueEntry {
		return matchmaker.QueueEntry{EntryID: "e" + id, LobbyID: "l" + id, PlayerIDs: []string{id},
			PlayerChars: map[string]string{id: "rookie"}, PlayerItems: map[string][]string{id: ids[:6]}}
	}
	for queue, want := range map[string]int{"boss:lava_crab": 6, matchmaker.RankedQueue: 3} {
		res := matchmaker.MatchResult{Queue: queue, Teams: [2]matchmaker.MatchTeam{{Entries: []matchmaker.QueueEntry{entry("p1")}}, {}}}
		ro := buildRankedRoster(res, map[string]rankedProfile{}, func() string { return "bot_x" }, nil)
		if got := ro.Players[0].Items; len(got) != want {
			t.Fatalf("%s: items %v", queue, got)
		}
	}
}

// The item reservation follows the room's mode: a boss match reserves all 6 items of a player,
// a ranked one refuses a loadout over the global 3 (the player then plays without items).
func TestReserveItemsModeLimit(t *testing.T) {
	pool := newReservePool(t)
	ids := setModeItemGameData(t)
	ctx := context.Background()
	for _, id := range ids[:6] {
		if _, err := pool.Exec(ctx, `INSERT INTO inventory_items (player_id, item_id, quantity) VALUES ('p1', $1, 1), ('p2', $1, 1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	boss := &Room{ID: "r1", db: &database.PostgresDB{Pool: pool}, State: RoomState{Mode: "boss:lava_crab", Players: []RoomPlayer{
		{PlayerID: "p1", Items: append([]string(nil), ids[:6]...)},
	}}}
	if err := boss.reservePlayerItems("m_boss"); err != nil {
		t.Fatalf("boss reservation: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE match_id = 'm_boss' AND player_id = 'p1'`).Scan(&n); err != nil || n != 6 {
		t.Fatalf("boss reservations %d (%v)", n, err)
	}
	pvp := &Room{ID: "r2", db: &database.PostgresDB{Pool: pool}, State: RoomState{Mode: matchmaker.RankedQueue, Players: []RoomPlayer{
		{PlayerID: "p2", Items: append([]string(nil), ids[:4]...)},
	}}}
	var le *LoadoutError
	if err := pvp.reservePlayerItems("m_pvp"); !errors.As(err, &le) || le.PlayerID != "p2" || !strings.Contains(le.Reason, "at most 3 items") {
		t.Fatalf("ranked 4 items: %v", err)
	}
	pvp.reserveRankedItems("m_pvp")
	if len(pvp.State.Players[0].Items) != 0 {
		t.Fatalf("ranked items %v", pvp.State.Players[0].Items)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM inventory_reservations WHERE match_id = 'm_pvp'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ranked reservations %d (%v)", n, err)
	}
}
