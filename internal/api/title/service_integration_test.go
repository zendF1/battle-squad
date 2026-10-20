package title

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"battle-squad/internal/api/milestone"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
	"battle-squad/internal/shared/observability"
)

func allMigrations(t *testing.T, upTo string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var names []string
	for _, f := range files {
		name := filepath.Base(f)
		if upTo != "" && name > upTo {
			break
		}
		names = append(names, name)
	}
	return names
}

func newService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, allMigrations(t, "")...)
	return NewService(NewRepository(db), db), db
}

func exec(t *testing.T, db *database.PostgresDB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func equippedID(t *testing.T, db *database.PostgresDB, playerID string) string {
	t.Helper()
	var id *string
	if err := db.Pool.QueryRow(context.Background(), `SELECT equipped_title_id FROM player_profiles WHERE player_id = $1`, playerID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id == nil {
		return "<null>"
	}
	return *id
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr model.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err %v, want %s", err, code)
	}
}

func call(t *testing.T, fn http.HandlerFunc, playerID, body string) (int, map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	if playerID != "" {
		req = req.WithContext(context.WithValue(req.Context(), observability.PlayerIDKey, playerID))
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return rec.Code, env, rec.Body.String()
}

func TestListShape(t *testing.T) {
	svc, db := newService(t, 0)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_titles (player_id, title_id, obtained_at) VALUES ('p1', 'smith', '2026-10-20T04:43:44.123+00:00')`)
	ctx := context.Background()

	list, err := svc.List(ctx, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Titles) != len(Defaults) || list.Equipped != nil {
		t.Fatalf("list %+v", list)
	}
	for i, d := range Defaults { // seeded in sort order
		got := list.Titles[i]
		if got.ID != d.ID || got.Name != d.Name || got.Rarity != d.Rarity || got.Description != d.Description {
			t.Errorf("title %d: %+v, want %+v", i, got, d)
		}
		if got.Owned != (d.ID == "smith") || (got.ObtainedAt != nil) != got.Owned {
			t.Errorf("%s: owned %v obtainedAt %v", d.ID, got.Owned, got.ObtainedAt)
		}
	}
	raw, _ := json.Marshal(list.Titles[:2])
	want := `[{"id":"veteran","name":"Chiến binh kỳ cựu","rarity":"blue","description":"1 nhân vật đạt cấp 40","owned":false,"obtainedAt":null},` +
		`{"id":"smith","name":"Thợ rèn lành nghề","rarity":"blue","description":"1 trang bị đạt +10","owned":true,"obtainedAt":"2026-10-20T04:43:44Z"}]`
	if string(raw) != want {
		t.Fatalf("json\n got %s\nwant %s", raw, want)
	}

	// Inactive titles are not listed; an equipped one that turns inactive reads as null
	if _, err := svc.Equip(ctx, "p1", "smith"); err != nil {
		t.Fatal(err)
	}
	if list, _ = svc.List(ctx, "p1"); list.Equipped == nil || *list.Equipped != (Ref{ID: "smith", Name: "Thợ rèn lành nghề", Rarity: "blue"}) {
		t.Fatalf("equipped %+v", list.Equipped)
	}
	exec(t, db, `UPDATE config_titles SET is_active = FALSE WHERE title_id = 'smith'`)
	list, _ = svc.List(ctx, "p1")
	if len(list.Titles) != len(Defaults)-1 || list.Equipped != nil {
		t.Fatalf("after deactivating %+v", list)
	}
	// Order: sort_order then id
	exec(t, db, `INSERT INTO config_titles (title_id, name, rarity, sort_order) VALUES ('aaa', 'A', 'red', 10)`)
	list, _ = svc.List(ctx, "p1")
	if list.Titles[0].ID != "aaa" || list.Titles[1].ID != "veteran" {
		t.Fatalf("order %s, %s", list.Titles[0].ID, list.Titles[1].ID)
	}
}

func TestEquip(t *testing.T) {
	svc, db := newService(t, 1) // a pool query while the equip tx holds the only connection hangs
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_titles (player_id, title_id) VALUES ('p1', 'smith'), ('p1', 'veteran')`)
	ctx := context.Background()

	ref, err := svc.Equip(ctx, "p1", "smith")
	if err != nil || ref == nil || *ref != (Ref{ID: "smith", Name: "Thợ rèn lành nghề", Rarity: "blue"}) || equippedID(t, db, "p1") != "smith" {
		t.Fatalf("equip %+v %v", ref, err)
	}
	// Not owned, unknown and inactive are TITLE_NOT_OWNED and change nothing
	for _, id := range []string{"grandmaster", "nope"} {
		_, err := svc.Equip(ctx, "p1", id)
		wantCode(t, err, "TITLE_NOT_OWNED")
	}
	exec(t, db, `UPDATE config_titles SET is_active = FALSE WHERE title_id = 'veteran'`)
	_, err = svc.Equip(ctx, "p1", "veteran")
	wantCode(t, err, "TITLE_NOT_OWNED")
	if equippedID(t, db, "p1") != "smith" {
		t.Fatalf("a refusal changed the equipped title: %s", equippedID(t, db, "p1"))
	}
	_, err = svc.Equip(ctx, "ghost", "smith")
	wantCode(t, err, "NOT_FOUND")

	if ref, err := svc.Equip(ctx, "p1", ""); err != nil || ref != nil || equippedID(t, db, "p1") != "<null>" {
		t.Fatalf("unequip %+v %v %s", ref, err, equippedID(t, db, "p1"))
	}
}

func TestHandlers(t *testing.T) {
	svc, db := newService(t, 0)
	h := NewHandler(svc)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	exec(t, db, `INSERT INTO player_titles (player_id, title_id) VALUES ('p1', 'legend_smith')`)

	code, _, body := call(t, h.Equip, "p1", `{"titleId":"legend_smith"}`)
	if code != 200 || !bytes.Contains([]byte(body), []byte(`"data":{"equipped":{"id":"legend_smith","name":"Thợ rèn huyền thoại","rarity":"red"}}`)) {
		t.Fatalf("equip %d %s", code, body)
	}
	code, env, _ := call(t, h.List, "p1", "")
	data, _ := env["data"].(map[string]any)
	titles, _ := data["titles"].([]any)
	eq, _ := data["equipped"].(map[string]any)
	if code != 200 || len(titles) != len(Defaults) || eq["id"] != "legend_smith" || eq["rarity"] != "red" {
		t.Fatalf("list %d %v", code, env)
	}
	first, _ := titles[0].(map[string]any)
	for _, k := range []string{"id", "name", "rarity", "description", "owned", "obtainedAt"} {
		if _, ok := first[k]; !ok {
			t.Errorf("title field %s missing: %v", k, first)
		}
	}

	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`{"titleId":"grandmaster"}`, 403, "TITLE_NOT_OWNED"},
		{`{"titleId":"nope"}`, 403, "TITLE_NOT_OWNED"},
		{`not json`, 400, "BAD_REQUEST"},
		{`{"titleId":5}`, 400, "BAD_REQUEST"},
	}
	for _, c := range cases {
		code, env, _ := call(t, h.Equip, "p1", c.body)
		e, _ := env["error"].(map[string]any)
		if code != c.status || e["code"] != c.code {
			t.Errorf("%s: %d %v, want %d %s", c.body, code, env, c.status, c.code)
		}
	}
	if equippedID(t, db, "p1") != "legend_smith" {
		t.Fatal("a refusal changed the equipped title")
	}
	for _, body := range []string{`{"titleId":null}`, `{}`, `{"titleId":""}`} {
		exec(t, db, `UPDATE player_profiles SET equipped_title_id = 'legend_smith' WHERE player_id = 'p1'`)
		code, _, out := call(t, h.Equip, "p1", body)
		if code != 200 || !bytes.Contains([]byte(out), []byte(`"data":{"equipped":null}`)) || equippedID(t, db, "p1") != "<null>" {
			t.Fatalf("unequip %s: %d %s", body, code, out)
		}
	}
	code, _, out := call(t, h.List, "p1", "")
	if code != 200 || !bytes.Contains([]byte(out), []byte(`"equipped":null`)) {
		t.Fatalf("list after unequip %d %s", code, out)
	}
	for _, fn := range []http.HandlerFunc{h.List, h.Equip} {
		code, env, _ := call(t, fn, "", `{}`)
		if e, _ := env["error"].(map[string]any); code != 401 || e["code"] != "AUTH_UNAUTHORIZED" {
			t.Errorf("no player: %d %v", code, env)
		}
	}
}

// The 027 backfill gives the title of every claimed title milestone once; reruns change
// nothing and keep the other reward items.
func TestMigrationBackfill(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t, "026_milestones.up.sql")...)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	dbtest.CreatePlayer(t, db, "p2", 0, 0)
	ctx := context.Background()
	exec(t, db, `INSERT INTO mission_progress (player_id, mission_id, current_value, is_claimed, updated_at) VALUES
		('p1', 'ms_char_lv_40', 40, TRUE, '2026-05-01T10:00:00Z'),
		('p1', 'ms_rank_master', 6, TRUE, '2026-06-01T10:00:00Z'),
		('p1', 'ms_char_lv_05', 40, TRUE, now()),
		('p2', 'ms_upgrade_10', 10, FALSE, now())`) // p2 has not claimed it

	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "027_titles.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	owned := func() string {
		var s *string
		if err := db.Pool.QueryRow(ctx, `SELECT string_agg(player_id || '/' || title_id || '@' || to_char(obtained_at AT TIME ZONE 'UTC', 'YYYY-MM-DD'), ',' ORDER BY player_id, title_id)
			FROM player_titles`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	for run := 0; run < 2; run++ {
		if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if got := owned(); got != "p1/grandmaster@2026-06-01,p1/veteran@2026-05-01" {
			t.Fatalf("run %d: owned %s", run, got)
		}
		var titles, items int
		if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_titles`).Scan(&titles); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM missions, jsonb_array_elements(reward_items) e WHERE e ->> 'kind' = 'title'`).Scan(&items); err != nil {
			t.Fatal(err)
		}
		if titles != len(Defaults) || items != len(milestone.TitleRewards) {
			t.Fatalf("run %d: %d titles, %d title items", run, titles, items)
		}
		for id, titleID := range milestone.TitleRewards {
			var n int
			if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM missions WHERE mission_id = $1 AND reward_items @> jsonb_build_array(jsonb_build_object('kind', 'title', 'id', $2::text))`, id, titleID).Scan(&n); err != nil || n != 1 {
				t.Fatalf("run %d: %s lacks title %s (%v)", run, id, titleID, err)
			}
		}
		var raw string
		if err := db.Pool.QueryRow(ctx, `SELECT reward_items::text FROM missions WHERE mission_id = 'ms_char_lv_40'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw != `[{"qty": 2, "kind": "stone", "level": 7}, {"id": "veteran", "kind": "title"}]` {
			t.Fatalf("run %d: ms_char_lv_40 items %s", run, raw)
		}
	}
	// Admin edits survive a rerun
	exec(t, db, `UPDATE config_titles SET name = 'Edited' WHERE title_id = 'smith'`)
	if _, err := db.Pool.Exec(ctx, string(sql)); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.Pool.QueryRow(ctx, `SELECT name FROM config_titles WHERE title_id = 'smith'`).Scan(&name); err != nil || name != "Edited" {
		t.Fatalf("rerun overwrote an edit: %s %v", name, err)
	}

	// Deleting a title clears it from the profiles wearing it
	exec(t, db, `UPDATE player_profiles SET equipped_title_id = 'veteran' WHERE player_id = 'p1'`)
	exec(t, db, `DELETE FROM config_titles WHERE title_id = 'veteran'`)
	if got := equippedID(t, db, "p1"); got != "<null>" {
		t.Fatalf("equipped after delete %s", got)
	}
}
