package battlestats

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

func withGameData(t *testing.T) {
	t.Helper()
	gamedata.UseData(t, &gamedata.GameData{
		Characters: map[string]gamedata.CharacterConfig{"tanko": {CharacterID: "tanko", HP: 1950, Defense: 80, Mobility: 80, WeaponID: "cannon_ball"}},
		Weapons:    map[string]gamedata.WeaponConfig{"cannon_ball": {WeaponID: "cannon_ball", Damage: 600}},
	})
}

func TestBaseAndWeaponDamage(t *testing.T) {
	withGameData(t)
	if got := Base("tanko"); got != (Stats{HP: 1950, Defense: 80, MoveEnergy: 80}) {
		t.Fatalf("tanko base %+v", got)
	}
	if got := Base("ghost"); got != (Stats{HP: 1500, Defense: 50, MoveEnergy: gamedata.DefaultMoveEnergy}) {
		t.Fatalf("unknown character base %+v", got)
	}
	if WeaponDamage("tanko") != 600 || WeaponDamage("ghost") != 0 {
		t.Fatalf("weapon damage %d / %d", WeaponDamage("tanko"), WeaponDamage("ghost"))
	}
}

func TestFromPoints(t *testing.T) {
	got := FromPoints(Points{HP: 1, Damage: 2, Mobility: 3, Defense: 4, SkillPower: 5, TerrainDamage: 6},
		Multipliers{HP: 50, Damage: 5, Mobility: 3, Defense: 5, SkillPower: 5, TerrainDamage: 3})
	want := Stats{HP: 50, DamageBonus: 10, MoveEnergyBonus: 9, Defense: 20, SkillPowerBonus: 25, TerrainDamageBonus: 18}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSumEquipment(t *testing.T) {
	ms := DefaultMilestones()
	sets := map[string][]SetBonus{
		"epic": {{PiecesRequired: 2, HPPct: 10, DMGPct: 0, DEFPct: 5, CritPct: 1}, {PiecesRequired: 4, HPPct: 20}},
	}
	tests := []struct {
		name  string
		items []EquippedItem
		want  EquipmentPoints
	}{
		{"none", nil, EquipmentPoints{}},
		{"upgrade +5: ×1.10", []EquippedItem{{HP: 100, Damage: 10, Defense: 20, Crit: 2, MoveEnergy: 5, UpgradeLevel: 5}},
			EquipmentPoints{HP: 110, Damage: 11, Defense: 22, Crit: 2.2, Mobility: 6}},
		{"upgrade +10: 1 + 0.20 + 0.10 + 0.20", []EquippedItem{{HP: 100, UpgradeLevel: 10}}, EquipmentPoints{HP: 150}},
		{"upgrade +16: 1 + 0.32 + every milestone", []EquippedItem{{HP: 100, UpgradeLevel: 16}}, EquipmentPoints{HP: 302}},
		{"upgrade rounds per item", []EquippedItem{{HP: 20, UpgradeLevel: 7}, {HP: 20, UpgradeLevel: 7}}, // 20 × 1.24 = 24.8 → 25 each
			EquipmentPoints{HP: 50}},
		{"gems add points (truncated)", []EquippedItem{{HP: 10, Gems: []Gem{{"hp", 7.9}, {"critical", 1.5}}}, {Gems: []Gem{{"damage", 3}, {"defense", 4}}}},
			EquipmentPoints{HP: 17, Crit: 1.5, Damage: 3, Defense: 4}},
		{"crafted set of 2: hp +10%, def +5%, crit +1", []EquippedItem{
			{HP: 100, Defense: 10, Category: "crafted", Tier: "epic"},
			{HP: 100, Defense: 10, Category: "crafted", Tier: "epic", Gems: []Gem{{"hp", 20}}}},
			EquipmentPoints{HP: 242, Defense: 21, Crit: 1}},
		{"set of 4 sums both rows", []EquippedItem{
			{HP: 100, Category: "crafted", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"},
			{HP: 100, Category: "crafted", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"}},
			EquipmentPoints{HP: 520, Crit: 1}},
		{"shop items do not count toward sets", []EquippedItem{
			{HP: 100, Category: "shop", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"}},
			EquipmentPoints{HP: 200}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SumEquipment(tt.items, sets, ms)
			if math.Abs(got.Crit-tt.want.Crit) > 1e-9 {
				t.Fatalf("crit %v, want %v", got.Crit, tt.want.Crit)
			}
			got.Crit = tt.want.Crit
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// Equipment stats are points converted with the allocated-point multipliers;
// crit stays a percent.
func TestFromEquipment(t *testing.T) {
	m := Multipliers{HP: 50, Damage: 5, Mobility: 3, Defense: 5, SkillPower: 5, TerrainDamage: 3}
	ms := DefaultMilestones()
	sets := map[string][]SetBonus{"epic": {{PiecesRequired: 2, HPPct: 10, DEFPct: 5, CritPct: 1}}}
	tests := []struct {
		name  string
		items []EquippedItem
		want  Stats
	}{
		{"helmet hp 20 → 20 points × 50", []EquippedItem{{HP: 20, Defense: 2}}, Stats{HP: 1000, Defense: 10}},
		{"upgrade first, then the multiplier: hp 20 at +5 = 22 points",
			[]EquippedItem{{HP: 20, Damage: 8, MoveEnergy: 5, UpgradeLevel: 5}}, Stats{HP: 1100, DamageBonus: 45, MoveEnergyBonus: 18}},
		{"gem values are points", []EquippedItem{{Gems: []Gem{{"hp", 20}, {"damage", 3}}}, {Gems: []Gem{{"defense", 2}}}},
			Stats{HP: 1000, DamageBonus: 15, Defense: 10}},
		{"set bonus applies to the points", []EquippedItem{
			{HP: 15, Defense: 3, Category: "crafted", Tier: "epic"}, {HP: 15, Defense: 3, Category: "crafted", Tier: "epic"}},
			// hp 30 × 1.10 = 33 points; def 6 × 1.05 = 6.3 → 6 points; crit +1
			Stats{HP: 1650, Defense: 30, CritChance: 1}},
		{"crit is a percent with no multiplier", []EquippedItem{{Crit: 2, Gems: []Gem{{"critical", 1.5}}}}, Stats{CritChance: 3.5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromEquipment(SumEquipment(tt.items, sets, ms), m)
			if math.Abs(got.CritChance-tt.want.CritChance) > 1e-9 {
				t.Fatalf("crit %v, want %v", got.CritChance, tt.want.CritChance)
			}
			got.CritChance = tt.want.CritChance
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestComputeSumsParts(t *testing.T) {
	withGameData(t)
	equip := EquipmentPoints{HP: 3, Defense: 4, Damage: 6, Mobility: 2, Crit: 1.5}
	b := Compute("tanko", Points{HP: 2, Mobility: 1}, Multipliers{HP: 50, Mobility: 3, Damage: 5, Defense: 5}, equip)
	wantEquip := Stats{HP: 150, Defense: 20, DamageBonus: 30, MoveEnergyBonus: 6, CritChance: 1.5}
	want := Stats{HP: 1950 + 100 + 150, Defense: 80 + 20, DamageBonus: 30, MoveEnergy: 80, MoveEnergyBonus: 3 + 6, CritChance: 1.5}
	if b.Total.TurnMoveEnergy() != 89 {
		t.Fatalf("turn move energy %d, want 80 + 9", b.Total.TurnMoveEnergy())
	}
	if b.Total != want || b.Base != Base("tanko") || b.FromEquipment != wantEquip || b.EquipmentPoints != equip ||
		b.FromPoints != (Stats{HP: 100, MoveEnergyBonus: 3}) {
		t.Fatalf("breakdown %+v", b)
	}
}

func TestWeaponUpgradeLevel(t *testing.T) {
	items := []EquippedItem{{ItemID: "normal_helmet_lv10", Slot: "helmet", UpgradeLevel: 15}, {ItemID: "crafted_weapon_gold", Slot: "weapon", UpgradeLevel: 14}}
	if got := WeaponUpgradeLevel(items); got != 14 {
		t.Fatalf("got %d", got)
	}
	if got := WeaponUpgradeLevel(items[:1]); got != 0 {
		t.Fatalf("no weapon: got %d", got)
	}
}

func TestWeaponItemID(t *testing.T) {
	items := []EquippedItem{{ItemID: "normal_helmet_lv10", Slot: "helmet"}, {ItemID: "crafted_weapon_gold", Slot: "weapon"}}
	if got := WeaponItemID(items); got != "crafted_weapon_gold" {
		t.Fatalf("got %q", got)
	}
	if got := WeaponItemID(items[:1]); got != "" {
		t.Fatalf("no weapon: got %q", got)
	}
}
