package match

import (
	"math"
	"math/rand"

	"battle-squad/internal/game/gamedata"
)

// ReduceByDefense is the damage a target with defense takes from raw damage with the current
// game data's combat.defense_k (reduceByDefense).
func ReduceByDefense(raw float64, defense int) float64 {
	return reduceByDefense(raw, defense, gamedata.DefenseK())
}

// reduceByDefense is raw × k / (k + defense).
func reduceByDefense(raw float64, defense int, k float64) float64 {
	return raw * k / (k + float64(defense))
}

// weaponCraterRadius is the crater a weapon's explosion carves, before skills, fragile
// terrain and the shooter's terrain damage bonus: terrainDamage × craterPxPerTerrainDamage
// (rules.terrain, 0.6 since migration 039: Rookie's and Spark's 20 carve 12 px, Flora's 24
// 14.4 px, Tanko's 30 18 px), or the blast radius for a weapon configured without terrain
// damage.
func weaponCraterRadius(gd *gamedata.Snapshot, w gamedata.WeaponConfig) float64 {
	return WeaponCraterRadius(gd, w.ExplosionRadius, w.TerrainDamage)
}

// craterRadius is the terrain an explosion carves: the projectile's crater grown by the
// shooter's terrain damage bonus, at most maxBonus % (rules.terrain
// maxTerrainDamageBonusPercent, 50: the bonus comes from stat points, 1 % each, up to ~790
// points at level 80; uncapped, Tanko's 18 px crater would reach ~160 px; at the cap Tanko
// carves 27 px, 19 px deep, and the battle maps keep at least 3 such craters of ground under
// every spawn). Damage falloff keeps the blast radius.
func craterRadius(shooter *BattlePlayerState, crater float64, maxBonus int) float64 {
	bonus := min(max(shooter.TerrainDamageBonus, 0), maxBonus)
	return crater * (1 + float64(bonus)/100)
}

// skillPowerFactor scales the shooter's skill damage and healing.
func skillPowerFactor(shooter *BattlePlayerState) float64 {
	return 1 + float64(shooter.SkillPowerBonus)/100
}

// CalculateExplosionDamage is calculateExplosionDamage with the current game data's
// combat.defense_k.
func CalculateExplosionDamage(playerPos Vector2, explosionCenter Vector2, baseDamage float64, explosionRadius float64, defense int) int {
	return calculateExplosionDamage(playerPos, explosionCenter, baseDamage, explosionRadius, defense, gamedata.DefenseK())
}

// calculateExplosionDamage is the damage a player at playerPos takes from an explosion: the
// base damage falling off linearly to 0 at the blast radius, reduced by defense with K.
func calculateExplosionDamage(playerPos Vector2, explosionCenter Vector2, baseDamage float64, explosionRadius float64, defense int, k float64) int {
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
	return int(math.Round(reduceByDefense(rawDamage, defense, k)))
}

// ApplyCritical rolls a critical hit with the current game data's crit multiplier.
func ApplyCritical(damage int, critChance float64) (int, bool) {
	return applyCritical(damage, critChance, gamedata.Current().Rules.Combat.CritMultiplier)
}

// applyCritical rolls critChance %: a critical hit deals multiplier × the damage.
func applyCritical(damage int, critChance, multiplier float64) (int, bool) {
	if critChance <= 0 {
		return damage, false
	}
	roll := rand.Float64() * 100.0
	if roll < critChance {
		return int(math.Round(float64(damage) * multiplier)), true
	}
	return damage, false
}

// CalculateFallDamage is calculateFallDamage with the current game data's fall.* rows.
func CalculateFallDamage(fallDistance float64) int {
	return calculateFallDamage(gamedata.Current().Physics, fallDistance)
}

// calculateFallDamage is the HP a fall costs (no defense): nothing up to fall.damage_threshold
// px (150), then fall.damage_per_pixel (3.75) per px beyond.
func calculateFallDamage(p *gamedata.PhysicsConfig, fallDistance float64) int {
	if fallDistance <= p.FallDamageThreshold {
		return 0
	}

	excessFall := fallDistance - p.FallDamageThreshold
	damage := excessFall * p.FallDamagePerPixel
	return int(math.Round(damage))
}
