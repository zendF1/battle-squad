package player

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"battle-squad/internal/shared/database/dbtest"
)

// GET /player/profile carries equippedTitle: the worn title while it is owned and active,
// else null (always present).
func TestProfileEquippedTitle(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var names []string
	for _, f := range files {
		names = append(names, filepath.Base(f))
	}
	db := dbtest.NewSchema(t, 0, names...)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	svc := NewService(NewRepository(db))
	ctx := context.Background()
	exec := func(sql string) {
		t.Helper()
		if _, err := db.Pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	profileJSON := func() string {
		t.Helper()
		p, err := svc.GetProfile(ctx, "p1")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(p)
		return string(raw)
	}

	if got := profileJSON(); !strings.Contains(got, `"equippedTitle":null`) {
		t.Fatalf("no title: %s", got)
	}
	exec(`UPDATE player_profiles SET equipped_title_id = 'veteran' WHERE player_id = 'p1'`) // not owned
	if got := profileJSON(); !strings.Contains(got, `"equippedTitle":null`) {
		t.Fatalf("not owned: %s", got)
	}
	exec(`INSERT INTO player_titles (player_id, title_id) VALUES ('p1', 'veteran')`)
	if got := profileJSON(); !strings.Contains(got, `"equippedTitle":{"id":"veteran","name":"Chiến binh kỳ cựu","rarity":"blue"}`) {
		t.Fatalf("equipped: %s", got)
	}
	exec(`UPDATE config_titles SET is_active = FALSE WHERE title_id = 'veteran'`)
	if got := profileJSON(); !strings.Contains(got, `"equippedTitle":null`) {
		t.Fatalf("inactive: %s", got)
	}
}
