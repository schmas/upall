package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/settings"
)

// TestStopCancelsCurrentRunNotSession proves the stop key cancels only the
// per-run child context (so the runner reaps its child) and never the session
// context — cancelling the session would dead-end every later retry/re-run.
func TestStopCancelsCurrentRunNotSession(t *testing.T) {
	sessionCancelled, runCancelled := false, false
	rc := &runControl{
		ctx:       context.Background(),
		cancel:    func() { sessionCancelled = true },
		runCancel: func() { runCancelled = true },
		runner:    engine.NewRunner("", nil),
		steps:     demoSteps(),
		launch:    func(func()) {},
	}
	m := New(demoSteps(), "", 0, rc, settings.Defaults(), "test")
	m.running = true

	m.stop()
	if !runCancelled {
		t.Error("stop must cancel the current run")
	}
	if sessionCancelled {
		t.Error("stop must NOT cancel the session context")
	}
}

// TestStopIsNoopWhenIdle proves the stop key does nothing when no run is active.
func TestStopIsNoopWhenIdle(t *testing.T) {
	runCancelled := false
	rc := &runControl{
		ctx:       context.Background(),
		cancel:    func() {},
		runCancel: func() { runCancelled = true },
		runner:    engine.NewRunner("", nil),
		steps:     demoSteps(),
		launch:    func(func()) {},
	}
	m := New(demoSteps(), "", 0, rc, settings.Defaults(), "test")
	m.running = false

	m.stop()
	if runCancelled {
		t.Error("stop should be a no-op when no run is active")
	}
}

// TestStopMarksAbortedAndStaysOpen drives a real 'x' keypress mid-run: the
// program must NOT exit, the active step ends aborted, the run goes idle, and a
// step that never started stays pending.
func TestStopMarksAbortedAndStaysOpen(t *testing.T) {
	m, _ := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm → RunAll launch
	tm.Send(startMsg{0})                                        // step 0 running
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // stop the run
	// Simulate the runner honoring the cancel: active step aborted, run finished.
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateAborted}})
	tm.Send(RunDoneMsg{})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // then quit

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	fm, ok := tm.FinalModel(t).(*Model)
	if !ok {
		t.Fatal("no final model")
	}
	if !fm.quitting {
		t.Error("program should have quit only on the final 'q', not on stop")
	}
	if fm.states[0] != engine.StateAborted {
		t.Errorf("stopped step state = %v, want aborted", fm.states[0])
	}
	if fm.states[1] != engine.StatePending {
		t.Errorf("not-started step state = %v, want pending", fm.states[1])
	}
	if fm.running {
		t.Error("run should be idle after stop")
	}
	if fm.totalEnd.IsZero() {
		t.Error("elapsed timer should be frozen (totalEnd set) after stop")
	}
}

// TestReRunWorksAfterStop proves the session context stays alive after a stop:
// re-run ('R') must still launch a runner. Launch count 2 = start + re-run.
func TestReRunWorksAfterStop(t *testing.T) {
	m, launched := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm → RunAll launch (1)
	tm.Send(startMsg{0})                                        // step 0 running
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // stop the run
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateAborted}})
	tm.Send(RunDoneMsg{})                                       // run idle
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")}) // re-run after stop → launch (2)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // then quit

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	if *launched != 2 {
		t.Fatalf("launch count = %d, want 2 (start + re-run after stop)", *launched)
	}
}
