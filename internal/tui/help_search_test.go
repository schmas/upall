package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/settings"
)

// typeQuery opens the filter prompt and types s into it.
func typeQuery(m *Model, s string) {
	pressRune(m, '/')
	for _, r := range s {
		pressRune(m, r)
	}
}

func searchModel(t *testing.T) *Model {
	t.Helper()
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')
	return m
}

func TestHelpFilterMatchesDescriptionKeysAndRawNames(t *testing.T) {
	secs := keysFrom(settings.Defaults()).helpSections()
	cases := []struct{ query, wantDesc string }{
		{"resume", "resume from the aborted step"}, // description
		{"tab", "focus the next pane"},             // displayed key text
		{"enter", "start the run"},                 // raw key name behind "⏎"
		{"CTRL", "quit upall"},                     // case-insensitive
	}
	for _, tc := range cases {
		found := false
		for _, s := range filterSections(secs, tc.query) {
			for _, r := range s.rows {
				if r.desc == tc.wantDesc {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("query %q did not find %q", tc.query, tc.wantDesc)
		}
	}
}

func TestHelpFilterDropsEmptySections(t *testing.T) {
	secs := keysFrom(settings.Defaults()).helpSections()
	got := filterSections(secs, "resume")
	if len(got) != 1 || got[0].title != "Run Control" || len(got[0].rows) != 1 {
		t.Errorf("filter kept %d sections, want just Run Control with one row: %+v", len(got), got)
	}
	if n := len(filterSections(secs, "")); n != len(secs) {
		t.Errorf("empty query is not the identity: %d sections, want %d", n, len(secs))
	}
}

func TestHelpNoMatchRendersWithoutPanic(t *testing.T) {
	m := searchModel(t)
	typeQuery(m, "zzzznope")
	view := m.View()
	if !strings.Contains(ansi.Strip(view), `no match for "zzzznope"`) {
		t.Errorf("no-match line missing from the panel:\n%s", ansi.Strip(view))
	}
	body, _ := m.helpLayout(m.width, m.height)
	if len(body) != 1 {
		t.Errorf("no-match body = %d lines, want 1", len(body))
	}
}

// TestHelpSearchEscContract is the single esc contract: one press clears the
// query and leaves the prompt; a second closes the panel.
func TestHelpSearchEscContract(t *testing.T) {
	m := searchModel(t)
	typeQuery(m, "retry")
	if !m.helpSearching || m.helpQuery != "retry" {
		t.Fatalf("prompt state = searching:%v query:%q", m.helpSearching, m.helpQuery)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpSearching || m.helpQuery != "" {
		t.Errorf("one esc should clear and exit: searching:%v query:%q", m.helpSearching, m.helpQuery)
	}
	if !m.helpOpen {
		t.Error("the first esc must not also close the panel")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.helpOpen {
		t.Error("the second esc should close the panel")
	}
}

func TestHelpSearchEnterKeepsFilter(t *testing.T) {
	m := searchModel(t)
	typeQuery(m, "retry")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.helpSearching {
		t.Error("enter should leave the prompt")
	}
	if m.helpQuery != "retry" {
		t.Errorf("enter should keep the filter, query = %q", m.helpQuery)
	}
	if !strings.Contains(ansi.Strip(m.View()), "/retry") {
		t.Error("a kept filter should still show in the border")
	}

	// Reopening starts clean.
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pressRune(m, '?')
	if m.helpQuery != "" {
		t.Errorf("reopened panel is still filtered by %q", m.helpQuery)
	}
}

// TestHelpSearchCapturesQuitKey covers the one place the quit binding is
// captured: 'q' is text in the prompt, while a non-printable chord still quits.
func TestHelpSearchCapturesQuitKey(t *testing.T) {
	m := searchModel(t)
	typeQuery(m, "q")
	if m.quitting {
		t.Fatal("'q' in the filter prompt must not quit")
	}
	if m.helpQuery != "q" {
		t.Errorf("query = %q, want \"q\"", m.helpQuery)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || !m.quitting {
		t.Error("ctrl+c must still quit from the filter prompt")
	}
}

func TestHelpSearchRespectsRebindableQuitChord(t *testing.T) {
	set := settings.Defaults()
	set.Keys["quit"] = []string{"ctrl+q"}
	m, _, _ := testModel(demoSteps())
	m.set = set
	m.keys = keysFrom(set)
	sizeUp(m)
	pressRune(m, '?')
	typeQuery(m, "q")

	if m.quitting {
		t.Fatal("'q' is text even when it is no longer the quit key")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ}); cmd == nil || !m.quitting {
		t.Error("the rebound non-printable quit chord must still quit")
	}
}

// TestHelpSearchSanitizesInput proves neither a pasted escape sequence nor an
// unbounded query can reach the renderer.
func TestHelpSearchSanitizesInput(t *testing.T) {
	m := searchModel(t)
	pressRune(m, '/')
	// Bracketed paste arrives as one KeyRunes carrying every rune, ESC included.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ok\x1b[2J\x07more")})
	if strings.ContainsAny(m.helpQuery, "\x1b\x07") {
		t.Errorf("control runes survived into the query: %q", m.helpQuery)
	}
	// The composited frame legitimately carries the compositor's own splice
	// resets, so the assertion targets the pasted payload itself.
	view := m.View()
	for _, bad := range []string{"\x1b[2J", "\x07"} {
		if strings.Contains(view, bad) {
			t.Errorf("pasted control sequence %q reached the rendered frame", bad)
		}
	}
	// The printable remainder stays as inert text: without its ESC, "[2J" is
	// just four characters.
	if m.helpQuery != "ok[2Jmore" {
		t.Errorf("query = %q, want the printable remainder %q", m.helpQuery, "ok[2Jmore")
	}
	if !strings.Contains(ansi.Strip(view), "no match for") {
		t.Error("the sanitized query should still drive the filter")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	pressRune(m, '/')
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("z", 100))})
	if n := len([]rune(m.helpQuery)); n != helpQueryMax {
		t.Errorf("query length = %d, want it capped at %d", n, helpQueryMax)
	}
	if !strings.Contains(ansi.Strip(m.View()), "/"+strings.Repeat("z", 10)) {
		t.Error("a capped query should still be visible in the border")
	}
}

func TestHelpSearchBackspaceAndOffsetReset(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pressRune(m, '?')
	pressRune(m, 'G')
	if m.helpOffset == 0 {
		t.Fatal("test needs a scrolled panel")
	}

	typeQuery(m, "re")
	if m.helpOffset != 0 {
		t.Errorf("a query change should reset the offset, got %d", m.helpOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.helpQuery != "r" {
		t.Errorf("backspace: query = %q, want \"r\"", m.helpQuery)
	}
	for i := 0; i < 3; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if m.helpQuery != "" {
		t.Errorf("backspace past empty: query = %q", m.helpQuery)
	}
}

// TestHelpSearchScrollsWithoutResettingOffset proves the filtered list is still
// reachable past the first viewport: the arrow keys scroll inside the prompt,
// and only a query change snaps back to the top.
func TestHelpSearchScrollsWithoutResettingOffset(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	pressRune(m, '?')
	typeQuery(m, "e") // matches far more rows than the viewport holds

	total, visible := m.helpScrollBounds()
	if total <= visible {
		t.Fatalf("test needs a scrollable filtered body: %d lines in %d rows", total, visible)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m.helpOffset != 1 {
		t.Fatalf("down inside the prompt: offset = %d, want 1", m.helpOffset)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.helpOffset <= 1 {
		t.Errorf("pgdown inside the prompt did not scroll: offset = %d", m.helpOffset)
	}
	// A key that leaves the query alone must not scroll back to the top.
	scrolled := m.helpOffset
	m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if m.helpOffset != scrolled {
		t.Errorf("an unrelated key reset the offset: %d, want %d", m.helpOffset, scrolled)
	}
	// Editing the query does reset it.
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.helpOffset != 0 {
		t.Errorf("a query change should reset the offset, got %d", m.helpOffset)
	}
}

// TestHelpPanelIdleStopAndTypeDoNotDismiss proves the panel only steps aside
// for keys that actually do something: both are no-ops with no run in flight.
func TestHelpPanelIdleStopAndTypeDoNotDismiss(t *testing.T) {
	for _, k := range []rune{'x', 'i'} {
		m, _, _ := testModel(demoSteps())
		sizeUp(m)
		pressRune(m, '?')
		pressRune(m, k)
		if !m.helpOpen {
			t.Errorf("idle %q closed the panel without doing anything", string(k))
		}
		if m.typing {
			t.Errorf("idle %q entered type mode", string(k))
		}
	}
}

// TestHelpSearchFooterStatesTheExtraPress proves the run-safety hints stay
// truthful while the prompt owns every printable key.
func TestHelpSearchFooterStatesTheExtraPress(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	startRunning(m)
	m.activeIdx = 0
	m.awaitInput = true
	pressRune(m, '?')
	pressRune(m, '/')

	foot := footerText(m)
	if !strings.Contains(foot, "esc i type password") {
		t.Errorf("prompt footer should spell out the escape, got %q", foot)
	}
	if !strings.Contains(foot, "⏎ done") || !strings.Contains(foot, "esc clear") {
		t.Errorf("prompt footer should say how to leave, got %q", foot)
	}
}
