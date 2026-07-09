package tui

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/vt"

	"github.com/schmas/upall/internal/engine"
)

// captureSink accumulates a step's raw pty output so a test can replay it into an
// emulator, exercising the true capture path: pty → Sink.Output → emulator.
type captureSink struct {
	mu  sync.Mutex
	raw []byte
}

func (s *captureSink) Skip(int, string)            {}
func (s *captureSink) StepStart(int)               {}
func (s *captureSink) StepDone(int, engine.Result) {}
func (s *captureSink) Output(_ int, p []byte) {
	s.mu.Lock()
	s.raw = append(s.raw, p...)
	s.mu.Unlock()
}

// TestPTYOutputRendersFaithfully runs a real command through the engine's pty and
// feeds its captured bytes to an emulator. It proves the end-to-end contract the
// synthetic tests assume: the pty's ONLCR turns the command's bare \n into \r\n,
// so multi-line output lands at column 0 (no drift), \r progress collapses, and
// SGR color survives — exactly what the TUI renders.
func TestPTYOutputRendersFaithfully(t *testing.T) {
	sink := &captureSink{}
	// The command prints bare \n; the pty adds the \r. It also does a \r progress
	// redraw and emits an SGR color run.
	steps := []engine.Step{{Key: "e2e", Run: []string{
		`printf 'alpha\nbeta\n1%%\r99%%\n\033[31mRED\033[0m\n'`,
	}}}
	engine.NewRunner("", sink).RunAll(context.Background(), steps)

	e := vt.NewEmulator(40, 10)
	e.SetScrollbackSize(scrollbackCap)
	sink.mu.Lock()
	e.Write(sink.raw)
	sink.mu.Unlock()
	got := renderTerm(e)

	// No column drift: "beta" starts at column 0 on its own line.
	if !strings.Contains(got, "\nbeta") && !strings.HasPrefix(got, "beta") {
		t.Fatalf("expected beta at column 0 on its own line (ONLCR), got %q", got)
	}
	if strings.Contains(got, "   beta") {
		t.Fatalf("column drift: bare \\n was not translated to \\r\\n by the pty: %q", got)
	}
	// \r progress collapsed to the final frame.
	if !strings.Contains(got, "99%") || strings.Contains(got, "1%99%") {
		t.Fatalf("progress should collapse to 99%%, got %q", got)
	}
	// SGR color survived the whole chain.
	if !strings.Contains(got, "RED") || !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("SGR color should survive pty→sink→emulator, got %q", got)
	}
}
