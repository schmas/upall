package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/settings"
)

type stepFix struct {
	file, key, label string
	state            engine.State
	log              string
}

func writeRun(t *testing.T, root, name string, fixes []stepFix) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	steps := make([]engine.Step, len(fixes))
	states := make([]engine.State, len(fixes))
	durs := make([]engine.Result, len(fixes))
	for i, f := range fixes {
		steps[i] = engine.Step{Key: f.key, Label: f.label}
		states[i] = f.state
		durs[i] = engine.Result{Dur: time.Second}
		if err := os.WriteFile(filepath.Join(dir, f.file), []byte(f.log), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := engine.WriteManifest(dir, steps, states, durs, engine.RunDirTime(dir).Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// historyModel builds a sized model whose run-log root holds two past runs. No
// run is in progress, so all past runs are browsable (the run dir is created
// lazily on the first run, not at startup).
func historyModel(t *testing.T) (*Model, string) {
	t.Helper()
	root := t.TempDir()
	writeRun(t, root, "20260709-090000", []stepFix{
		{"01-brew.log", "brew", "Homebrew", engine.StateOK, "brew \x1b[32mok\x1b[0m\n"},
		{"02-mise.log", "mise", "Mise", engine.StateFailed, "mise boom\n"},
	})
	writeRun(t, root, "20260708-090000", []stepFix{
		{"01-brew.log", "brew", "Homebrew", engine.StateOK, "older run\n"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	m := New(demoSteps(), root, 0, rc, settings.Defaults())
	sizeUp(m)
	m.focus = FocusHistory
	return m, root
}

func TestHistoryPaneListsRunsNewestFirst(t *testing.T) {
	m, _ := historyModel(t)
	if len(m.runs) != 2 {
		t.Fatalf("runs = %d, want 2 (live run excluded)", len(m.runs))
	}
	if !strings.Contains(m.runs[0].Dir, "20260709-090000") {
		t.Errorf("newest run first, got %s", m.runs[0].Dir)
	}
	v := ansi.Strip(m.renderHistoryPane())
	if !strings.Contains(v, m.runs[0].Label) {
		t.Errorf("history pane should list the newest run label %q:\n%s", m.runs[0].Label, v)
	}
}

func TestHistoryExpandCollapse(t *testing.T) {
	m, _ := historyModel(t)
	if len(m.histRows()) != 2 {
		t.Fatalf("collapsed rows = %d, want 2 (runs only)", len(m.histRows()))
	}
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	if !m.histExpanded[0] {
		t.Fatal("enter should expand the run")
	}
	// header + 2 steps + all-logs + second run header = 5
	if got := len(m.histRows()); got != 5 {
		t.Fatalf("expanded rows = %d, want 5", got)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // collapse
	if m.histExpanded[0] {
		t.Error("left should collapse the run")
	}
	if m.histCursor != 0 {
		t.Errorf("cursor should snap back to the header, got %d", m.histCursor)
	}
}

func TestHistorySelectStepLoadsLog(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	m.histCursor = 1                         // brew step child
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select it

	if m.out.kind != outHistStep || m.out.run != 0 || m.out.step != 0 {
		t.Fatalf("out = %+v, want history step run0 step0", m.out)
	}
	// ANSI is decoded by the scratch emulator; the visible text survives.
	got := ansi.Strip(m.vp.View())
	if !strings.Contains(got, "brew") || !strings.Contains(got, "ok") {
		t.Errorf("Output should show the decoded brew log, got %q", got)
	}
	// Selecting turns off follow so a live start cannot steal the Output.
	if m.follow {
		t.Error("selecting a history log should disable follow")
	}
}

func TestHistorySelectAllConcatenates(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	// rows: 0 header, 1 brew, 2 mise, 3 all-logs
	m.histCursor = 3
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.out.kind != outHistAll || m.out.run != 0 {
		t.Fatalf("out = %+v, want history all run0", m.out)
	}
	got := ansi.Strip(m.vp.View())
	if !strings.Contains(got, "brew") || !strings.Contains(got, "mise") {
		t.Errorf("All logs should concatenate both steps, got %q", got)
	}
}

// TestHistoryStepShowsDuration proves an expanded run's step rows display their
// per-step duration (from the manifest), not just the run total on the header.
func TestHistoryStepShowsDuration(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	v := ansi.Strip(m.renderHistoryPane())
	// Each fixture step ran for 1s (engine.Hms(time.Second) == "1s").
	if !strings.Contains(v, "Homebrew 1s") {
		t.Errorf("expanded step row should show its duration, got:\n%s", v)
	}
}

// TestHistoryMouseClickExpandsAndSelects proves left-click in the History pane
// moves the cursor to the clicked row, toggles a run header open/closed, and
// selects a step child's log into Output — previously a click only focused the
// pane, leaving the cursor (and highlight) elsewhere.
func TestHistoryMouseClickExpandsAndSelects(t *testing.T) {
	m, _ := historyModel(t)
	click := func(row int) {
		m.Update(tea.MouseMsg{X: 2, Y: m.histRect.y + 1 + row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}

	// Click the first run header (content row 0): expands it and lands the cursor.
	click(0)
	if !m.histExpanded[0] {
		t.Fatal("clicking a collapsed run header should expand it")
	}
	if m.histCursor != 0 || m.focus != FocusHistory {
		t.Errorf("click should focus History and move the cursor to the header, cursor=%d focus=%v", m.histCursor, m.focus)
	}

	// rows: 0 header, 1 brew, 2 mise, 3 all-logs, 4 header(run1). Click brew.
	click(1)
	if m.out.kind != outHistStep || m.out.run != 0 || m.out.step != 0 {
		t.Fatalf("clicking a step child should select it, out=%+v", m.out)
	}
	if m.histCursor != 1 {
		t.Errorf("cursor should follow the clicked child row, got %d", m.histCursor)
	}
	if got := ansi.Strip(m.vp.View()); !strings.Contains(got, "brew") {
		t.Errorf("Output should show the clicked step log, got %q", got)
	}

	// Click the header again: collapses it.
	click(0)
	if m.histExpanded[0] {
		t.Error("clicking an expanded run header should collapse it")
	}
}

// TestHistoryNavigateLoadsDebounced proves ↓/↑ in the History pane schedule a
// debounced load rather than loading immediately, and that only the tick whose
// generation matches the cursor's final rest actually loads the log.
func TestHistoryNavigateLoadsDebounced(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	// rows: 0 header0, 1 brew, 2 mise, 3 all-logs, 4 header(run1)
	m.histCursor = 0
	m.out = outSel{kind: outLiveStep, step: 0} // start on a live selection

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd == nil {
		t.Fatal("history ↓ should schedule a debounced load command")
	}
	if m.histCursor != 1 {
		t.Fatalf("cursor = %d, want 1 after ↓", m.histCursor)
	}
	// Navigation alone must not load — the log loads only when the tick fires.
	if m.out.kind != outLiveStep {
		t.Errorf("navigation alone must not load history yet, out=%+v", m.out)
	}
	gen := m.histSelGen

	// A superseded tick is ignored.
	m.Update(histSelectMsg{gen: gen - 1})
	if m.out.kind != outLiveStep {
		t.Errorf("stale debounce tick must not load, out=%+v", m.out)
	}

	// The current tick loads the row under the cursor (the brew step child).
	m.Update(histSelectMsg{gen: gen})
	if m.out.kind != outHistStep || m.out.run != 0 || m.out.step != 0 {
		t.Fatalf("debounce should load the cursor's step, out=%+v", m.out)
	}
	if got := ansi.Strip(m.vp.View()); !strings.Contains(got, "brew") {
		t.Errorf("Output should show the brew log, got %q", got)
	}
	if m.follow {
		t.Error("load-on-navigate should disable follow")
	}
}

// TestHistoryDebounceIgnoredWhenUnfocused proves a pending debounce tick does not
// steal the Output once the user has tabbed away from the History pane.
func TestHistoryDebounceIgnoredWhenUnfocused(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.out = outSel{kind: outLiveStep, step: 0}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // schedules, cursor → 1
	gen := m.histSelGen
	m.focus = FocusSteps // user tabbed away before the debounce fired
	m.Update(histSelectMsg{gen: gen})
	if m.out.kind != outLiveStep {
		t.Errorf("debounce must not steal Output after focus left History, out=%+v", m.out)
	}
}

// TestWrapToggleKey proves the 'w' key flips the Output wrap state and the
// history subtitle tracks it.
func TestWrapToggleKey(t *testing.T) {
	m, _ := historyModel(t)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand run 0
	m.histCursor = 1
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select the brew step
	if !m.wrap {
		t.Fatal("wrap should default on")
	}
	if _, sub := m.outputTitleCount(); !strings.Contains(sub, "wrap:on") {
		t.Fatalf("subtitle should show wrap:on, got %q", sub)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if m.wrap {
		t.Error("'w' should toggle wrap off")
	}
	if _, sub := m.outputTitleCount(); !strings.Contains(sub, "wrap:off") {
		t.Errorf("subtitle should show wrap:off after toggle, got %q", sub)
	}
}

// TestHistoryReadOnly proves a history selection can never trigger a live-run
// mutation: retry is a no-op even when a live step is failed.
func TestHistoryReadOnly(t *testing.T) {
	m, _ := historyModel(t)
	m.states[0] = engine.StateFailed // would be retry-able if it were selected
	m.out = outSel{kind: outHistStep, run: 0, step: 0}
	m.running = false
	if cmd := m.retry(); cmd != nil {
		t.Error("retry must be a no-op while a history log is selected")
	}
	if m.states[0] != engine.StateFailed {
		t.Error("retry must not have reset the live step")
	}
}

// TestHistoryMissingLogIsGraceful proves logs are read on selection (not scan)
// and a missing file degrades gracefully rather than crashing.
func TestHistoryMissingLogIsGraceful(t *testing.T) {
	m, _ := historyModel(t)
	// Delete a logfile after the scan; selecting it must not panic.
	os.Remove(m.runs[0].Steps[0].LogPath)
	m.histCursor = 0
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // expand
	m.histCursor = 1                         // brew (now missing)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // select
	if got := ansi.Strip(m.vp.View()); !strings.Contains(got, "unavailable") {
		t.Errorf("missing log should render a graceful notice, got %q", got)
	}
}

// TestHistoryCapTruncates proves an over-cap log is truncated in-pane and the
// subtitle flags it.
func TestHistoryCapTruncates(t *testing.T) {
	root := t.TempDir()
	var big strings.Builder
	for i := 0; i < scrollbackCap+50; i++ {
		big.WriteString("line\n")
	}
	writeRun(t, root, "20260709-090000", []stepFix{
		{"01-brew.log", "brew", "Homebrew", engine.StateOK, big.String()},
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	m := New(demoSteps(), root, 0, rc, settings.Defaults())
	sizeUp(m)

	m.out = outSel{kind: outHistStep, run: 0, step: 0}
	m.rebuildContent()
	if !m.histTruncated {
		t.Error("an over-cap log should set histTruncated")
	}
	_, sub := m.outputTitleCount()
	if !strings.Contains(sub, "truncated") {
		t.Errorf("subtitle should hint truncation, got %q", sub)
	}
}

func TestResolvePagerPrecedence(t *testing.T) {
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat not available")
	}
	// Configured pager wins.
	if bin, _ := resolvePager("cat"); filepath.Base(bin) != "cat" {
		t.Errorf("configured pager = %q, want cat", bin)
	}
	// $PAGER used when no configured pager.
	t.Setenv("PAGER", "cat")
	if bin, _ := resolvePager(""); filepath.Base(bin) != "cat" {
		t.Errorf("$PAGER pager = %q, want cat", bin)
	}
}
