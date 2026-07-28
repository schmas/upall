package tui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/settings"
)

// helpModel builds a sized model with the panel already open.
func helpModel(t *testing.T) *Model {
	t.Helper()
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.helpOpen = true
	return m
}

// rebindModel builds a sized model whose settings rebind one action.
func rebindModel(t *testing.T, action string, keys ...string) *Model {
	t.Helper()
	set := settings.Defaults()
	set.Keys[action] = keys
	m, _, _ := testModel(demoSteps())
	m.set = set
	m.keys = keysFrom(set)
	sizeUp(m)
	return m
}

// helpText renders the panel's body as plain, whitespace-collapsed lines.
func helpText(m *Model) string {
	body, _ := m.helpLayout(m.width, m.height)
	return strings.Join(strings.Fields(ansi.Strip(strings.Join(body, "\n"))), " ")
}

func TestFormatKeys(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"up", "k"}, "↑/k"},
		{[]string{"enter"}, "⏎"},
		{[]string{"shift+tab"}, "⇧tab"},
		{[]string{" "}, "space"},
		{[]string{"q", "ctrl+c"}, "q/ctrl+c"},
		{[]string{"g", "home"}, "g/home"},
		{nil, ""},
	}
	for _, tc := range cases {
		if got := formatKeys(tc.in); got != tc.want {
			t.Errorf("formatKeys(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestHelpSectionsCoverEveryBinding is the guard against a future keyMap field
// being added and silently never listed in the panel.
func TestHelpSectionsCoverEveryBinding(t *testing.T) {
	k := keysFrom(settings.Defaults())
	// Keyed on keys+desc rather than desc alone: two bindings sharing a
	// description would otherwise cover for each other and the guard would pass
	// for a binding that is in fact unlisted.
	type ident struct{ keys, desc string }
	listed := map[ident]int{}
	for _, s := range k.helpSections() {
		for _, r := range s.rows {
			listed[ident{r.keys, r.desc}]++
		}
	}
	v := reflect.ValueOf(k)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		b, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			t.Fatalf("keyMap field %s is not a key.Binding", name)
		}
		switch listed[ident{b.Help().Key, b.Help().Desc}] {
		case 1: // listed exactly once
		case 0:
			t.Errorf("binding %s (%q) is in no help section", name, b.Help().Desc)
		default:
			t.Errorf("binding %s (%q) is listed more than once", name, b.Help().Desc)
		}
	}
}

func TestHelpSectionsListModalKeys(t *testing.T) {
	secs := keysFrom(settings.Defaults()).helpSections()
	var prompts *helpSection
	for i := range secs {
		if secs[i].title == "Prompts" {
			prompts = &secs[i]
		}
	}
	if prompts == nil {
		t.Fatal("no Prompts section")
	}
	got := map[string]bool{}
	for _, r := range prompts.rows {
		got[r.keys] = true
	}
	for _, want := range []string{"esc", "y/n"} {
		if !got[want] {
			t.Errorf("Prompts section is missing %q (has %v)", want, got)
		}
	}
}

// TestHelpPanelReflectsRebinds is the feature's whole point: no key string in
// the panel may be hardcoded.
func TestHelpPanelReflectsRebinds(t *testing.T) {
	m := rebindModel(t, "quit", "Q")
	txt := helpText(m)
	if !strings.Contains(txt, "Q quit upall") {
		t.Errorf("rebound quit not shown, got %q", txt)
	}
	if strings.Contains(txt, "q/ctrl+c quit upall") {
		t.Error("panel still prints the default quit keys after a rebind")
	}

	m = rebindModel(t, "up", "w")
	if txt := helpText(m); !strings.Contains(txt, "w move up") {
		t.Errorf("rebound up not shown, got %q", txt)
	}
}

// TestHelpLayoutFitsFrame asserts containment against the FRAME, not against
// the panel's own reported width — the latter cannot fail.
func TestHelpLayoutFitsFrame(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	for _, f := range []struct{ w, h int }{{200, 50}, {80, 24}, {60, 20}, {34, 10}, {20, 6}} {
		_, r := m.helpLayout(f.w, f.h)
		if r.w > f.w {
			t.Errorf("%dx%d: panel width %d exceeds the frame", f.w, f.h, r.w)
		}
		if r.h > f.h {
			t.Errorf("%dx%d: panel height %d exceeds the frame", f.w, f.h, r.h)
		}
		if r.x < 0 || r.y < 0 || r.x+r.w > f.w || r.y+r.h > f.h {
			t.Errorf("%dx%d: panel rect %+v falls outside the frame", f.w, f.h, r)
		}
	}
}

func TestHelpCountIndicator(t *testing.T) {
	m := helpModel(t)
	if got := m.helpCount(0, 10, 42, 10); got != "1–10 of 42" {
		t.Errorf("indicator = %q", got)
	}
	if got := m.helpCount(32, 42, 42, 10); got != "33–42 of 42" {
		t.Errorf("indicator at the end = %q", got)
	}
	if got := m.helpCount(0, 8, 8, 20); got != "" {
		t.Errorf("indicator should be suppressed when everything fits, got %q", got)
	}
	if got := m.helpCount(0, 0, 5, 0); got != "" {
		t.Errorf("indicator should be suppressed with no viewport, got %q", got)
	}
}

func TestHelpScrollbarThumbTracksOffset(t *testing.T) {
	m := helpModel(t)
	total, visible := m.helpScrollBounds()
	if total <= visible {
		t.Fatalf("test needs a scrollable panel: %d lines in %d rows", total, visible)
	}
	if scrollbarThumb(visible, total, visible, 0) == nil {
		t.Error("scrollable panel should carry a thumb")
	}
	if scrollbarThumb(visible, visible, visible, 0) != nil {
		t.Error("a body that fits should carry no thumb")
	}
}

// TestHelpOverlayShortBodyDoesNotPanic drives the REAL helpOverlay with a
// one-line body and a stale out-of-range offset. A panic here would propagate
// out of View and cancel a live run.
func TestHelpOverlayShortBodyDoesNotPanic(t *testing.T) {
	frame := strings.Repeat("0123456789012345678901234567890123456789\n", 20)
	m := helpModel(t)
	m.helpQuery = "zzzznope" // filters the body down to the single no-match line
	m.helpOffset = 99        // an offset the shorter body cannot honour

	out := m.helpOverlay(frame)
	assertSameShape(t, frame, out, "one-line body")
	if !strings.Contains(ansi.Strip(out), "no match") {
		t.Error("the no-match line did not render")
	}
}

// TestHelpOverlayPreservesFrameShape is Phase 1's invariant applied to the real
// rendered frame, asserted against that frame rather than m.width/m.height (the
// frame is taller than a short terminal and its footer line is 2 cells narrower).
func TestHelpOverlayPreservesFrameShape(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{120, 40}, {80, 24}, {60, 15}, {20, 6}} {
		m, _, _ := testModel(demoSteps())
		m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		base := m.viewWithoutHelp()
		if got := overlayedView(m); ansi.Strip(got) == ansi.Strip(base) {
			t.Errorf("%dx%d: panel did not change the frame", sz.w, sz.h)
		}
		assertSameShape(t, base, overlayedView(m), "composited frame")
	}
}

// viewWithoutHelp renders the frame the panel will be drawn onto.
func (m *Model) viewWithoutHelp() string {
	open := m.helpOpen
	m.helpOpen = false
	defer func() { m.helpOpen = open }()
	return m.View()
}

func overlayedView(m *Model) string {
	m.helpOpen = true
	return m.View()
}
