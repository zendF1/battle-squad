package equipment

import (
	"math"
	"testing"
)

const floatEpsilon = 1e-9

func TestCalculateUpgradeMultiplier(t *testing.T) {
	tests := []struct {
		level    int
		expected float64
	}{
		{0, 1.0},
		{1, 1.02},
		{5, 1.10},
		{6, 1.22},   // 6*0.02=0.12 + milestone 0.10 = 0.22, total 1.22
		{10, 1.50},  // 10*0.02=0.20 + 0.10 + 0.20 = 0.50, total 1.50
		{14, 1.98},  // 14*0.02=0.28 + 0.10 + 0.20 + 0.40 = 0.98, total 1.98
		{16, 3.02},  // 16*0.02=0.32 + 0.10 + 0.20 + 0.40 + 1.00 = 2.02, total 3.02
	}

	for _, tt := range tests {
		got := CalculateUpgradeMultiplier(tt.level)
		if math.Abs(got-tt.expected) > floatEpsilon {
			t.Errorf("CalculateUpgradeMultiplier(%d) = %f, want %f", tt.level, got, tt.expected)
		}
	}
}

func TestGetSafezoneStart(t *testing.T) {
	tests := []struct {
		level    int
		expected int
	}{
		{1, 0}, {5, 0}, {6, 6}, {8, 6}, {10, 10}, {13, 10}, {14, 14}, {16, 14},
	}

	for _, tt := range tests {
		got := getSafezoneStart(tt.level)
		if got != tt.expected {
			t.Errorf("getSafezoneStart(%d) = %d, want %d", tt.level, got, tt.expected)
		}
	}
}
