package mission

import (
	"fmt"
	"time"
)

// periodZone is Vietnam time, a fixed UTC+7 (no tz database): the day starts at 00:00 UTC+7
// (17:00 UTC), the week on Monday 00:00 UTC+7.
var periodZone = time.FixedZone("UTC+7", 7*60*60)

// Periods are the keys of the current day and ISO week and when they end.
type Periods struct {
	Day         string    // 2026-10-21
	DayResetAt  time.Time // next 00:00 UTC+7, in UTC
	Week        string    // 2026-W43 (ISO year and week)
	WeekResetAt time.Time // next Monday 00:00 UTC+7, in UTC
}

// PeriodsAt returns the periods now falls in.
func PeriodsAt(now time.Time) Periods {
	t := now.In(periodZone)
	y, m, d := t.Date()
	dayStart := time.Date(y, m, d, 0, 0, 0, 0, periodZone)
	sinceMonday := (int(t.Weekday()) + 6) % 7
	weekStart := dayStart.AddDate(0, 0, -sinceMonday)
	isoYear, isoWeek := t.ISOWeek()
	return Periods{
		Day:         dayStart.Format("2006-01-02"),
		DayResetAt:  dayStart.AddDate(0, 0, 1).UTC(),
		Week:        fmt.Sprintf("%04d-W%02d", isoYear, isoWeek),
		WeekResetAt: weekStart.AddDate(0, 0, 7).UTC(),
	}
}

// Key is the period key of track: the day for daily, the ISO week for weekly ("" otherwise).
func (p Periods) Key(track string) string {
	switch track {
	case TrackDaily:
		return p.Day
	case TrackWeekly:
		return p.Week
	}
	return ""
}
