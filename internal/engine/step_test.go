package engine

import (
	"testing"
	"time"
)

func TestHms(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{3*time.Minute + 4*time.Second, "3m4s"},
		{60 * time.Second, "1m0s"},
		{time.Hour + 2*time.Minute, "1h2m"},
		{3600 * time.Second, "1h0m"},
	}
	for _, c := range cases {
		if got := Hms(c.d); got != c.want {
			t.Errorf("Hms(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestGlyph(t *testing.T) {
	cases := map[State]string{
		StateOK:      "✓",
		StateFailed:  "✗",
		StateRunning: "▶",
		StateSkipped: "⊘",
		StateAborted: "⊗",
		StatePending: "·",
	}
	for st, want := range cases {
		if got := Glyph(st); got != want {
			t.Errorf("Glyph(%v) = %q, want %q", st, got, want)
		}
	}
}
