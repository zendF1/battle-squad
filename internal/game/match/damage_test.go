package match

import (
	"testing"
)

func TestCalculateExplosionDamage(t *testing.T) {
	playerPos := Vector2{X: 100, Y: 100}
	explosionCenter := Vector2{X: 120, Y: 100} // distance 20 pixels
	baseDamage := 100.0
	explosionRadius := 50.0
	defense := 0

	// 1. Without defense:
	// dist = 20, radius = 50 -> multiplier = 1 - 20/50 = 0.6 -> dmg = 100 * 0.6 = 60
	dmg := CalculateExplosionDamage(playerPos, explosionCenter, baseDamage, explosionRadius, defense)
	if dmg != 60 {
		t.Errorf("Expected damage to be 60, got %d", dmg)
	}

	// 2. With defense = 100 (50% reduction):
	// rawDmg = 60 -> finalDmg = 60 * (100 / (100 + 100)) = 30
	defense = 100
	dmg = CalculateExplosionDamage(playerPos, explosionCenter, baseDamage, explosionRadius, defense)
	if dmg != 30 {
		t.Errorf("Expected damage to be 30, got %d", dmg)
	}

	// 3. Out of blast radius (distance 60 pixels > radius 50)
	explosionCenter = Vector2{X: 160, Y: 100}
	dmg = CalculateExplosionDamage(playerPos, explosionCenter, baseDamage, explosionRadius, defense)
	if dmg != 0 {
		t.Errorf("Expected damage to be 0 for out of blast radius, got %d", dmg)
	}
}

func TestCalculateFallDamage(t *testing.T) {
	// Fall distance <= 150 -> 0 damage
	dmg := CalculateFallDamage(120.0)
	if dmg != 0 {
		t.Errorf("Expected 0 fall damage for 120px fall, got %d", dmg)
	}

	// Fall distance 190 -> excess 40 -> 40 * 0.25 = 10 damage
	dmg = CalculateFallDamage(190.0)
	if dmg != 10 {
		t.Errorf("Expected 10 fall damage for 190px fall, got %d", dmg)
	}
}
