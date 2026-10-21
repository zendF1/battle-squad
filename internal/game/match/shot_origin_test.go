package match

import (
	"testing"

	"battle-squad/internal/game/gamedata"
)

// originTerrain is 100x100 with ground from y=80 down.
func originTerrain() *Terrain {
	t := &Terrain{Width: 100, Height: 100}
	t.Mask = make([]uint64, maskSize(100, 100))
	t.DestructibleMask = make([]uint64, maskSize(100, 100))
	for y := 80; y < 100; y++ {
		for x := 0; x < 100; x++ {
			setBit(t.Mask, x, y, t.Width)
		}
	}
	return t
}

func TestShotOriginIsBodyCentre(t *testing.T) {
	if ShotOriginLift != 20 {
		t.Fatalf("ShotOriginLift = %v, want 20 (PlayerHeight / 2)", ShotOriginLift)
	}
	terrain := originTerrain()
	p := &BattlePlayerState{Position: Vector2{X: 50, Y: 80}}
	if got, want := shotOrigin(terrain, p), (Vector2{X: 50, Y: 60}); got != want {
		t.Fatalf("shotOrigin = %v, want %v", got, want)
	}
	// Near the top of the map the lifted point is in the sky, which is free.
	if got, want := shotOriginAt(terrain, Vector2{X: 10, Y: 5}), (Vector2{X: 10, Y: -15}); got != want {
		t.Fatalf("shotOriginAt near top = %v, want %v", got, want)
	}
}

func TestShotOriginFallsBackUnderLowCeiling(t *testing.T) {
	terrain := originTerrain()
	// Ceiling pixel 12 px above the feet: the origin is the highest free pixel below it.
	setBit(terrain.Mask, 50, 68, terrain.Width)
	if got, want := shotOriginAt(terrain, Vector2{X: 50, Y: 80}), (Vector2{X: 50, Y: 69}); got != want {
		t.Fatalf("shotOriginAt under ceiling = %v, want %v", got, want)
	}
	// Free pixels above a ceiling don't count: the scan stops at the first solid pixel.
	setBit(terrain.Mask, 40, 75, terrain.Width)
	if got, want := shotOriginAt(terrain, Vector2{X: 40, Y: 80}), (Vector2{X: 40, Y: 76}); got != want {
		t.Fatalf("shotOriginAt under thin ceiling = %v, want %v", got, want)
	}
	// Embedded in terrain (the pixel above the feet is solid): the origin is the feet.
	if got, want := shotOriginAt(terrain, Vector2{X: 50, Y: 90}), (Vector2{X: 50, Y: 90}); got != want {
		t.Fatalf("shotOriginAt embedded = %v, want %v", got, want)
	}
}

func TestShotStartsAtBodyCentre(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter := m.State.Players[cur]
	groundPlayers(m, clients)
	want := Vector2{X: shooter.Position.X, Y: shooter.Position.Y - ShotOriginLift}

	send(m, clients[cur], "Shoot", ShootAction{Angle: 45, Power: 50, ActionMode: "weapon"})
	s := shotResolved(t, clients[cur])
	if len(s.Projectiles) != 1 || len(s.Projectiles[0].Path) == 0 {
		t.Fatalf("expected one projectile with a path, got %+v", s.Projectiles)
	}
	if got := s.Projectiles[0].Path[0].Position; got != want {
		t.Fatalf("shot starts at %v, want body centre %v", got, want)
	}
}

func TestSkillShotsStartAtBodyCentre(t *testing.T) {
	m, clients := newFlowMatch(t)
	gamedata.Data.Characters["c1"] = gamedata.CharacterConfig{CharacterID: "c1", WeaponID: "w1", SkillID: "chain"}
	gamedata.Data.Skills["chain"] = gamedata.SkillConfig{SkillID: "chain", EffectType: effectBombChain, DamageMultiplier: 1}
	cur := m.State.CurrentPlayerID
	shooter := m.State.Players[cur]
	shooter.SkillEnergy = 100
	groundPlayers(m, clients)
	want := Vector2{X: shooter.Position.X, Y: shooter.Position.Y - ShotOriginLift}

	send(m, clients[cur], "Shoot", ShootAction{Angle: 60, Power: 40, ActionMode: "skill"})
	s := shotResolved(t, clients[cur])
	if len(s.Projectiles) != 2 {
		t.Fatalf("got %d projectiles, want 2", len(s.Projectiles))
	}
	for i, p := range s.Projectiles {
		if got := p.Path[0].Position; got != want {
			t.Fatalf("bomb %d starts at %v, want %v", i, got, want)
		}
	}
}

func TestTeleportShotStartsAtBodyCentre(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter := m.State.Players[cur]
	shooter.Items = []string{"teleport"}
	groundPlayers(m, clients)
	want := Vector2{X: shooter.Position.X, Y: shooter.Position.Y - ShotOriginLift}

	item := "teleport"
	send(m, clients[cur], "Shoot", ShootAction{Angle: 60, Power: 30, ActionMode: "item", ItemID: &item})
	s := shotResolved(t, clients[cur])
	if got := s.Projectiles[0].Path[0].Position; got != want {
		t.Fatalf("teleport starts at %v, want %v", got, want)
	}
}
