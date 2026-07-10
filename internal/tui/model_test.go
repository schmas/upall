package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/settings"
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
	return New(steps, "", 0, rc, settings.Defaults()), &launched, &canceled
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
	m.Update(bytesMsg{0: []byte("hello\r\nworld\r\n")})
	if got := renderTerm(m.terms[0]); !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Fatalf("emulator content = %q, want hello+world", got)
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
	m.out = outSel{kind: outLiveStep, step: 0}

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
	// All logs is the top row now; Down moves to the first step.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.isAllLogs() {
		t.Error("down from All logs should move selection to a step")
	}
	if m.follow {
		t.Error("manual navigation should disable follow")
	}
}

// TestDashboardRendersOnLaunchAndStartsOnConfirm verifies the full three-pane
// dashboard (no preview screen) shows on launch, the run does not start until
// the start key, and then begins exactly once.
func TestDashboardRendersOnLaunchAndStartsOnConfirm(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)

	if m.started {
		t.Fatal("should open idle, not started")
	}
	v := m.View()
	for _, title := range []string{"Steps", "History", "Output"} {
		if !strings.Contains(v, title) {
			t.Errorf("dashboard should show the %q pane:\n%s", title, v)
		}
	}
	if strings.Contains(v, "will run") {
		t.Error("no preview screen path should remain")
	}
	// A non-start key does not launch the run.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.started || *launched != 0 {
		t.Fatal("nothing should run before the start key")
	}
	// Enter begins the run exactly once.
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.started || !m.running {
		t.Fatal("enter should start the run")
	}
	if *launched != 1 {
		t.Fatalf("launch count = %d, want 1 after start", *launched)
	}
}

// TestPreRunFooterShowsToggleHint proves the idle Steps footer advertises the
// space toggle so pre-run include/exclude is discoverable, and that the hint
// drops once a run starts (toggle is a no-op after start).
func TestPreRunFooterShowsToggleHint(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	if foot := ansi.Strip(m.renderFooterBar()); !strings.Contains(foot, "space toggle") {
		t.Errorf("pre-run footer should show the space toggle hint, got %q", foot)
	}
	startRunning(m)
	if foot := ansi.Strip(m.renderFooterBar()); strings.Contains(foot, "space toggle") {
		t.Errorf("started footer should drop the toggle hint, got %q", foot)
	}
}

// runModelInRoot builds a synchronous model rooted at a real (temp) run-log
// root, so run-dir creation and history scanning hit the filesystem.
func runModelInRoot(t *testing.T, root string) *Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	m := New(demoSteps(), root, 5, rc, settings.Defaults())
	sizeUp(m)
	return m
}

// TestOpenDoesNotRecordHistory proves merely opening upall records nothing: no
// run dir is created, the root is untouched, and a quit-without-run writes no
// manifest. This is the fix for a phantom history entry appearing on every open.
func TestOpenDoesNotRecordHistory(t *testing.T) {
	root := t.TempDir()
	m := runModelInRoot(t, root)

	if m.runDir != "" {
		t.Errorf("open must not create a run dir, got %q", m.runDir)
	}
	if dirs := engine.RunDirs(root); len(dirs) != 0 {
		t.Errorf("open must leave the root empty, got %v", dirs)
	}
	if len(m.runs) != 0 {
		t.Errorf("history should be empty on a fresh root, got %d", len(m.runs))
	}
	// Quit-without-run path: started is false, so nothing is recorded.
	m.recordManifest()
	if dirs := engine.RunDirs(root); len(dirs) != 0 {
		t.Errorf("recordManifest before any run must write nothing, got %v", dirs)
	}
}

// TestRunRecordsAndRefreshesHistory proves a run creates its dir lazily, is
// hidden from History while in flight, and is recorded + shown as the latest
// entry once it finishes.
func TestRunRecordsAndRefreshesHistory(t *testing.T) {
	root := t.TempDir()
	m := runModelInRoot(t, root)

	m.begin()
	if m.runDir == "" {
		t.Fatal("begin should create the run dir")
	}
	if len(m.runs) != 0 {
		t.Errorf("the in-flight run must be hidden from History, got %d", len(m.runs))
	}

	for i := range m.steps {
		m.states[i] = engine.StateOK
		m.durs[i] = engine.Result{Dur: time.Second}
	}
	m.Update(RunDoneMsg{})

	if len(m.runs) != 1 {
		t.Fatalf("finished run should appear in History, got %d", len(m.runs))
	}
	if _, err := engine.ReadManifest(m.runDir); err != nil {
		t.Errorf("finished run should have a manifest on disk: %v", err)
	}
}

// TestCursorStyleIsReverseVideo proves the list cursor uses a reverse-video bar
// (visible against green ✓ glyphs/labels), while the shared selected style stays
// foreground-only so it never inverts the progress bar or filter tabs.
func TestCursorStyleIsReverseVideo(t *testing.T) {
	st := testStyles()
	if !st.cursor.GetReverse() {
		t.Error("cursor highlight should be reverse video so it is visible against green text")
	}
	if st.selected.GetReverse() {
		t.Error("selected (tabs/progress fill) must not be reverse — it would invert the progress bar")
	}
}

// TestConfigOpenKeysWired proves the config-open keys resolve to a command from
// any pane (global handler), so 'c' opens config.toml and 'C' opens the config
// dir. The command's filesystem work is deferred into the returned tea.Cmd, so
// this only checks wiring — it never touches the real config path.
func TestConfigOpenKeysWired(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}); cmd == nil {
		t.Error("'c' should return an open-config command")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}}); cmd == nil {
		t.Error("'C' should return an open-config-dir command")
	}
}

// TestTabCyclesFocus proves Tab advances Steps→Output→History→Steps, Shift+Tab
// reverses, and the footer text tracks the focused pane.
func TestTabCyclesFocus(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	if m.focus != FocusSteps {
		t.Fatalf("default focus = %v, want Steps", m.focus)
	}
	foot := m.renderFooterBar()
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != FocusOutput {
		t.Errorf("tab → %v, want Output", m.focus)
	}
	if m.renderFooterBar() == foot {
		t.Error("footer should change with focus")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != FocusHistory {
		t.Errorf("tab → %v, want History", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != FocusSteps {
		t.Errorf("tab wrap → %v, want Steps", m.focus)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m.focus != FocusHistory {
		t.Errorf("shift+tab → %v, want History", m.focus)
	}
}

// TestKeysBuiltFromSettings proves a rebound key from Settings is honored and
// the old default no longer fires.
func TestKeysBuiltFromSettings(t *testing.T) {
	set := settings.Defaults()
	set.Keys["quit"] = []string{"Q"}
	canceled := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := &runControl{
		ctx:    ctx,
		cancel: func() { canceled = true; cancel() },
		runner: engine.NewRunner("", nil),
		steps:  demoSteps(),
		launch: func(func()) {},
	}
	m := New(demoSteps(), "", 0, rc, set)
	sizeUp(m)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if m.quitting {
		t.Error("default 'q' should not quit after rebinding quit to 'Q'")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Q")})
	if !m.quitting || !canceled {
		t.Error("rebound 'Q' should quit and cancel")
	}
}

// TestLayoutWideVsNarrow proves the wide layout puts Output in the right column
// and the narrow layout stacks Steps, Output, History full-width.
func TestLayoutWideVsNarrow(t *testing.T) {
	m, _, _ := testModel(demoSteps())

	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if !m.wide {
		t.Fatal("120 cols should be wide")
	}
	if m.outRect.x == 0 {
		t.Errorf("wide Output should be in the right column, x=%d", m.outRect.x)
	}
	if m.stepsRect.x != 0 || m.histRect.x != 0 {
		t.Error("wide left column (Steps/History) should be at x=0")
	}

	m.Update(tea.WindowSizeMsg{Width: 70, Height: 40})
	if m.wide {
		t.Fatal("70 cols should be narrow")
	}
	if m.outRect.x != 0 || m.stepsRect.x != 0 || m.histRect.x != 0 {
		t.Error("narrow panes should all be full-width at x=0")
	}
	if !(m.stepsRect.y < m.outRect.y && m.outRect.y < m.histRect.y) {
		t.Errorf("narrow order should be Steps, Output, History: %d %d %d",
			m.stepsRect.y, m.outRect.y, m.histRect.y)
	}
}

// TestHeaderShowsProgressCount proves the header reflects the done/total count.
func TestHeaderShowsProgressCount(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	if h := m.renderHeaderBar(); !strings.Contains(h, "0/2") {
		t.Errorf("idle header should show 0/2 done: %q", h)
	}
	m.states[0] = engine.StateOK
	if h := m.renderHeaderBar(); !strings.Contains(h, "1/2") {
		t.Errorf("header should show 1/2 after one done: %q", h)
	}
}

// TestMouseClickSelectsStep proves left-click routing on the running dashboard:
// clicking a step row focuses Steps and selects it (stops following), clicking
// the "All logs" row selects that, and clicking the Output pane focuses Output
// without reselecting a step.
func TestMouseClickSelectsStep(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m) // 120 cols → wide layout
	startRunning(m)
	m.follow = true

	// Content rows below the top border: 0 filter tabs, 1 All logs, 2 step0,
	// 3 step1. Click the second step (content row 3).
	m.Update(tea.MouseMsg{X: 2, Y: m.stepsRect.y + 1 + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.isLiveStep() || m.out.step != 1 {
		t.Fatalf("out = %+v after click, want live step 1", m.out)
	}
	if m.follow {
		t.Error("click should disable follow")
	}
	if m.focus != FocusSteps {
		t.Error("clicking the Steps pane should focus it")
	}

	// The All-logs row is content row 1.
	m.Update(tea.MouseMsg{X: 2, Y: m.stepsRect.y + 1 + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.isAllLogs() {
		t.Errorf("click on All-logs row should select it, out=%+v", m.out)
	}

	// A click in the Output pane focuses it and must not reselect a step.
	m.out = outSel{kind: outLiveStep, step: 0}
	m.Update(tea.MouseMsg{X: m.outRect.x + 5, Y: m.outRect.y + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.isLiveStep() || m.out.step != 0 {
		t.Errorf("click in Output pane should not reselect a step, out=%+v", m.out)
	}
	if m.focus != FocusOutput {
		t.Error("clicking the Output pane should focus it")
	}
}

// TestLogLinesStayInsideBox drives the real bytesMsg path and proves the two
// overflow modes from the reported screenshots are gone: carriage-return progress
// redraws collapse to their final frame (left overflow over the master pane), and
// every rendered line fits the pane width (right-edge overflow) because the
// emulator wraps to its own column count.
func TestLogLinesStayInsideBox(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m) // wide layout, viewport sized
	startRunning(m)
	m.out = outSel{kind: outLiveStep, step: 0}
	m.follow = false

	progress := "Downloading 16MB\rDownloading 200MB\rDownloaded 333MB\x1b[K\r\n"
	long := strings.Repeat("cask-claude-", 40) + "\r\n" // ~480 cols, must wrap
	m.Update(bytesMsg{0: []byte(progress + long)})

	got := renderTerm(m.terms[0])
	if strings.ContainsRune(got, '\r') {
		t.Errorf("render retains carriage return: %q", got)
	}
	if !strings.Contains(got, "Downloaded 333MB") {
		t.Errorf("progress should collapse to its final frame, got %q", got)
	}
	if strings.Contains(got, "Downloading 16MB") || strings.Contains(got, "Downloading 200MB") {
		t.Errorf("overwritten progress frames should be gone, got %q", got)
	}
	for _, ln := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(ln); w > m.vp.Width {
			t.Fatalf("rendered line width %d exceeds pane %d: %q", w, m.vp.Width, ln)
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
	if got := m.renderHeaderBar(); !strings.Contains(got, engine.Hms(60*time.Second)) {
		t.Fatalf("idle header should freeze at 1m0s; got %q", got)
	}

	// While running (totalEnd zero) it is live and must not show the frozen value.
	m.totalEnd = time.Time{}
	if got := m.renderHeaderBar(); strings.Contains(got, engine.Hms(60*time.Second)) {
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
