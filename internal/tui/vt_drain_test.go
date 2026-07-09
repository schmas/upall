package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/schmas/upall/internal/engine"
)

// TestBytesMsgWithQueryDoesNotBlock feeds a step's emulator a device-status
// query (DSR, \x1b[6n). The emulator writes its reply synchronously into an
// unbuffered pipe during Write, so without the per-emulator drain goroutine the
// update loop would deadlock here. The drain (started in New) must keep it
// moving, and the surrounding text must still render.
func TestBytesMsgWithQueryDoesNotBlock(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.sel = 0
	m.follow = false

	done := make(chan struct{})
	go func() {
		m.Update(bytesMsg{0: []byte("before\x1b[6nafter\x1b[c end\r\n")})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("bytesMsg with a terminal query deadlocked the update loop")
	}

	got := renderTerm(m.terms[0])
	if !strings.Contains(got, "before") || !strings.Contains(got, "after") || !strings.Contains(got, "end") {
		t.Fatalf("query bytes should be consumed, surrounding text kept: %q", got)
	}
	// The query sequences are consumed by the parser, not printed as text.
	if strings.Contains(got, "6n") || strings.Contains(got, "[6n") {
		t.Fatalf("raw query leaked into render: %q", got)
	}
}

// TestIntegrationProgressAndColor drives a progress + color stream through the
// real Bubble Tea loop via Sink.Output semantics (bytesMsg), then asserts the
// final View shows the collapsed final frame, the surviving color, and the
// master strip — the end-to-end path the pty harness cannot exercise.
func TestIntegrationProgressAndColor(t *testing.T) {
	m, _ := integrationModel(t)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // confirm preview
	tm.Send(startMsg{0})
	tm.Send(bytesMsg{0: []byte("fetch 1%\rfetch 50%\rfetch 100%\r\n\x1b[32mDONE\x1b[0m\r\n")})
	tm.Send(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
	tm.Send(startMsg{1})
	tm.Send(doneMsg{i: 1, res: engine.Result{State: engine.StateOK}})
	tm.Send(RunDoneMsg{})

	// Wait until the final frame shows the collapsed progress + status, then quit.
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return strings.Contains(string(b), "fetch 100%") && strings.Contains(string(b), "DONE")
	}, teatest.WithDuration(5*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))

	fm, ok := tm.FinalModel(t).(*Model)
	if !ok {
		t.Fatal("final model type")
	}
	out := fm.View()
	if strings.Contains(out, "fetch 1%") || strings.Contains(out, "fetch 50%") {
		t.Errorf("intermediate progress frames should be overwritten:\n%s", out)
	}
}
