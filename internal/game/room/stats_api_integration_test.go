package room

import (
	"context"
	"fmt"
	"math"
	"testing"

	"battle-squad/internal/api/character"
	"battle-squad/internal/api/economy"
	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/match"
	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/database/dbtest"
)

type testItem struct {
	slot, category, tier     string
	hp, dmg, def, moveEnergy int
	crit                     float64
	upgrade                  int
	gems                     []string // gem types, level 1
}

func equipTestItem(t *testing.T, db *database.PostgresDB, playerID, characterID string, n int, it testItem) {
	t.Helper()
	ctx := context.Background()
	itemID := fmt.Sprintf("%s_%s_%d", playerID, characterID, n)
	var tier *string
	if it.tier != "" {
		tier = &it.tier
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO config_equipment_items (item_id, name, slot, category, tier, stat_hp, stat_damage, stat_defense, stat_crit, stat_move_energy)
		VALUES ($1, $1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		itemID, it.slot, it.category, tier, it.hp, it.dmg, it.def, it.crit, it.moveEnergy); err != nil {
		t.Fatal(err)
	}
	gemIDs := [2]*string{}
	for i, g := range it.gems {
		var id string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO player_gems (player_id, gem_type, gem_level) VALUES ($1, $2, 1) RETURNING gem_id`,
			playerID, g).Scan(&id); err != nil {
			t.Fatal(err)
		}
		gemIDs[i] = &id
	}
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO player_equipment (player_id, item_id, slot, category, tier, upgrade_level, gem_slot_1, gem_slot_2, is_equipped, equipped_on)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, TRUE, $9)`,
		playerID, itemID, it.slot, it.category, tier, it.upgrade, gemIDs[0], gemIDs[1], characterID); err != nil {
		t.Fatal(err)
	}
}

// GET /player/characters must report exactly the stats the room builds the
// match player with.
func TestAPICharacterStatsMatchBattle(t *testing.T) {
	db := dbtest.NewSchema(t, 0,
		"001_init_schema.up.sql", "004_admin_config_tables.up.sql", "006_character_progression.up.sql",
		"010_equipment_system.up.sql", "011_equipment_slot_unique.up.sql")
	prev := gamedata.Data
	gamedata.Data = &gamedata.GameData{
		Characters: map[string]gamedata.CharacterConfig{
			"rookie": {CharacterID: "rookie", HP: 100, Defense: 50, WeaponID: "basic_rocket"},
			"tanko":  {CharacterID: "tanko", HP: 130, Defense: 80, WeaponID: "cannon_ball"},
			"spark":  {CharacterID: "spark", HP: 90, Defense: 40, WeaponID: "electric_orb"},
			"flora":  {CharacterID: "flora", HP: 95, Defense: 50, WeaponID: "seed_bomb"},
		},
		Weapons: map[string]gamedata.WeaponConfig{
			"basic_rocket": {Damage: 60}, "cannon_ball": {Damage: 75}, "electric_orb": {Damage: 55}, "seed_bomb": {Damage: 45},
		},
	}
	t.Cleanup(func() { gamedata.Data = prev })

	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO config_gems (gem_type, gem_level, stat_value) VALUES ('hp', 1, 25), ('damage', 1, 4), ('defense', 1, 3), ('critical', 1, 1.5);
		INSERT INTO config_set_bonuses (tier, pieces_required, bonus_hp_pct, bonus_dmg_pct, bonus_def_pct, bonus_crit_pct)
		VALUES ('rare', 2, 10, 5, 0, 1), ('rare', 3, 5, 0, 10, 0);
		INSERT INTO game_settings (key, value) VALUES ('equipment.milestone_6_bonus', '0.15');
		UPDATE game_settings SET value = '{"statMultipliers":{"hp":40,"damage":6,"mobility":4,"defense":2,"skill_power":7,"terrain_damage":5}}'
		WHERE key = 'character_progression';`); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		character string
		points    [6]int // hp, damage, mobility, defense, skill_power, terrain_damage
		items     []testItem
		wantMaxHP int // pinned so a formula change on both sides is noticed
		wantHPPts int // equipment hp points
	}{
		{name: "no points or equipment", character: "rookie", wantMaxHP: 100},
		{name: "points only", character: "tanko", points: [6]int{3, 2, 4, 1, 5, 2}, wantMaxHP: 130 + 3*40},
		{name: "points + upgraded shop item with gems", character: "spark", points: [6]int{1, 0, 2, 0, 1, 0},
			items: []testItem{{slot: "weapon", category: "shop", hp: 40, dmg: 8, def: 6, crit: 2, moveEnergy: 5, upgrade: 7, gems: []string{"hp", "critical"}}},
			// item hp 40 × (1 + 0.14 + 0.15) = 51.6 → 52 points, + gem 25 points; × 40 HP per point
			wantMaxHP: 90 + 40 + (52+25)*40, wantHPPts: 77},
		{name: "points + crafted set of 3", character: "flora", points: [6]int{2, 1, 1, 3, 0, 4},
			items: []testItem{
				{slot: "weapon", category: "crafted", tier: "rare", hp: 20, dmg: 10, upgrade: 10, gems: []string{"damage", "defense"}},
				{slot: "armor", category: "crafted", tier: "rare", hp: 60, def: 12, upgrade: 3},
				{slot: "helmet", category: "crafted", tier: "rare", hp: 30, def: 5, crit: 1, moveEnergy: 3},
			},
			// hp points: 20×1.55=31 + 60×1.06=63.6→64 + 30 = 125, × (1 + 15%) = 143.75 → 144; × 40 HP per point
			wantMaxHP: 95 + 80 + 144*40, wantHPPts: 144},
	}

	svc := character.NewService(character.NewRepository(db), economy.NewRepository(), db)
	r := &Room{db: db}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			playerID := fmt.Sprintf("p%d", i)
			dbtest.CreatePlayer(t, db, playerID, 0, 0)
			p := tt.points
			if _, err := db.Pool.Exec(ctx, `
				INSERT INTO player_characters (player_id, character_id, bonus_hp, bonus_damage, bonus_mobility, bonus_defense, bonus_skill_power, bonus_terrain_damage)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, playerID, tt.character, p[0], p[1], p[2], p[3], p[4], p[5]); err != nil {
				t.Fatal(err)
			}
			for n, it := range tt.items {
				equipTestItem(t, db, playerID, tt.character, n, it)
			}

			resp, err := svc.GetCharacters(ctx, playerID)
			if err != nil {
				t.Fatal(err)
			}
			if len(resp.Characters) != 1 || resp.Characters[0].Stats == nil {
				t.Fatalf("characters %+v", resp.Characters)
			}
			api := resp.Characters[0].Stats
			bp := newBattlePlayer(RoomPlayer{PlayerID: playerID, CharacterID: tt.character}, match.Vector2{},
				r.getActualStats(playerID, tt.character), false)

			if api.MaxHP != bp.MaxHP || api.Defense != bp.Defense || api.DamageBonus != bp.DamageBonus ||
				api.MoveEnergy != bp.MoveEnergy || api.SkillPowerBonus != bp.SkillPowerBonus ||
				api.TerrainDamageBonus != bp.TerrainDamageBonus || api.CritChance != bp.CritChance {
				t.Fatalf("API stats %+v, battle player %+v", api.StatValues, bp)
			}
			if api.MaxHP != tt.wantMaxHP {
				t.Fatalf("maxHp %d, want %d", api.MaxHP, tt.wantMaxHP)
			}
			if want := gamedata.Data.Weapons[gamedata.Data.Characters[tt.character].WeaponID].Damage; api.WeaponDamage != want {
				t.Fatalf("weaponDamage %d, want %d", api.WeaponDamage, want)
			}
			// total = base + fromPoints + fromEquipment
			b, fp, fe := api.Base, api.FromPoints, api.FromEquipment
			if b.MaxHP+fp.MaxHP+fe.MaxHP != api.MaxHP || b.Defense+fp.Defense+fe.Defense != api.Defense ||
				b.DamageBonus+fp.DamageBonus+fe.DamageBonus != api.DamageBonus ||
				b.MoveEnergy+fp.MoveEnergy+fe.MoveEnergy != api.MoveEnergy ||
				b.SkillPowerBonus+fp.SkillPowerBonus+fe.SkillPowerBonus != api.SkillPowerBonus ||
				b.TerrainDamageBonus+fp.TerrainDamageBonus+fe.TerrainDamageBonus != api.TerrainDamageBonus ||
				math.Abs(b.CritChance+fp.CritChance+fe.CritChance-api.CritChance) > 1e-9 {
				t.Fatalf("parts do not sum: %+v", api)
			}
			if fp.MaxHP != p[0]*40 || fp.MoveEnergy != p[2]*4 || fp.SkillPowerBonus != p[4]*7 {
				t.Fatalf("fromPoints %+v ignores the configured multipliers", fp)
			}
			// fromEquipment is the equipment points through the same multipliers
			ep := api.EquipmentPoints
			if ep.HP != tt.wantHPPts || fe.MaxHP != ep.HP*40 || fe.DamageBonus != ep.Damage*6 || fe.Defense != ep.Defense*2 ||
				fe.MoveEnergy != ep.Mobility*4 || fe.CritChance != ep.Crit || fe.SkillPowerBonus != 0 || fe.TerrainDamageBonus != 0 {
				t.Fatalf("equipmentPoints %+v, fromEquipment %+v", ep, fe)
			}
		})
	}

	resp, err := svc.GetCharacters(ctx, "p0")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatMultipliers.HP != 40 || resp.StatMultipliers.SkillPower != 7 || resp.StatMultipliers.TerrainDamage != 5 {
		t.Fatalf("statMultipliers %+v", resp.StatMultipliers)
	}
	if len(resp.BaseStats) != 4 {
		t.Fatalf("baseStats %+v", resp.BaseStats)
	}
	for id := range gamedata.Data.Characters {
		bp := newBattlePlayer(RoomPlayer{CharacterID: id}, match.Vector2{}, baseStats(id), true)
		got := resp.BaseStats[id]
		if got.MaxHP != bp.MaxHP || got.Defense != bp.Defense || got.MoveEnergy != bp.MoveEnergy || got.DamageBonus != 0 ||
			got.WeaponDamage != gamedata.Data.Weapons[gamedata.Data.Characters[id].WeaponID].Damage {
			t.Fatalf("baseStats[%s] %+v, bot %+v", id, got, bp)
		}
	}
}
