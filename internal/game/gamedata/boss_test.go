package gamedata

import (
	"testing"
)

// The default bosses are valid against the YAML characters and items; ValidateBoss refuses
// what the match cannot run.
func TestValidateBoss(t *testing.T) {
	snap, err := LoadYAML("../../../configs")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range DefaultBosses() {
		if err := ValidateBoss(b, snap.Data.Characters, snap.Data.Items); err != nil {
			t.Fatalf("%s: %v", b.BossID, err)
		}
		if _, ok := snap.Data.Maps[b.MapID]; !ok {
			t.Fatalf("%s: arena %s not in configs/maps.yaml", b.BossID, b.MapID)
		}
	}
	bad := map[string]func(b *BossConfig){
		"colon in id":     func(b *BossConfig) { b.BossID = "a:b" },
		"no skills":       func(b *BossConfig) { b.Skills = nil },
		"body size":       func(b *BossConfig) { b.BodySize = 10 },
		"out of map":      func(b *BossConfig) { b.OutOfMap = "fly" },
		"kind":            func(b *BossConfig) { b.Skills[0].Kind = "meteor" },
		"rain count":      func(b *BossConfig) { b.Skills[0].Count = 0 },
		"status turns":    func(b *BossConfig) { b.Skills[0].Status, b.Skills[0].StatusTurns = "burn", 0 },
		"summon minion":   func(b *BossConfig) { b.Skills[2].MinionID = "ghost" },
		"minion char":     func(b *BossConfig) { b.Minions[0].CharacterID = "nobody" },
		"initial > max":   func(b *BossConfig) { b.Waves.MaxAlive = 1 },
		"drop item":       func(b *BossConfig) { b.Rewards.Bonus.Drops[0].ItemID = "golden_ticket" },
		"drop min > max":  func(b *BossConfig) { b.Rewards.Normal.Drops[0].Min = 3 },
		"repeated skill":  func(b *BossConfig) { b.Skills[1].SkillID = b.Skills[0].SkillID },
		"shield factor":   func(b *BossConfig) { b.ShieldedDamageFactor = -0.1 },
		"zero turn limit": func(b *BossConfig) { b.TurnLimit = 0 },
	}
	for name, mut := range bad {
		b := DefaultBosses()[0]
		mut(&b)
		if ValidateBoss(b, snap.Data.Characters, snap.Data.Items) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// Boss queues and arenas of a snapshot; BossSpawns splits an arena's spawn points.
func TestBossLookups(t *testing.T) {
	bosses := DefaultBosses()
	bosses[1].Enabled = false
	s := NewSnapshot(nil).WithBosses(bosses)
	if b, ok := s.BossOfQueue("boss:steel_robot"); !ok || b.BossID != "steel_robot" {
		t.Fatal("steel_robot")
	}
	if _, ok := s.BossOfQueue("boss:lava_crab"); ok {
		t.Fatal("a disabled boss has a queue")
	}
	if q := s.BossQueues(); len(q) != 2 || q[1] != "boss:ice_witch" {
		t.Fatalf("queues %v", q)
	}
	if !s.IsBossArena("arena_lava_crab") || s.IsBossArena("ice_bridge") {
		t.Fatal("arenas")
	}
	if id, ok := BossIDOfQueue("boss:x"); !ok || id != "x" {
		t.Fatal("BossIDOfQueue")
	}
	if _, ok := BossIDOfQueue("ranked_2v2"); ok {
		t.Fatal("ranked is no boss queue")
	}
	m := MapConfig{GridWidth: 150, GridHeight: 70, CellSize: 16, Tiles: [][]int{{1}}, SpawnPoints: []SpawnPoint{{1, 0}, {2, 0}, {3, 0}, {4, 0}, {5, 0}, {6, 0}, {7, 0}}}
	party, boss, minions := BossSpawns(m)
	if len(party) != 4 || party[3].X != 4 || boss.X != 5 || len(minions) != 2 || minions[1].X != 7 {
		t.Fatalf("spawns %v %v %v", party, boss, minions)
	}
}
