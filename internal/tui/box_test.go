package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/schmas/upall/internal/settings"
)

func testStyles() styles { return buildStyles(settings.Defaults().Theme) }

// forceColor pins a deterministic color profile so border-color assertions are
// meaningful in a non-TTY test process, restoring the prior profile after.
func forceColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.DefaultRenderer().ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func lines(s string) []string { return strings.Split(s, "\n") }

func TestTitledBoxRendersBorderTitleCount(t *testing.T) {
	const w, h = 24, 5
	out := titledBox("Steps", "2/3", "body one\nbody two", true, w, h, testStyles())
	plain := ansi.Strip(out)

	for _, r := range []string{boxTopLeft, boxTopRight, boxBottomLeft, boxBottomRight} {
		if !strings.Contains(plain, r) {
			t.Errorf("box missing corner %q:\n%s", r, plain)
		}
	}
	if !strings.Contains(lines(plain)[0], "Steps") || !strings.Contains(lines(plain)[0], "2/3") {
		t.Errorf("top border should carry title + count: %q", lines(plain)[0])
	}
	ls := lines(plain)
	if len(ls) != h {
		t.Fatalf("box height = %d lines, want %d", len(ls), h)
	}
	for i, ln := range ls {
		if got := ansi.StringWidth(ln); got != w {
			t.Errorf("line %d width = %d, want %d: %q", i, got, w, ln)
		}
	}
}

// TestTitledBoxFocusOnlyChangesColor proves focused vs unfocused differ only in
// border color: the stripped text is identical, and the color codes differ.
func TestTitledBoxFocusOnlyChangesColor(t *testing.T) {
	forceColor(t)
	st := testStyles()
	focused := titledBox("T", "", "hi", true, 12, 3, st)
	unfocused := titledBox("T", "", "hi", false, 12, 3, st)

	if ansi.Strip(focused) != ansi.Strip(unfocused) {
		t.Error("focused and unfocused should have identical text, only color differs")
	}
	if focused == unfocused {
		t.Error("focused and unfocused should differ (border color)")
	}
	if !strings.Contains(focused, "38;5;42") {
		t.Errorf("focused border should use accent 42:\n%q", focused)
	}
	if !strings.Contains(unfocused, "38;5;240") {
		t.Errorf("unfocused border should use dim 240:\n%q", unfocused)
	}
}

// TestTitledBoxTitleTruncates proves a too-long title never breaks the border at
// a narrow width: the top row stays exactly w wide and keeps both corners.
func TestTitledBoxTitleTruncates(t *testing.T) {
	const w = 10
	out := ansi.Strip(titledBox("a-very-long-title", "99/99", "x", false, w, 3, testStyles()))
	top := lines(out)[0]
	if ansi.StringWidth(top) != w {
		t.Errorf("top border width = %d, want %d: %q", ansi.StringWidth(top), w, top)
	}
	if !strings.HasPrefix(top, boxTopLeft) || !strings.HasSuffix(top, boxTopRight) {
		t.Errorf("truncation broke the border corners: %q", top)
	}
}
