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

// promptTailLine returns the last non-blank rendered line of an emulator's
// visible screen, with styling and trailing spaces stripped — the line a
// program's prompt sits on while it waits for input. It reads only the visible
// screen (not scrollback): a prompt is on the cursor's line, which is always
// visible.
func promptTailLine(e *vt.Emulator) string {
	lines := strings.Split(trimTrailingBlankLines(e.Render()), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimRight(ansi.Strip(lines[len(lines)-1]), " ")
}

// looksLikePasswordPrompt matches a trailing line that asks for a password or
// passphrase (sudo, ssh, an ssh key). It is deliberately narrow — the line must
// end in ':' and mention password/passphrase — so ordinary output never trips
// the "press i to type" hint. The match is only a hint anyway: keystrokes are
// never auto-forwarded, so a miss or a false positive is harmless.
func looksLikePasswordPrompt(line string) bool {
	if !strings.HasSuffix(line, ":") {
		return false
	}
	l := strings.ToLower(line)
	return strings.Contains(l, "password") || strings.Contains(l, "passphrase")
}

// resetTerm returns an emulator to a clean slate in place: it clears the visible
// screen, homes the cursor, and empties the scrollback. Resetting in place (vs
// creating a fresh emulator) keeps the emulator's long-lived drain goroutine and
// never writes the emulator's closed flag, so retry/re-run stays race-free.
func resetTerm(e *vt.Emulator) {
	e.WriteString("\x1b[2J\x1b[H")
	e.ClearScrollback()
}
