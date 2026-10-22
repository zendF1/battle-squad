package admin

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/matchmaker"
	"battle-squad/internal/shared/database/dbtest"
)

// 043 seeds rules.bots with the Go defaults and gives every bot_difficulty tier a
// medkitForgetChance (bronze 0.15, the others 0); a rerun keeps admin edits; down / up gives
// the defaults back.
func TestBotLoadoutMigration(t *testing.T) {
	names := registered(t)
	if !slices.Contains(names, "043_bot_loadout.up.sql") {
		t.Fatalf("cmd/migrate does not run 043: %v", names)
	}
	db := dbtest.NewSchema(t, 0, names...)
	tiers := func() matchmaker.BotDifficultyConfig {
		var c matchmaker.BotDifficultyConfig
		if err := json.Unmarshal([]byte(setting(t, db, "bot_difficulty")), &c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	bots := func() gamedata.BotRules {
		r, errs := gamedata.RulesFromSettings(map[string]string{"rules.bots": setting(t, db, "rules.bots")})
		if len(errs) != 0 {
			t.Fatal(errs)
		}
		return r.Bots
	}
	check := func(when string) {
		t.Helper()
		if got := bots(); !reflect.DeepEqual(got, gamedata.DefaultRules().Bots) {
			t.Fatalf("%s: rules.bots %+v", when, got)
		}
		for tier, c := range tiers().Tiers {
			if c != matchmaker.DefaultBotDifficultyConfig().Tiers[tier] {
				t.Fatalf("%s: %s %+v", when, tier, c)
			}
		}
	}
	check("up")

	execSQL(t, db, `UPDATE game_settings SET value = jsonb_set(value::jsonb, '{tiers,bronze,medkitForgetChance}', '0.4')::text WHERE key = 'bot_difficulty'`)
	execSQL(t, db, `UPDATE game_settings SET value = '{"levelSpread": 5}' WHERE key = 'rules.bots'`)
	runSQL(t, db, "043_bot_loadout.up.sql")
	if tiers().Tiers["bronze"].MedkitForgetChance != 0.4 || bots().LevelSpread != 5 {
		t.Fatalf("a rerun lost an admin edit: %+v %+v", tiers().Tiers["bronze"], bots())
	}

	runSQL(t, db, "043_bot_loadout.down.sql")
	var n int
	_ = db.Pool.QueryRow(t.Context(), `SELECT COUNT(*) FROM game_settings WHERE key = 'rules.bots'
		OR (key = 'bot_difficulty' AND value LIKE '%medkitForgetChance%')`).Scan(&n)
	if n != 0 {
		t.Fatalf("down left %d rows", n)
	}
	runSQL(t, db, "043_bot_loadout.up.sql")
	check("down / up")
}
