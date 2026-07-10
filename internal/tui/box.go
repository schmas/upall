package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Rounded-border glyphs. Lip Gloss v1.1.0 has no border-title API, so the box is
// composed by hand to splice a title (and right-aligned count) into the top edge.
const (
	boxTopLeft     = "╭"
	boxTopRight    = "╮"
	boxBottomLeft  = "╰"
	boxBottomRight = "╯"
	boxHoriz       = "─"
	boxVert        = "│"
)

// titledBox renders body inside a rounded border whose top edge carries a left
// title and an optional right-aligned count. w and h are the OUTER dimensions
// (including the 1-cell border on each side); body is clipped and padded to the
// inner area so the box is always exactly w×h with no overflow. The border color
// is the theme accent when focused, else dim.
func titledBox(title, count, body string, focused bool, w, h int, st styles) string {
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	innerW, innerH := w-2, h-2

	col := st.dim
	if focused {
		col = st.accent
	}
	bs := lipgloss.NewStyle().Foreground(col)
	vert := bs.Render(boxVert)

	var b strings.Builder
	b.WriteString(bs.Render(boxTopLeft + horizFill(title, count, innerW) + boxTopRight))
	b.WriteByte('\n')
	for _, ln := range fitLines(body, innerW, innerH) {
		b.WriteString(vert)
		b.WriteString(ln)
		b.WriteString(vert)
		b.WriteByte('\n')
	}
	b.WriteString(bs.Render(boxBottomLeft + strings.Repeat(boxHoriz, innerW) + boxBottomRight))
	return b.String()
}

// horizFill builds the top border interior (between the corners), exactly innerW
// cells wide: "─ title ────…──── count ─". The title is truncated (then dropped)
// before the count so the border never overflows at narrow widths.
func horizFill(title, count string, innerW int) string {
	left, right := "", ""
	if title != "" {
		left = boxHoriz + " " + title + " "
	}
	if count != "" {
		right = " " + count + " " + boxHoriz
	}
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)

	if lw+rw > innerW {
		avail := innerW - rw - 3 // "─ " + trailing " " around the title
		if avail < 1 {
			left, lw = "", 0
			if rw > innerW { // even the count alone will not fit
				right, rw = "", 0
			}
		} else {
			left = boxHoriz + " " + ansi.Truncate(title, avail, "…") + " "
			lw = ansi.StringWidth(left)
		}
	}

	fill := innerW - lw - rw
	if fill < 0 {
		fill = 0
	}
	return left + strings.Repeat(boxHoriz, fill) + right
}

// fitLines clips body to exactly innerH lines of innerW visible width, padding
// short lines and blank rows with spaces so every body row fills the box.
func fitLines(body string, innerW, innerH int) []string {
	raw := strings.Split(body, "\n")
	out := make([]string, innerH)
	for i := 0; i < innerH; i++ {
		line := ""
		if i < len(raw) {
			line = raw[i]
		}
		line = ansi.Truncate(line, innerW, "")
		if pad := innerW - ansi.StringWidth(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		out[i] = line
	}
	return out
}
