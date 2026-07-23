package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

// TestLooksLikePasswordPrompt pins the prompt matcher: it fires on the common
// sudo/ssh password and passphrase prompts (which end in ':') and stays quiet
// on ordinary output, so the "press i to type" hint tracks real prompts only.
func TestLooksLikePasswordPrompt(t *testing.T) {
	yes := []string{
		"Password:",
		"[sudo] password for diego:",
		"diego@host's password:",
		"Enter passphrase for key '/home/diego/.ssh/id_ed25519':",
		"PASSWORD:", // case-insensitive
	}
	no := []string{
		"",
		"Building project...",
		"done:",                  // colon but no password/passphrase
		"password saved to disk", // mentions password but is not a prompt (no trailing colon)
		"cloning https://host:",  // trailing colon, not a credential prompt
	}
	for _, s := range yes {
		if !looksLikePasswordPrompt(s) {
			t.Errorf("looksLikePasswordPrompt(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikePasswordPrompt(s) {
			t.Errorf("looksLikePasswordPrompt(%q) = true, want false", s)
		}
	}
}

// TestAwaitInputTracksActiveStepOutput proves the running step's live output
// flips awaitInput: a sudo prompt sets it, later non-prompt output clears it.
func TestAwaitInputTracksActiveStepOutput(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	m.out = outSel{kind: outLiveStep, step: 0}

	m.Update(bytesMsg{0: []byte("[sudo] password for diego: ")})
	if !m.awaitInput {
		t.Fatal("awaitInput should be true after the active step prints a sudo prompt")
	}

	m.Update(bytesMsg{0: []byte("\r\nInstalling updates...\r\n")})
	if m.awaitInput {
		t.Error("awaitInput should clear once the step prints past the prompt")
	}
}

// TestAwaitInputIgnoresNonActiveStep guards against a prompt in a step that is
// not the running one lighting the hint (only the active step can be typed to).
func TestAwaitInputIgnoresNonActiveStep(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0

	m.Update(bytesMsg{1: []byte("Password:")}) // step 1, not the active step 0
	if m.awaitInput {
		t.Error("awaitInput should stay false for a prompt from a non-active step")
	}
}

// TestAwaitInputClearsWhenRunEnds proves the hint never lingers past the state
// that could act on it: the step finishing, the run ending, or stop.
func TestAwaitInputClearsWhenRunEnds(t *testing.T) {
	set := func() *Model {
		m, _, _ := testModel(demoSteps())
		sizeUp(m)
		startRunning(m)
		m.activeIdx = 0
		m.awaitInput = true
		return m
	}

	t.Run("step done", func(t *testing.T) {
		m := set()
		m.Update(doneMsg{i: 0, res: engine.Result{State: engine.StateOK}})
		if m.awaitInput {
			t.Error("awaitInput should clear when the step finishes")
		}
	})
	t.Run("run done", func(t *testing.T) {
		m := set()
		m.Update(RunDoneMsg{})
		if m.awaitInput {
			t.Error("awaitInput should clear when the run ends")
		}
	})
	t.Run("stop", func(t *testing.T) {
		m := set()
		m.rc.runCancel = func() {}
		m.stop()
		if m.awaitInput {
			t.Error("awaitInput should clear when stop cancels the run")
		}
	})
	t.Run("next step starts", func(t *testing.T) {
		m := set()
		m.Update(startMsg{i: 1})
		if m.awaitInput {
			t.Error("awaitInput should reset when a fresh step starts")
		}
	})
}

// TestTypeKeyEntersTypeModeFromAnyPane is the fix's core: the "press i to type"
// hint must be actionable wherever focus is. Pressing the type key from a pane
// other than Output during a run enters type mode and pulls Output onto the
// running step, so a user staring at a sudo prompt does not first have to hunt
// for the right pane.
func TestTypeKeyEntersTypeModeFromAnyPane(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 1
	m.focus = FocusSteps
	m.out = outSel{kind: outLiveAll}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})

	if !m.typing {
		t.Error("type key from the Steps pane should enter type mode during a run")
	}
	if m.focus != FocusOutput {
		t.Errorf("focus = %v, want FocusOutput after entering type mode", m.focus)
	}
	if !m.isLiveStep() || m.out.step != 1 {
		t.Errorf("out = %+v, want the live active step 1", m.out)
	}
}

// TestAwaitInputHintsSurfaced proves the detected prompt reaches the user: the
// footer leads with the type-password hint and the Output title warns, both
// regardless of which pane is focused.
func TestAwaitInputHintsSurfaced(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	m.out = outSel{kind: outLiveStep, step: 0}
	m.focus = FocusSteps
	m.awaitInput = true

	hints := m.footerHints()
	if len(hints) == 0 || hints[0].label != "type password" {
		t.Errorf("footer should lead with the type-password hint, got %+v", hints)
	}

	title, _ := m.outputTitleCount()
	if !strings.Contains(title, "press i to type") {
		t.Errorf("Output title should warn about the waiting prompt, got %q", title)
	}
}
