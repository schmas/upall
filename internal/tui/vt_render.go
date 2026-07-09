package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

// renderTerm returns a step's emulator as styled text: its bounded scrollback
// followed by the visible screen. The emulator already wrapped every line to its
// own column count, so no hard-wrap is needed on this path (unlike the old
// per-line sanitize + ansi.Hardwrap). uv.Line.Render and Emulator.Render both
// emit SGR styling, so color, \r overwrite, and cursor redraws survive intact.
//
// Emulator.Render pads the screen to its full height with blank rows; those
// trailing blanks are trimmed so a short step shows no empty gap and "follow"
// bottoms out on the last real line (matching the old ring feel).
func renderTerm(e *vt.Emulator) string {
	var b strings.Builder
	sb := e.Scrollback()
	for i := 0; i < sb.Len(); i++ {
		b.WriteString(sb.Line(i).Render())
		b.WriteByte('\n')
	}
	b.WriteString(e.Render())
	return trimTrailingBlankLines(b.String())
}

// trimTrailingBlankLines drops trailing lines that carry no printable content
// (ansi.StringWidth 0), i.e. the emulator's screen padding.
func trimTrailingBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	end := len(lines)
	for end > 0 && ansi.StringWidth(lines[end-1]) == 0 {
		end--
	}
	return strings.Join(lines[:end], "\n")
}

// resetTerm returns an emulator to a clean slate in place: it clears the visible
// screen, homes the cursor, and empties the scrollback. Resetting in place (vs
// creating a fresh emulator) keeps the emulator's long-lived drain goroutine and
// never writes the emulator's closed flag, so retry/re-run stays race-free.
func resetTerm(e *vt.Emulator) {
	e.WriteString("\x1b[2J\x1b[H")
	e.ClearScrollback()
}
