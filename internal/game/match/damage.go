package match

import (
	"math"
	"math/rand"
)

const (
	fallDamageThreshold  = 150.0 // threshold height in pixels
	fallDamageMultiplier = 0.25  // 1 HP per 4 pixels of fall beyond threshold
)

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

	// 3. Defense reduction formula
	// finalDamage = rawDamage * (100 / (100 + defense))
	defenseFactor := 100.0 / (100.0 + float64(defense))
	finalDamage := rawDamage * defenseFactor

	return int(math.Round(finalDamage))
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
