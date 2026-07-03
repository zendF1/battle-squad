package matchhistory

import "time"

type MatchHistoryEntry struct {
	MatchID      string    `json:"matchId"`
	Mode         string    `json:"mode"`
	MapID        string    `json:"mapId"`
	Result       string    `json:"result"` // win, loss, draw
	ExpGained    int       `json:"expGained"`
	CoinGained   int       `json:"coinGained"`
	RatingChange int       `json:"ratingChange"`
	PlayedAt     time.Time `json:"playedAt"`
}
