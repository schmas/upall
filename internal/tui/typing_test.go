package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

// TestKeyToBytesMapping is the pure byte contract type mode forwards to the
// pty: runes/space pass through verbatim, a few control keys map to the bytes
// a real terminal would send, and anything unmapped is dropped (nil) so it
// never reaches WriteInput.
func TestKeyToBytesMapping(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"runes", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")}, "hi"},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}, " "},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"backspace", tea.KeyMsg{Type: tea.KeyBackspace}, "\x7f"},
		{"ctrl-c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(keyToBytes(c.msg)); got != c.want {
				t.Errorf("keyToBytes(%+v) = %q, want %q", c.msg, got, c.want)
			}
		})
	}
	if got := keyToBytes(tea.KeyMsg{Type: tea.KeyUp}); got != nil {
		t.Errorf("keyToBytes(KeyUp) = %q, want nil (unmapped)", got)
	}
}

// TestCanTypeGuard is the run-state machine for type mode: it is enterable
// only while a run is active AND the Output pane is showing the single live
// step that is actually running (not All logs, not a different step).
func TestCanTypeGuard(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	if m.canType() {
		t.Error("canType should be false before any run has started")
	}

	startRunning(m)
	m.activeIdx = 0
	m.out = outSel{kind: outLiveStep, step: 0}
	if !m.canType() {
		t.Error("canType should be true when Output shows the running step")
	}

	m.out = outSel{kind: outLiveStep, step: 1}
	if m.canType() {
		t.Error("canType should be false when viewing a different step than the running one")
	}

	m.out = outSel{kind: outLiveAll}
	if m.canType() {
		t.Error("canType should be false for All logs")
	}

	m.out = outSel{kind: outLiveStep, step: 0}
	m.running = false
	if m.canType() {
		t.Error("canType should be false once idle")
	}
}

// TestTypeKeyEntersTypeModeOnlyWhenTypable proves the type key ('i' by
// default) is gated by canType exactly like retry/continue are gated by their
// own guards.
func TestTypeKeyEntersTypeModeOnlyWhenTypable(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	m.out = outSel{kind: outLiveStep, step: 0}
	m.focus = FocusOutput

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if !m.typing {
		t.Error("type key should enter type mode when canType is true")
	}

	m.typing = false
	m.running = false // no longer typable
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if m.typing {
		t.Error("type key should not enter type mode when canType is false")
	}
}

// TestTypingModeInterceptsNavKeys proves Update routes every key to
// handleTypingKey while typing, bypassing the normal dispatch entirely — Tab
// normally cycles focus (a global key), so unchanged focus after a Tab
// keypress shows the key never reached handleKey.
func TestTypingModeInterceptsNavKeys(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.focus = FocusOutput
	m.typing = true

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != FocusOutput {
		t.Errorf("focus = %v, want unchanged FocusOutput (tab must be intercepted while typing)", m.focus)
	}
	if !m.typing {
		t.Error("typing mode should still be active (tab maps to no bytes, so it is just dropped)")
	}
}

// TestEscExitsTypingMode proves Esc (hardcoded, not a rebindable action) exits
// type mode and hands keys back to normal navigation.
func TestEscExitsTypingMode(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.focus = FocusOutput
	m.typing = true

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.typing {
		t.Error("Esc should exit type mode")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus == FocusOutput {
		t.Error("focus should cycle again once type mode has exited")
	}
}

// TestTypingModeAutoExits proves a run ending (however it ends) can never
// strand the model in type mode with nothing left to type into.
func TestTypingModeAutoExits(t *testing.T) {
	t.Run("step done", func(t *testing.T) {
		m, _, _ := testModel(demoSteps())
		sizeUp(m)
		startRunning(m)
		m.activeIdx = 0
		m.typing = true

		m.Update(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
		if m.typing {
			t.Error("typing mode should auto-exit when the step being typed into finishes")
		}
	})

	t.Run("run done", func(t *testing.T) {
		m, _, _ := testModel(demoSteps())
		sizeUp(m)
		startRunning(m)
		m.typing = true

		m.Update(RunDoneMsg{})
		if m.typing {
			t.Error("typing mode should auto-exit when the run ends")
		}
	})

	t.Run("stop", func(t *testing.T) {
		m, _, _ := testModel(demoSteps())
		sizeUp(m)
		startRunning(m)
		m.rc.runCancel = func() {}
		m.typing = true

		m.stop()
		if m.typing {
			t.Error("typing mode should auto-exit when stop cancels the run")
		}
	})
}
