package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

// startRunning puts the model past the preview into the running phase, as if
// the user had confirmed. Tests of running-mode keys need this.
func startRunning(m *Model) {
	m.started = true
	m.running = true
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
	startRunning(m)

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
	startRunning(m)
	m.states[0] = engine.StateFailed
	m.sel = 0

	// Blocked while a run is active (retry launches synchronously; count stays 0).
	m.running = true
	m.retry()
	if *launched != 0 {
		t.Error("no launch expected while running")
	}

	// Allowed when idle and the step failed.
	m.running = false
	m.retry()
	if !m.running {
		t.Error("retry should set running")
	}
	if m.states[0] != engine.StatePending {
		t.Error("retry should reset the step to pending")
	}
	if *launched != 1 {
		t.Errorf("launch count = %d, want 1", *launched)
	}

	// Blocked for a non-failed step.
	m.running = false
	m.states[0] = engine.StateOK
	m.retry()
	if *launched != 1 {
		t.Error("retry should be blocked for a non-failed step")
	}
}

func TestQuitCancelsAndAborts(t *testing.T) {
	m, _, canceled := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
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
	startRunning(m)

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

// TestPreviewDoesNotRunUntilConfirmed verifies the run does not start on launch:
// the preview is shown, no launch happens, and pressing the start key begins it.
func TestPreviewDoesNotRunUntilConfirmed(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)

	if m.started {
		t.Fatal("should open in preview, not started")
	}
	// A non-start key does nothing in preview.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.started || *launched != 0 {
		t.Fatal("nothing should run before confirmation")
	}
	// The preview View renders and mentions steps.
	if !strings.Contains(m.View(), "will run") {
		t.Errorf("preview should show what will run, got:\n%s", m.View())
	}
	// Enter confirms → run begins exactly once.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.started || !m.running {
		t.Fatal("enter should start the run")
	}
	if *launched != 1 {
		t.Fatalf("launch count = %d, want 1 after confirm", *launched)
	}
}

// TestMouseClickSelectsStep proves left-click selection in the running dashboard:
// a click on a step row selects it (and stops following), a click on the "All
// logs" row selects that, and a click out in the log pane does not reselect.
func TestMouseClickSelectsStep(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m) // 120 cols → wide layout
	startRunning(m)
	m.follow = true

	// Second step row sits at header height + its index.
	m.Update(tea.MouseMsg{X: 4, Y: headerHeight + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.sel != 1 {
		t.Fatalf("sel = %d after click, want 1", m.sel)
	}
	if m.follow {
		t.Error("click should disable follow")
	}

	// The "All logs" row is one past the last step.
	m.Update(tea.MouseMsg{X: 4, Y: headerHeight + m.allLogsIndex(), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.isAllLogs() {
		t.Errorf("click on All-logs row should select it, sel=%d", m.sel)
	}

	// A click in the log pane (wide layout, X past the master column) must not reselect.
	m.sel = 0
	m.Update(tea.MouseMsg{X: masterWidth + 10, Y: headerHeight + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.sel != 0 {
		t.Errorf("click in log pane should not reselect, sel=%d", m.sel)
	}
}

// TestMouseClickSelectsInPreview proves click selection works in the preview and
// does not start the run.
func TestMouseClickSelectsInPreview(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(tea.MouseMsg{X: 4, Y: previewTop + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if m.sel != 1 {
		t.Fatalf("preview click sel = %d, want 1", m.sel)
	}
	if m.started {
		t.Error("click should not start the run")
	}
}

// TestLogLinesStayInsideBox drives the real linesMsg path and proves the two
// overflow modes from the reported screenshots are gone: carriage-return progress
// redraws no longer reach the terminal (left overflow over the master pane), and
// every wrapped line fits the pane width (right-edge overflow).
func TestLogLinesStayInsideBox(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m) // wide layout, viewport sized
	startRunning(m)
	m.sel = 0
	m.follow = false

	progress := []byte("Downloading 16MB\rDownloading 200MB\rDownloaded 333MB\x1b[K")
	long := []byte(strings.Repeat("cask-claude-", 40)) // ~480 cols, must wrap
	m.Update(linesMsg{0: {progress, long}})

	got := m.rings[0].String()
	if strings.ContainsRune(got, '\r') {
		t.Errorf("ring retains carriage return: %q", got)
	}
	if !strings.Contains(got, "Downloaded 333MB") {
		t.Errorf("progress should collapse to its final frame, got %q", got)
	}
	for _, ln := range strings.Split(m.wrap(got), "\n") {
		if w := ansi.StringWidth(ln); w > m.vp.Width {
			t.Fatalf("wrapped line width %d exceeds pane %d: %q", w, m.vp.Width, ln)
		}
	}
}

// TestHeaderTimerFreezesWhenIdle proves the header elapsed freezes at the run's
// end once idle, and ticks live (off totalEnd being zero) while running.
func TestHeaderTimerFreezesWhenIdle(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.started = true

	// A run that started 90s ago and finished after 60s: the header must show the
	// frozen 60s duration, not the 90s of wall-clock since start.
	m.totalStart = time.Now().Add(-90 * time.Second)
	m.totalEnd = m.totalStart.Add(60 * time.Second)
	if got := m.renderHeader(); !strings.Contains(got, engine.Hms(60*time.Second)) {
		t.Fatalf("idle header should freeze at 1m0s; got %q", got)
	}

	// While running (totalEnd zero) it is live and must not show the frozen value.
	m.totalEnd = time.Time{}
	if got := m.renderHeader(); strings.Contains(got, engine.Hms(60*time.Second)) {
		t.Errorf("running header should be live, not frozen at 1m0s; got %q", got)
	}

	// RunDoneMsg is what stamps totalEnd in the real loop.
	m.Update(RunDoneMsg{})
	if m.totalEnd.IsZero() {
		t.Error("RunDoneMsg should stamp totalEnd to freeze the timer")
	}
}

// TestRestartAllResetsAndRelaunches proves 'R' re-runs everything from a clean
// slate, but only when no run is active.
func TestRestartAllResetsAndRelaunches(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.begin() // first RunAll launch
	m.Update(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
	m.Update(doneMsg{i: 1, res: engine.Result{State: engine.StateFailed, RC: 1}})
	m.Update(RunDoneMsg{}) // idle; totalEnd set

	m.restartAll()
	if !m.running {
		t.Error("restart should set running")
	}
	if *launched != 2 {
		t.Errorf("launch count = %d, want 2 (begin + restart)", *launched)
	}
	if m.states[0] != engine.StatePending || m.states[1] != engine.StatePending {
		t.Error("restart should reset every step to pending")
	}
	if !m.totalEnd.IsZero() {
		t.Error("restart should clear totalEnd so the timer runs live again")
	}

	// Blocked while a run is active (no double-launch race).
	m.restartAll()
	if *launched != 2 {
		t.Error("restart must not relaunch while a run is active")
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
