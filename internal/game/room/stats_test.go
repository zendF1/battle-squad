package room

import (
	"context"
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
		got := addStatPoints(battleStats{}, statPoints{1, 1, 1, 1, 1, 1}, cfg)
		wantHP := 50
		if value != "" && value != "not json" {
			wantHP = 20
		}
		want := battleStats{HP: wantHP, DamageBonus: 5, MoveEnergyBonus: 3, Defense: 5, SkillPowerBonus: 5, TerrainDamageBonus: 3}
		if got != want {
			t.Fatalf("config %q: got %+v, want %+v", value, got, want)
		}
	}
}

func TestNewBattlePlayerCarriesStats(t *testing.T) {
	p := RoomPlayer{PlayerID: "p1", DisplayName: "P", TeamID: 2, CharacterID: "tanko", Items: []string{"medkit"}}
	s := battleStats{HP: 180, Defense: 60, DamageBonus: 10, MoveEnergyBonus: 9, CritChance: 2, SkillPowerBonus: 15, TerrainDamageBonus: 6}
	bp := newBattlePlayer(p, match.Vector2{X: 5, Y: 6}, s, false)

	if bp.HP != 180 || bp.MaxHP != 180 || bp.Defense != 60 || bp.DamageBonus != 10 ||
		bp.MoveEnergy != 109 || bp.MoveEnergyBonus != 9 || bp.CritChance != 2 ||
		bp.SkillPowerBonus != 15 || bp.TerrainDamageBonus != 6 {
		t.Fatalf("stats not carried: %+v", bp)
	}
	if bp.PlayerID != "p1" || bp.TeamID != 2 || bp.CharacterID != "tanko" || bp.Position != (match.Vector2{X: 5, Y: 6}) ||
		len(bp.Items) != 1 || !bp.IsAlive || bp.IsBot || bp.StatusEffects == nil {
		t.Fatalf("player fields not carried: %+v", bp)
	}
}

func TestGetActualStatsAppliesAllStatPoints(t *testing.T) {
	db := dbtest.NewSchema(t, 0,
		"001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql")
	prev := gamedata.Data
	gamedata.Data = &gamedata.GameData{Characters: map[string]gamedata.CharacterConfig{
		"tanko": {CharacterID: "tanko", HP: 130, Defense: 80},
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

	// migrations/006 seed: hp 50, damage 5, mobility 3, defense 5, skill_power 5, terrain_damage 3
	got := r.getActualStats("p1", "tanko")
	want := battleStats{HP: 180, Defense: 100, DamageBonus: 10, MoveEnergyBonus: 9, SkillPowerBonus: 25, TerrainDamageBonus: 18}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// A character the player does not own keeps its base stats
	if got := r.getActualStats("p1", "spark"); got != baseStats("spark") {
		t.Fatalf("unowned character got %+v", got)
	}
}
