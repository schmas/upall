package tui

import (
	"reflect"
	"strconv"
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
	v := m.helpLayout(m.width, m.height)
	return strings.Join(strings.Fields(ansi.Strip(strings.Join(v.body, "\n"))), " ")
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
		r := m.helpLayout(f.w, f.h).rect
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

// TestHelpCountIndicator covers the border readout: always present, counted in
// bindings, and showing what the filter matched.
func TestHelpCountIndicator(t *testing.T) {
	m := helpModel(t)
	total := countRows(m.keys.helpSections())

	if got, want := m.helpCount(helpView{shown: total, total: total}), strconv.Itoa(total); got != want {
		t.Errorf("unfiltered indicator = %q, want %q", got, want)
	}
	m.helpQuery = "retry"
	if got := m.helpCount(helpView{shown: 3, total: total}); got != "/retry · 3 of "+strconv.Itoa(total) {
		t.Errorf("filtered indicator = %q", got)
	}
	m.helpQuery = strings.Repeat("z", 40)
	if got := m.helpCount(helpView{shown: 0, total: total}); !strings.Contains(got, "…") ||
		ansi.StringWidth(got) > helpQueryShown+18 {
		t.Errorf("a long query should be display-truncated, got %q", got)
	}
}

// TestHelpCountIsAlwaysVisible proves the count reaches the rendered border in
// both states — the earlier version suppressed it whenever the list fit, which
// is most terminals.
func TestHelpCountIsAlwaysVisible(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 60}) // tall enough to fit everything
	pressRune(m, '?')
	total := countRows(m.keys.helpSections())

	if view := ansi.Strip(m.View()); !strings.Contains(view, strconv.Itoa(total)) {
		t.Errorf("border is missing the %d-binding count:\n%s", total, view)
	}
	typeQuery(m, "retry")
	if view := ansi.Strip(m.View()); !strings.Contains(view, "of "+strconv.Itoa(total)) {
		t.Errorf("border is missing the filtered count:\n%s", view)
	}
}

// TestHelpFilterDoesNotResizePanel is the reason geometry is measured off the
// unfiltered list: a box that shrank as you typed would be unreadable.
func TestHelpFilterDoesNotResizePanel(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	pressRune(m, '?')
	want := m.helpLayout(m.width, m.height).rect

	for _, q := range []string{"r", "retry", "zzzznope"} {
		m.helpQuery = q
		if got := m.helpLayout(m.width, m.height).rect; got != want {
			t.Errorf("query %q moved or resized the panel: %+v, want %+v", q, got, want)
		}
	}
}

// TestHelpPanelIsWide keeps the panel a panel: it should take a real share of a
// wide frame rather than shrinking to its longest description.
func TestHelpPanelIsWide(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	for _, f := range []struct{ w, min int }{{200, helpMaxW - 1}, {120, 80}, {96, 66}} {
		if got := m.helpLayout(f.w, 40).rect.w; got < f.min {
			t.Errorf("%d-col frame: panel width %d, want at least %d", f.w, got, f.min)
		}
	}
	// …but it still leaves the panes visible around it.
	if r := m.helpLayout(120, 40).rect; r.x < helpMargin {
		t.Errorf("panel should leave a margin, got x=%d w=%d", r.x, r.w)
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
