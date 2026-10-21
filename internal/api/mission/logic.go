package mission

import "sort"

// missionProgress is the mission's progress in the current day: 0 for a row of an older day.
func missionProgress(m DailyMission, p Periods) (value int, claimed bool) {
	if m.Period != p.Day {
		return 0, false
	}
	return m.Value, m.Claimed
}

// MissionState is progress (not done), claimable (done, not claimed) or claimed.
func MissionState(m DailyMission, p Periods) string {
	value, claimed := missionProgress(m, p)
	switch {
	case claimed:
		return StateClaimed
	case value >= m.Required:
		return StateClaimable
	}
	return StateProgress
}

// Activity returns today's and this week's activity points; rows of older periods count 0.
func Activity(rows []ActivityRow, p Periods) (daily, weekly int) {
	for _, r := range rows {
		if r.Period == "" || r.Period != p.Key(r.Track) {
			continue
		}
		if r.Track == TrackDaily {
			daily = r.Points
		} else {
			weekly = r.Points
		}
	}
	return daily, weekly
}

// ChestState is claimed (claimed in the chest's current period), claimable (its track's
// activity reached the threshold) or locked.
func ChestState(c Chest, p Periods, daily, weekly int) string {
	key := p.Key(c.Track)
	if c.Claimed && c.Period == key {
		return StateClaimed
	}
	activity := daily
	if c.Track == TrackWeekly {
		activity = weekly
	}
	if activity >= c.Threshold {
		return StateClaimable
	}
	return StateLocked
}

// BuildState builds GET /missions/daily: missions by sort order then id, chests of each track
// by threshold then id; a chest of an unknown track is left out.
func BuildState(p Periods, missions []DailyMission, chests []Chest, activity []ActivityRow) DailyState {
	daily, weekly := Activity(activity, p)
	st := DailyState{
		Day: p.Day, DayResetAt: p.DayResetAt, Week: p.Week, WeekResetAt: p.WeekResetAt,
		Activity: daily, WeeklyActivity: weekly,
		Missions: []MissionView{}, Chests: []ChestView{}, Weekly: []ChestView{},
	}

	ms := append([]DailyMission(nil), missions...)
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].SortOrder != ms[j].SortOrder {
			return ms[i].SortOrder < ms[j].SortOrder
		}
		return ms[i].ID < ms[j].ID
	})
	for _, m := range ms {
		value, _ := missionProgress(m, p)
		st.Missions = append(st.Missions, MissionView{
			ID: m.ID, Title: m.Title, Target: m.Target, Required: m.Required,
			Progress: min(max(value, 0), m.Required), Activity: m.Activity, Goto: m.Goto,
			State: MissionState(m, p),
		})
	}

	cs := append([]Chest(nil), chests...)
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Threshold != cs[j].Threshold {
			return cs[i].Threshold < cs[j].Threshold
		}
		return cs[i].ID < cs[j].ID
	})
	for _, c := range cs {
		v := ChestView{ID: c.ID, Track: c.Track, Threshold: c.Threshold, State: ChestState(c, p, daily, weekly),
			Rewards: normalized(c.Rewards)}
		switch c.Track {
		case TrackDaily:
			st.Chests = append(st.Chests, v)
		case TrackWeekly:
			st.Weekly = append(st.Weekly, v)
		}
	}
	return st
}

// normalized gives rewards an empty items list instead of null.
func normalized(r Rewards) Rewards {
	if r.Items == nil {
		r.Items = []RewardItem{}
	}
	return r
}

// matchDeltas is what a counted match adds per target (zero targets left out).
func matchDeltas(s MatchStats) map[string]int {
	d := map[string]int{TargetPlayMatch: 1}
	add := func(target string, v int) {
		if v > 0 {
			d[target] = v
		}
	}
	if s.Won {
		d[TargetWinMatch] = 1
	}
	add(TargetDamageDealt, s.Damage)
	add(TargetEnemyKilled, s.Kills)
	add(TargetShotsHit, s.ShotsHit)
	add(TargetSkillUsed, s.SkillUses)
	add(TargetItemUsed, s.ItemsUsed)
	return d
}
