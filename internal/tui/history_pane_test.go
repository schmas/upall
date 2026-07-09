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

// historyModel builds a sized model whose run-dir parent holds two past runs.
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
	cur := filepath.Join(root, "20260709-120000") // the "live" run, excluded from history
	if err := os.MkdirAll(cur, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	m := New(demoSteps(), cur, rc, settings.Defaults())
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
	cur := filepath.Join(root, "20260709-120000")
	os.MkdirAll(cur, 0o700)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{ctx: ctx, cancel: cancel, runner: engine.NewRunner("", nil), steps: demoSteps(), launch: func(func()) {}}
	m := New(demoSteps(), cur, rc, settings.Defaults())
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
