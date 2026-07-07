package match

import "math"

type EloParams struct {
	KFactor     int
	RatingFloor int
	BotModifier float64 // 0-1, multiplier for matches with bots
	HasBot      bool
}

// CalculateEloChange returns the rating change for a team.
// teamRating and opponentRating are averages of team members' ratings.
// actualScore: 1.0 = win, 0.0 = loss, 0.5 = draw
func CalculateEloChange(teamRating, opponentRating int, actualScore float64, params EloParams) int {
	expected := 1.0 / (1.0 + math.Pow(10, float64(opponentRating-teamRating)/400.0))
	change := float64(params.KFactor) * (actualScore - expected)

	if params.HasBot {
		change *= params.BotModifier
	}

	return int(math.Round(change))
}

// TeamAvgRating returns the average rating of a slice of ratings.
func TeamAvgRating(ratings []int) int {
	if len(ratings) == 0 {
		return 1000
	}
	sum := 0
	for _, r := range ratings {
		sum += r
	}
	return sum / len(ratings)
}
