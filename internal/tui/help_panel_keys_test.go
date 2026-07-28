package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/settings"
)

func pressRune(m *Model, r rune) tea.Cmd {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return cmd
}

func TestHelpKeyOpensAndClosesPanel(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	pressRune(m, '?')
	if !m.helpOpen {
		t.Fatal("? should open the panel")
	}
	if !strings.Contains(ansi.Strip(m.View()), helpTitle) {
		t.Error("open panel is not visible in the rendered frame")
	}

	pressRune(m, '?')
	if m.helpOpen {
		t.Error("? should close the panel again")
	}

	pressRune(m, '?')
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpOpen {
		t.Error("esc should close the panel")
	}
}

// TestHelpPanelDoesNotCaptureQuit locks in the deliberate divergence from the
// issue text: q keeps one meaning everywhere in this TUI.
func TestHelpPanelDoesNotCaptureQuit(t *testing.T) {
	m, _, canceled := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')

	if cmd := pressRune(m, 'q'); cmd == nil {
		t.Fatal("q from inside the panel should return tea.Quit")
	}
	if !m.quitting || !*canceled {
		t.Error("q from inside the panel should quit and cancel the session")
	}
}

func TestHelpPanelCtrlCQuitsAndAbortsStep(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	pressRune(m, '?')

	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c from inside the panel should return tea.Quit")
	}
	if m.states[0] != engine.StateAborted {
		t.Errorf("active step state = %v, want aborted", m.states[0])
	}
}

// TestHelpPanelKeepsRunSafetySurface is the core safety guarantee: a help
// screen must never hide a live sudo prompt or block the keys that answer it.
func TestHelpPanelKeepsRunSafetySurface(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	m.awaitInput = true
	pressRune(m, '?')

	foot := footerText(m)
	for _, want := range []string{"type password", "stop"} {
		if !strings.Contains(foot, want) {
			t.Errorf("open panel dropped the %q hint, footer = %q", want, foot)
		}
	}
	if !strings.Contains(foot, "esc close") {
		t.Errorf("panel footer should say how to close it, got %q", foot)
	}
}

func TestHelpPanelStopStillWorks(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	stopped := false
	m.rc.runCancel = func() { stopped = true }
	startRunning(m)
	pressRune(m, '?')

	pressRune(m, 'x')
	if !stopped {
		t.Error("stop must still fire with the panel open")
	}
	if m.helpOpen {
		t.Error("stop should close the panel on its way through")
	}
}

func TestHelpPanelTypeModeStillWorks(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	pressRune(m, '?')

	pressRune(m, 'i')
	if !m.typing {
		t.Error("type mode must still be reachable with the panel open")
	}
	if m.helpOpen {
		t.Error("entering type mode should close the panel")
	}
}

// TestHelpPanelSwallowsOtherKeys proves nothing leaks to the panes underneath.
func TestHelpPanelSwallowsOtherKeys(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	kind, focus := m.out.kind, m.focus
	pressRune(m, '?')

	pressRune(m, 'a')
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	pressRune(m, 'w')

	if m.out.kind != kind {
		t.Errorf("'a' leaked through: out.kind = %v", m.out.kind)
	}
	if m.focus != focus {
		t.Errorf("tab leaked through: focus = %v", m.focus)
	}
	if !m.helpOpen {
		t.Error("unhandled keys should not close the panel")
	}
}

// TestHelpKeyIsSwallowedByUpdateModal proves the two modals cannot stack.
func TestHelpKeyIsSwallowedByUpdateModal(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.updateState = updateConfirming

	pressRune(m, '?')
	if m.helpOpen {
		t.Error("? must not open the panel while the update prompt owns the keyboard")
	}
}

func TestHelpPanelScrolling(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20}) // short enough that the list scrolls
	pressRune(m, '?')
	total, visible := m.helpScrollBounds()
	if total <= visible {
		t.Fatalf("test needs a scrollable panel: %d lines in %d rows", total, visible)
	}
	maxOff := total - visible

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.helpOffset != 1 {
		t.Errorf("down: offset = %d, want 1", m.helpOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.helpOffset != 0 {
		t.Errorf("up past the top: offset = %d, want 0", m.helpOffset)
	}
	pressRune(m, 'G')
	if m.helpOffset != maxOff {
		t.Errorf("bottom: offset = %d, want %d", m.helpOffset, maxOff)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.helpOffset != maxOff {
		t.Errorf("pgdown past the end: offset = %d, want %d", m.helpOffset, maxOff)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.helpOffset >= maxOff {
		t.Errorf("pgup did not move: offset = %d", m.helpOffset)
	}
	pressRune(m, 'g')
	if m.helpOffset != 0 {
		t.Errorf("top: offset = %d, want 0", m.helpOffset)
	}
}

func TestHelpPanelWheelScrolls(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pressRune(m, '?')
	focus := m.focus

	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.helpOffset == 0 {
		t.Error("wheel down should scroll the panel")
	}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.helpOffset != 0 {
		t.Error("wheel up should scroll back")
	}
	m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: 4})
	if m.focus != focus {
		t.Error("a click behind the panel must be inert")
	}
}

// TestHelpOffsetSurvivesResize proves a shrink cannot leave the offset past the
// end of the newly shorter body.
func TestHelpOffsetSurvivesResize(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pressRune(m, '?')
	pressRune(m, 'G')
	if m.helpOffset == 0 {
		t.Fatal("test needs a scrolled panel")
	}

	m.Update(tea.WindowSizeMsg{Width: 100, Height: 60})
	total, visible := m.helpScrollBounds()
	if m.helpOffset > max(0, total-visible) {
		t.Errorf("offset %d out of range after resize (%d lines in %d rows)",
			m.helpOffset, total, visible)
	}
}

// TestHelpPanelFooterFollowsRebinds proves the panel's own footer — the sole
// mitigation for not capturing quit — cannot print a stale key.
func TestHelpPanelFooterFollowsRebinds(t *testing.T) {
	set := settings.Defaults()
	set.Keys["quit"] = []string{"Q"}
	m, _, _ := testModel(demoSteps())
	m.set = set
	m.keys = keysFrom(set)
	sizeUp(m)
	m.helpOpen = true

	foot := footerText(m)
	if !strings.Contains(foot, "Q quit") {
		t.Errorf("panel footer should advertise the rebound quit key, got %q", foot)
	}
}
