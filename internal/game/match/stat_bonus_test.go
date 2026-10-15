package match

import (
	"math"
	"testing"

	"battle-squad/internal/game/gamedata"
)

func TestTerrainDamageBonusGrowsCraterOnly(t *testing.T) {
	m, clients := newFlowMatch(t)
	cur := m.State.CurrentPlayerID
	shooter, target := m.State.Players[cur], m.State.Players[m.other(cur)]
	shooter.TerrainDamageBonus = 30
	target.Position.X = shooter.Position.X + 15
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
	// The terrain was carved with the crater radius
	p := *ex[0].Position
	if m.Terrain.IsSolid(p.X, p.Y+36) {
		t.Fatalf("terrain inside the crater radius is still solid")
	}
}

// skillDamage fires a single-projectile skill straight down at the target next
// to the shooter and returns the damage it dealt.
func skillDamage(t *testing.T, skillPowerBonus int) int {
	t.Helper()
	m, clients := newFlowMatch(t)
	gamedata.Data.Characters["c1"] = gamedata.CharacterConfig{CharacterID: "c1", WeaponID: "w1", SkillID: "zap"}
	gamedata.Data.Skills["zap"] = gamedata.SkillConfig{SkillID: "zap", EffectType: "debuff", DamageMultiplier: 1}
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

func TestSkillPowerBonusScalesHeal(t *testing.T) {
	m, clients := newFlowMatch(t)
	gamedata.Data.Characters["c1"] = gamedata.CharacterConfig{CharacterID: "c1", WeaponID: "w1", SkillID: "bloom"}
	gamedata.Data.Skills["bloom"] = gamedata.SkillConfig{SkillID: "bloom", EffectType: "heal"}
	cur := m.State.CurrentPlayerID
	p := m.State.Players[cur]
	p.SkillEnergy = 100
	p.SkillPowerBonus = 50
	p.HP = 20
	groundPlayers(m, clients)

	send(m, clients[cur], "Shoot", ShootAction{Angle: 60, Power: 20, ActionMode: "skill"})
	s := shotResolved(t, clients[cur])
	if heals := eventsOf(s, "heal", cur); len(heals) != 1 || heals[0].Amount != 45 || p.HP != 65 {
		t.Fatalf("heal events %+v, hp=%d; want 30 * 1.5 = 45", heals, p.HP)
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
	// Weapon damage 30; +30 flat doubles it
	base, boosted := weaponDamage(0), weaponDamage(30)
	if base <= 0 || math.Abs(float64(boosted-2*base)) > 1 {
		t.Fatalf("weapon damage %d with +30 bonus, want about 2 x %d", boosted, base)
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
