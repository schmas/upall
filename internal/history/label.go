package history

import (
	"fmt"
	"time"
)

// Label renders a run's start time as a human label relative to now: "today
// 15:24", "yesterday 09:14", "2d ago" (up to a week), else the date. A zero
// time (unparseable run dir) reads as "unknown". Times are local.
func Label(when, now time.Time) string {
	if when.IsZero() {
		return "unknown"
	}
	days := daysApart(when, now)
	switch {
	case days <= 0:
		return "today " + when.Format("15:04")
	case days == 1:
		return "yesterday " + when.Format("15:04")
	case days <= 6:
		return fmt.Sprintf("%dd ago", days)
	default:
		return when.Format("2006-01-02")
	}
}

// daysApart is the number of calendar days between when and now, measured from
// local midnight so "yesterday" is a date difference, not a 24h difference.
func daysApart(when, now time.Time) int {
	dw := startOfDay(when)
	dn := startOfDay(now)
	return int(dn.Sub(dw).Hours()/24 + 0.5)
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
