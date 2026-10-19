package match

import (
	"math"
	"math/rand"

	"battle-squad/internal/game/gamedata"
)

const (
	fallDamageThreshold  = 150.0 // threshold height in pixels
	fallDamageMultiplier = 3.75  // HP per pixel of fall beyond threshold (fall damage ignores defense)
)

// ReduceByDefense is the damage a target with defense takes from raw damage:
// raw × K / (K + defense), K = combat.defense_k (gamedata.DefenseK).
func ReduceByDefense(raw float64, defense int) float64 {
	k := gamedata.DefenseK()
	return raw * k / (k + float64(defense))
}

// craterPxPerTerrainDamage converts a weapon's terrain damage to the radius (px) of the
// crater its explosion carves: Rookie's 50 carves 40 px, the size of his blast.
const craterPxPerTerrainDamage = 0.8

// weaponCraterRadius is the crater a weapon's explosion carves, before skills, fragile
// terrain and the shooter's terrain damage bonus: terrainDamage × craterPxPerTerrainDamage,
// or the blast radius for a weapon configured without terrain damage.
func weaponCraterRadius(w gamedata.WeaponConfig) float64 {
	if w.TerrainDamage <= 0 {
		return float64(w.ExplosionRadius)
	}
	return float64(w.TerrainDamage) * craterPxPerTerrainDamage
}

// craterRadius is the terrain an explosion carves: the projectile's crater grown by the
// shooter's terrain damage bonus. Damage falloff keeps the blast radius.
func craterRadius(shooter *BattlePlayerState, crater float64) float64 {
	return crater * (1 + float64(shooter.TerrainDamageBonus)/100)
}

// skillPowerFactor scales the shooter's skill damage and healing.
func skillPowerFactor(shooter *BattlePlayerState) float64 {
	return 1 + float64(shooter.SkillPowerBonus)/100
}

func CalculateExplosionDamage(playerPos Vector2, explosionCenter Vector2, baseDamage float64, explosionRadius float64, defense int) int {
	// 1. Calculate distance from center
	dx := playerPos.X - explosionCenter.X
	dy := playerPos.Y - explosionCenter.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	if distance >= explosionRadius {
		return 0 // Out of blast radius
	}

	// 2. Linear falloff multiplier
	damageMultiplier := 1.0 - (distance / explosionRadius)
	if damageMultiplier < 0 {
		damageMultiplier = 0
	} else if damageMultiplier > 1 {
		damageMultiplier = 1
	}

	rawDamage := baseDamage * damageMultiplier

	// 3. Defense reduction: rawDamage × K / (K + defense)
	return int(math.Round(ReduceByDefense(rawDamage, defense)))
}

func ApplyCritical(damage int, critChance float64) (int, bool) {
	if critChance <= 0 {
		return damage, false
	}
	roll := rand.Float64() * 100.0
	if roll < critChance {
		return int(math.Round(float64(damage) * 1.5)), true
	}
	return damage, false
}

func CalculateFallDamage(fallDistance float64) int {
	if fallDistance <= fallDamageThreshold {
		return 0
	}

	excessFall := fallDistance - fallDamageThreshold
	damage := excessFall * fallDamageMultiplier
	return int(math.Round(damage))
}
