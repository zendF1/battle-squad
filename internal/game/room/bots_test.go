package room

import (
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/game/battlestats"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/shared/database/dbtest"
)

func configsDir() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..", "..", "configs")
}

// testBotWeapons is the active weapon catalog of the seed (migrations 019 / 020, admin
// SeedEquipmentConfig): each character's gun at 10/20/30/40 and the crafted ones at 15/35/55/75.
func testBotWeapons() []botWeaponOption {
	var out []botWeaponOption
	for _, c := range []string{"rookie", "tanko", "spark", "flora"} {
		for _, w := range []struct{ lv, dmg int }{{10, 8}, {20, 15}, {30, 25}, {40, 38}} {
			out = append(out, botWeaponOption{ItemID: fmt.Sprintf("%s_weapon_lv%d", c, w.lv), Category: "normal", CharacterID: c,
				RequiredLevel: w.lv, Damage: w.dmg})
		}
	}
	for _, w := range []struct {
		tier    string
		lv, dmg int
	}{{"silver", 15, 10}, {"gold", 35, 33}, {"titan", 55, 57}, {"diamond", 75, 76}} {
		out = append(out, botWeaponOption{ItemID: "crafted_weapon_" + w.tier, Category: "crafted", Tier: w.tier, RequiredLevel: w.lv, Damage: w.dmg})
	}
	return out
}

// useBotGameData plays the test with the YAML game data and the game_settings rows settings.
func useBotGameData(t *testing.T, settings map[string]string) *gamedata.Snapshot {
	t.Helper()
	snap := gamedata.UseYAML(t, configsDir())
	s := snap.WithData(snap.Data)
	s.Settings = settings
	gamedata.UseForTest(t, s)
	return s
}

// The character_progression and character_levels rows of the dev DB.
var botTestSettings = map[string]string{
	character.SettingsKeyProgression: `{"pointsPerLevel":10,"statMultipliers":{"hp":2,"damage":5,"mobility":1,"defense":1,"skill_power":1,"terrain_damage":1}}`,
	character.SettingsKeyLevels:      `{"levels":[{"level":2,"expRequired":200},{"level":80,"expRequired":151325}]}`,
}

func seededIntn(seed uint64) func(int) int {
	r := rand.New(rand.NewPCG(seed, 7))
	return r.IntN
}

// Bots of one match get different playable characters from the game data, levels within the
// spread, a gun of their own character (or a crafted one) for that level, and the default
// items plus one from the pool.
func TestBotOutfitVaried(t *testing.T) {
	gd := useBotGameData(t, botTestSettings)
	pool := gd.Rules.Bots.RandomItems
	for seed := uint64(0); seed < 50; seed++ {
		o := newBotOutfitter(gd, testBotWeapons(), seededIntn(seed))
		chars := map[string]bool{}
		for b := 0; b < 3; b++ {
			l := o.outfit(25)
			if _, ok := gd.Data.Characters[l.CharacterID]; !ok || chars[l.CharacterID] {
				t.Fatalf("seed %d bot %d: character %q (taken %v)", seed, b, l.CharacterID, chars)
			}
			chars[l.CharacterID] = true
			if l.Level < 22 || l.Level > 28 {
				t.Fatalf("seed %d: level %d outside 25 ± 3", seed, l.Level)
			}
			if len(l.Items) != 3 || l.Items[0] != "medkit" || l.Items[1] != "teleport" || !slices.Contains(pool, l.Items[2]) {
				t.Fatalf("seed %d: items %v", seed, l.Items)
			}
			gun := l.Look.WeaponItemID
			if gun != l.CharacterID+"_weapon_lv20" && gun != "crafted_weapon_silver" {
				t.Fatalf("seed %d: level %d %s carries %q", seed, l.Level, l.CharacterID, gun)
			}
			req := 20
			if strings.HasPrefix(gun, "crafted") {
				req = 15
			}
			if l.Look.WeaponUpgradeLevel < 0 || l.Look.WeaponUpgradeLevel > req/5 {
				t.Fatalf("seed %d: %s +%d, want 0..%d", seed, gun, l.Look.WeaponUpgradeLevel, req/5)
			}
		}
	}
	// a sixth bot reuses a character once all five (tide too) are taken
	o := newBotOutfitter(gd, nil, seededIntn(1))
	seen := map[string]int{}
	for b := 0; b < 6; b++ {
		seen[o.outfit(1).CharacterID]++
	}
	if len(seen) != 5 || seen["tide"] == 0 {
		t.Fatalf("5 bots got %v", seen)
	}
}

// The gun follows the level: none below the first tier (a new player's basic gun), the
// highest tier at or below the level, never another character's gun.
func TestBotWeaponForLevel(t *testing.T) {
	gd := useBotGameData(t, botTestSettings)
	o := newBotOutfitter(gd, testBotWeapons(), nil)
	for _, tc := range []struct {
		level int
		want  []string
	}{
		{1, nil}, {9, nil},
		{10, []string{"tanko_weapon_lv10"}},
		{19, []string{"tanko_weapon_lv10", "crafted_weapon_silver"}},
		{37, []string{"tanko_weapon_lv30", "crafted_weapon_gold"}},
		{80, []string{"tanko_weapon_lv40", "crafted_weapon_diamond"}},
	} {
		got := map[string]bool{}
		for i := 0; i < 40; i++ {
			w, ok := o.weapon("tanko", tc.level)
			if ok != (tc.want != nil) {
				t.Fatalf("level %d: weapon %v %v", tc.level, w, ok)
			}
			if ok {
				got[w.ItemID] = true
			}
		}
		for id := range got {
			if !slices.Contains(tc.want, id) {
				t.Fatalf("level %d: %s, want one of %v", tc.level, id, tc.want)
			}
		}
		if len(got) != len(tc.want) {
			t.Fatalf("level %d: drew %v, want each of %v", tc.level, got, tc.want)
		}
	}
	// the upgrade: 0 .. required level × 0.2, at most rules.equipment maxUpgradeLevel
	for i := 0; i < 200; i++ {
		if u := o.upgrade(botWeaponOption{RequiredLevel: 75}); u < 0 || u > 15 {
			t.Fatalf("diamond +%d", u)
		}
		if u := o.upgrade(botWeaponOption{RequiredLevel: 10}); u < 0 || u > 2 {
			t.Fatalf("lv10 +%d", u)
		}
	}
}

// The level stays within 1 .. the top of character_levels.
func TestBotLevelClamp(t *testing.T) {
	gd := useBotGameData(t, botTestSettings)
	o := newBotOutfitter(gd, nil, nil)
	for i := 0; i < 200; i++ {
		if l := o.level(1); l < 1 || l > 4 {
			t.Fatalf("avg 1: level %d", l)
		}
		if l := o.level(80); l < 77 || l > 80 {
			t.Fatalf("avg 80: level %d", l)
		}
	}
}

// A bot's stats are battlestats.Compute of its level's points (spread evenly) and its gun,
// as a player's. A level-1 bot has no points and no gun: its character's base stats, as
// every bot had before.
func TestBotStatsFollowLevelAndGun(t *testing.T) {
	gd := useBotGameData(t, botTestSettings)
	for _, char := range []string{"rookie", "tanko", "spark", "flora"} {
		if got := botStats(gd, char, 1, nil, 0); got != baseStats(char) {
			t.Fatalf("level-1 %s: %+v, want base %+v", char, got, baseStats(char))
		}
	}
	w := testBotWeapons()[3] // rookie_weapon_lv40, 38 damage points
	got := botStats(gd, "rookie", 40, &w, 8)
	pts := evenPoints(39 * 10)
	if pts != (statPoints{HP: 65, Damage: 65, Defense: 65, Mobility: 65, SkillPower: 65, TerrainDamage: 65}) {
		t.Fatalf("points %+v", pts)
	}
	cfg := character.LoadProgressionConfig(botTestSettings[character.SettingsKeyProgression])
	eq := battlestats.SumEquipment([]battlestats.EquippedItem{{ItemID: w.ItemID, Slot: "weapon", Damage: 38, UpgradeLevel: 8, Category: "normal"}},
		nil, battlestats.DefaultMilestones())
	want := battlestats.Compute("rookie", pts, cfg.Multipliers(), eq).Total
	if got != want || got.DamageBonus != 65*5+eq.Damage*5 || got.HP != 1500+130 {
		t.Fatalf("level-40 rookie +8: %+v, want %+v", got, want)
	}
	if evenPoints(7) != (statPoints{HP: 2, Damage: 1, Defense: 1, Mobility: 1, SkillPower: 1, TerrainDamage: 1}) {
		t.Fatalf("remainder %+v", evenPoints(7))
	}
}

// Items: only items of the game data the bot can use, within maxLoadout and each item's
// per-match limit; an empty pool is every item the bot can use.
func TestBotItemsFollowTheRules(t *testing.T) {
	gd := useBotGameData(t, botTestSettings)
	r := gd.Rules.Clone()
	r.Bots.DefaultItems = []string{"medkit", "medkit", "nope"}
	r.Bots.RandomItems = []string{"nope", "medkit", "power_shot"}
	r.Bots.RandomItemCount = 5
	s := gd.WithRules(r)
	s.Settings = gd.Settings
	if got := newBotOutfitter(s, nil, nil).items(); !slices.Equal(got, []string{"medkit", "power_shot"}) {
		t.Fatalf("items %v", got)
	}
	r.Bots.DefaultItems, r.Bots.RandomItems, r.Bots.RandomItemCount = nil, nil, 9
	if got := newBotOutfitter(s, nil, nil).items(); len(got) != r.Items.MaxLoadout {
		t.Fatalf("empty pool: %v, want %d items", got, r.Items.MaxLoadout)
	}
}

// The weapon catalog of a migrated DB: every character's gun and the crafted ones, not the
// inactive generic weapons; a bot of level 40 carries a real one of them.
func TestLoadBotWeaponsFromDB(t *testing.T) {
	db := dbtest.NewSchema(t, 0, allMigrations(t)...)
	weapons, err := loadBotWeapons(t.Context(), db.Pool)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]botWeaponOption{}
	for _, w := range weapons {
		ids[w.ItemID] = w
	}
	if w := ids["spark_weapon_lv30"]; w.CharacterID != "spark" || w.RequiredLevel != 30 || w.Damage != 25 || w.Category != "normal" {
		t.Fatalf("spark_weapon_lv30 %+v", w)
	}
	if _, ok := ids["normal_weapon_lv10"]; ok {
		t.Fatal("inactive generic weapon loaded")
	}
	if w, ok := ids["crafted_weapon_gold"]; ok && (w.CharacterID != "" || w.Tier != "gold") {
		t.Fatalf("crafted_weapon_gold %+v", w)
	}
	gd := useBotGameData(t, botTestSettings)
	l := newBotOutfitter(gd, weapons, nil).outfit(40)
	if _, ok := ids[l.Look.WeaponItemID]; !ok {
		t.Fatalf("bot gun %q not in the catalog", l.Look.WeaponItemID)
	}
}
