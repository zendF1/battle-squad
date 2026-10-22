// Package ranked holds the rank rules shared by the game server (matchmaking,
// match rewards, lobbies) and the API (/rank): tiers and divisions, the Elo
// config with the default rating, and the active season.
package ranked

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is what both a pool and a transaction offer.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Tiers in rank order, lowest first.
var Tiers = []string{"bronze", "silver", "gold", "platinum", "diamond", "master"}

// TierIndex is the tier's position in Tiers; unknown tiers count as bronze (0).
func TierIndex(tier string) int {
	for i, t := range Tiers {
		if t == tier {
			return i
		}
	}
	return 0
}

// CompareTiers is > 0 when a ranks above b, 0 when equal, < 0 below.
func CompareTiers(a, b string) int {
	return TierIndex(a) - TierIndex(b)
}

// Band is a tier's rating range: from MinRating up to the next band's MinRating (the last
// band has no top). Its Divisions split it into DivisionSize rating points each, from the
// lowest (division Divisions) up to division 1, which keeps everything above.
type Band struct {
	Tier         string
	MinRating    int
	Divisions    int
	DivisionSize int // 0 for a tier with a single division
}

// Bands are the tiers' rating ranges, lowest first (one per Tiers entry): bronze < 1000 in
// divisions of 333, silver < 1200 / 66, gold < 1500 / 100, platinum < 1800 / 100, diamond <
// 2200 / 133, then master (a single division). GET /gamedata sends them to the client.
var Bands = []Band{
	{Tier: "bronze", MinRating: 0, Divisions: 3, DivisionSize: 333},
	{Tier: "silver", MinRating: 1000, Divisions: 3, DivisionSize: 66},
	{Tier: "gold", MinRating: 1200, Divisions: 3, DivisionSize: 100},
	{Tier: "platinum", MinRating: 1500, Divisions: 3, DivisionSize: 100},
	{Tier: "diamond", MinRating: 1800, Divisions: 3, DivisionSize: 133},
	{Tier: "master", MinRating: 2200, Divisions: 1},
}

// Tier maps a rating to its tier: bronze < 1000, silver < 1200, gold < 1500,
// platinum < 1800, diamond < 2200, master.
func Tier(rating int) string {
	t, _ := TierAndDivision(rating)
	return t
}

// TierAndDivision maps a rating to its tier and division (3 lowest .. 1 highest;
// master is always 1): the last band whose MinRating the rating reaches (bronze below 0 too),
// division Divisions − (rating − MinRating) / DivisionSize (integer division) clamped to
// 1..Divisions.
func TierAndDivision(rating int) (string, int) {
	b := Bands[0]
	for _, band := range Bands[1:] {
		if rating >= band.MinRating {
			b = band
		}
	}
	if b.Divisions <= 1 || b.DivisionSize <= 0 {
		return b.Tier, 1
	}
	d := b.Divisions - (rating-b.MinRating)/b.DivisionSize
	return b.Tier, min(max(d, 1), b.Divisions)
}

// EloConfig is the game_settings "elo" row.
type EloConfig struct {
	KFactor       int `json:"kFactor"`
	RatingFloor   int `json:"ratingFloor"`
	DefaultRating int `json:"defaultRating"`
}

// DefaultEloConfig matches the seed in migrations/005_add_loadouts_and_matchmaking.up.sql.
func DefaultEloConfig() EloConfig {
	return EloConfig{KFactor: 32, RatingFloor: 0, DefaultRating: 1000}
}

// LoadEloConfig reads the "elo" row; a missing or broken row gives the defaults
// (with the error, for logging). Missing fields keep their defaults.
func LoadEloConfig(ctx context.Context, q Querier) (EloConfig, error) {
	cfg := DefaultEloConfig()
	var raw string
	if err := q.QueryRow(ctx, `SELECT value FROM game_settings WHERE key = 'elo'`).Scan(&raw); err != nil {
		return cfg, fmt.Errorf("game_settings elo: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return DefaultEloConfig(), fmt.Errorf("game_settings elo: %w", err)
	}
	return cfg, nil
}

// Season is the active rank season.
type Season struct {
	SeasonID string    `json:"seasonId"`
	Name     string    `json:"name"`
	StartsAt time.Time `json:"-"`
	EndsAt   time.Time `json:"endsAt"`
	Status   string    `json:"-"`
}

// SeasonLength is how long an auto-created season runs.
const SeasonLength = 90 * 24 * time.Hour

// EnsureActiveSeason returns the active season, creating one when none is
// active: "season_<n>" named "Mùa <n>" (n = seasons so far + 1), running
// SeasonLength from now. Racing creators compute the same id, so only one row
// is inserted.
func EnsureActiveSeason(ctx context.Context, q Querier) (Season, error) {
	s, err := activeSeason(ctx, q)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Season{}, err
	}
	if _, err := q.Exec(ctx, `
		WITH n AS (SELECT COUNT(*) + 1 AS n FROM rank_seasons)
		INSERT INTO rank_seasons (season_id, name, starts_at, ends_at, status)
		SELECT 'season_' || n.n, 'Mùa ' || n.n, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + make_interval(secs => $1), 'active'
		FROM n
		WHERE NOT EXISTS (SELECT 1 FROM rank_seasons WHERE status = 'active')
		ON CONFLICT (season_id) DO NOTHING`, SeasonLength.Seconds()); err != nil {
		return Season{}, fmt.Errorf("create season: %w", err)
	}
	s, err = activeSeason(ctx, q)
	if err != nil {
		return Season{}, fmt.Errorf("active season: %w", err)
	}
	return s, nil
}

func activeSeason(ctx context.Context, q Querier) (Season, error) {
	var s Season
	err := q.QueryRow(ctx, `
		SELECT season_id, name, starts_at, ends_at, status FROM rank_seasons
		WHERE status = 'active' ORDER BY starts_at DESC, season_id LIMIT 1`).
		Scan(&s.SeasonID, &s.Name, &s.StartsAt, &s.EndsAt, &s.Status)
	return s, err
}
