package match

import (
	"math/rand"
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

func TestApplyCritical_ZeroChance(t *testing.T) {
	// 0% crit chance → never crits
	for i := 0; i < 100; i++ {
		dmg, isCrit := ApplyCritical(100, 0)
		if isCrit {
			t.Fatal("should never crit with 0% chance")
		}
		if dmg != 100 {
			t.Fatalf("damage should be unchanged, got %d", dmg)
		}
	}
}

func TestApplyCritical_NegativeChance(t *testing.T) {
	dmg, isCrit := ApplyCritical(100, -10)
	if isCrit {
		t.Fatal("should never crit with negative chance")
	}
	if dmg != 100 {
		t.Fatalf("damage should be unchanged, got %d", dmg)
	}
}

func TestApplyCritical_HundredPercent(t *testing.T) {
	// 100% crit → always crits, damage ×1.5
	for i := 0; i < 100; i++ {
		dmg, isCrit := ApplyCritical(100, 100)
		if !isCrit {
			t.Fatal("should always crit with 100% chance")
		}
		if dmg != 150 {
			t.Fatalf("100 damage × 1.5 = 150, got %d", dmg)
		}
	}
}

func TestApplyCritical_Multiplier(t *testing.T) {
	// Test exact 1.5x multiplier with various damage values
	testCases := []struct {
		baseDmg  int
		expected int
	}{
		{100, 150},
		{200, 300},
		{1, 2},   // 1 * 1.5 = 1.5, rounded to 2
		{33, 50},  // 33 * 1.5 = 49.5, rounded to 50
		{0, 0},    // 0 damage stays 0
	}
	for _, tc := range testCases {
		dmg, _ := ApplyCritical(tc.baseDmg, 100)
		if dmg != tc.expected {
			t.Errorf("ApplyCritical(%d, 100) = %d, want %d", tc.baseDmg, dmg, tc.expected)
		}
	}
}

func TestApplyCritical_StatisticalDistribution(t *testing.T) {
	// With 50% crit chance over many trials, roughly half should crit
	rand.Seed(42) // deterministic seed for reproducibility
	critCount := 0
	trials := 10000
	for i := 0; i < trials; i++ {
		_, isCrit := ApplyCritical(100, 50)
		if isCrit {
			critCount++
		}
	}
	// Expect ~50% crits, allow ±5% tolerance
	ratio := float64(critCount) / float64(trials)
	if ratio < 0.45 || ratio > 0.55 {
		t.Errorf("50%% crit chance: got %.2f%% crit rate over %d trials (expected ~50%%)", ratio*100, trials)
	}
}

func TestApplyCritical_WithEquipmentDefense(t *testing.T) {
	// Full combat scenario: explosion damage → defense → critical
	// Base 100 dmg, dist 0 (direct hit), defense 100 (50% reduction), 100% crit
	baseDmg := CalculateExplosionDamage(
		Vector2{X: 100, Y: 100},
		Vector2{X: 100, Y: 100}, // direct hit
		100.0,
		50.0,
		100, // defense = 50% reduction
	)
	// Expected: 100 * 1.0 (direct hit) * (100/200) = 50
	if baseDmg != 50 {
		t.Fatalf("base damage with defense should be 50, got %d", baseDmg)
	}

	// Apply crit: 50 * 1.5 = 75
	finalDmg, isCrit := ApplyCritical(baseDmg, 100)
	if !isCrit {
		t.Fatal("should crit with 100% chance")
	}
	if finalDmg != 75 {
		t.Errorf("crit damage should be 75, got %d", finalDmg)
	}
}
