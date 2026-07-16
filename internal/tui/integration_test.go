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

// integrationModel wires a model with a stubbed run control so teatest can
// drive the real Bubble Tea event loop without spawning subprocesses. launched
// counts retry/RunAll launches.
func integrationModel(t *testing.T) (*Model, *int) {
	t.Helper()
	launched := 0
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	rc := &runControl{
		ctx:    ctx,
		cancel: cancel,
		runner: engine.NewRunner("", nil),
		steps:  demoSteps(),
		launch: func(func()) { launched++ },
	}
	return New(demoSteps(), "", 0, rc, settings.Defaults(), "test"), &launched
}

// TestProgramQuitsOnQ is the end-to-end proof the pty harness could not give:
// the real program event loop must terminate when 'q' is pressed. teatest
// injects messages straight into the loop, bypassing the OS input reader.
func TestProgramQuitsOnQ(t *testing.T) {
	m, _ := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Confirm the preview to start, simulate a completed run, then quit.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(startMsg{0})
	tm.Send(bytesMsg{0: []byte("output line\r\n")})
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
	tm.Send(startMsg{1})
	tm.Send(doneMsg{i: 1, res: engine.Result{State: engine.StateOK}})
	tm.Send(RunDoneMsg{})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	fm, ok := tm.FinalModel(t).(*Model)
	if !ok || !fm.quitting {
		t.Fatal("program did not quit on 'q'")
	}
}

// TestRetryLaunchesThroughLoop drives a failed step and a real 'r' keypress
// through the event loop, proving the retry path fires (and only when idle).
func TestRetryLaunchesThroughLoop(t *testing.T) {
	m, launched := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // confirm preview → RunAll launch
	tm.Send(startMsg{0})
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateFailed, RC: 1}})
	tm.Send(RunDoneMsg{}) // run idle now
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	// confirm-start RunAll launch + the retry RunOne launch = 2.
	if *launched != 2 {
		t.Fatalf("launch count = %d, want 2 (confirm-start + retry)", *launched)
	}
}

// TestTypeModeForwardsThroughLoop drives type mode through the real event
// loop: entering it must swallow even keys that are normally global (like
// stop and quit), proving Update's typing branch runs before handleKey's
// dispatch, not alongside it. The fake runner has no real process behind it,
// so those keystrokes' WriteInput calls harmlessly fail — but that must not
// exit type mode on its own (only Esc, or the run actually ending, does);
// only after Esc does 'q' reach handleKey and quit.
func TestTypeModeForwardsThroughLoop(t *testing.T) {
	m, _ := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})                     // confirm preview → RunAll launch
	tm.Send(startMsg{0})                                        // step 0 running; follow puts Output on it
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})                       // Steps → History
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})                       // History → Output
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")}) // enter type mode
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // normally "stop" — must be swallowed
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // normally "quit" — must be swallowed too
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})                       // exit type mode
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}) // now this actually quits

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	fm, ok := tm.FinalModel(t).(*Model)
	if !ok {
		t.Fatal("no final model")
	}
	if !fm.running {
		t.Error("'x' while typing should have been forwarded as input, not treated as stop")
	}
	if !fm.quitting {
		t.Error("program should have quit only on the 'q' after Esc, not the one swallowed while typing")
	}
}

// TestContinueLaunchesThroughLoop simulates stop cutting a run short (step 0
// aborted mid-flight, step 1 never started) and drives a real 'u' keypress
// through the event loop, proving continue resumes the interrupted suffix.
func TestContinueLaunchesThroughLoop(t *testing.T) {
	m, launched := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // confirm preview → RunAll launch
	tm.Send(startMsg{0})
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateAborted}}) // stop hit mid-step
	tm.Send(RunDoneMsg{})                                                  // run idle now; step 1 stayed pending
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})

	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	// confirm-start RunAll launch + the continue RunFrom launch = 2.
	if *launched != 2 {
		t.Fatalf("launch count = %d, want 2 (confirm-start + continue)", *launched)
	}
}
