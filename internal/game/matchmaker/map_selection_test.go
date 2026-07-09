package matchmaker

import "testing"

func TestTierIndex(t *testing.T) {
	tests := []struct {
		tier string
		want int
	}{
		{"bronze", 0},
		{"silver", 1},
		{"gold", 2},
		{"platinum", 3},
		{"diamond", 4},
		{"master", 5},
		{"unknown", 0},
		{"", 0},
	}
	for _, tt := range tests {
		if got := tierIndex(tt.tier); got != tt.want {
			t.Errorf("tierIndex(%q) = %d, want %d", tt.tier, got, tt.want)
		}
	}
}

func TestRatingToTier(t *testing.T) {
	tests := []struct {
		rating int
		want   string
	}{
		{500, "bronze"},
		{999, "bronze"},
		{1000, "silver"},
		{1199, "silver"},
		{1200, "gold"},
		{1499, "gold"},
		{1500, "platinum"},
		{1800, "diamond"},
		{2200, "master"},
		{3000, "master"},
	}
	for _, tt := range tests {
		if got := ratingToTier(tt.rating); got != tt.want {
			t.Errorf("ratingToTier(%d) = %q, want %q", tt.rating, got, tt.want)
		}
	}
}
