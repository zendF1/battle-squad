package mission

import "time"

type Mission struct {
	MissionID     string `json:"missionId"`
	Type          string `json:"type"` // daily, achievement
	Target        string `json:"target"` // play_match, win_match, damage_dealt, item_used, enemy_killed, terrain_destroyed
	RequiredValue int    `json:"requiredValue"`
	RewardCoin    int    `json:"rewardCoin"`
	RewardGem     int    `json:"rewardGem"`
	RewardItems   string `json:"rewardItems"` // json array string
	IsActive      bool   `json:"isActive"`
}

type MissionProgress struct {
	PlayerID     string    `json:"playerId"`
	MissionID    string    `json:"missionId"`
	CurrentValue int       `json:"currentValue"`
	IsClaimed    bool      `json:"isClaimed"`
	UpdatedAt    time.Time `json:"updatedAt"`
	
	// Config fields mapped on query
	Type          string `json:"type"`
	Target        string `json:"target"`
	RequiredValue int    `json:"requiredValue"`
	RewardCoin    int    `json:"rewardCoin"`
	RewardGem     int    `json:"rewardGem"`
}

type ClaimRewardRequest struct {
	MissionID string `json:"missionId"`
}
