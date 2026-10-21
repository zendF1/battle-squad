package rank

import "time"

type Season struct {
	SeasonID string    `json:"seasonId"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"startsAt"`
	EndsAt   time.Time `json:"endsAt"`
	Status   string    `json:"status"` // upcoming, active, ended, reward_granting, closed
}

type PlayerRank struct {
	PlayerID    string    `json:"playerId"`
	DisplayName string    `json:"displayName"` // populated on query joining profiles
	SeasonID    string    `json:"seasonId"`
	Rating      int       `json:"rating"`
	Tier        string    `json:"tier"` // bronze, silver, gold, platinum, diamond, master
	Division    int       `json:"division"`
	Wins        int       `json:"wins"`
	Losses      int       `json:"losses"`
	Draws       int       `json:"draws"`
	WinStreak   int       `json:"winStreak"`
	HighestTier string    `json:"highestTier"`
	UpdatedAt   time.Time `json:"updatedAt"`
	RankPos     int       `json:"rankPos"` // 1-based leaderboard position in the season, 0 = no row
	// Season is set by GET /rank/me only
	Season *SeasonRef `json:"season,omitempty"`
}

// SeasonRef is the season a /rank/me answer belongs to.
type SeasonRef struct {
	SeasonID string    `json:"seasonId"`
	Name     string    `json:"name"`
	EndsAt   time.Time `json:"endsAt"`
}

type LeaderboardResponse struct {
	SeasonID string       `json:"seasonId"`
	Leader   []PlayerRank `json:"leaderboard"`
	Page     int          `json:"page"`
	Limit    int          `json:"limit"`
}

type ClaimSeasonRewardRequest struct {
	SeasonID string `json:"seasonId"`
}
