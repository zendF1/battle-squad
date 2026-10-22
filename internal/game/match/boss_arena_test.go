package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
	"battle-squad/internal/game/mapgen"
)

// The arenas: valid maps (as the admin editor checks them), every spawn stands on ground, the
// boss on hard (indestructible) ground; each is a boss's arena (kept out of the PvP maps).
func TestBossArenasAreSound(t *testing.T) {
	loadConfigs(t)
	gd := gamedata.Current()
	for _, a := range mapgen.Arenas {
		cfg, ok := gd.Data.Maps[a.ID]
		if !ok {
			t.Fatalf("%s not in configs/maps.yaml", a.ID)
		}
		if err := gamedata.ValidateMap(cfg, gd.Bricks); err != nil {
			t.Fatalf("%s: %v", a.ID, err)
		}
		tr := NewTerrainFor(gd, cfg)
		party, boss, minions := gamedata.BossSpawns(cfg)
		for i, sp := range append(append(party, boss), minions...) {
			if !tr.CanStandAt(sp.X, sp.Y) {
				t.Fatalf("%s spawn %d (%v) does not stand", a.ID, i, sp)
			}
		}
		y := tr.SpawnY(boss.X, boss.Y)
		for dx := -40.0; dx <= 40; dx += 8 {
			if !tr.IsSolid(boss.X+dx, y+1) || tr.IsDestructible(int(boss.X+dx), int(y+1)) {
				t.Fatalf("%s: the boss's ground at %+.0f is not hard", a.ID, dx)
			}
		}
		if !gd.IsBossArena(a.ID) {
			t.Fatalf("%s is not a boss arena", a.ID)
		}
	}
}
