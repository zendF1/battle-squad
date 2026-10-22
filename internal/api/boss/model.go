// Package boss serves GET /boss/runs: the boss runs a player has left today with the big
// reward table, for the client's Boss tab cards (the bosses themselves are in GET /gamedata
// bosses).
package boss

import "time"

// RunsState answers GET /boss/runs: today (a UTC+7 day, as the daily missions count days),
// when it ends (RFC3339 UTC) and one entry per enabled boss, in display order.
type RunsState struct {
	Day     string     `json:"day"`
	ResetAt time.Time  `json:"resetAt"`
	Bosses  []BossRuns `json:"bosses"`
}

// BossRuns is a boss's runs of the day: DailyBonusRuns (config_bosses.daily_bonus_runs), the
// runs finished today (RunsToday) and the bonus runs left (BonusRunsLeft = max(daily −
// runs, 0)); later runs use the small reward table.
type BossRuns struct {
	BossID         string `json:"bossId"`
	DailyBonusRuns int    `json:"dailyBonusRuns"`
	RunsToday      int    `json:"runsToday"`
	BonusRunsLeft  int    `json:"bonusRunsLeft"`
}
