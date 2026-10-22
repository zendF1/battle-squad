package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// lobbyModes lists the lobby rows as "id name category icon party ranked enabled order".
func lobbyModes(t *testing.T, db *database.PostgresDB) []string {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(), `SELECT mode_id, name, category, icon, party_size, ranked, enabled, sort_order
		FROM config_game_modes WHERE kind = 'lobby' ORDER BY sort_order, mode_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, name, cat, icon string
		var party, order int
		var ranked, enabled bool
		if err := rows.Scan(&id, &name, &cat, &icon, &party, &ranked, &enabled, &order); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s|%s|%s|%s|%d|%v|%v|%d", id, name, cat, icon, party, ranked, enabled, order))
	}
	return out
}

func modeCategories(t *testing.T, db *database.PostgresDB) []string {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(), `SELECT category_id, name, sort_order FROM config_mode_categories ORDER BY sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, name string
		var order int
		if err := rows.Scan(&id, &name, &order); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s|%s|%d", id, name, order))
	}
	return out
}

// 044 gives the lobby modes their category, icon, party size and ranked flag, adds the
// casual rows and the categories, renames the old default names; a rerun keeps admin edits;
// down / up gives the defaults back.
func TestModeSelectMigration(t *testing.T) {
	names := registered(t)
	if !slices.Contains(names, "044_mode_select.up.sql") {
		t.Fatalf("cmd/migrate does not run 044: %v", names)
	}
	db := dbtest.NewSchema(t, 0, names...)
	defaults := []string{
		"casual|Đấu thường 2vs2|pvp|swords|2|false|true|1",
		"ranked|Đấu hạng 2vs2|pvp|trophy|2|true|true|2",
		"bot|Đấu máy|pvp|robot|1|false|true|3",
		"practice|Luyện tập|pvp|target|1|false|true|4",
	}
	cats := []string{"pvp|PvP|1", "boss|Boss|2"}
	if got := lobbyModes(t, db); !reflect.DeepEqual(got, defaults) {
		t.Fatalf("lobby modes %v", got)
	}
	if got := modeCategories(t, db); !reflect.DeepEqual(got, cats) {
		t.Fatalf("categories %v", got)
	}
	var casualMatch string
	if err := db.Pool.QueryRow(context.Background(), `SELECT name FROM config_game_modes WHERE mode_id = 'casual_2v2' AND kind = 'match'`).
		Scan(&casualMatch); err != nil || casualMatch != "Đấu thường 2vs2" {
		t.Fatalf("casual_2v2 match row %q %v", casualMatch, err)
	}

	execSQL(t, db, `UPDATE config_game_modes SET name = 'Hạng', icon = 'crown', sort_order = 9 WHERE mode_id = 'ranked'`)
	execSQL(t, db, `UPDATE config_game_modes SET enabled = false, description = 'x' WHERE mode_id = 'casual'`)
	execSQL(t, db, `UPDATE config_game_modes SET sort_order = 1, category = 'boss' WHERE mode_id = 'practice'`)
	execSQL(t, db, `UPDATE config_mode_categories SET name = 'Đấu người' WHERE category_id = 'pvp'`)
	runSQL(t, db, "044_mode_select.up.sql")
	edited := []string{
		"casual|Đấu thường 2vs2|pvp|swords|2|false|false|1",
		"practice|Luyện tập|boss|target|1|false|true|1",
		"bot|Đấu máy|pvp|robot|1|false|true|3",
		"ranked|Hạng|pvp|crown|2|true|true|9",
	}
	if got := lobbyModes(t, db); !reflect.DeepEqual(got, edited) {
		t.Fatalf("a rerun lost an admin edit: %v", got)
	}
	if got := modeCategories(t, db); got[0] != "pvp|Đấu người|1" {
		t.Fatalf("a rerun lost a category edit: %v", got)
	}

	runSQL(t, db, "044_mode_select.down.sql")
	execSQL(t, db, `UPDATE config_game_modes SET name = 'Xếp hạng 2v2', sort_order = 2 WHERE mode_id = 'ranked'`)
	runSQL(t, db, "044_mode_select.up.sql")
	if got := lobbyModes(t, db); !reflect.DeepEqual(got, defaults) {
		t.Fatalf("down / up: %v", got)
	}
	if got := modeCategories(t, db); !reflect.DeepEqual(got, cats) {
		t.Fatalf("down / up categories %v", got)
	}
}

// The Game Modes page edits the new columns and rejects a mode the server cannot run; a
// category in use cannot be deleted.
func TestGameModesAdminChecks(t *testing.T) {
	_, db, h := adminServer(t)
	saved := func(rec *httptest.ResponseRecorder) bool {
		loc := rec.Header().Get("Location")
		return rec.Code == http.StatusSeeOther && strings.Contains(loc, "flash=") && !strings.Contains(loc, "error=")
	}
	mode := func(key, queue, category, party string, ranked bool) url.Values {
		v := url.Values{"new": {"1"}, "key": {key}, "c_kind": {"lobby"}, "c_name": {"Thử"}, "c_description": {""}, "c_category": {category},
			"c_icon": {"skull"}, "c_queue": {queue}, "c_bot": {""}, "c_party_size": {party}, "c_enabled": {"on"}, "c_sort_order": {"5"}}
		if ranked {
			v.Set("c_ranked", "on")
		}
		return v
	}
	for name, v := range map[string]url.Values{
		"unknown queue":        mode("boss_dragon", "boss_4p", "boss", "2", false),
		"casual marked ranked": mode("boss_dragon", "casual_2v2", "boss", "2", true),
		"ranked not marked":    mode("boss_dragon", "ranked_2v2", "boss", "2", false),
		"unknown category":     mode("boss_dragon", "casual_2v2", "raid", "2", false),
		"party without queue":  mode("boss_dragon", "", "boss", "2", false),
		"party too big":        mode("boss_dragon", "casual_2v2", "boss", "4", false),
	} {
		if rec := post(h, "/texts/game-modes/save", v); saved(rec) {
			t.Fatalf("%s accepted", name)
		}
	}
	if rec := post(h, "/texts/game-modes/save", mode("boss_dragon", "casual_2v2", "boss", "2", false)); !saved(rec) {
		t.Fatalf("valid mode: %s", rec.Header().Get("Location"))
	}
	if got := lobbyModes(t, db); !slices.Contains(got, "boss_dragon|Thử|boss|skull|2|false|true|5") {
		t.Fatalf("saved mode %v", got)
	}
	if rec := post(h, "/texts/mode-categories/delete", url.Values{"key": {"boss"}}); saved(rec) {
		t.Fatal("a category in use was deleted")
	}
	if rec := post(h, "/texts/game-modes/delete", url.Values{"key": {"boss_dragon"}}); !saved(rec) {
		t.Fatalf("delete mode: %s", rec.Header().Get("Location"))
	}
	if rec := post(h, "/texts/mode-categories/save", url.Values{"new": {"1"}, "key": {"event"}, "c_name": {"Sự kiện"}, "c_sort_order": {"3"}}); !saved(rec) {
		t.Fatalf("new category: %s", rec.Header().Get("Location"))
	}
	if body := get(h, "/texts/game-modes").Body.String(); !strings.Contains(body, "casual_2v2") || !strings.Contains(body, `name="c_party_size"`) {
		t.Fatal("game modes page lacks the new columns")
	}
}
