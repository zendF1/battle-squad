package matchmaker

import (
	"context"
	"encoding/json"
	"fmt"

	"battle-squad/internal/shared/database"
	"battle-squad/internal/shared/observability"
	"battle-squad/internal/shared/ranked"
)

// MatchmakingConfig holds the queue-expansion and bot-fill parameters loaded
// from the game_settings table (key = "matchmaking").
type MatchmakingConfig struct {
	TickInterval    int `json:"tickInterval"`
	BaseRatingRange int `json:"baseRatingRange"`
	ExpandInterval  int `json:"expandInterval"`
	ExpandStep      int `json:"expandStep"`
	MaxRatingRange  int `json:"maxRatingRange"`
	MaxWaitTime     int `json:"maxWaitTime"` // unused by matching since botFillAfter; kept for the admin form
	// BotFillAfter: seconds an entry waits before its missing slots are filled with bots
	BotFillAfter int `json:"botFillAfter"`
	// MatchStartDelay: seconds between MatchFound and MatchStarted (the client's reveal)
	MatchStartDelay     int     `json:"matchStartDelay"`
	BotRatingModifier   float64 `json:"botRatingModifier"`
	PartyRatingStrategy string  `json:"partyRatingStrategy"`
	WeightedRatio       float64 `json:"weightedRatio"`
}

// EloConfig holds the Elo rating algorithm parameters loaded from the
// game_settings table (key = "elo").
type EloConfig = ranked.EloConfig

// BotTierConfig holds the bot behaviour for a single rank tier.
type BotTierConfig struct {
	HitChance     float64 `json:"hitChance"` // % of shots aimed to hit (0–100)
	UseItemChance float64 `json:"useItemChance"`
	MovementSmart float64 `json:"movementSmart"`
}

// BotDifficultyConfig is the top-level struct loaded from the game_settings
// table (key = "bot_difficulty"). Tiers maps rank-tier names (e.g. "bronze",
// "silver") to their respective BotTierConfig.
type BotDifficultyConfig struct {
	Tiers map[string]BotTierConfig `json:"tiers"`
}

// QueueEntry represents a single matchmaking queue entry. A solo player has
// one element in PlayerIDs; a party has two. Rating is the party rating
// (partyRatingStrategy), PlayerRatings the members' own ratings.
type QueueEntry struct {
	EntryID       string              `json:"entryId"`
	LobbyID       string              `json:"lobbyId"`
	PlayerIDs     []string            `json:"playerIds"`
	Rating        int                 `json:"rating"`
	PlayerRatings map[string]int      `json:"playerRatings"`
	PlayerChars   map[string]string   `json:"playerChars"`
	PlayerItems   map[string][]string `json:"playerItems"`
	PlayerNames   map[string]string   `json:"playerNames"`
	TeamSize      int                 `json:"teamSize"`
	QueuedAt      int64               `json:"queuedAt"`
	NodeID        string              `json:"nodeId"`
}

// ---------------------------------------------------------------------------
// Default constructors
// ---------------------------------------------------------------------------

// DefaultMatchmakingConfig returns the canonical defaults that match the seed
// values in migrations/005_add_loadouts_and_matchmaking.up.sql.
func DefaultMatchmakingConfig() MatchmakingConfig {
	return MatchmakingConfig{
		TickInterval:        3,
		BaseRatingRange:     100,
		ExpandInterval:      10,
		ExpandStep:          50,
		MaxRatingRange:      300,
		MaxWaitTime:         60,
		BotFillAfter:        20,
		MatchStartDelay:     3,
		BotRatingModifier:   0.5,
		PartyRatingStrategy: "max",
		WeightedRatio:       0.7,
	}
}

// DefaultEloConfig returns the canonical Elo defaults that match the seed
// values in migrations/005_add_loadouts_and_matchmaking.up.sql.
func DefaultEloConfig() EloConfig {
	return ranked.DefaultEloConfig()
}

// DefaultBotDifficultyConfig returns the canonical bot difficulty defaults
// that match the seed values in
// migrations/005_add_loadouts_and_matchmaking.up.sql and
// migrations/012_bot_hit_chance.up.sql.
func DefaultBotDifficultyConfig() BotDifficultyConfig {
	return BotDifficultyConfig{
		Tiers: map[string]BotTierConfig{
			"bronze": {
				HitChance:     60,
				UseItemChance: 0.3,
				MovementSmart: 0.3,
			},
			"silver": {
				HitChance:     70,
				UseItemChance: 0.4,
				MovementSmart: 0.4,
			},
			"gold": {
				HitChance:     80,
				UseItemChance: 0.55,
				MovementSmart: 0.55,
			},
			"platinum": {
				HitChance:     90,
				UseItemChance: 0.7,
				MovementSmart: 0.7,
			},
			"diamond": {
				HitChance:     95,
				UseItemChance: 0.85,
				MovementSmart: 0.85,
			},
			"master": {
				HitChance:     99,
				UseItemChance: 0.9,
				MovementSmart: 0.95,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// DB loading helpers
// ---------------------------------------------------------------------------

// LoadConfigFromDB reads a single row from game_settings by key and
// JSON-unmarshals the value column into dest.
func LoadConfigFromDB(ctx context.Context, db *database.PostgresDB, key string, dest interface{}) error {
	var raw string
	err := db.Pool.QueryRow(ctx,
		`SELECT value FROM game_settings WHERE key = $1`, key,
	).Scan(&raw)
	if err != nil {
		return fmt.Errorf("game_settings: query key %q: %w", key, err)
	}
	if err := json.Unmarshal([]byte(raw), dest); err != nil {
		return fmt.Errorf("game_settings: unmarshal key %q: %w", key, err)
	}
	return nil
}

// LoadMatchmakingConfig loads MatchmakingConfig from the DB. On any error it
// logs a warning and returns the hard-coded defaults.
func LoadMatchmakingConfig(ctx context.Context, db *database.PostgresDB) MatchmakingConfig {
	cfg := DefaultMatchmakingConfig()
	if err := LoadConfigFromDB(ctx, db, "matchmaking", &cfg); err != nil {
		observability.Log.Warn().
			Err(err).
			Msg("matchmaker: failed to load matchmaking config from DB, using defaults")
	}
	return cfg
}

// LoadEloConfig loads EloConfig from the DB. On any error it logs a warning
// and returns the hard-coded defaults.
func LoadEloConfig(ctx context.Context, db *database.PostgresDB) EloConfig {
	cfg, err := ranked.LoadEloConfig(ctx, db.Pool)
	if err != nil {
		observability.Log.Warn().
			Err(err).
			Msg("matchmaker: failed to load elo config from DB, using defaults")
	}
	return cfg
}

// LoadBotDifficultyConfig loads BotDifficultyConfig from the DB. On any error
// it logs a warning and returns the hard-coded defaults.
func LoadBotDifficultyConfig(ctx context.Context, db *database.PostgresDB) BotDifficultyConfig {
	cfg := DefaultBotDifficultyConfig()
	if err := LoadConfigFromDB(ctx, db, "bot_difficulty", &cfg); err != nil {
		observability.Log.Warn().
			Err(err).
			Msg("matchmaker: failed to load bot_difficulty config from DB, using defaults")
	}
	return cfg
}
