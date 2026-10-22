package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/gamedatasync"
)

// 045 / 046 seed the arenas, the three bosses (equal to gamedata.DefaultBosses), their lobby
// and match modes and player_boss_runs; a rerun keeps admin edits; down / up gives the
// defaults back.
func TestBossMigration(t *testing.T) {
	names := registered(t)
	if !slices.Contains(names, "045_boss_arenas.up.sql") || !slices.Contains(names, "046_boss_mode.up.sql") {
		t.Fatalf("cmd/migrate does not run 045 / 046: %v", names)
	}
	db := seededDB(t) // every registered migration, then the admin seed (characters, items)
	ctx := context.Background()
	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Bosses, gamedata.DefaultBosses()) {
		got, _ := json.Marshal(snap.Bosses)
		t.Fatalf("seeded bosses differ from DefaultBosses: %s", got)
	}
	for _, b := range snap.Bosses {
		if _, ok := snap.Data.Maps[b.MapID]; !ok {
			t.Fatalf("arena %s not loaded", b.MapID)
		}
		if snap.LobbyPartySize(b.Queue(), gamedata.BossPartySize) != 4 {
			t.Fatalf("%s lobby mode", b.BossID)
		}
	}

	execSQL(t, db, `UPDATE config_bosses SET hp = 999, name = 'Thép', skills = '[{"skillId":"x","name":"X","kind":"beam","weight":1,"damage":1,"halfWidth":10}]'
		WHERE boss_id = 'steel_robot'`)
	execSQL(t, db, `UPDATE config_game_modes SET party_size = 3 WHERE mode_id = 'boss_lava_crab'`)
	execSQL(t, db, `UPDATE config_maps SET name = 'Xưởng' WHERE map_id = 'arena_steel_robot'`)
	execSQL(t, db, `INSERT INTO player_boss_runs (player_id, boss_id, day, runs) SELECT player_id, 'steel_robot', '2026-10-22', 2 FROM player_profiles LIMIT 0`)
	runSQL(t, db, "045_boss_arenas.up.sql")
	runSQL(t, db, "046_boss_mode.up.sql")
	var hp, party int
	var name, skills, mapName string
	if err := db.Pool.QueryRow(ctx, `SELECT hp, name, skills::text FROM config_bosses WHERE boss_id = 'steel_robot'`).Scan(&hp, &name, &skills); err != nil ||
		hp != 999 || name != "Thép" || !strings.Contains(skills, `"skillId": "x"`) {
		t.Fatalf("a rerun lost a boss edit: %d %q %s %v", hp, name, skills, err)
	}
	_ = db.Pool.QueryRow(ctx, `SELECT party_size FROM config_game_modes WHERE mode_id = 'boss_lava_crab'`).Scan(&party)
	_ = db.Pool.QueryRow(ctx, `SELECT name FROM config_maps WHERE map_id = 'arena_steel_robot'`).Scan(&mapName)
	if party != 3 || mapName != "Xưởng" {
		t.Fatalf("a rerun lost a mode / arena edit: %d %q", party, mapName)
	}

	runSQL(t, db, "046_boss_mode.down.sql")
	runSQL(t, db, "046_boss_mode.up.sql")
	if snap, err := gamedata.LoadFromDB(ctx, db); err != nil || !reflect.DeepEqual(snap.Bosses, gamedata.DefaultBosses()) {
		t.Fatalf("down / up: %v", err)
	}
}

// The Boss page edits every field: a valid save is stored and bumps the game data revision;
// bad JSON, a bad number, an unknown item or character is refused and nothing changes. Game
// Modes accepts boss:<id> of an enabled boss with up to 4 players, nothing else; a boss used by
// a mode cannot be deleted.
func TestBossAdminPage(t *testing.T) {
	_, db, h := adminServer(t)
	ctx := context.Background()
	if rec := get(h, "/bosses"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Rô-bốt Thép") {
		t.Fatalf("list %d", rec.Code)
	}
	if rec := get(h, "/bosses/edit?id=lava_crab"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "fire_bombs") {
		t.Fatalf("edit %d", rec.Code)
	}

	b := gamedata.DefaultBosses()[0]
	form := func(mut func(url.Values)) url.Values {
		js := func(v any) string { out, _ := json.Marshal(v); return string(out) }
		v := url.Values{"boss_id": {b.BossID}, "name": {"Rô-bốt Thép X"}, "description": {b.Description}, "recommended_level": {"12"},
			"enabled": {"on"}, "sort_order": {"1"}, "map_id": {b.MapID}, "hp": {"15000"}, "defense": {"60"}, "damage": {"420"},
			"body_size": {"3"}, "turn_limit": {"12"}, "shielded_damage_factor": {"0.2"}, "out_of_map": {"clamp"}, "daily_bonus_runs": {"5"},
			"skills": {js(b.Skills)}, "minions": {js(b.Minions)}, "waves": {js(b.Waves)}, "rewards": {js(b.Rewards)}}
		if mut != nil {
			mut(v)
		}
		return v
	}
	revision := func() int64 {
		r, err := gamedatasync.Revision(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	before := revision()
	if rec := post(h, "/bosses/save", form(nil)); rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "flash=") {
		t.Fatalf("save %d %s", rec.Code, rec.Body)
	}
	if revision() == before {
		t.Fatal("save kept the revision")
	}
	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := snap.Boss("steel_robot")
	if saved.Name != "Rô-bốt Thép X" || saved.HP != 15000 || saved.TurnLimit != 12 || saved.ShieldedDamageFactor != 0.2 ||
		saved.DailyBonusRuns != 5 || saved.RecommendedLevel != 12 || !reflect.DeepEqual(saved.Skills, b.Skills) {
		t.Fatalf("saved %+v", saved)
	}

	for name, mut := range map[string]func(url.Values){
		"bad JSON":      func(v url.Values) { v.Set("skills", "[{") },
		"unknown field": func(v url.Values) { v.Set("waves", `{"initial":[],"maxAlive":4,"oops":1}`) },
		"bad number":    func(v url.Values) { v.Set("hp", "lots") },
		"zero hp":       func(v url.Values) { v.Set("hp", "0") },
		"unknown item": func(v url.Values) {
			v.Set("rewards", strings.Replace(form(nil).Get("rewards"), "medkit", "golden_ticket", 1))
		},
		"unknown char":   func(v url.Values) { v.Set("minions", strings.Replace(form(nil).Get("minions"), "spark", "nobody", 1)) },
		"unknown map":    func(v url.Values) { v.Set("map_id", "atlantis") },
		"unknown kind":   func(v url.Values) { v.Set("skills", `[{"skillId":"s","name":"S","kind":"meteor","weight":1}]`) },
		"factor above 1": func(v url.Values) { v.Set("shielded_damage_factor", "2") },
		"summon stranger": func(v url.Values) {
			v.Set("skills", `[{"skillId":"s","name":"S","kind":"summon","weight":1,"minionId":"ghost","count":1}]`)
		},
	} {
		rec := post(h, "/bosses/save", form(mut))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lưu thất bại") {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	var hp int
	_ = db.Pool.QueryRow(ctx, `SELECT hp FROM config_bosses WHERE boss_id = 'steel_robot'`).Scan(&hp)
	if hp != 15000 {
		t.Fatalf("a refused save changed hp to %d", hp)
	}

	saved1 := func(rec interface{ Header() http.Header }) bool {
		loc := rec.Header().Get("Location")
		return strings.Contains(loc, "flash=") && !strings.Contains(loc, "error=")
	}
	mode := func(key, queue, party string) url.Values {
		return url.Values{"new": {"1"}, "key": {key}, "c_kind": {"lobby"}, "c_name": {"Boss thử"}, "c_description": {""}, "c_category": {"boss"},
			"c_icon": {"boss"}, "c_queue": {queue}, "c_bot": {""}, "c_party_size": {party}, "c_enabled": {"on"}, "c_sort_order": {"20"}}
	}
	if rec := post(h, "/texts/game-modes/save", mode("boss_x", "boss:ice_witch", "4")); !saved1(rec) {
		t.Fatalf("boss mode of 4: %s", rec.Header().Get("Location"))
	}
	for name, v := range map[string]url.Values{
		"unknown boss": mode("boss_y", "boss:dragon", "4"),
		"party of 5":   mode("boss_y", "boss:ice_witch", "5"),
		"2v2 of 3":     mode("boss_y", "casual_2v2", "3"),
	} {
		if rec := post(h, "/texts/game-modes/save", v); saved1(rec) {
			t.Fatalf("%s accepted", name)
		}
	}
	execSQL(t, db, `UPDATE config_bosses SET enabled = false WHERE boss_id = 'lava_crab'`)
	if rec := post(h, "/texts/game-modes/save", mode("boss_z", "boss:lava_crab", "2")); saved1(rec) {
		// the existing boss_lava_crab row now fails too: any save is refused until it is fixed
		t.Fatal("a mode of a disabled boss was accepted")
	}
	execSQL(t, db, `UPDATE config_bosses SET enabled = true WHERE boss_id = 'lava_crab'`)

	if rec := post(h, "/bosses/delete", url.Values{"id": {"ice_witch"}}); !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Fatal("a boss used by a mode was deleted")
	}
	execSQL(t, db, `DELETE FROM config_game_modes WHERE queue = 'boss:ice_witch'`)
	if rec := post(h, "/bosses/delete", url.Values{"id": {"ice_witch"}}); !strings.Contains(rec.Header().Get("Location"), "flash=") {
		t.Fatalf("delete: %s", rec.Header().Get("Location"))
	}
	var n int
	_ = db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM config_bosses`).Scan(&n)
	if n != 2 {
		t.Fatalf("%s bosses left", strconv.Itoa(n))
	}
}
