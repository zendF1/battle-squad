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
// and match modes and player_boss_runs (047 / 048 / 049: boss v2, 050 / 051: boss v3, 052: status chance and the
// boss modes' 6 items); a rerun keeps admin edits;
// down / up gives the defaults back.
func TestBossMigration(t *testing.T) {
	names := registered(t)
	for _, f := range []string{"045_boss_arenas.up.sql", "046_boss_mode.up.sql", "047_boss_arenas_v2.up.sql", "048_boss_mode_v2.up.sql",
		"049_boss_placement.up.sql", "050_boss_mode_v3.up.sql", "051_boss_arena_islands.up.sql", "052_boss_status_items.up.sql"} {
		if !slices.Contains(names, f) {
			t.Fatalf("cmd/migrate does not run %s: %v", f, names)
		}
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
	crabSpawns := func() []gamedata.SpawnPoint {
		var raw []byte
		if err := db.Pool.QueryRow(ctx, `SELECT spawn_points FROM config_maps WHERE map_id = 'arena_lava_crab_mound'`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var out []gamedata.SpawnPoint
		_ = json.Unmarshal(raw, &out)
		return out
	}
	// 049: the mound's minion spawns off the crab's body (configs/maps.yaml has them too)
	if sp := crabSpawns(); len(sp) != 9 || sp[5].X != 936 || sp[6].X != 1464 || sp[5].Y != 764 ||
		!reflect.DeepEqual(sp, snap.Data.Maps["arena_lava_crab_mound"].SpawnPoints) {
		t.Fatalf("crab arena spawns %+v", sp)
	}
	for _, b := range snap.Bosses {
		if _, ok := snap.Data.Maps[b.MapID]; !ok {
			t.Fatalf("arena %s not loaded", b.MapID)
		}
		if snap.LobbyPartySize(b.Queue(), gamedata.BossPartySize) != 4 {
			t.Fatalf("%s lobby mode", b.BossID)
		}
	}

	// 052: the boss lobby modes take 6 items, the other modes the global rule; the status skills roll
	for _, b := range snap.Bosses {
		if n := snap.ModeMaxItems(b.Queue()); n != 6 {
			t.Fatalf("%s: %d items", b.BossID, n)
		}
	}
	if n := snap.ModeMaxItems("ranked_2v2"); n != snap.Rules.Items.MaxLoadout {
		t.Fatalf("ranked: %d items", n)
	}
	if crab, _ := snap.Boss("lava_crab"); crab.Skills[0].StatusChance != 0.5 || crab.Skills[0].Damage != 0.455 {
		t.Fatalf("crab 052 %+v", crab.Skills[0])
	}

	// 050 / 051: the free summons, the witch on the islands with her v3 numbers
	if witch, _ := snap.Boss("ice_witch"); witch.MapID != "arena_ice_witch_islands" || witch.Summon == nil || witch.Summon.Count != 5 ||
		witch.Minions[0].Behavior != gamedata.MinionLeaper || len(witch.AttackSkills()) != len(witch.Skills) {
		t.Fatalf("witch v3 %+v", witch)
	}

	execSQL(t, db, `UPDATE config_bosses SET hp = 999, name = 'Thép', skills = '[{"skillId":"x","name":"X","kind":"beam","weight":1,"damage":1,"halfWidth":10}]'
		WHERE boss_id = 'steel_robot'`)
	execSQL(t, db, `UPDATE config_game_modes SET party_size = 3 WHERE mode_id = 'boss_lava_crab'`)
	execSQL(t, db, `UPDATE config_maps SET name = 'Xưởng' WHERE map_id = 'arena_steel_robot'`)
	execSQL(t, db, `INSERT INTO player_boss_runs (player_id, boss_id, day, runs) SELECT player_id, 'steel_robot', '2026-10-22', 2 FROM player_profiles LIMIT 0`)
	// boss v2 edits: a minion, the witch's turn limit, hp, flying, hover and arena, a v2 arena
	execSQL(t, db, `UPDATE config_bosses SET minions = jsonb_set(minions, '{0,flyRange}', '150') WHERE boss_id = 'steel_robot'`)
	execSQL(t, db, `UPDATE config_bosses SET turn_limit = 25, hp = 27000, flying = false, hover = NULL, map_id = 'arena_steel_robot'
		WHERE boss_id = 'ice_witch'`)
	execSQL(t, db, `UPDATE config_bosses SET waves = '{"initial":[{"minionId":"crab","count":1}],"maxAlive":2}' WHERE boss_id = 'lava_crab'`)
	execSQL(t, db, `UPDATE config_maps SET name = 'Gò', spawn_points = jsonb_set(spawn_points, '{5,x}', '900') WHERE map_id = 'arena_lava_crab_mound'`)
	// boss v3 edits: the crab's summon and a skill, the witch's snowmen, the islands arena
	execSQL(t, db, `UPDATE config_bosses SET summon = jsonb_set(summon, '{count}', '2'), damage = 700,
		skills = jsonb_set(skills, '{1,minFactor}', '0.5') WHERE boss_id = 'lava_crab'`)
	execSQL(t, db, `UPDATE config_bosses SET minions = jsonb_set(minions, '{0,explodeDamagePct}', '25') WHERE boss_id = 'ice_witch'`)
	execSQL(t, db, `UPDATE config_maps SET name = 'Đảo' WHERE map_id = 'arena_ice_witch_islands'`)
	// 052 edits: a boss mode's items, the witch's frost beam chance
	execSQL(t, db, `UPDATE config_game_modes SET max_items = 4 WHERE mode_id = 'boss_ice_witch'`)
	execSQL(t, db, `UPDATE config_bosses SET skills = jsonb_set(skills, '{1,statusChance}', '0.8') WHERE boss_id = 'ice_witch'`)
	for _, f := range []string{"045_boss_arenas", "046_boss_mode", "047_boss_arenas_v2", "048_boss_mode_v2", "049_boss_placement",
		"050_boss_mode_v3", "051_boss_arena_islands", "052_boss_status_items"} {
		runSQL(t, db, f+".up.sql")
	}
	if sp := crabSpawns(); sp[5].X != 900 || sp[6].X != 1464 {
		t.Fatalf("a rerun lost a crab arena spawn edit: %+v", sp)
	}
	{
		snap, err := gamedata.LoadFromDB(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		robot, _ := snap.Boss("steel_robot")
		witch, _ := snap.Boss("ice_witch")
		crab, _ := snap.Boss("lava_crab")
		var arena, islands string
		_ = db.Pool.QueryRow(ctx, `SELECT name FROM config_maps WHERE map_id = 'arena_lava_crab_mound'`).Scan(&arena)
		_ = db.Pool.QueryRow(ctx, `SELECT name FROM config_maps WHERE map_id = 'arena_ice_witch_islands'`).Scan(&islands)
		if robot.Minions[0].FlyRange != 150 || witch.TurnLimit != 25 || witch.HP != 27000 || witch.Flying || witch.Hover != nil ||
			witch.MapID != "arena_steel_robot" || crab.Waves.MaxAlive != 2 || arena != "Gò" {
			t.Fatalf("a rerun lost a v2 edit: robot %+v witch %d %d %v %v %s crab %+v arena %q", robot.Minions[0], witch.TurnLimit, witch.HP,
				witch.Flying, witch.Hover, witch.MapID, crab.Waves, arena)
		}
		if n := snap.ModeMaxItems("boss:ice_witch"); n != 4 || witch.Skills[1].StatusChance != 0.8 || witch.HP != 27000 {
			t.Fatalf("a rerun lost a 052 edit: %d items, frost beam %+v", n, witch.Skills[1])
		}
		if crab.Summon == nil || crab.Summon.Count != 2 || crab.Damage != 700 || crab.Skills[1].MinFactor != 0.5 ||
			witch.Minions[0].ExplodeDamagePct != 25 || islands != "Đảo" {
			t.Fatalf("a rerun lost a v3 edit: crab %+v %d %+v witch %+v islands %q", crab.Summon, crab.Damage, crab.Skills[1], witch.Minions[0], islands)
		}
	}
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

	runSQL(t, db, "052_boss_status_items.down.sql")
	runSQL(t, db, "051_boss_arena_islands.down.sql")
	runSQL(t, db, "050_boss_mode_v3.down.sql")
	runSQL(t, db, "049_boss_placement.down.sql")
	runSQL(t, db, "048_boss_mode_v2.down.sql")
	runSQL(t, db, "046_boss_mode.down.sql")
	runSQL(t, db, "047_boss_arenas_v2.down.sql")
	runSQL(t, db, "047_boss_arenas_v2.up.sql")
	runSQL(t, db, "046_boss_mode.up.sql")
	runSQL(t, db, "048_boss_mode_v2.up.sql")
	// 047 / 048 alone: the f1e239f spawns and band; 049 moves them, its down moves them back
	if sp := crabSpawns(); sp[5].X != 1080 || sp[6].X != 1320 {
		t.Fatalf("047 crab spawns %+v", sp)
	}
	runSQL(t, db, "049_boss_placement.up.sql")
	runSQL(t, db, "049_boss_placement.up.sql")
	if sp := crabSpawns(); sp[5].X != 936 || sp[6].X != 1464 {
		t.Fatalf("049 crab spawns %+v", sp)
	}
	runSQL(t, db, "049_boss_placement.down.sql")
	var minY float64
	_ = db.Pool.QueryRow(ctx, `SELECT (hover->>'minY')::float8 FROM config_bosses WHERE boss_id = 'ice_witch'`).Scan(&minY)
	if sp := crabSpawns(); sp[5].X != 1080 || minY != 200 {
		t.Fatalf("049 down: crab spawns %+v, witch minY %v", sp, minY)
	}
	runSQL(t, db, "049_boss_placement.up.sql")
	// 050 / 051 on the v2 rows: the v3 defaults; 050's down gives the v2 rows back (summoning with
	// the v2 summon skills), its up the v3 ones again
	runSQL(t, db, "050_boss_mode_v3.up.sql")
	runSQL(t, db, "051_boss_arena_islands.up.sql")
	runSQL(t, db, "051_boss_arena_islands.up.sql")
	runSQL(t, db, "052_boss_status_items.up.sql")
	runSQL(t, db, "052_boss_status_items.up.sql")
	if snap, err := gamedata.LoadFromDB(ctx, db); err != nil || !reflect.DeepEqual(snap.Bosses, gamedata.DefaultBosses()) ||
		snap.ModeMaxItems("boss:lava_crab") != 6 {
		t.Fatalf("down / up: %v", err)
	}
	// 052's down gives the v3 skills, HP and the global item rule back
	runSQL(t, db, "052_boss_status_items.down.sql")
	{
		var cols, witchHP int
		var beamDamage float64
		var beamChance, crabDamage string
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
			AND table_name = 'config_game_modes' AND column_name = 'max_items'`).Scan(&cols)
		_ = db.Pool.QueryRow(ctx, `SELECT hp, (skills->1->>'damage')::float8, COALESCE(skills->1->>'statusChance', '') FROM config_bosses
			WHERE boss_id = 'ice_witch'`).Scan(&witchHP, &beamDamage, &beamChance)
		_ = db.Pool.QueryRow(ctx, `SELECT skills->0->>'damage' FROM config_bosses WHERE boss_id = 'lava_crab'`).Scan(&crabDamage)
		if cols != 0 || witchHP != 13000 || beamDamage != 0.8 || beamChance != "" || crabDamage != "0.35" {
			t.Fatalf("052 down: %d max_items columns, witch %d %v %q, crab %s", cols, witchHP, beamDamage, beamChance, crabDamage)
		}
	}
	runSQL(t, db, "050_boss_mode_v3.down.sql")
	{
		var cols int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
			AND table_name = 'config_bosses' AND column_name = 'summon'`).Scan(&cols)
		var witchMap, crabSkills string
		var witchHP int
		_ = db.Pool.QueryRow(ctx, `SELECT map_id, hp FROM config_bosses WHERE boss_id = 'ice_witch'`).Scan(&witchMap, &witchHP)
		_ = db.Pool.QueryRow(ctx, `SELECT skills::text FROM config_bosses WHERE boss_id = 'lava_crab'`).Scan(&crabSkills)
		if cols != 0 || witchMap != "arena_ice_witch_pillars" || witchHP != 30000 || !strings.Contains(crabSkills, "crab_wave") {
			t.Fatalf("050 down: %d summon columns, witch %s %d, crab skills %s", cols, witchMap, witchHP, crabSkills)
		}
	}
	runSQL(t, db, "050_boss_mode_v3.up.sql")
	runSQL(t, db, "050_boss_mode_v3.up.sql")
	runSQL(t, db, "052_boss_status_items.up.sql")
	if snap, err := gamedata.LoadFromDB(ctx, db); err != nil || !reflect.DeepEqual(snap.Bosses, gamedata.DefaultBosses()) {
		t.Fatalf("050 down / up: %v", err)
	}
	runSQL(t, db, "052_boss_status_items.down.sql")
	// 048's down gives the phase 1 rows back (still loadable)
	runSQL(t, db, "050_boss_mode_v3.down.sql")
	runSQL(t, db, "049_boss_placement.down.sql")
	runSQL(t, db, "048_boss_mode_v2.down.sql")
	var cols int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'config_bosses' AND column_name IN ('flying', 'hover')`).Scan(&cols)
	var witchMap string
	var witchTurns int
	_ = db.Pool.QueryRow(ctx, `SELECT map_id, turn_limit FROM config_bosses WHERE boss_id = 'ice_witch'`).Scan(&witchMap, &witchTurns)
	if cols != 0 || witchMap != "arena_ice_witch" || witchTurns != 15 {
		t.Fatalf("048 down: %d columns, witch %s %d", cols, witchMap, witchTurns)
	}
	runSQL(t, db, "048_boss_mode_v2.up.sql")
	runSQL(t, db, "049_boss_placement.up.sql")
	runSQL(t, db, "050_boss_mode_v3.up.sql")
	runSQL(t, db, "052_boss_status_items.up.sql")
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

	// boss v2 fields: flying + hover, the minion behavior / movement / blast numbers
	drone := `[{"minionId":"drone","name":"Drone","characterId":"spark","hp":650,"defense":20,"damage":160,"moveEnergy":80,"bodySize":0.7,
		"behavior":"bomber","movement":"fly","flyRange":120,"explodeDamageOf":"self","explodeDamagePct":40,"explodeOnDeath":true}]`
	v2 := form(func(v url.Values) {
		v.Set("flying", "on")
		v.Set("hover", `{"minX": 300, "maxX": 2100, "minY": 150, "maxY": 400, "minMove": 250}`)
		v.Set("minions", drone)
	})
	if rec := post(h, "/bosses/save", v2); rec.Code != http.StatusSeeOther {
		t.Fatalf("v2 save %d %s", rec.Code, rec.Body)
	}
	snap, _ = gamedata.LoadFromDB(ctx, db)
	saved, _ = snap.Boss("steel_robot")
	if m := saved.Minions[0]; !saved.Flying || saved.Hover == nil || saved.Hover.MinMove != 250 || saved.Hover.MaxX != 2100 ||
		m.Behavior != "bomber" || m.FlyRange != 120 || m.ExplodeDamageOf != "self" || m.ExplodeDamagePct != 40 || !m.ExplodeOnDeath {
		t.Fatalf("v2 saved %+v %+v", saved, saved.Hover)
	}
	if rec := get(h, "/bosses/edit?id=steel_robot"); !strings.Contains(rec.Body.String(), `minMove&#34;: 250`) {
		t.Fatal("the edit page does not show the hover band")
	}
	for name, mut := range map[string]func(url.Values){
		"hover grounded": func(v url.Values) { v.Del("flying") },
		"hover band":     func(v url.Values) { v.Set("hover", `{"minX": 900, "maxX": 100, "minY": 1, "maxY": 2}`) },
		"hover field":    func(v url.Values) { v.Set("hover", `{"minX": 1, "speed": 2}`) },
		"behavior":       func(v url.Values) { v.Set("minions", strings.Replace(drone, "bomber", "kamikaze", 1)) },
		"movement":       func(v url.Values) { v.Set("minions", strings.Replace(drone, `"fly"`, `"swim"`, 1)) },
		"explode of":     func(v url.Values) { v.Set("minions", strings.Replace(drone, `"self"`, `"boss"`, 1)) },
		"explode pct": func(v url.Values) {
			v.Set("minions", strings.Replace(drone, `"explodeDamagePct":40`, `"explodeDamagePct":400`, 1))
		},
	} {
		vals := form(func(v url.Values) {
			for k, x := range v2 {
				v[k] = x
			}
			mut(v)
		})
		if rec := post(h, "/bosses/save", vals); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lưu thất bại") {
			t.Fatalf("%s: %d %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	// a grounded boss without a band clears both
	if rec := post(h, "/bosses/save", form(nil)); rec.Code != http.StatusSeeOther {
		t.Fatalf("plain save %d", rec.Code)
	}
	snap, _ = gamedata.LoadFromDB(ctx, db)
	if saved, _ = snap.Boss("steel_robot"); saved.Flying || saved.Hover != nil || saved.Summon != nil {
		t.Fatalf("flying %v hover %v summon %v", saved.Flying, saved.Hover, saved.Summon)
	}
	// boss v3: the summon config, a leaper and a wide skill
	v3 := form(func(v url.Values) {
		v.Set("summon", `{"minionId": "drone", "count": 3, "cooldownRounds": 2, "firstRound": 3}`)
		v.Set("waves", `{"initial": [{"minionId": "drone", "count": 2}], "maxAlive": 3}`)
	})
	if rec := post(h, "/bosses/save", v3); rec.Code != http.StatusSeeOther {
		t.Fatalf("v3 save %d %s", rec.Code, rec.Body)
	}
	snap, _ = gamedata.LoadFromDB(ctx, db)
	if saved, _ = snap.Boss("steel_robot"); saved.Summon == nil || *saved.Summon != (gamedata.BossSummon{MinionID: "drone", Count: 3, CooldownRounds: 2, FirstRound: 3}) {
		t.Fatalf("summon %+v", saved.Summon)
	}
	if rec := get(h, "/bosses/edit?id=steel_robot"); !strings.Contains(rec.Body.String(), `cooldownRounds&#34;: 2`) {
		t.Fatal("the edit page does not show the summon")
	}
	for name, summon := range map[string]string{
		"summon minion": `{"minionId": "ghost", "count": 1, "cooldownRounds": 0, "firstRound": 1}`,
		"summon field":  `{"minionId": "drone", "every": 2}`,
		"summon count":  `{"minionId": "drone", "count": 0, "cooldownRounds": 0, "firstRound": 1}`,
	} {
		bad := form(func(v url.Values) { v.Set("summon", summon) })
		if rec := post(h, "/bosses/save", bad); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Lưu thất bại") {
			t.Fatalf("%s: %d", name, rec.Code)
		}
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
	// max_items (052): a number 0..20, empty = NULL (the global rule); the page shows it
	maxItems := func(key string) string {
		var v string
		_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(max_items::text, 'NULL') FROM config_game_modes WHERE mode_id = $1`, key).Scan(&v)
		return v
	}
	if maxItems("boss_x") != "NULL" || maxItems("boss_lava_crab") != "6" {
		t.Fatalf("max_items boss_x %s, boss_lava_crab %s", maxItems("boss_x"), maxItems("boss_lava_crab"))
	}
	withItems := mode("boss_x", "boss:ice_witch", "4")
	withItems.Del("new")
	withItems.Set("c_max_items", "5")
	if rec := post(h, "/texts/game-modes/save", withItems); !saved1(rec) || maxItems("boss_x") != "5" {
		t.Fatalf("max_items 5: %s, %s", rec.Header().Get("Location"), maxItems("boss_x"))
	}
	if body := get(h, "/texts/game-modes").Body.String(); !strings.Contains(body, `name="c_max_items"`) {
		t.Fatal("the Game Modes page has no max_items")
	}
	withItems.Set("c_max_items", "21")
	if rec := post(h, "/texts/game-modes/save", withItems); saved1(rec) || maxItems("boss_x") != "5" {
		t.Fatal("max_items 21 accepted")
	}
	withItems.Set("c_max_items", "")
	if rec := post(h, "/texts/game-modes/save", withItems); !saved1(rec) || maxItems("boss_x") != "NULL" {
		t.Fatalf("empty max_items: %s", maxItems("boss_x"))
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
