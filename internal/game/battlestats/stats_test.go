package battlestats

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

func withGameData(t *testing.T) {
	t.Helper()
	prev := gamedata.Data
	gamedata.Data = &gamedata.GameData{
		Characters: map[string]gamedata.CharacterConfig{"tanko": {CharacterID: "tanko", HP: 130, Defense: 80, WeaponID: "cannon_ball"}},
		Weapons:    map[string]gamedata.WeaponConfig{"cannon_ball": {WeaponID: "cannon_ball", Damage: 75}},
	}
	t.Cleanup(func() { gamedata.Data = prev })
}

func TestBaseAndWeaponDamage(t *testing.T) {
	withGameData(t)
	if got := Base("tanko"); got != (Stats{HP: 130, Defense: 80}) {
		t.Fatalf("tanko base %+v", got)
	}
	if got := Base("ghost"); got != (Stats{HP: 100, Defense: 50}) {
		t.Fatalf("unknown character base %+v", got)
	}
	if WeaponDamage("tanko") != 75 || WeaponDamage("ghost") != 0 {
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

func TestFromEquipment(t *testing.T) {
	ms := DefaultMilestones()
	sets := map[string][]SetBonus{
		"epic": {{PiecesRequired: 2, HPPct: 10, DMGPct: 0, DEFPct: 5, CritPct: 1}, {PiecesRequired: 4, HPPct: 20}},
	}
	tests := []struct {
		name  string
		items []EquippedItem
		want  Stats
	}{
		{"none", nil, Stats{}},
		{"upgrade +5: ×1.10", []EquippedItem{{HP: 100, Damage: 10, Defense: 20, Crit: 2, MoveEnergy: 5, UpgradeLevel: 5}},
			Stats{HP: 110, DamageBonus: 11, Defense: 22, CritChance: 2.2, MoveEnergyBonus: 6}},
		{"upgrade +10: 1 + 0.20 + 0.10 + 0.20", []EquippedItem{{HP: 100, UpgradeLevel: 10}}, Stats{HP: 150}},
		{"upgrade +16: 1 + 0.32 + every milestone", []EquippedItem{{HP: 100, UpgradeLevel: 16}}, Stats{HP: 302}},
		{"gems add flat values (truncated)", []EquippedItem{{HP: 10, Gems: []Gem{{"hp", 7.9}, {"critical", 1.5}}}, {Gems: []Gem{{"damage", 3}, {"defense", 4}}}},
			Stats{HP: 17, CritChance: 1.5, DamageBonus: 3, Defense: 4}},
		{"crafted set of 2: hp +10%, def +5%, crit +1", []EquippedItem{
			{HP: 100, Defense: 10, Category: "crafted", Tier: "epic"},
			{HP: 100, Defense: 10, Category: "crafted", Tier: "epic", Gems: []Gem{{"hp", 20}}}},
			Stats{HP: 242, Defense: 21, CritChance: 1}},
		{"set of 4 sums both rows", []EquippedItem{
			{HP: 100, Category: "crafted", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"},
			{HP: 100, Category: "crafted", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"}},
			Stats{HP: 520, CritChance: 1}},
		{"shop items do not count toward sets", []EquippedItem{
			{HP: 100, Category: "shop", Tier: "epic"}, {HP: 100, Category: "crafted", Tier: "epic"}},
			Stats{HP: 200}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromEquipment(tt.items, sets, ms)
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
	equip := Stats{HP: 30, Defense: 4, DamageBonus: 6, MoveEnergyBonus: 2, CritChance: 1.5}
	b := Compute("tanko", Points{HP: 2, Mobility: 1}, Multipliers{HP: 50, Mobility: 3}, equip)
	want := Stats{HP: 130 + 100 + 30, Defense: 84, DamageBonus: 6, MoveEnergyBonus: 5, CritChance: 1.5}
	if b.Total != want || b.Base != Base("tanko") || b.FromEquipment != equip || b.FromPoints != (Stats{HP: 100, MoveEnergyBonus: 3}) {
		t.Fatalf("breakdown %+v", b)
	}
}
