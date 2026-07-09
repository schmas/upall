package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

// testModel builds a model with a runControl whose launch/cancel are recorded
// rather than spawning real goroutines, so Update can be driven synchronously.
func testModel(steps []engine.Step) (*Model, *int, *bool) {
	launched := 0
	canceled := false
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runControl{
		ctx:    ctx,
		cancel: func() { canceled = true; cancel() },
		runner: engine.NewRunner("", nil),
		steps:  steps,
		launch: func(func()) { launched++ },
	}
	return New(steps, "", rc), &launched, &canceled
}

func demoSteps() []engine.Step {
	return []engine.Step{{Key: "a", Label: "Alpha"}, {Key: "b", Label: "Beta"}}
}

func sizeUp(m *Model) {
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
}

func TestResizeMakesReady(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	if !m.ready {
		t.Fatal("model not ready after WindowSizeMsg")
	}
	if m.vp.Width < 1 || m.vp.Height < 1 {
		t.Fatalf("viewport unsized: %dx%d", m.vp.Width, m.vp.Height)
	}
	if !m.wide {
		t.Error("120 cols should be wide layout")
	}
}

func TestStepLifecycle(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	m.Update(startMsg{0})
	if m.states[0] != engine.StateRunning || m.activeIdx != 0 {
		t.Fatalf("after start: state=%v active=%d", m.states[0], m.activeIdx)
	}
	m.Update(linesMsg{0: {[]byte("hello"), []byte("world")}})
	if m.rings[0].size != 2 {
		t.Fatalf("ring size = %d, want 2", m.rings[0].size)
	}
	m.Update(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
	if m.states[0] != engine.StateOK || m.activeIdx != -1 {
		t.Fatalf("after done: state=%v active=%d", m.states[0], m.activeIdx)
	}
	if strings.TrimSpace(m.View()) == "" {
		t.Error("View should render after lifecycle")
	}
}

// TestRetryGuard is the run-state machine: retry fires only when no run is
// active AND the selected step failed.
func TestRetryGuard(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.states[0] = engine.StateFailed
	m.sel = 0

	// Blocked while a run is active.
	m.running = true
	if cmd := m.retry(); cmd != nil {
		t.Error("retry should be blocked while running")
	}
	if *launched != 0 {
		t.Error("no launch expected while running")
	}

	// Allowed when idle and the step failed.
	m.running = false
	cmd := m.retry()
	if cmd == nil {
		t.Fatal("retry should be allowed for a failed step when idle")
	}
	if !m.running {
		t.Error("retry should set running")
	}
	if m.states[0] != engine.StatePending {
		t.Error("retry should reset the step to pending")
	}
	cmd() // invoke the launch command
	if *launched != 1 {
		t.Errorf("launch count = %d, want 1", *launched)
	}

	// Blocked for a non-failed step.
	m.running = false
	m.states[0] = engine.StateOK
	if cmd := m.retry(); cmd != nil {
		t.Error("retry should be blocked for a non-failed step")
	}
}

func TestQuitCancelsAndAborts(t *testing.T) {
	m, _, canceled := testModel(demoSteps())
	sizeUp(m)
	m.running = true
	m.activeIdx = 1
	m.states[1] = engine.StateRunning

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !*canceled {
		t.Error("quit should cancel the run context")
	}
	if !m.quitting {
		t.Error("quit should set quitting")
	}
	if m.states[1] != engine.StateAborted {
		t.Error("active step should be marked aborted on quit")
	}
	if cmd == nil {
		t.Error("quit should return a command (tea.Quit)")
	}
}

func TestNavigationAndAllLogs(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !m.isAllLogs() {
		t.Error("'a' should select All logs")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.isAllLogs() {
		t.Error("up from All logs should move selection")
	}
	if m.follow {
		t.Error("manual navigation should disable follow")
	}
}

func TestRingEviction(t *testing.T) {
	r := newRing(3)
	for _, s := range []string{"1", "2", "3", "4", "5"} {
		r.append([]byte(s))
	}
	if got := r.String(); got != "3\n4\n5" {
		t.Fatalf("ring = %q, want last 3", got)
	}
	r.reset()
	if r.String() != "" {
		t.Error("reset should empty the ring")
	}
}
