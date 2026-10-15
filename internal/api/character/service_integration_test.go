package character

import (
	"context"
	"errors"
	"sync"
	"testing"

	"battle-squad/internal/api/economy"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
	"battle-squad/internal/shared/model"
)

// newCharacterService uses the migrations/006 progression seed: reset costs 500 coin.
func newCharacterService(t *testing.T, maxConns int32) (*Service, *database.PostgresDB) {
	t.Helper()
	db := dbtest.NewSchema(t, maxConns, "001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql")
	return NewService(NewRepository(db), economy.NewRepository(), db), db
}

func addCharacter(t *testing.T, db *database.PostgresDB, playerID, characterID string, statPoints, bonusHP, bonusSkillPower int) {
	t.Helper()
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO player_characters (player_id, character_id, stat_points, bonus_hp, bonus_skill_power)
		VALUES ($1, $2, $3, $4, $5)`, playerID, characterID, statPoints, bonusHP, bonusSkillPower); err != nil {
		t.Fatal(err)
	}
}

func characterPoints(t *testing.T, db *database.PostgresDB, playerID, characterID string) (statPoints, allocated int) {
	t.Helper()
	if err := db.Pool.QueryRow(context.Background(), `
		SELECT stat_points, bonus_hp + bonus_damage + bonus_mobility + bonus_defense + bonus_skill_power + bonus_terrain_damage
		FROM player_characters WHERE player_id = $1 AND character_id = $2`, playerID, characterID).Scan(&statPoints, &allocated); err != nil {
		t.Fatal(err)
	}
	return statPoints, allocated
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var appErr model.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("err %v, want %s", err, code)
	}
}

// One pool connection: the reset must not query through the pool while its tx holds it.
func TestResetStatsChargesAndRefundsInOneTx(t *testing.T) {
	svc, db := newCharacterService(t, 1)
	dbtest.CreatePlayer(t, db, "p1", 600, 0)
	addCharacter(t, db, "p1", "tanko", 2, 3, 5)

	if err := svc.ResetStats(context.Background(), "p1", "tanko"); err != nil {
		t.Fatal(err)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 100 {
		t.Fatalf("coin %d, want 100", coin)
	}
	if pts, alloc := characterPoints(t, db, "p1", "tanko"); pts != 10 || alloc != 0 {
		t.Fatalf("stat_points %d allocated %d, want 10 and 0", pts, alloc)
	}
}

func TestResetStatsRejectionsChargeNothing(t *testing.T) {
	svc, db := newCharacterService(t, 1)
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 600, 0)
	dbtest.CreatePlayer(t, db, "poor", 499, 0)
	addCharacter(t, db, "p1", "spark", 4, 0, 0)
	addCharacter(t, db, "poor", "tanko", 0, 2, 0)

	wantCode(t, svc.ResetStats(ctx, "p1", "flora"), "CHARACTER_NOT_FOUND")
	wantCode(t, svc.ResetStats(ctx, "nobody", "flora"), "CHARACTER_NOT_FOUND")
	wantCode(t, svc.ResetStats(ctx, "p1", "spark"), "CHARACTER_NO_STATS_TO_RESET")
	wantCode(t, svc.ResetStats(ctx, "poor", "tanko"), "CHARACTER_INSUFFICIENT_BALANCE")

	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 600 {
		t.Fatalf("p1 coin %d, want 600", coin)
	}
	if coin, _ := dbtest.Balance(t, db, "poor"); coin != 499 {
		t.Fatalf("poor coin %d, want 499", coin)
	}
	if pts, alloc := characterPoints(t, db, "poor", "tanko"); pts != 0 || alloc != 2 {
		t.Fatalf("failed reset changed points: stat_points %d allocated %d", pts, alloc)
	}
}

func TestConcurrentResetsChargeOnce(t *testing.T) {
	svc, db := newCharacterService(t, 8)
	dbtest.CreatePlayer(t, db, "p1", 5000, 0)
	addCharacter(t, db, "p1", "tanko", 0, 4, 1)

	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = svc.ResetStats(context.Background(), "p1", "tanko")
		}()
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		if err == nil {
			ok++
			continue
		}
		wantCode(t, err, "CHARACTER_NO_STATS_TO_RESET")
	}
	if ok != 1 {
		t.Fatalf("%d resets succeeded, want 1", ok)
	}
	if coin, _ := dbtest.Balance(t, db, "p1"); coin != 4500 {
		t.Fatalf("coin %d, want 4500 (charged once)", coin)
	}
	if pts, alloc := characterPoints(t, db, "p1", "tanko"); pts != 5 || alloc != 0 {
		t.Fatalf("stat_points %d allocated %d, want 5 and 0", pts, alloc)
	}
}
