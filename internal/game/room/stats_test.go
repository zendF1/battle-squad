package room

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/shared/database/dbtest"
)

func TestAddStatPointsUsesConfiguredMultipliers(t *testing.T) {
	cfg := character.LoadProgressionConfig(`{"statMultipliers":{"hp":10,"damage":2,"mobility":4,"defense":1,"skill_power":7,"terrain_damage":6}}`)
	got := addStatPoints(battleStats{HP: 100, Defense: 50, CritChance: 3}, statPoints{
		HP: 1, Damage: 2, Mobility: 3, Defense: 4, SkillPower: 5, TerrainDamage: 6,
	}, cfg)
	want := battleStats{
		HP: 110, Defense: 54, DamageBonus: 4, MoveEnergyBonus: 12,
		CritChance: 3, SkillPowerBonus: 35, TerrainDamageBonus: 36,
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAddStatPointsDefaultsMissingMultipliers(t *testing.T) {
	// hp is configured; the rest (and an unreadable config) fall back to the defaults
	for _, value := range []string{`{"statMultipliers":{"hp":20,"damage":0}}`, "", "not json"} {
		cfg := character.LoadProgressionConfig(value)
		got := addStatPoints(battleStats{}, statPoints{HP: 1, Damage: 1, Mobility: 1, Defense: 1, SkillPower: 1, TerrainDamage: 1}, cfg)
		wantHP := 2
		if value != "" && value != "not json" {
			wantHP = 20
		}
		want := battleStats{HP: wantHP, DamageBonus: 5, MoveEnergyBonus: 1, Defense: 1, SkillPowerBonus: 1, TerrainDamageBonus: 1}
		if got != want {
			t.Fatalf("config %q: got %+v, want %+v", value, got, want)
		}
	}
}

func TestNewBattlePlayerCarriesStats(t *testing.T) {
	p := RoomPlayer{PlayerID: "p1", DisplayName: "P", TeamID: 2, CharacterID: "tanko", Items: []string{"medkit"}}
	s := battleStats{HP: 180, Defense: 60, DamageBonus: 10, MoveEnergy: 80, MoveEnergyBonus: 9, CritChance: 2, SkillPowerBonus: 15, TerrainDamageBonus: 6}
	bp := newBattlePlayer(p, match.Vector2{X: 5, Y: 6}, s, equippedWeapon{ItemID: "tanko_weapon_lv30", UpgradeLevel: 14}, false)

	if bp.HP != 180 || bp.MaxHP != 180 || bp.Defense != 60 || bp.DamageBonus != 10 ||
		bp.MoveEnergy != 89 || bp.MaxMoveEnergy != 89 || bp.MoveEnergyBonus != 9 || bp.CritChance != 2 ||
		bp.SkillPowerBonus != 15 || bp.TerrainDamageBonus != 6 {
		t.Fatalf("stats not carried: %+v", bp)
	}
	if bp.PlayerID != "p1" || bp.TeamID != 2 || bp.CharacterID != "tanko" || bp.Position != (match.Vector2{X: 5, Y: 6}) ||
		len(bp.Items) != 1 || !bp.IsAlive || bp.IsBot || bp.StatusEffects == nil {
		t.Fatalf("player fields not carried: %+v", bp)
	}
	if bp.WeaponItemID != "tanko_weapon_lv30" || bp.WeaponUpgradeLevel != 14 {
		t.Fatalf("weapon not carried: %q +%d", bp.WeaponItemID, bp.WeaponUpgradeLevel)
	}
	raw, err := json.Marshal(bp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"weaponItemId":"tanko_weapon_lv30"`) || !strings.Contains(string(raw), `"weaponUpgradeLevel":14`) {
		t.Fatalf("weaponItemId / weaponUpgradeLevel not serialised: %s", raw)
	}
	// A bot (no weapon) still sends the field as 0
	raw, err = json.Marshal(newBattlePlayer(RoomPlayer{PlayerID: "bot_1", CharacterID: "rookie"}, match.Vector2{}, baseStats("rookie"), equippedWeapon{}, true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"weaponItemId":""`) || !strings.Contains(string(raw), `"weaponUpgradeLevel":0`) {
		t.Fatalf("bot weapon fields: %s", raw)
	}
}

func TestGetActualStatsAppliesAllStatPoints(t *testing.T) {
	db := dbtest.NewSchema(t, 0,
		"001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql")
	prev := gamedata.Data
	gamedata.Data = &gamedata.GameData{Characters: map[string]gamedata.CharacterConfig{
		"tanko": {CharacterID: "tanko", HP: 1950, Defense: 80},
	}}
	t.Cleanup(func() { gamedata.Data = prev })

	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO player_characters (player_id, character_id, bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage)
		VALUES ('p1', 'tanko', 1, 2, 3, 4, 5, 6)`); err != nil {
		t.Fatal(err)
	}
	r := &Room{db: db}

	// migrations/006 seed: hp 2, damage 5, mobility 1, defense 1, skill_power 1, terrain_damage 1
	got, weapon := r.getActualStats("p1", "tanko")
	want := battleStats{HP: 1952, Defense: 84, DamageBonus: 10, MoveEnergy: gamedata.DefaultMoveEnergy, MoveEnergyBonus: 3, SkillPowerBonus: 5, TerrainDamageBonus: 6}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if weapon != (equippedWeapon{}) {
		t.Fatalf("no weapon equipped, got %+v", weapon)
	}

	// A character the player does not own keeps its base stats
	if got, weapon := r.getActualStats("p1", "spark"); got != baseStats("spark") || weapon != (equippedWeapon{}) {
		t.Fatalf("unowned character got %+v, %+v", got, weapon)
	}
}

// The weapon equipped on the character comes back with its stats and upgrade
// level (the same query); other slots and other characters' weapons do not
// count.
func TestGetActualStatsReturnsEquippedWeapon(t *testing.T) {
	db := dbtest.NewSchema(t, 0,
		"001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql")
	ctx := context.Background()
	dbtest.CreatePlayer(t, db, "p1", 0, 0)
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO player_characters (player_id, character_id) VALUES ('p1', 'tanko'), ('p1', 'rookie');
		INSERT INTO config_equipment_items (item_id, name, slot, category, stat_damage, stat_hp) VALUES
			('tanko_weapon_lv30', 'Mithril Cannon', 'weapon', 'normal', 25, 0),
			('rookie_weapon_lv10', 'Iron Bazooka', 'weapon', 'normal', 8, 0),
			('normal_helmet_lv10', 'Iron Cap', 'helmet', 'normal', 0, 20);
		INSERT INTO player_equipment (player_id, item_id, slot, category, is_equipped, equipped_on, upgrade_level) VALUES
			('p1', 'normal_helmet_lv10', 'helmet', 'normal', TRUE, 'tanko', 15),
			('p1', 'tanko_weapon_lv30', 'weapon', 'normal', TRUE, 'tanko', 16),
			('p1', 'rookie_weapon_lv10', 'weapon', 'normal', FALSE, NULL, 14)`); err != nil {
		t.Fatal(err)
	}
	r := &Room{db: db}

	s, weapon := r.getActualStats("p1", "tanko")
	if weapon != (equippedWeapon{ItemID: "tanko_weapon_lv30", UpgradeLevel: 16}) {
		t.Fatalf("tanko weapon %+v, want tanko_weapon_lv30 +16", weapon)
	}
	if s.DamageBonus == 0 || s.HP <= baseStats("tanko").HP {
		t.Fatalf("equipment stats not applied: %+v", s)
	}
	if _, weapon := r.getActualStats("p1", "rookie"); weapon != (equippedWeapon{}) {
		t.Fatalf("rookie wears no weapon (one is owned, not equipped), got %+v", weapon)
	}
}
