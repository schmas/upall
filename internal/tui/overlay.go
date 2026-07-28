package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// spliceReset closes both SGR state and any OSC 8 hyperlink open at a cut, so
// the panel can never inherit the base line's pen or link, and the base's
// right-hand segment cannot inherit the panel's. \x1b[0m alone does not close a
// hyperlink, which is why the OSC 8 terminator is part of the same constant.
const spliceReset = "\x1b[0m\x1b]8;;\x1b\\"

// overlay writes panel's lines onto base starting at cell (x, y), returning a
// frame with the SAME shape as base: same line count, and the same visible
// width on every line. That invariant is stated against base rather than the
// terminal size on purpose — the rendered frame is not rectangular (the footer
// line is 2 cells narrower) and at small heights it is taller than the terminal
// (resize floors the body height at 3 rows).
//
// Rows and columns outside base are clipped, never grown, so an oversized panel
// or an out-of-range origin cannot reshape the frame.
func overlay(base, panel string, x, y int) string {
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	baseLines := strings.Split(base, "\n")
	panelLines := strings.Split(panel, "\n")

	panelW := 0
	for _, pl := range panelLines {
		if w := ansi.StringWidth(pl); w > panelW {
			panelW = w
		}
	}

	for i, pl := range panelLines {
		row := y + i
		if row >= len(baseLines) {
			break
		}
		line := baseLines[row]
		baseW := ansi.StringWidth(line)
		if x >= baseW {
			continue // the whole panel row falls past this line's right edge
		}
		// A short panel row is padded to the panel's box width so the base's
		// right-hand segment lands back at the column it came from.
		if pad := panelW - ansi.StringWidth(pl); pad > 0 {
			pl += strings.Repeat(" ", pad)
		}

		left := ansi.Truncate(line, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(line, x+panelW, "")

		out := left + spliceReset + pl + spliceReset + right

		// Width repair, load-bearing rather than defensive: the two upstream
		// cutters disagree at a grapheme straddling the cut. Truncate DROPS a
		// cluster whose end crosses it while TruncateLeft KEEPS it, so a
		// double-width glyph sitting on either splice boundary leaves the
		// composed line a cell short or a cell long. A single overlong line
		// soft-wraps and shears every row below it for the rest of the session.
		if w := ansi.StringWidth(out); w != baseW {
			if w > baseW {
				out = ansi.Truncate(out, baseW, "")
			}
			if pad := baseW - ansi.StringWidth(out); pad > 0 {
				out += strings.Repeat(" ", pad)
			}
		}
		baseLines[row] = out
	}
	return strings.Join(baseLines, "\n")
}
