package history

import (
	"testing"
	"time"
)

func TestLabel(t *testing.T) {
	now := time.Date(2026, 7, 9, 15, 0, 0, 0, time.Local)
	cases := []struct {
		when time.Time
		want string
	}{
		{time.Date(2026, 7, 9, 9, 24, 0, 0, time.Local), "today 09:24"},
		{time.Date(2026, 7, 8, 9, 14, 0, 0, time.Local), "yesterday 09:14"},
		{time.Date(2026, 7, 7, 10, 0, 0, 0, time.Local), "2d ago"},
		{time.Date(2026, 7, 3, 10, 0, 0, 0, time.Local), "6d ago"},
		{time.Date(2026, 7, 2, 10, 0, 0, 0, time.Local), "2026-07-02"}, // 7 days → date
		{time.Date(2026, 6, 1, 10, 0, 0, 0, time.Local), "2026-06-01"},
		{time.Time{}, "unknown"},
	}
	for _, c := range cases {
		if got := Label(c.when, now); got != c.want {
			t.Errorf("Label(%v) = %q, want %q", c.when.Format(time.RFC3339), got, c.want)
		}
	}
}
