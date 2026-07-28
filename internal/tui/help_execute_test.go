package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/settings"
)

// TestKeyMsgForRoundTrips is the guard on the hand-written reverse of
// tea.Key.String(): every key any default binding can name must convert back
// into an event that reports the same name, or enter would run the wrong
// action — or none.
func TestKeyMsgForRoundTrips(t *testing.T) {
	for action, keys := range settings.Defaults().Keys {
		for _, k := range keys {
			msg, ok := keyMsgFor(k)
			if !ok {
				t.Errorf("%s: no event for key %q", action, k)
				continue
			}
			if got := msg.String(); got != k {
				t.Errorf("%s: keyMsgFor(%q) reports %q", action, k, got)
			}
		}
	}
	// Chords a user might rebind onto, beyond the defaults' ctrl+c.
	for _, k := range []string{"ctrl+w", "ctrl+q", "ctrl+z", "shift+tab", "pgdown", " "} {
		msg, ok := keyMsgFor(k)
		if !ok || msg.String() != k {
			t.Errorf("keyMsgFor(%q) = %q, ok=%v", k, msg.String(), ok)
		}
	}
	if _, ok := keyMsgFor("y/n"); ok {
		t.Error("a literal prompt row's caption should not convert to a key")
	}
}

// selectRow moves the panel's cursor onto the binding with the given
// description, mirroring what the user does with ↑/↓.
func selectRow(t *testing.T, m *Model, desc string) {
	t.Helper()
	v := m.helpLayout(m.width, m.height)
	i := 0
	for _, s := range filterSections(m.keys.helpSections(), m.helpQuery) {
		for _, r := range s.rows {
			if r.desc == desc {
				m.helpCursor = i
				m.moveHelpCursor(0)
				return
			}
			i++
		}
	}
	t.Fatalf("no row described %q among %d", desc, v.shown)
}

// TestHelpEnterRunsSelectedBinding proves enter actually performs the action,
// through the same path as typing its key.
func TestHelpEnterRunsSelectedBinding(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	wrap := m.wrap
	pressRune(m, '?')
	selectRow(t, m, "toggle wrapping of history output")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.wrap == wrap {
		t.Error("enter did not run the wrap binding")
	}
	if m.helpOpen {
		t.Error("running a binding should close the panel")
	}
}

// TestHelpEnterRunsRebindableAction proves execution follows a rebind, since it
// dispatches the bound key rather than a hardcoded one.
func TestHelpEnterRunsRebindableAction(t *testing.T) {
	set := settings.Defaults()
	set.Keys["wrap"] = []string{"ctrl+w"}
	m, _, _ := testModel(demoSteps())
	m.set = set
	m.keys = keysFrom(set)
	sizeUp(m)
	wrap := m.wrap
	pressRune(m, '?')
	selectRow(t, m, "toggle wrapping of history output")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.wrap == wrap {
		t.Error("enter did not run the rebound wrap binding")
	}
}

// TestHelpEnterOnPromptRowIsInert covers the literal rows: they describe keys
// that belong to a prompt, so there is nothing to execute.
func TestHelpEnterOnPromptRowIsInert(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')
	selectRow(t, m, "confirm or decline a self-update")

	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.helpOpen {
		t.Error("enter on a prompt-key row should do nothing, not close the panel")
	}
	if m.quitting {
		t.Error("enter on a prompt-key row quit the program")
	}
}

// TestHelpEnterOnNoMatchIsInert guards the empty-list path.
func TestHelpEnterOnNoMatchIsInert(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')
	typeQuery(m, "zzzznope")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // enter leaves the prompt, keeping the filter
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // …and this one has nothing to run

	if !m.helpOpen || m.quitting {
		t.Error("enter with no matches should be inert")
	}
}

// TestHelpFilterFindsActionNames proves the name written in config.toml finds
// its row even when the description never contains that word.
func TestHelpFilterFindsActionNames(t *testing.T) {
	secs := keysFrom(settings.Defaults()).helpSections()
	for _, q := range []string{"retry", "restart", "self-update", "all-logs", "toggle"} {
		got := filterSections(secs, q)
		if countRows(got) == 0 {
			t.Errorf("query %q matched nothing", q)
		}
	}
}

// TestHelpCursorRowIsHighlighted proves the selection is actually drawn, not
// just tracked.
func TestHelpCursorRowIsHighlighted(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')
	first := m.View()
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if second := m.View(); first == second {
		t.Error("moving the cursor did not change the rendered panel")
	}
	// The cursor style is applied to stripped text, so the highlighted row's
	// description survives as plain content in the frame.
	if !strings.Contains(m.View(), "move down") {
		t.Error("the highlighted row lost its text")
	}
}
