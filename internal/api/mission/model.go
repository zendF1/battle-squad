// Package mission serves the daily missions ("Nhiệm vụ ngày"): missions rows with type
// 'daily' whose progress counts per day, claimed for activity points ("Năng động"), and the
// activity chests (type 'mission_chest') claimed once per day (track daily) or ISO week
// (track weekly). Periods are UTC+7 days / ISO weeks; every row keeps the period it counts for
// and a row of an older period reads as 0 / unclaimed (lazy reset, see period.go).
// It also still lists the achievements (type 'achievement').
package mission

import (
	"time"

	"battle-squad/internal/api/milestone"
)

// missions.type of a daily mission and of an activity chest.
const (
	TypeDaily = "daily"
	TypeChest = "mission_chest"
)

// Chest tracks (missions.track of a chest, player_mission_activity.track).
const (
	TrackDaily  = "daily"
	TrackWeekly = "weekly"
)

// Targets of the daily missions (missions.target).
const (
	TargetLogin        = "login"         // first GET /missions/daily (or claim) of the day
	TargetPlayMatch    = "play_match"    // counted match (milestone rule)
	TargetWinMatch     = "win_match"     // counted win
	TargetDamageDealt  = "damage_dealt"  // damage dealt in counted matches
	TargetEnemyKilled  = "enemy_killed"  // kills in counted matches
	TargetShotsHit     = "shots_hit"     // hits in counted matches
	TargetSkillUsed    = "skill_used"    // skills fired in counted matches
	TargetItemUsed     = "item_used"     // items spent in counted matches
	TargetUpgrade      = "upgrade"       // equipment upgrade attempt, success or fail
	TargetShopPurchase = "shop_purchase" // successful coin / gem shop purchase
)

// Mission states.
const (
	StateProgress  = "progress"
	StateClaimable = "claimable"
	StateClaimed   = "claimed"
)

// Chest states (StateClaimable / StateClaimed are shared).
const StateLocked = "locked"

// EconomySource is economy_transactions.source of chest coin and gem; ref_id is the chest id.
const EconomySource = "mission"

// ItemSource is inventory_items.source of a chest item.
const ItemSource = "mission_reward"

// Rewards is the milestone reward format: {coin, gem, items:[{kind, id, qty, level}]}.
type (
	Rewards    = milestone.Rewards
	RewardItem = milestone.RewardItem
)

// DailyMission is a missions row of type daily with the player's progress row
// (Value / Claimed / Period: zero values without one).
type DailyMission struct {
	ID        string
	Title     string
	Target    string
	Required  int
	Activity  int
	Goto      string
	SortOrder int
	Value     int
	Claimed   bool
	Period    string
}

// Chest is a missions row of type mission_chest with the player's claim row.
type Chest struct {
	ID        string
	Track     string
	Threshold int
	Rewards   Rewards
	Claimed   bool
	Period    string
}

// ActivityRow is one player_mission_activity row.
type ActivityRow struct {
	Track  string
	Period string
	Points int
}

// MissionView is one mission of GET /missions/daily; Progress is capped at Required.
type MissionView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Target   string `json:"target"`
	Required int    `json:"required"`
	Progress int    `json:"progress"`
	Activity int    `json:"activity"`
	Goto     string `json:"goto"`
	State    string `json:"state"`
}

type ChestView struct {
	ID        string  `json:"id"`
	Track     string  `json:"track"`
	Threshold int     `json:"threshold"`
	State     string  `json:"state"`
	Rewards   Rewards `json:"rewards"`
}

// DailyState answers GET /missions/daily: the current day and ISO week (UTC+7), when they
// end (UTC), today's and this week's activity, the missions in seed order, the daily chests
// and the weekly rewards by threshold.
type DailyState struct {
	Day            string        `json:"day"`
	DayResetAt     time.Time     `json:"dayResetAt"`
	Week           string        `json:"week"`
	WeekResetAt    time.Time     `json:"weekResetAt"`
	Activity       int           `json:"activity"`
	WeeklyActivity int           `json:"weeklyActivity"`
	Missions       []MissionView `json:"missions"`
	Chests         []ChestView   `json:"chests"`
	Weekly         []ChestView   `json:"weekly"`
}

type ClaimMissionRequest struct {
	MissionID string `json:"missionId"`
}

// ClaimMissionResult answers POST /missions/claim: the activity gained and the new state.
type ClaimMissionResult struct {
	Activity int        `json:"activity"`
	State    DailyState `json:"state"`
}

type ClaimChestRequest struct {
	ChestID string `json:"chestId"`
}

// ClaimChestResult answers POST /missions/chests/claim: what the chest granted and the new state.
type ClaimChestResult struct {
	Rewards Rewards    `json:"rewards"`
	State   DailyState `json:"state"`
}

// MatchStats is what one counted match adds to the player's daily missions.
type MatchStats struct {
	Won       bool
	Damage    int
	Kills     int
	ShotsHit  int
	SkillUses int
	ItemsUsed int
}

// MissionProgress is one achievement of GET /missions/achievements.
type MissionProgress struct {
	PlayerID     string    `json:"playerId"`
	MissionID    string    `json:"missionId"`
	CurrentValue int       `json:"currentValue"`
	IsClaimed    bool      `json:"isClaimed"`
	UpdatedAt    time.Time `json:"updatedAt"`

	Type          string `json:"type"`
	Target        string `json:"target"`
	RequiredValue int    `json:"requiredValue"`
	RewardCoin    int    `json:"rewardCoin"`
	RewardGem     int    `json:"rewardGem"`
}
