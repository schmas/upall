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
	out := titledBox("Steps", "2/3", "body one\nbody two", true, w, h, testStyles(), nil)
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
	focused := titledBox("T", "", "hi", true, 12, 3, st, nil)
	unfocused := titledBox("T", "", "hi", false, 12, 3, st, nil)

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
	out := ansi.Strip(titledBox("a-very-long-title", "99/99", "x", false, w, 3, testStyles(), nil))
	top := lines(out)[0]
	if ansi.StringWidth(top) != w {
		t.Errorf("top border width = %d, want %d: %q", ansi.StringWidth(top), w, top)
	}
	if !strings.HasPrefix(top, boxTopLeft) || !strings.HasSuffix(top, boxTopRight) {
		t.Errorf("truncation broke the border corners: %q", top)
	}
}

// TestScrollbarThumbNilWhenContentFits proves no thumb is drawn once all
// content fits the track: total <= visible must never index into the border.
func TestScrollbarThumbNilWhenContentFits(t *testing.T) {
	if thumb := scrollbarThumb(10, 10, 10, 0); thumb != nil {
		t.Errorf("total == visible should return nil, got %v", thumb)
	}
	if thumb := scrollbarThumb(10, 5, 10, 0); thumb != nil {
		t.Errorf("total < visible should return nil, got %v", thumb)
	}
	if thumb := scrollbarThumb(0, 100, 10, 0); thumb != nil {
		t.Errorf("zero track height should return nil, got %v", thumb)
	}
}

// TestScrollbarThumbClampsNegativeOffset proves an out-of-range negative
// offset (never produced by viewport.Model, whose YOffset is always clamped
// to >= 0, but not guaranteed by scrollbarThumb's own signature) clamps to the
// top row instead of indexing out of bounds.
func TestScrollbarThumbClampsNegativeOffset(t *testing.T) {
	thumb := scrollbarThumb(10, 100, 10, -50)
	if thumb == nil || !thumb[0] {
		t.Errorf("negative offset should clamp the thumb to row 0: %v", thumb)
	}
}

// TestScrollbarThumbTracksPosition proves the thumb sits at the top when
// offset is 0, at the bottom when offset is maxed, and always stays within
// the track bounds with a size proportional to visible/total.
func TestScrollbarThumbTracksPosition(t *testing.T) {
	const trackH, total, visible = 10, 100, 10
	top := scrollbarThumb(trackH, total, visible, 0)
	if top == nil || !top[0] {
		t.Fatalf("offset 0 should start the thumb at row 0: %v", top)
	}
	maxOffset := total - visible
	bottom := scrollbarThumb(trackH, total, visible, maxOffset)
	if bottom == nil || !bottom[trackH-1] {
		t.Fatalf("max offset should end the thumb at the last row: %v", bottom)
	}
	mid := scrollbarThumb(trackH, total, visible, maxOffset/2)
	if mid == nil {
		t.Fatal("mid-scroll should return a thumb")
	}
	for _, thumb := range [][]bool{top, bottom, mid} {
		n := 0
		for _, on := range thumb {
			if on {
				n++
			}
		}
		if n < 1 || n > trackH {
			t.Errorf("thumb size %d out of track bounds [1,%d]: %v", n, trackH, thumb)
		}
	}
}

// TestTitledBoxThumbRendersOnRightBorderOnly proves the thumb rows swap the
// heavy boxThumb glyph in on the right border while the left border and
// non-thumb rows keep the plain boxVert bar.
func TestTitledBoxThumbRendersOnRightBorderOnly(t *testing.T) {
	const w, h = 12, 6 // innerH = 4
	thumb := []bool{false, true, true, false}
	out := ansi.Strip(titledBox("Output", "4L", "a\nb\nc\nd", true, w, h, testStyles(), thumb))
	ls := lines(out)
	for i, want := range thumb {
		row := ls[i+1] // row 0 is the top border
		left, right := string([]rune(row)[0]), string([]rune(row)[len([]rune(row))-1])
		if left != boxVert {
			t.Errorf("row %d left border = %q, want plain %q: %q", i, left, boxVert, row)
		}
		wantRight := boxVert
		if want {
			wantRight = boxThumb
		}
		if right != wantRight {
			t.Errorf("row %d right border = %q, want %q: %q", i, right, wantRight, row)
		}
	}
}
