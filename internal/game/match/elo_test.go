package match

import "testing"

func TestCalculateEloChange(t *testing.T) {
	params := EloParams{KFactor: 32, RatingFloor: 0, BotModifier: 1.0, HasBot: false}

	// Equal teams, winner gets +16
	change := CalculateEloChange(1200, 1200, 1.0, params)
	if change != 16 {
		t.Errorf("equal teams win: expected 16, got %d", change)
	}

	// Equal teams, loser gets -16
	change = CalculateEloChange(1200, 1200, 0.0, params)
	if change != -16 {
		t.Errorf("equal teams loss: expected -16, got %d", change)
	}

	// Underdog wins (team 1250 beats team 1450)
	change = CalculateEloChange(1250, 1450, 1.0, params)
	if change < 20 {
		t.Errorf("underdog win: expected >20, got %d", change)
	}

	// Favorite wins (team 1450 beats team 1250)
	change = CalculateEloChange(1450, 1250, 1.0, params)
	if change > 12 {
		t.Errorf("favorite win: expected <12, got %d", change)
	}

	// Bot modifier halves the change
	botParams := EloParams{KFactor: 32, RatingFloor: 0, BotModifier: 0.5, HasBot: true}
	change = CalculateEloChange(1200, 1200, 1.0, botParams)
	if change != 8 {
		t.Errorf("bot modifier: expected 8, got %d", change)
	}

	// Draw
	change = CalculateEloChange(1200, 1200, 0.5, params)
	if change != 0 {
		t.Errorf("draw equal teams: expected 0, got %d", change)
	}
}

func TestTeamAvgRating(t *testing.T) {
	avg := TeamAvgRating([]int{1200, 1400})
	if avg != 1300 {
		t.Errorf("expected 1300, got %d", avg)
	}

	avg = TeamAvgRating([]int{})
	if avg != 1000 {
		t.Errorf("empty: expected 1000, got %d", avg)
	}
}
