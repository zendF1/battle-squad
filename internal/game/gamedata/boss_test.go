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
		"behavior":        func(b *BossConfig) { b.Minions[0].Behavior = "kamikaze" },
		"movement":        func(b *BossConfig) { b.Minions[0].Movement = "swim" },
		"explode of":      func(b *BossConfig) { b.Minions[0].ExplodeDamageOf = "boss" },
		"explode pct":     func(b *BossConfig) { b.Minions[0].ExplodeDamagePct = 150 },
		"fly range":       func(b *BossConfig) { b.Minions[0].FlyRange = -1 },
		"walk range":      func(b *BossConfig) { b.Minions[0].WalkRange = 5000 },
		"fly speed":       func(b *BossConfig) { b.Minions[0].FlySpeed = 5 },
		"hover grounded":  func(b *BossConfig) { b.Hover = &BossHover{MinX: 1, MaxX: 2, MinY: 1, MaxY: 2} },
		"hover band": func(b *BossConfig) {
			b.Flying, b.Hover = true, &BossHover{MinX: 900, MaxX: 100, MinY: 1, MaxY: 2}
		},
	}
	for name, mut := range bad {
		b := DefaultBosses()[0]
		mut(&b)
		if ValidateBoss(b, snap.Data.Characters, snap.Data.Items) == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// Zero minion fields are the defaults; a bomber's shots are weak by default; the hover JSON
// round-trips (null: none).
func TestMinionDefaults(t *testing.T) {
	m := MinionConfig{MinionID: "x", Behavior: MinionBomber}.Normalized()
	if m.Movement != MinionMoveWalk || m.ShotDamageFactor != BomberShotDamageFactor || m.DetonateRange != 70 || m.FlyRange != 90 ||
		m.WalkRange != 80 || m.HoverHeight != 60 || m.FlySpeed != 220 || m.ExplodeDamageOf != ExplodeOfTarget || m.ExplodeDamagePct != 50 ||
		m.ExplodeRadius != 80 || m.ExplodeCrater != 20 {
		t.Fatalf("defaults %+v", m)
	}
	if s := (MinionConfig{}).Normalized(); s.Behavior != MinionShooter || s.ShotDamageFactor != 1 {
		t.Fatalf("shooter %+v", s)
	}
	var b BossConfig
	if err := ParseBossHover(&b, []byte(`{"minX":1,"maxX":2,"minY":3,"maxY":4}`)); err != nil || b.Hover == nil ||
		b.Hover.Normalized().MinMove != 200 || b.Hover.Normalized().FlySpeed != 220 {
		t.Fatalf("hover %+v %v", b.Hover, err)
	}
	if err := ParseBossHover(&b, []byte(`{"minX":1,"speed":2}`)); err == nil {
		t.Fatal("unknown hover field accepted")
	}
	if err := ParseBossHover(&b, nil); err != nil || b.Hover != nil || b.HoverJSON() != nil {
		t.Fatal("no hover")
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
	m := MapConfig{GridWidth: 150, GridHeight: 70, CellSize: 16, Tiles: [][]int{{1}}, SpawnPoints: []SpawnPoint{{X: 1}, {X: 2}, {X: 3}, {X: 4}, {X: 5}, {X: 6}, {X: 7}}}
	party, boss, minions := BossSpawns(m)
	if len(party) != 4 || party[3].X != 4 || boss.X != 5 || len(minions) != 2 || minions[1].X != 7 {
		t.Fatalf("spawns %v %v %v", party, boss, minions)
	}
}
