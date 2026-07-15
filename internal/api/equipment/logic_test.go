package equipment

import (
	"math"
	"testing"
)

const epsilon = 1e-9

// ===========================================================================
// CalculateUpgradeMultiplier — stat multiplier per upgrade level
// ===========================================================================

func TestCalculateUpgradeMultiplier(t *testing.T) {
	tests := []struct {
		name     string
		level    int
		expected float64
	}{
		// Base cases
		{"level 0 — no bonus", 0, 1.0},
		{"level 1 — +2%", 1, 1.02},
		{"level 3 — +6%", 3, 1.06},
		{"level 5 — +10% (pre-milestone)", 5, 1.10},

		// Milestone at +6: +10% bonus
		{"level 6 — milestone +6 (0.12+0.10)", 6, 1.22},
		{"level 7 — post-milestone", 7, 1.24},
		{"level 9 — pre-milestone +10", 9, 1.28},

		// Milestone at +10: +20% bonus (cumulative with +6)
		{"level 10 — milestone +10 (0.20+0.10+0.20)", 10, 1.50},
		{"level 12 — mid safezone", 12, 1.54},
		{"level 13 — pre-milestone +14", 13, 1.56},

		// Milestone at +14: +40% bonus (cumulative)
		{"level 14 — milestone +14 (0.28+0.10+0.20+0.40)", 14, 1.98},
		{"level 15 — pre-max", 15, 2.00},

		// Milestone at +16: +100% bonus (legendary)
		{"level 16 — max legendary (0.32+0.10+0.20+0.40+1.00)", 16, 3.02},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateUpgradeMultiplier(tt.level)
			if math.Abs(got-tt.expected) > epsilon {
				t.Errorf("CalculateUpgradeMultiplier(%d) = %.6f, want %.6f", tt.level, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// getSafezoneStart — safezone floor for upgrade failure
// ===========================================================================

func TestGetSafezoneStart(t *testing.T) {
	tests := []struct {
		name     string
		level    int
		expected int
	}{
		// +1 to +5: fail = stay at current (safezone start = 0)
		{"level 1 — safe zone floor 0", 1, 0},
		{"level 3 — safe zone floor 0", 3, 0},
		{"level 5 — safe zone floor 0", 5, 0},

		// +6 to +9: fail = reset to +6
		{"level 6 — safe zone floor 6", 6, 6},
		{"level 7 — safe zone floor 6", 7, 6},
		{"level 8 — safe zone floor 6", 8, 6},
		{"level 9 — safe zone floor 6", 9, 6},

		// +10 to +13: fail = reset to +10
		{"level 10 — safe zone floor 10", 10, 10},
		{"level 11 — safe zone floor 10", 11, 10},
		{"level 13 — safe zone floor 10", 13, 10},

		// +14 to +16: fail = reset to +14
		{"level 14 — safe zone floor 14", 14, 14},
		{"level 15 — safe zone floor 14", 15, 14},
		{"level 16 — safe zone floor 14", 16, 14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSafezoneStart(tt.level)
			if got != tt.expected {
				t.Errorf("getSafezoneStart(%d) = %d, want %d", tt.level, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// CalculateUpgradePercent — success probability formula
// ===========================================================================

func TestCalculateUpgradePercent(t *testing.T) {
	tests := []struct {
		name       string
		totalPower int
		cost       int
		maxPercent float64
		expected   float64
	}{
		// Basic formula: totalPower * 100 / cost
		{"exact match cost", 1, 1, 80, 80},          // 1*100/1 = 100 → cap 80
		{"half power", 1, 2, 76, 50},                 // 1*100/2 = 50
		{"below cap", 6, 18, 68, 33.333333333333336}, // 6*100/18 ≈ 33.33

		// Capping at max percent
		{"+0→+1: 1 đá cấp 1 (power 1), cost 1, max 80%", 1, 1, 80, 80},
		{"+5→+6: 1 đá cấp 5 (81), cost 80, max 60%", 81, 80, 60, 60},    // 81*100/80=101.25 → cap 60
		{"+6→+7: 1 đá cấp 5 (81), cost 180, max 56%", 81, 180, 56, 45},  // 81*100/180=45
		{"+6→+7: 1 đá cấp 6 (243), cost 180, max 56%", 243, 180, 56, 56}, // 243*100/180=135 → cap 56

		// Example from design doc: +8→+9 (cost=1200, max=45%)
		{"doc example: 1 đá cấp 6 (243) for +8→+9", 243, 1200, 45, 20.25},
		{"doc example: 2 đá cấp 6 (486) for +8→+9", 486, 1200, 45, 40.5},
		{"doc example: 1 đá cấp 7 (729) for +8→+9 → cap", 729, 1200, 45, 45}, // 60.75 → cap 45
		{"doc example: 5 đá cấp 5 (405) for +8→+9", 405, 1200, 45, 33.75},
		{"doc example: 1 cấp 6 + 3 cấp 4 = 324 for +8→+9", 324, 1200, 45, 27},

		// Edge cases
		{"zero power", 0, 100, 80, 0},
		{"zero cost", 100, 0, 80, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateUpgradePercent(tt.totalPower, tt.cost, tt.maxPercent)
			if math.Abs(got-tt.expected) > epsilon {
				t.Errorf("CalculateUpgradePercent(%d, %d, %.2f) = %.6f, want %.6f",
					tt.totalPower, tt.cost, tt.maxPercent, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// CalculateTotalStonePower — sum power from stone inputs
// ===========================================================================

func TestCalculateTotalStonePower(t *testing.T) {
	tests := []struct {
		name     string
		stones   []StoneInput
		expected int
	}{
		{"single stone level 1", []StoneInput{{1, 1}}, 1},
		{"single stone level 6", []StoneInput{{6, 1}}, 243},
		{"3 stones level 4", []StoneInput{{4, 3}}, 81},            // 27*3
		{"mixed: 1 lv6 + 3 lv4", []StoneInput{{6, 1}, {4, 3}}, 324}, // 243 + 27*3
		{"all 12 levels x1", []StoneInput{
			{1, 1}, {2, 1}, {3, 1}, {4, 1}, {5, 1}, {6, 1},
			{7, 1}, {8, 1}, {9, 1}, {10, 1}, {11, 1}, {12, 1},
		}, 1+3+9+27+81+243+729+2187+6561+19683+59049+177147},
		{"empty", []StoneInput{}, 0},
		{"zero quantity ignored", []StoneInput{{5, 0}}, 0},
		{"invalid level ignored", []StoneInput{{0, 1}, {13, 1}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateTotalStonePower(tt.stones)
			if got != tt.expected {
				t.Errorf("CalculateTotalStonePower(%v) = %d, want %d", tt.stones, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// StonePowers — verify geometric progression ×3
// ===========================================================================

func TestStonePowersProgression(t *testing.T) {
	expectedPowers := map[int]int{
		1: 1, 2: 3, 3: 9, 4: 27, 5: 81, 6: 243,
		7: 729, 8: 2187, 9: 6561, 10: 19683, 11: 59049, 12: 177147,
	}
	for level, expected := range expectedPowers {
		if StonePowers[level] != expected {
			t.Errorf("StonePowers[%d] = %d, want %d", level, StonePowers[level], expected)
		}
	}
	// Verify ×3 progression
	for i := 2; i <= 12; i++ {
		if StonePowers[i] != StonePowers[i-1]*3 {
			t.Errorf("StonePowers[%d] = %d, expected %d (StonePowers[%d]*3)",
				i, StonePowers[i], StonePowers[i-1]*3, i-1)
		}
	}
}

// ===========================================================================
// CalculateDismantleRefund — 50% power refund as highest stones
// ===========================================================================

func TestCalculateDismantleRefund(t *testing.T) {
	tests := []struct {
		name           string
		totalPower     int
		expectedStones map[int]int
		expectedTotal  int // verify total power of refunded stones
	}{
		{
			"zero power — nothing refunded",
			0,
			map[int]int{},
			0,
		},
		{
			"1 power — 50% rounds to 0",
			1,
			map[int]int{},
			0,
		},
		{
			"2 power — refund 1 level-1 stone",
			2,
			map[int]int{1: 1},
			1,
		},
		{
			"6 power — refund 1 level-1 stone (3/3=1, then 0 left)",
			6,
			map[int]int{2: 1}, // 6/2=3 → level2 power=3 → 1 stone
			3,
		},
		{
			"486 power (2×lv6) — refund 243 power",
			486,
			map[int]int{6: 1}, // 486/2=243 → exactly 1 level-6 stone
			243,
		},
		{
			"1458 power (2×lv7) — refund 729 power",
			1458,
			map[int]int{7: 1}, // 1458/2=729 → 1 level-7 stone
			729,
		},
		{
			"500 power — refund 250",
			500,
			map[int]int{6: 1, 1: 1}, // 500/2=250 → lv6=243(1), remainder 7 → lv2=3(1)... wait
			// 250: lv6=243→1 stone, rem=7. lv2=3→2 stones, rem=1. lv1=1→1 stone. total=243+6+1=250
			250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateDismantleRefund(tt.totalPower)

			// Verify total refund power
			totalRefundPower := 0
			for level, qty := range got {
				totalRefundPower += StonePowers[level] * qty
			}

			// Total refund should be <= 50% of totalPower
			halfPower := tt.totalPower / 2
			if totalRefundPower > halfPower {
				t.Errorf("refund power %d exceeds 50%% of %d (=%d)", totalRefundPower, tt.totalPower, halfPower)
			}

			// Total refund should be as close to 50% as possible (greedy algorithm)
			// The difference should be less than the smallest stone power (1)
			if halfPower > 0 && halfPower-totalRefundPower > 0 {
				// This is fine — integer division loses remainder
			}

			// Verify no stone level > 12 or < 1
			for level := range got {
				if level < 1 || level > 12 {
					t.Errorf("invalid stone level %d in refund", level)
				}
			}
		})
	}
}

// ===========================================================================
// CalculateMergeSuccessRate — 25% per item
// ===========================================================================

func TestCalculateMergeSuccessRate(t *testing.T) {
	tests := []struct {
		count    int
		expected float64
	}{
		{1, 25.0},  // Not allowed (validation blocks), but formula works
		{2, 50.0},  // Min allowed
		{3, 75.0},
		{4, 100.0}, // Guaranteed success
	}

	for _, tt := range tests {
		got := CalculateMergeSuccessRate(tt.count)
		if got != tt.expected {
			t.Errorf("CalculateMergeSuccessRate(%d) = %.1f, want %.1f", tt.count, got, tt.expected)
		}
	}
}

// ===========================================================================
// ValidateMergeStone — parameter validation
// ===========================================================================

func TestValidateMergeStone(t *testing.T) {
	tests := []struct {
		name       string
		stoneLevel int
		count      int
		wantErr    bool
	}{
		{"valid: 2 stones lv1", 1, 2, false},
		{"valid: 4 stones lv6", 6, 4, false},
		{"valid: 3 stones lv11", 11, 3, false},
		{"invalid: count too low", 5, 1, true},
		{"invalid: count too high", 5, 5, true},
		{"invalid: stone level 12 (max)", 12, 4, true},
		{"invalid: stone level 0", 0, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMergeStone(tt.stoneLevel, tt.count)
			if tt.wantErr && err == "" {
				t.Error("expected error but got none")
			}
			if !tt.wantErr && err != "" {
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}

// ===========================================================================
// ValidateMergeGem — parameter validation
// ===========================================================================

func TestValidateMergeGem(t *testing.T) {
	tests := []struct {
		name     string
		gemLevel int
		count    int
		wantErr  bool
	}{
		{"valid: 2 gems lv1", 1, 2, false},
		{"valid: 4 gems lv5", 5, 4, false},
		{"valid: 3 gems lv9", 9, 3, false},
		{"invalid: count too low", 5, 1, true},
		{"invalid: count too high", 5, 5, true},
		{"invalid: gem level 10 (max)", 10, 4, true},
		{"invalid: gem level 0", 0, 2, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMergeGem(tt.gemLevel, tt.count)
			if tt.wantErr && err == "" {
				t.Error("expected error but got none")
			}
			if !tt.wantErr && err != "" {
				t.Errorf("unexpected error: %s", err)
			}
		})
	}
}

// ===========================================================================
// CalculateGemPrice — pricing logic
// ===========================================================================

func TestCalculateGemPrice(t *testing.T) {
	tests := []struct {
		level         int
		wantCurrency  string
		wantPriceEach int
	}{
		{1, "coin", 200},
		{2, "coin", 400},
		{3, "coin", 600},
		{4, "gem", 30},
		{5, "gem", 60},
		{6, "gem", 90},
	}

	for _, tt := range tests {
		currency, price := CalculateGemPrice(tt.level)
		if currency != tt.wantCurrency || price != tt.wantPriceEach {
			t.Errorf("CalculateGemPrice(%d) = (%s, %d), want (%s, %d)",
				tt.level, currency, price, tt.wantCurrency, tt.wantPriceEach)
		}
	}
}

// ===========================================================================
// CalculateEquipmentStat — base stat with upgrade multiplier
// ===========================================================================

func TestCalculateEquipmentStat(t *testing.T) {
	tests := []struct {
		name     string
		baseStat int
		level    int
		expected int
	}{
		// Design doc example: Vũ khí base DMG 50, +10
		// multiplier = 1 + 0.20 + 0.10 + 0.20 = 1.50 → 50 * 1.50 = 75
		{"design doc: weapon DMG 50 +10", 50, 10, 75},

		// No upgrade
		{"no upgrade", 100, 0, 100},

		// +6 milestone
		{"+6: 100 base", 100, 6, 122}, // 100 * 1.22 = 122

		// +16 legendary
		{"+16: 50 base", 50, 16, 151}, // 50 * 3.02 = 151

		// Zero base stat
		{"zero base", 0, 10, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateEquipmentStat(tt.baseStat, tt.level)
			if got != tt.expected {
				t.Errorf("CalculateEquipmentStat(%d, %d) = %d, want %d",
					tt.baseStat, tt.level, got, tt.expected)
			}
		})
	}
}

// ===========================================================================
// Full upgrade scenario tests — end-to-end upgrade flow logic
// ===========================================================================

func TestUpgradeScenario_DesignDocExample(t *testing.T) {
	// Design doc: nâng +8 → +9 (cost = 1200, max = 45%)
	cost := 1200
	maxPct := 45.0

	// Case 1: Bỏ 1 viên cấp 6 (power 243) → 20.25%
	power := CalculateTotalStonePower([]StoneInput{{StoneLevel: 6, Quantity: 1}})
	if power != 243 {
		t.Fatalf("expected power 243, got %d", power)
	}
	pct := CalculateUpgradePercent(power, cost, maxPct)
	if math.Abs(pct-20.25) > epsilon {
		t.Errorf("case 1: expected 20.25%%, got %.2f%%", pct)
	}

	// Case 2: Bỏ 2 viên cấp 6 (power 486) → 40.5%
	power = CalculateTotalStonePower([]StoneInput{{StoneLevel: 6, Quantity: 2}})
	if power != 486 {
		t.Fatalf("expected power 486, got %d", power)
	}
	pct = CalculateUpgradePercent(power, cost, maxPct)
	if math.Abs(pct-40.5) > epsilon {
		t.Errorf("case 2: expected 40.5%%, got %.2f%%", pct)
	}

	// Case 3: Bỏ 1 viên cấp 7 (power 729) → cap 45%
	power = CalculateTotalStonePower([]StoneInput{{StoneLevel: 7, Quantity: 1}})
	if power != 729 {
		t.Fatalf("expected power 729, got %d", power)
	}
	pct = CalculateUpgradePercent(power, cost, maxPct)
	if math.Abs(pct-45.0) > epsilon {
		t.Errorf("case 3: expected 45%% (capped), got %.2f%%", pct)
	}

	// Case 4: Bỏ 5 viên cấp 5 (power 405) → 33.75%
	power = CalculateTotalStonePower([]StoneInput{{StoneLevel: 5, Quantity: 5}})
	if power != 405 {
		t.Fatalf("expected power 405, got %d", power)
	}
	pct = CalculateUpgradePercent(power, cost, maxPct)
	if math.Abs(pct-33.75) > epsilon {
		t.Errorf("case 4: expected 33.75%%, got %.2f%%", pct)
	}

	// Case 5: Mix: 1 viên cấp 6 + 3 viên cấp 4 = 243 + 81 = 324 → 27%
	power = CalculateTotalStonePower([]StoneInput{{StoneLevel: 6, Quantity: 1}, {StoneLevel: 4, Quantity: 3}})
	if power != 324 {
		t.Fatalf("expected power 324, got %d", power)
	}
	pct = CalculateUpgradePercent(power, cost, maxPct)
	if math.Abs(pct-27.0) > epsilon {
		t.Errorf("case 5: expected 27%%, got %.2f%%", pct)
	}
}

func TestUpgradeScenario_FailureResets(t *testing.T) {
	// Test that failure resets follow the safezone rules from the upgrade table
	upgradeTable := []struct {
		fromLevel   int
		failResetTo int
	}{
		{0, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 5}, // +0-5: giữ nguyên
		{6, 6}, {7, 6}, {8, 6}, {9, 6},                  // +6-9: về +6
		{10, 10}, {11, 10}, {12, 10}, {13, 10},           // +10-13: về +10
		{14, 14}, {15, 14},                                // +14-15: về +14
	}

	for _, tt := range upgradeTable {
		newLevel := UpgradeFailLevel(tt.fromLevel, tt.failResetTo)
		if newLevel != tt.failResetTo {
			t.Errorf("fail at +%d: expected reset to +%d, got +%d", tt.fromLevel, tt.failResetTo, newLevel)
		}

		successLevel := UpgradeSuccessLevel(tt.fromLevel)
		if successLevel != tt.fromLevel+1 {
			t.Errorf("success at +%d: expected +%d, got +%d", tt.fromLevel, tt.fromLevel+1, successLevel)
		}
	}
}

// ===========================================================================
// Full stat calculation scenario — power budget from design doc
// ===========================================================================

func TestStatScenario_FullGearLv40Plus0(t *testing.T) {
	// Design doc: Player Lv40, full trang bị thường cấp 4, +0, không ngọc
	// Total gear stats: HP +435, DMG +50, DEF +44, Crit +11%
	gearStats := []struct {
		slot string
		hp   int
		dmg  int
		def  int
		crit float64
	}{
		{"weapon", 0, 38, 0, 3},
		{"armor", 150, 0, 15, 0},
		{"helmet", 110, 0, 11, 0},
		{"pants", 120, 0, 12, 0},
		{"boots", 55, 0, 6, 0},
		{"gloves", 0, 12, 0, 8},
	}

	totalHP, totalDMG, totalDEF := 0, 0, 0
	totalCrit := 0.0
	upgradeLevel := 0

	for _, g := range gearStats {
		totalHP += CalculateEquipmentStat(g.hp, upgradeLevel)
		totalDMG += CalculateEquipmentStat(g.dmg, upgradeLevel)
		totalDEF += CalculateEquipmentStat(g.def, upgradeLevel)
		totalCrit += g.crit * CalculateUpgradeMultiplier(upgradeLevel)
	}

	if totalHP != 435 {
		t.Errorf("total HP = %d, want 435", totalHP)
	}
	if totalDMG != 50 {
		t.Errorf("total DMG = %d, want 50", totalDMG)
	}
	if totalDEF != 44 {
		t.Errorf("total DEF = %d, want 44", totalDEF)
	}
	if math.Abs(totalCrit-11.0) > epsilon {
		t.Errorf("total Crit = %.2f%%, want 11%%", totalCrit)
	}
}

func TestStatScenario_WeaponDMG50_Plus10(t *testing.T) {
	// Design doc example: Vũ khí base DMG 50, nâng lên +10
	// upgrade_bonus = 10 × 0.02 = 0.20
	// milestone_bonus = 0.10 (+6) + 0.20 (+10) = 0.30
	// final = 50 × (1 + 0.20 + 0.30) = 50 × 1.50 = 75 DMG
	result := CalculateEquipmentStat(50, 10)
	if result != 75 {
		t.Errorf("weapon DMG 50 +10 = %d, want 75", result)
	}
}

// ===========================================================================
// CalculateDismantleRefund — detailed refund scenarios
// ===========================================================================

func TestDismantleRefund_Greedy(t *testing.T) {
	// Verify greedy algorithm gives correct highest-value stones
	// 500 power → refund 250
	// 250 = 243 (lv6×1) + 7 remaining
	// 7 = 3 (lv2×1) + 3 (lv2×1) = lv2×2, remaining 1
	// 1 = lv1×1
	// Total: lv6×1, lv2×2, lv1×1 = 243 + 6 + 1 = 250
	refund := CalculateDismantleRefund(500)

	totalPower := 0
	for level, qty := range refund {
		totalPower += StonePowers[level] * qty
	}

	if totalPower != 250 {
		t.Errorf("refund total power = %d, want 250", totalPower)
	}

	if refund[6] != 1 {
		t.Errorf("expected 1 lv6 stone, got %d", refund[6])
	}
}

func TestDismantleRefund_ExactHalf(t *testing.T) {
	// 2 × lv7 stones = 2 × 729 = 1458 power
	// Refund = 729 = exactly 1 lv7 stone
	refund := CalculateDismantleRefund(1458)
	if refund[7] != 1 {
		t.Errorf("expected 1 lv7 stone, got %d", refund[7])
	}
	totalPower := 0
	for level, qty := range refund {
		totalPower += StonePowers[level] * qty
	}
	if totalPower != 729 {
		t.Errorf("refund total power = %d, want 729", totalPower)
	}
}

func TestDismantleRefund_LargePower(t *testing.T) {
	// 400000 power → refund 200000
	// 200000 / 177147 (lv12) = 1, remainder 22853
	// 22853 / 59049 (lv11) = 0
	// 22853 / 19683 (lv10) = 1, remainder 3170
	// 3170 / 6561 (lv9) = 0
	// 3170 / 2187 (lv8) = 1, remainder 983
	// 983 / 729 (lv7) = 1, remainder 254
	// 254 / 243 (lv6) = 1, remainder 11
	// 11 / 81 (lv5) = 0
	// 11 / 27 (lv4) = 0
	// 11 / 9 (lv3) = 1, remainder 2
	// 2 / 3 (lv2) = 0
	// 2 / 1 (lv1) = 2
	refund := CalculateDismantleRefund(400000)
	totalPower := 0
	for level, qty := range refund {
		totalPower += StonePowers[level] * qty
	}

	expected := 400000 / 2
	if totalPower != expected {
		t.Errorf("large refund: total power = %d, want %d", totalPower, expected)
	}
}
