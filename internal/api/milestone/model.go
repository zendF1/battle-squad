// Package milestone serves the one-time progress rewards ("Mốc thưởng"): missions rows with
// type 'milestone', claimed by hand once per account and never reset. Progress is derived at
// read and claim time from the player's characters and equipment plus player_stat_counters
// (matches, wins, upgrade and rank peaks), which the match end and the upgrade tx keep.
package milestone

// MissionType is missions.type of a milestone row.
const MissionType = "milestone"

// Tracks in the order GET /milestones lists them.
const (
	TrackCharacter = "character"
	TrackUpgrade   = "upgrade"
	TrackCombat    = "combat"
	TrackRank      = "rank"
)

var TrackOrder = []string{TrackCharacter, TrackUpgrade, TrackCombat, TrackRank}

// Targets (missions.target). A set target is SetTargetPrefix + the level, e.g. "set_level:6".
const (
	TargetCharLevel   = "char_level"          // highest level among the player's characters
	TargetUpgradePeak = "upgrade_peak"        // highest level any piece ever reached
	TargetWeaponPeak  = "weapon_upgrade_peak" // highest level any weapon ever reached
	SetTargetPrefix   = "set_level:"          // one character wearing all 6 slots at >= level; required = slots
	TargetMatches     = "matches"             // counted matches
	TargetWins        = "wins"                // counted wins
	TargetCharWins    = "char_wins"           // most counted wins with one character
	TargetRankPeak    = "rank_peak"           // highest tier ever reached (TierOrder)
)

// player_stat_counters.counter names. CounterCharWinsPrefix + character id counts that
// character's wins.
const (
	CounterMatches        = "matches"
	CounterWins           = "wins"
	CounterCharWinsPrefix = "char_wins:"
	CounterUpgradePeak    = "upgrade_peak"
	CounterWeaponPeak     = "weapon_upgrade_peak"
	CounterRankPeak       = "rank_peak"
	CounterRatingPeak     = "rating_peak"
)

// SlotCount is the number of equipment slots a full set covers.
const SlotCount = 6

// Reward item kinds the server can grant. A milestone whose reward_items holds another kind
// is not served and cannot be claimed until its kind is added here and in GrantItemTx.
const (
	RewardKindStone = "stone" // {"kind":"stone","level":L,"qty":Q} → player_stones
	RewardKindItem  = "item"  // {"kind":"item","id":"medkit","qty":Q} → inventory_items
	RewardKindTitle = "title" // {"kind":"title","id":"veteran"} → player_titles (no qty; owned once)
)

var supportedRewardKinds = map[string]bool{RewardKindStone: true, RewardKindItem: true, RewardKindTitle: true}

// Node states.
const (
	StateClaimed   = "claimed"
	StateClaimable = "claimable"
	StateLocked    = "locked"
)

// RewardItem is one entry of missions.reward_items. Name and Rarity are not stored: the
// server fills them for a title item from its active config_titles row (Repository.Milestones),
// so clients draw the title plate with no extra lookup; a title item without one is not
// supported.
type RewardItem struct {
	Kind   string `json:"kind"`
	Level  int    `json:"level,omitempty"`  // stone level
	ID     string `json:"id,omitempty"`     // item id or title id
	Qty    int    `json:"qty,omitempty"`    // stone / item quantity; titles have none
	Name   string `json:"name,omitempty"`   // title name (config_titles.name)
	Rarity string `json:"rarity,omitempty"` // title rarity: blue, orange, purple, red
}

type Rewards struct {
	Coin  int          `json:"coin"`
	Gem   int          `json:"gem"`
	Items []RewardItem `json:"items"`
}

// Milestone is a missions row of type milestone with the player's claim flag.
type Milestone struct {
	ID          string
	Track       string
	Target      string
	Required    int
	SortOrder   int
	Description string
	Rewards     Rewards
	Claimed     bool
}

type Node struct {
	ID          string  `json:"id"`
	Track       string  `json:"track"`
	Target      string  `json:"target"`
	Required    int     `json:"required"`
	Progress    int     `json:"progress"`
	State       string  `json:"state"`
	Rewards     Rewards `json:"rewards"`
	Description string  `json:"description"`
}

type Track struct {
	Track string `json:"track"`
	Nodes []Node `json:"nodes"`
}

type ListResponse struct {
	Tracks         []Track `json:"tracks"`
	ClaimableCount int     `json:"claimableCount"`
}

type SummaryResponse struct {
	ClaimableCount int `json:"claimableCount"`
}

type ClaimRequest struct {
	ID string `json:"id"`
}

type StoneBalance struct {
	StoneLevel int `json:"stoneLevel"`
	Quantity   int `json:"quantity"`
}

// ClaimResult answers POST /milestones/claim and /milestones/claim-all: the claimed ids, what
// they granted in total, the new balances and stones, and the claimable count left.
type ClaimResult struct {
	Claimed        []string       `json:"claimed"`
	Rewards        Rewards        `json:"rewards"`
	Coin           int            `json:"coin"`
	Gem            int            `json:"gem"`
	Stones         []StoneBalance `json:"stones"`
	ClaimableCount int            `json:"claimableCount"`
}

// PlayerProgress is what node progress is derived from.
type PlayerProgress struct {
	Counters   map[string]int
	CharLevels map[string]int
	Pieces     []Piece
}

// Piece is one owned equipment piece; EquippedOn is "" when not worn.
type Piece struct {
	Slot       string
	Level      int
	EquippedOn string
}
