package admin

import (
	"context"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

// 054 seeds the elements (config_elements, rules.elements, every character's and boss's), the
// bounce physics, Tide (her character, weapon and skill equal to configs/*.yaml, gun kind, type
// word, offers, tips) and Tanko's earth texts; 055 Tide's guns (equal to the admin seed). A
// rerun keeps admin edits; down / up gives the defaults back; a down keeps a Tide someone owns.
func TestElementsMigration(t *testing.T) {
	names := registered(t)
	for _, f := range []string{"054_elements.up.sql", "055_tide_guns.up.sql"} {
		if !slices.Contains(names, f) {
			t.Fatalf("cmd/migrate does not run %s: %v", f, names)
		}
	}
	db := seededDB(t)
	ctx := context.Background()
	yaml, err := gamedata.LoadYAML(configsDir())
	if err != nil {
		t.Fatal(err)
	}
	checkSeeded := func(when string) {
		t.Helper()
		snap, err := gamedata.LoadFromDB(ctx, db)
		if err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if !reflect.DeepEqual(snap.Elements, gamedata.DefaultElements()) || snap.Rules.Elements != gamedata.DefaultRules().Elements ||
			snap.Rules.Physics != gamedata.DefaultRules().Physics {
			t.Fatalf("%s: elements %+v rules %+v physics %+v", when, snap.Elements, snap.Rules.Elements, snap.Rules.Physics)
		}
		for id, c := range yaml.Data.Characters {
			if got := snap.Data.Characters[id]; got.Element != c.Element || c.Element == "" {
				t.Fatalf("%s: %s element %q, yaml %q", when, id, got.Element, c.Element)
			}
		}
		if !reflect.DeepEqual(snap.Data.Characters["tide"], yaml.Data.Characters["tide"]) ||
			!reflect.DeepEqual(snap.Data.Weapons["water_orb"], yaml.Data.Weapons["water_orb"]) ||
			!reflect.DeepEqual(snap.Data.Skills["snow_nova"], yaml.Data.Skills["snow_nova"]) {
			t.Fatalf("%s: tide %+v\n%+v\n%+v", when, snap.Data.Characters["tide"], snap.Data.Weapons["water_orb"], snap.Data.Skills["snow_nova"])
		}
		for _, id := range []string{"basic_rocket", "cannon_ball", "electric_orb", "seed_bomb"} {
			if snap.Data.Weapons[id].Bounces != 0 {
				t.Fatalf("%s: %s bounces", when, id)
			}
		}
		hb, yhb := snap.Data.Skills["heavy_bomb"], yaml.Data.Skills["heavy_bomb"]
		if hb.Name != "Địa chấn liên hoàn" || hb.Name != yhb.Name || hb.Description != yhb.Description || hb.DescriptionTemplate != yhb.DescriptionTemplate ||
			snap.Data.Characters["tanko"].Playstyle != yaml.Data.Characters["tanko"].Playstyle {
			t.Fatalf("%s: tanko %+v", when, hb)
		}
		if !reflect.DeepEqual(snap.Bosses, gamedata.DefaultBosses()) {
			t.Fatalf("%s: bosses differ from DefaultBosses", when)
		}
		var gun, word, offers, tips, guns int
		_ = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM config_gun_kinds WHERE gun_kind = 'water_gun' AND name = 'Súng nước'),
			(SELECT count(*) FROM config_item_type_words WHERE word = 'water gun' AND gun_kind = 'water_gun'),
			(SELECT count(*) FROM shop_offers WHERE item_id = 'tide' AND offer_type = 'character_unlock' AND is_active),
			(SELECT count(*) FROM config_tips WHERE tip_id IN ('tip_13', 'tip_14') OR text LIKE 'Địa chấn liên hoàn%'),
			(SELECT count(*) FROM config_equipment_items WHERE character_id = 'tide' AND slot = 'weapon' AND is_active)`).Scan(&gun, &word, &offers, &tips, &guns)
		if gun != 1 || word != 1 || offers != 2 || tips != 3 || guns != 4 {
			t.Fatalf("%s: gun kind %d, type word %d, offers %d, tips %d, guns %d", when, gun, word, offers, tips, guns)
		}
	}
	checkSeeded("fresh install")

	// admin edits survive a rerun of every migration
	execSQL(t, db, `UPDATE config_characters SET element = 'fire' WHERE character_id = 'tide'`)
	execSQL(t, db, `UPDATE config_characters SET element = '' WHERE character_id = 'rookie'`)
	execSQL(t, db, `UPDATE config_bosses SET element = 'wood' WHERE boss_id = 'steel_robot'`)
	execSQL(t, db, `UPDATE config_elements SET name = 'Kim loại' WHERE element_id = 'metal'`)
	execSQL(t, db, `UPDATE game_settings SET value = '{"pvpStrongPct": 15, "pvpWeakPct": 5, "bossStrongPct": 30, "bossTakenPct": 0}' WHERE key = 'rules.elements'`)
	execSQL(t, db, `UPDATE game_settings SET value = (value::jsonb || '{"bounceRestitution": 0.4}'::jsonb)::text WHERE key = 'rules.physics'`)
	execSQL(t, db, `UPDATE config_weapons SET bounces = 2 WHERE weapon_id = 'water_orb'`)
	execSQL(t, db, `UPDATE config_skills SET name = 'Đá lở', params = params || '{"radius": 450}'::jsonb WHERE skill_id IN ('heavy_bomb', 'snow_nova')`)
	execSQL(t, db, `UPDATE config_characters SET hp = 1500 WHERE character_id = 'tide'`)
	execSQL(t, db, `UPDATE config_equipment_items SET price_coin = 777 WHERE item_id = 'tide_weapon_lv10'`)
	for _, n := range names {
		runSQL(t, db, n)
	}
	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	robot, _ := snap.Boss("steel_robot")
	metal, _ := snap.Element("metal")
	var price int
	_ = db.Pool.QueryRow(ctx, `SELECT price_coin FROM config_equipment_items WHERE item_id = 'tide_weapon_lv10'`).Scan(&price)
	if snap.Data.Characters["tide"].Element != "fire" || snap.Data.Characters["rookie"].Element != "" || robot.Element != "wood" ||
		metal.Name != "Kim loại" || snap.Rules.Elements != (gamedata.ElementRules{PvpStrongPct: 15, PvpWeakPct: 5, BossStrongPct: 30}) ||
		snap.Rules.Physics.BounceRestitution != 0.4 || snap.Rules.Physics.BounceFriction != 0.85 || snap.Data.Weapons["water_orb"].Bounces != 2 ||
		snap.Data.Skills["heavy_bomb"].Name != "Đá lở" || snap.Data.Skills["snow_nova"].Name != "Đá lở" ||
		gamedata.NovaTuningOf(snap.Data.Skills["snow_nova"]).Radius != 450 || snap.Data.Characters["tide"].HP != 1500 || price != 777 {
		t.Fatalf("a rerun lost an admin edit: tide %+v robot %q metal %+v rules %+v physics %+v", snap.Data.Characters["tide"], robot.Element, metal,
			snap.Rules.Elements, snap.Rules.Physics)
	}

	// down / up: the defaults again (the edited rows go with their columns and rows)
	execSQL(t, db, `UPDATE config_skills SET name = 'Địa chấn liên hoàn' WHERE skill_id = 'heavy_bomb'`)
	runSQL(t, db, "055_tide_guns.down.sql")
	runSQL(t, db, "054_elements.down.sql")
	if _, err := gamedata.LoadFromDB(ctx, db); err == nil {
		t.Fatal("game data read without the 054 columns")
	}
	var cols, tide, rules int
	var physics, hbName string
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND ((table_name IN ('config_characters', 'config_bosses') AND column_name = 'element') OR (table_name = 'config_weapons' AND column_name = 'bounces')
		OR table_name = 'config_elements')`).Scan(&cols)
	_ = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM config_characters WHERE character_id = 'tide') + (SELECT count(*) FROM config_skills WHERE skill_id = 'snow_nova')
		+ (SELECT count(*) FROM config_weapons WHERE weapon_id = 'water_orb') + (SELECT count(*) FROM shop_offers WHERE item_id = 'tide')
		+ (SELECT count(*) FROM config_equipment_items WHERE character_id = 'tide') + (SELECT count(*) FROM config_gun_kinds WHERE gun_kind = 'water_gun')`).Scan(&tide)
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM game_settings WHERE key = 'rules.elements'`).Scan(&rules)
	_ = db.Pool.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'rules.physics'`).Scan(&physics)
	_ = db.Pool.QueryRow(ctx, `SELECT name FROM config_skills WHERE skill_id = 'heavy_bomb'`).Scan(&hbName)
	if cols != 0 || tide != 0 || rules != 0 || strings.Contains(physics, "bounce") || hbName != "Bom liên hoàn" {
		t.Fatalf("down: %d columns, %d tide rows, %d rules rows, physics %s, tanko %q", cols, tide, rules, physics, hbName)
	}
	runSQL(t, db, "054_elements.up.sql")
	runSQL(t, db, "055_tide_guns.up.sql")
	runSQL(t, db, "054_elements.up.sql")
	runSQL(t, db, "055_tide_guns.up.sql")
	checkSeeded("down / up") // the dev DB order too: the characters existed before 054

	// a down keeps a Tide a player owns (her rows; the columns and the rest go)
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	execSQL(t, db, `INSERT INTO player_characters (player_id, character_id) VALUES ('p1', 'tide')`)
	runSQL(t, db, "054_elements.down.sql")
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM config_characters WHERE character_id = 'tide'`).Scan(&tide)
	if tide != 1 {
		t.Fatal("down deleted an owned Tide")
	}
	if _, err := gamedata.LoadFromDB(ctx, db); err == nil {
		t.Fatal("read without the columns")
	}
	runSQL(t, db, "054_elements.up.sql")
	if snap, err := gamedata.LoadFromDB(ctx, db); err != nil || snap.Data.Characters["tide"].Element != "water" {
		t.Fatalf("up after a kept Tide: %v", err)
	}
}

// The admin edits the elements: a character's and a boss's (a known element or none), the
// element table (an element must beat another one), the rule percentages (0..100) and a weapon's
// bounces (0..5).
func TestElementsAdminPages(t *testing.T) {
	_, db, h := adminServer(t)
	ctx := context.Background()
	saved := func(rec *httptest.ResponseRecorder) bool {
		loc := rec.Header().Get("Location")
		return rec.Code == 303 && strings.Contains(loc, "flash=") && !strings.Contains(loc, "error=")
	}
	for _, p := range []string{"/characters/detail?id=tide", "/bosses/edit?id=ice_witch", "/texts/elements", "/rules", "/weapons/edit?id=water_orb"} {
		body := get(h, p).Body.String()
		if strings.Contains(body, "template exec error") || !strings.Contains(body, "Thủy") && p != "/weapons/edit?id=water_orb" && p != "/rules" {
			t.Fatalf("%s: %.300s", p, body)
		}
	}
	if body := get(h, "/rules").Body.String(); !strings.Contains(body, "rules.elements") || !strings.Contains(body, "bossTakenPct") {
		t.Fatal("rules page lacks the elements group")
	}

	// a character's element
	c := yamlCharacter(t, "tide")
	form := url.Values{"character_id": {"tide"}, "name": {c.Name}, "role": {c.Role}, "hp": {"1400"}, "damage": {"500"}, "mobility": {"110"},
		"defense": {"45"}, "skill_power": {"70"}, "terrain_damage": {"16"}, "difficulty": {"2"}, "weapon_id": {c.WeaponID}, "skill_id": {c.SkillID},
		"role_name": {c.RoleName}, "playstyle": {c.Playstyle}, "gun_kind": {c.GunKind}, "sort_order": {"5"}, "element": {"earth"},
		"skill_name": {"Nổ bông tuyết"}, "effect_type": {"nova"}, "projectile_count": {"0"}, "status_effect_id": {"freeze"}, "damage_multiplier": {"1"}}
	if rec := post(h, "/characters/detail/save", form); !saved(rec) {
		t.Fatalf("character save: %s", rec.Header().Get("Location"))
	}
	form.Set("element", "plasma")
	if rec := post(h, "/characters/detail/save", form); saved(rec) {
		t.Fatal("unknown element accepted")
	}
	var el string
	_ = db.Pool.QueryRow(ctx, `SELECT element FROM config_characters WHERE character_id = 'tide'`).Scan(&el)
	if el != "earth" {
		t.Fatalf("tide element %q", el)
	}

	// a boss's element
	b, _ := gamedata.Current().Boss("ice_witch")
	b.Element = "fire"
	if err := NewRepository(db, nil).SaveBoss(ctx, b); err != nil {
		t.Fatal(err)
	}
	b.Element = "plasma"
	if err := NewRepository(db, nil).SaveBoss(ctx, b); err == nil {
		t.Fatal("unknown boss element accepted")
	}
	_ = db.Pool.QueryRow(ctx, `SELECT element FROM config_bosses WHERE boss_id = 'ice_witch'`).Scan(&el)
	if el != "fire" {
		t.Fatalf("witch element %q", el)
	}

	// the element table: an element must beat another one; no new rows
	if rec := post(h, "/texts/elements/save", url.Values{"key": {"metal"}, "c_name": {"Kim"}, "c_beats": {"metal"}, "c_sort_order": {"1"}}); saved(rec) {
		t.Fatal("metal beating itself accepted")
	}
	if rec := post(h, "/texts/elements/save", url.Values{"new": {"1"}, "key": {"void"}, "c_name": {"Hư"}, "c_beats": {"metal"}, "c_sort_order": {"9"}}); saved(rec) {
		t.Fatal("a sixth element added")
	}
	if rec := post(h, "/texts/elements/save", url.Values{"key": {"metal"}, "c_name": {"Kim khí"}, "c_beats": {"wood"}, "c_sort_order": {"1"}}); !saved(rec) {
		t.Fatalf("element save: %s", rec.Header().Get("Location"))
	}

	// the rule percentages: 0..100
	g, _ := gamedata.RuleGroup("rules.elements")
	rform := url.Values{"group": {g.Key}}
	for k, v := range g.FormValues(gamedata.DefaultRules()) {
		rform.Set("f_"+k, v)
	}
	rform.Set("f_bossStrongPct", "150")
	if rec := post(h, "/rules/save", rform); saved(rec) {
		t.Fatal("150 % accepted")
	}
	rform.Set("f_bossStrongPct", "25")
	if rec := post(h, "/rules/save", rform); !saved(rec) {
		t.Fatalf("rules save: %s", rec.Header().Get("Location"))
	}

	// a weapon's bounces: 0..5
	wform := url.Values{"weapon_id": {"water_orb"}, "name": {"Water Orb"}, "damage": {"500"}, "explosion_radius": {"38"}, "terrain_damage": {"16"},
		"projectile_weight": {"0.9"}, "wind_influence": {"1"}, "multi_hit": {"1"}, "bounces": {"9"}}
	if rec := post(h, "/weapons/save", wform); saved(rec) {
		t.Fatal("9 bounces accepted")
	}
	wform.Set("bounces", "2")
	if rec := post(h, "/weapons/save", wform); !saved(rec) {
		t.Fatalf("weapon save: %s", rec.Header().Get("Location"))
	}

	snap, err := gamedata.LoadFromDB(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	metal, _ := snap.Element("metal")
	if snap.Rules.Elements.BossStrongPct != 25 || snap.Data.Weapons["water_orb"].Bounces != 2 || metal.Name != "Kim khí" ||
		snap.Data.Characters["tide"].Element != "earth" {
		t.Fatalf("saved %+v %+v %+v", snap.Rules.Elements, snap.Data.Weapons["water_orb"], metal)
	}
}

func yamlCharacter(t *testing.T, id string) gamedata.CharacterConfig {
	t.Helper()
	snap, err := gamedata.LoadYAML(configsDir())
	if err != nil {
		t.Fatal(err)
	}
	return snap.Data.Characters[id]
}
