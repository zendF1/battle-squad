package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

// The terrain damage bonus grows craters by at most MaxTerrainDamageBonus percent.
func TestTerrainDamageBonusIsCapped(t *testing.T) {
	for _, c := range []struct {
		bonus int
		want  float64
	}{{0, 52}, {30, 67.6}, {50, 78}, {120, 78}, {790, 78}, {-20, 52}} {
		if got := craterRadius(&BattlePlayerState{TerrainDamageBonus: c.bonus}, 52, MaxTerrainDamageBonus); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("bonus %d: crater %v, want %v", c.bonus, got, c.want)
		}
	}
}

func TestTerrainDamageBonusGrowsCraterOnly(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter, target := m.State.Players[cur], m.State.Players[m.other(cur)]
	shooter.TerrainDamageBonus = 30
	target.Position.X = shooter.Position.X + 300 // the shell falls back into the ground at the shooter's feet
	groundPlayers(m, clients)

	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
	s := shotResolved(t, clients[cur])

	ex := eventsOf(s, "explode", "")
	if len(ex) != 1 {
		t.Fatalf("got %d explode events, want 1", len(ex))
	}
	if blast := s.Projectiles[0].ExplosionRadius; blast != 30 {
		t.Fatalf("blast radius %v, want the weapon's 30", blast)
	}
	if ex[0].Radius != 39 {
		t.Fatalf("crater radius %v, want 30 * 1.30 = 39", ex[0].Radius)
	}
	// The terrain was carved with the crater radius: a bowl 39 × bowlDepth = 28.1 px under the
	// impact (the weapon's 30 alone: 21.6) and 39 px to each side at its height
	p := *ex[0].Position
	if m.Terrain.IsSolid(p.X, p.Y+26) || m.Terrain.IsSolid(p.X+36, p.Y) || m.Terrain.IsSolid(p.X-36, p.Y) {
		t.Fatalf("terrain inside the crater radius is still solid")
	}
	if !m.Terrain.IsSolid(p.X, p.Y+30) {
		t.Fatalf("carved under the bowl's floor")
	}
}

// skillDamage fires a single-projectile skill straight down at the target next
// to the shooter and returns the damage it dealt.
func skillDamage(t *testing.T, skillPowerBonus int) int {
	t.Helper()
	m, clients := newFlowMatch(t)
	gamedata.Current().Data.Characters["c1"] = gamedata.CharacterConfig{CharacterID: "c1", WeaponID: "w1", SkillID: "zap"}
	gamedata.Current().Data.Skills["zap"] = gamedata.SkillConfig{SkillID: "zap", EffectType: "debuff", DamageMultiplier: 1}
	cur := m.State.CurrentPlayerID
	shooter, target := m.State.Players[cur], m.State.Players[m.other(cur)]
	shooter.SkillEnergy = 100
	shooter.SkillPowerBonus = skillPowerBonus
	target.Position.X = shooter.Position.X + 15
	groundPlayers(m, clients)

	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "skill"})
	s := shotResolved(t, clients[cur])
	hits := eventsOf(s, "damage", target.PlayerID)
	if len(hits) == 0 {
		t.Fatalf("skill did not hit the target: %+v", s.Events)
	}
	return hits[0].Amount
}

func TestSkillPowerBonusScalesSkillDamage(t *testing.T) {
	base := skillDamage(t, 0)
	boosted := skillDamage(t, 100)
	if base <= 0 || math.Abs(float64(boosted-2*base)) > 1 {
		t.Fatalf("skill damage %d with +100%% skill power, want about 2 x %d", boosted, base)
	}
}

// TestBloomIgnoresSkillPower: bloom heals a percent of max HP, not scaled by skill power.
func TestBloomIgnoresSkillPower(t *testing.T) {
	m, clients := newFlowMatch(t)
	gamedata.Current().Data.Characters["c1"] = gamedata.CharacterConfig{CharacterID: "c1", WeaponID: "w1", SkillID: "bloom"}
	gamedata.Current().Data.Skills["bloom"] = gamedata.SkillConfig{SkillID: "bloom", EffectType: effectBloom}
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.SkillEnergy = 100
	p.SkillPowerBonus = 50
	p.HP = 300
	groundPlayers(m, clients)

	send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 50, ActionMode: "skill"})
	s := shotResolved(t, clients[cur])
	if heals := eventsOf(s, "heal", cur); len(heals) != 1 || heals[0].Amount != 600 || p.HP != 900 {
		t.Fatalf("heal events %+v, hp=%d; want 40%% of 1500 = 600", heals, p.HP)
	}
}

func TestDamageBonusAddsToWeaponDamage(t *testing.T) {
	weaponDamage := func(bonus int) int {
		m, clients := newFlowMatch(t)
		cur := m.State.CurrentPlayerID
		shooter, target := m.State.Players[cur], m.State.Players[m.other(cur)]
		shooter.DamageBonus = bonus
		target.Position.X = shooter.Position.X + 15
		groundPlayers(m, clients)
		send(m, clients[cur], "Shoot", ShootAction{Angle: 90, Power: 10, ActionMode: "weapon"})
		hits := eventsOf(shotResolved(t, clients[cur]), "damage", target.PlayerID)
		if len(hits) == 0 {
			t.Fatal("shot did not hit the target")
		}
		return hits[0].Amount
	}
	// Weapon damage 240; +240 flat doubles it
	base, boosted := weaponDamage(0), weaponDamage(240)
	if base <= 0 || math.Abs(float64(boosted-2*base)) > 1 {
		t.Fatalf("weapon damage %d with +240 bonus, want about 2 x %d", boosted, base)
	}
}

func TestMoveEnergyBonusRefillsEachTurn(t *testing.T) {
	m, _ := newFlowMatch(t)
	next := m.State.Players[m.other(m.State.CurrentPlayerID)]
	next.MoveEnergyBonus = 9
	next.MoveEnergy = 0
	m.endTurn(t.Context())
	if m.State.CurrentPlayerID != next.PlayerID || next.MoveEnergy != 109 {
		t.Fatalf("current=%s energy=%d, want %s with 109", m.State.CurrentPlayerID, next.MoveEnergy, next.PlayerID)
	}
}
