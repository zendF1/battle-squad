package match

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/ws"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

// newReservedMatch builds a tutorial-like match (p1 against an idle bot) on a
// throwaway schema where p1 owns 2 medkits, both reserved for the match.
func newReservedMatch(t *testing.T, db *database.PostgresDB, matchID string) (*Match, *ws.Client) {
	t.Helper()
	ctx := context.Background()
	setFlowGameData(t)
	if _, err := db.Pool.Exec(ctx, `INSERT INTO inventory_items (player_id, item_id, quantity, source) VALUES ('p1', 'medkit', 2, 'test')
		ON CONFLICT (player_id, item_id) DO UPDATE SET quantity = 2`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO inventory_reservations (reservation_id, player_id, match_id, item_id, quantity, status)
			VALUES ($1, 'p1', $2, 'medkit', 1, 'reserved')`, fmt.Sprintf("%s_r%d", matchID, i), matchID); err != nil {
			t.Fatal(err)
		}
	}

	players := []*BattlePlayerState{
		{PlayerID: "p1", TeamID: 1, CharacterID: "c1", HP: 100, MaxHP: 100, Position: Vector2{X: 200, Y: 300}, IsAlive: true, Items: []string{"medkit", "medkit"}},
		{PlayerID: "bot_test", TeamID: 2, CharacterID: "c1", HP: 100, MaxHP: 100, Position: Vector2{X: 1400, Y: 300}, IsAlive: true, IsBot: true, IsIdle: true},
	}
	client := &ws.Client{PlayerID: "p1", Send: make(chan ws.Message, 256)}
	m := NewMatch(matchID, "r_"+matchID, "pvp_1v1", "none", players, map[string]*ws.Client{"p1": client}, db, nil, economy.NewRepository(), nil)
	t.Cleanup(m.Stop)
	return m, client
}

// newMatchSchema applies every migration, as cmd/migrate does: the rewards
// transaction reads tables from several of them.
func newMatchSchema(t *testing.T) *database.PostgresDB {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = filepath.Base(f)
	}
	return dbtest.NewSchema(t, 0, names...)
}

func reservationCounts(t *testing.T, db *database.PostgresDB, matchID string) map[string]int {
	t.Helper()
	rows, err := db.Pool.Query(context.Background(),
		`SELECT status, COUNT(*) FROM inventory_reservations WHERE match_id = $1 GROUP BY status`, matchID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			t.Fatal(err)
		}
		counts[status] = n
	}
	return counts
}

func medkits(t *testing.T, db *database.PostgresDB) int {
	t.Helper()
	var q int
	if err := db.Pool.QueryRow(context.Background(), `SELECT quantity FROM inventory_items WHERE player_id = 'p1' AND item_id = 'medkit'`).Scan(&q); err != nil {
		t.Fatal(err)
	}
	return q
}

func TestStoppedMatchReleasesReservationsDB(t *testing.T) {
	db := newMatchSchema(t)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	m, _ := newReservedMatch(t, db, "m_stop")

	done := runMatch(t, m)
	m.Stop()
	waitRun(t, done)

	if got := reservationCounts(t, db, "m_stop"); got["released"] != 2 || len(got) != 1 {
		t.Fatalf("reservations = %v, want 2 released", got)
	}
	if q := medkits(t, db); q != 2 {
		t.Fatalf("medkits = %d, want 2 (nothing consumed)", q)
	}
}

func TestEndedMatchConsumesUsedItemOnceDB(t *testing.T) {
	db := newMatchSchema(t)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	m, client := newReservedMatch(t, db, "m_end")
	m.State.Players["p1"].UsedItems = []string{"medkit"}
	settle := m.processRewards
	var rewardsErr error
	m.processRewards = func(ctx context.Context, stats map[string]*PlayerStats, usedItems map[string][]string) (map[string]RewardResult, error) {
		r, err := settle(ctx, stats, usedItems)
		rewardsErr = err
		return r, err
	}

	done := runMatch(t, m)
	m.LeaveAndStop(context.Background(), client)
	waitRun(t, done)

	if rewardsErr != nil {
		t.Fatalf("rewards: %v", rewardsErr)
	}

	// Run's exit already ran; repeating the release must change nothing
	m.reservationsSettled = false
	m.releaseUnsettledReservations()
	if err := ReleaseMatchReservations(context.Background(), db, "m_end"); err != nil {
		t.Fatal(err)
	}

	if got := reservationCounts(t, db, "m_end"); got["consumed"] != 1 || got["released"] != 1 || len(got) != 2 {
		t.Fatalf("reservations = %v, want 1 consumed and 1 released", got)
	}
	if q := medkits(t, db); q != 1 {
		t.Fatalf("medkits = %d, want 1 (one used)", q)
	}
	var histories int
	if err := db.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM match_histories WHERE match_id = 'm_end'`).Scan(&histories); err != nil {
		t.Fatal(err)
	}
	if histories != 1 {
		t.Fatalf("match histories = %d, want 1 (rewards paid once)", histories)
	}
}
