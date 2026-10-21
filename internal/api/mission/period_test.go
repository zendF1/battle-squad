package mission

import (
	"testing"
	"time"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestPeriodsAt(t *testing.T) {
	cases := []struct {
		now, day, dayReset, week, weekReset string
	}{
		// Wednesday 23:59:59 in Vietnam, then Thursday 00:00 (17:00 UTC)
		{"2026-10-21T16:59:59Z", "2026-10-21", "2026-10-21T17:00:00Z", "2026-W43", "2026-10-25T17:00:00Z"},
		{"2026-10-21T17:00:00Z", "2026-10-22", "2026-10-22T17:00:00Z", "2026-W43", "2026-10-25T17:00:00Z"},
		// 00:30 UTC is already 07:30 in Vietnam: the same day as the UTC date
		{"2026-10-21T00:30:00Z", "2026-10-21", "2026-10-21T17:00:00Z", "2026-W43", "2026-10-25T17:00:00Z"},
		// Sunday 23:59:59 in Vietnam is still week 43; Monday 00:00 (Sunday 17:00 UTC) is week 44
		{"2026-10-25T16:59:59Z", "2026-10-25", "2026-10-25T17:00:00Z", "2026-W43", "2026-10-25T17:00:00Z"},
		{"2026-10-25T17:00:00Z", "2026-10-26", "2026-10-26T17:00:00Z", "2026-W44", "2026-11-01T17:00:00Z"},
		// ISO week across a year boundary: 2026 has 53 weeks, 2027-W01 starts Monday 4 January
		{"2026-12-31T10:00:00Z", "2026-12-31", "2026-12-31T17:00:00Z", "2026-W53", "2027-01-03T17:00:00Z"},
		{"2027-01-03T16:59:59Z", "2027-01-03", "2027-01-03T17:00:00Z", "2026-W53", "2027-01-03T17:00:00Z"},
		{"2027-01-03T17:00:00Z", "2027-01-04", "2027-01-04T17:00:00Z", "2027-W01", "2027-01-10T17:00:00Z"},
		// Monday 30 December 2024 in Vietnam is ISO 2025-W01 (in UTC it is still Sunday, 2024-W52)
		{"2024-12-29T17:00:00Z", "2024-12-30", "2024-12-30T17:00:00Z", "2025-W01", "2025-01-05T17:00:00Z"},
		{"2024-12-29T16:59:59Z", "2024-12-29", "2024-12-29T17:00:00Z", "2024-W52", "2024-12-29T17:00:00Z"},
	}
	for _, c := range cases {
		p := PeriodsAt(utc(c.now))
		if p.Day != c.day || !p.DayResetAt.Equal(utc(c.dayReset)) || p.Week != c.week || !p.WeekResetAt.Equal(utc(c.weekReset)) {
			t.Errorf("%s: %s %s %s %s, want %s %s %s %s", c.now, p.Day, p.DayResetAt.Format(time.RFC3339), p.Week,
				p.WeekResetAt.Format(time.RFC3339), c.day, c.dayReset, c.week, c.weekReset)
		}
		if p.DayResetAt.Location() != time.UTC || p.WeekResetAt.Location() != time.UTC {
			t.Errorf("%s: reset times not in UTC", c.now)
		}
	}

	// The location of now does not matter
	ny := time.FixedZone("UTC-5", -5*60*60)
	if a, b := PeriodsAt(utc("2026-10-25T17:00:00Z").In(ny)), PeriodsAt(utc("2026-10-25T17:00:00Z")); a != b {
		t.Errorf("zone changed the periods: %+v %+v", a, b)
	}
	if p := PeriodsAt(utc("2026-10-21T10:00:00Z")); p.Key(TrackDaily) != p.Day || p.Key(TrackWeekly) != p.Week || p.Key("monthly") != "" {
		t.Errorf("keys %+v", p)
	}
}
