package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/settings"
)

// modelWithSettings builds a synchronous test model with custom settings.
func modelWithSettings(set settings.Settings) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	return New(demoSteps(), "", rc, set)
}

// TestNarrowLayoutNoOverflow proves the three stacked panes always sum to the
// body height (no vertical overflow) for every realistic terminal height, and
// each pane stays renderable.
func TestNarrowLayoutNoOverflow(t *testing.T) {
	m := modelWithSettings(settings.Defaults())
	for h := 10; h <= 50; h++ {
		m.Update(tea.WindowSizeMsg{Width: 70, Height: h}) // 70 < wide_threshold → narrow
		bodyH := h - headerH - footerH
		sum := m.stepsRect.h + m.outRect.h + m.histRect.h
		if sum != bodyH {
			t.Fatalf("h=%d: pane heights sum %d, want bodyH %d", h, sum, bodyH)
		}
		if bottom := m.histRect.y + m.histRect.h; bottom > h-footerH {
			t.Fatalf("h=%d: history bottom %d exceeds body area %d", h, bottom, h-footerH)
		}
		for _, r := range []rect{m.stepsRect, m.outRect, m.histRect} {
			if r.h < 2 {
				t.Fatalf("h=%d: pane height %d < 2 (unrenderable)", h, r.h)
			}
		}
	}
}

// TestWideLayoutHardFloor proves an absurdly low wide_threshold cannot force a
// side-by-side layout too narrow to fit both columns (no negative widths).
func TestWideLayoutHardFloor(t *testing.T) {
	set := settings.Defaults()
	set.UI.WideThreshold = 20 // below the hard floor
	m := modelWithSettings(set)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 24}) // 40 ≥ threshold but < minWideWidth
	if m.wide {
		t.Error("40 cols must stay narrow despite a low wide_threshold (hard floor)")
	}
	for _, r := range []rect{m.stepsRect, m.outRect, m.histRect} {
		if r.w < 1 {
			t.Errorf("pane width %d < 1", r.w)
		}
	}
}

// TestGlyphUsesThemeColors proves the [theme] success/failure colors are wired
// into the status glyphs (the config options are not inert).
func TestGlyphUsesThemeColors(t *testing.T) {
	forceColor(t)
	m := modelWithSettings(settings.Defaults()) // success 42, failure 203
	if got := m.glyph(engine.StateFailed); !strings.Contains(got, "38;5;203") {
		t.Errorf("failed glyph should use failure color 203: %q", got)
	}
	if got := m.glyph(engine.StateOK); !strings.Contains(got, "38;5;42") {
		t.Errorf("ok glyph should use success color 42: %q", got)
	}
	if got := m.glyph(engine.StatePending); strings.Contains(got, "38;5;") {
		t.Errorf("pending glyph should be uncolored: %q", got)
	}
}

// TestRetryRearmsTimer proves retry clears the frozen elapsed so the header
// ticks live during the retry instead of showing the previous run's value.
func TestRetryRearmsTimer(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.states[0] = engine.StateFailed
	m.out = outSel{kind: outLiveStep, step: 0}
	m.running = false
	m.totalEnd = time.Now() // frozen, as after a finished run
	m.retry()
	if !m.totalEnd.IsZero() {
		t.Error("retry should clear totalEnd to re-arm the header timer")
	}
}
