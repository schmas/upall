package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// render feeds a raw byte stream to a fresh emulator sized w×h and returns the
// rendered pane (scrollback + screen, trailing blanks trimmed). No drain
// goroutine is needed because these streams emit no terminal queries.
//
// Streams use \r\n line endings on purpose: captured output reaches the emulator
// through a pty whose ONLCR post-processing turns every \n into \r\n, so \r\n is
// exactly what a real terminal receives. A bare \n is line-feed only (moves down,
// keeps the column), which does not happen on the real capture path.
func render(w, h int, stream string) string {
	e := vt.NewEmulator(w, h)
	e.SetScrollbackSize(scrollbackCap)
	e.WriteString(stream)
	return renderTerm(e)
}

// TestRenderCarriageReturnOverwrite: a \r progress redraw collapses to its final
// on-screen frame; earlier frames do not linger.
func TestRenderCarriageReturnOverwrite(t *testing.T) {
	got := render(40, 6, "downloading 10%\rdownloading 100%")
	if got != "downloading 100%" {
		t.Fatalf("render = %q, want %q", got, "downloading 100%")
	}
}

// TestRenderCursorUpRedraw: a multi-line progress block rewritten via cursor-up
// (cargo/npm style) shows only the final redraw, with no ghost of the first pass.
func TestRenderCursorUpRedraw(t *testing.T) {
	stream := "line 1\r\nline 2\r\nline 3\r\n" + // first pass
		"\x1b[3A" + // cursor up 3 to the top of the block
		"new 1\x1b[K\r\nnew 2\x1b[K\r\nnew 3\x1b[K\r\n" // rewrite each line
	got := render(40, 6, stream)
	for _, want := range []string{"new 1", "new 2", "new 3"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render missing %q; got %q", want, got)
		}
	}
	if strings.Contains(got, "line 1") || strings.Contains(got, "line 2") {
		t.Fatalf("stale first-pass lines survived redraw: %q", got)
	}
}

// TestRenderEraseLineAndScreen: erase-in-line (\x1b[K) and erase-in-display
// (\x1b[2J) clear content as a real terminal would.
func TestRenderEraseLineAndScreen(t *testing.T) {
	// Erase-in-line: rewrite after \r + \x1b[K drops the longer prior text.
	if got := render(40, 4, "AAAAAAAA\r\x1b[KBB"); got != "BB" {
		t.Fatalf("erase-line render = %q, want %q", got, "BB")
	}
	// Erase-in-display + home: the visible screen is cleared. (\x1b[2J scrolls
	// the old rows into scrollback rather than discarding them, so assert on the
	// screen via Render, not the full scrollback+screen pane.)
	e := vt.NewEmulator(40, 4)
	e.SetScrollbackSize(scrollbackCap)
	e.WriteString("GARBAGE\r\nMORE GARBAGE\r\n\x1b[2J\x1b[Hfresh")
	if screen := trimTrailingBlankLines(e.Render()); screen != "fresh" {
		t.Fatalf("erase-screen visible screen = %q, want %q", screen, "fresh")
	}
}

// TestRenderSGRColorPreserved: an SGR color sequence survives into the rendered
// output (the whole point of dropping the sanitizer).
func TestRenderSGRColorPreserved(t *testing.T) {
	got := render(40, 4, "\x1b[31mERR\x1b[0m ok")
	if !strings.Contains(got, "ERR") || !strings.Contains(got, "ok") {
		t.Fatalf("text lost: %q", got)
	}
	if !strings.Contains(got, "\x1b[31m") {
		t.Fatalf("red SGR did not survive: %q", got)
	}
}

// TestRenderScrollbackBounded: writing far more lines than the cap evicts the
// oldest and holds scrollback at scrollbackCap — memory stays bounded.
func TestRenderScrollbackBounded(t *testing.T) {
	e := vt.NewEmulator(40, 5)
	e.SetScrollbackSize(scrollbackCap)
	for i := 0; i < scrollbackCap+300; i++ {
		e.WriteString(fmt.Sprintf("row %d\r\n", i))
	}
	if sb := e.ScrollbackLen(); sb != scrollbackCap {
		t.Fatalf("scrollback len = %d, want cap %d", sb, scrollbackCap)
	}
	// The oldest lines were evicted; the newest still render.
	got := renderTerm(e)
	if strings.HasPrefix(got, "row 0\n") || strings.Contains(got, "\nrow 0\n") {
		t.Fatalf("oldest row should be evicted from scrollback")
	}
	if !strings.Contains(got, fmt.Sprintf("row %d", scrollbackCap+299)) {
		t.Fatalf("newest row missing from render")
	}
}

// TestRenderWideRunes: multibyte and CJK/emoji content renders without panic and
// stays within the pane width (the emulator is uniseg-backed).
func TestRenderWideRunes(t *testing.T) {
	const w = 20
	got := render(w, 4, "CJK 你好世界 😀 end\r\n")
	if !strings.Contains(got, "你好世界") {
		t.Fatalf("wide runes lost: %q", got)
	}
	for _, ln := range strings.Split(got, "\n") {
		if cw := ansi.StringWidth(ln); cw > w {
			t.Fatalf("line width %d exceeds pane %d: %q", cw, w, ln)
		}
	}
}

// TestRenderAKFixture: a representative nested ak-update stream (colored banner,
// \r spinner, colored status) renders cleanly — spinner collapses to its final
// frame, color survives, no raw CR leaks, widths fit. This is the automated gate
// for the Phase 3 NO_COLOR removal; a real `upall ak` run is a manual check.
func TestRenderAKFixture(t *testing.T) {
	const w = 60
	stream := "" +
		"\x1b[1;36m╭─ AgentKit Update ──────────────╮\x1b[0m\r\n" +
		"⠋ updating\r⠙ updating\r⠹ updating\r\x1b[32m✓\x1b[0m updated 3 plugins\r\n" +
		"\x1b[33mWARN\x1b[0m one skill pinned\r\n"
	got := render(w, 20, stream)
	if strings.ContainsRune(got, '\r') {
		t.Errorf("raw CR leaked: %q", got)
	}
	if strings.Contains(got, "⠋") || strings.Contains(got, "⠙") {
		t.Errorf("intermediate spinner frames should be overwritten: %q", got)
	}
	if !strings.Contains(got, "✓") || !strings.Contains(got, "updated 3 plugins") {
		t.Errorf("final spinner frame missing: %q", got)
	}
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("expected SGR color to survive: %q", got)
	}
	for _, ln := range strings.Split(got, "\n") {
		if cw := ansi.StringWidth(ln); cw > w {
			t.Errorf("line width %d exceeds %d: %q", cw, w, ln)
		}
	}
}
