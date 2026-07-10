package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/schmas/upall/internal/settings"
)

// styles holds the lipgloss styles derived from the user's [theme]. Building
// them once (in New) keeps colors configurable without re-parsing per render,
// and replaces the old package-level style vars so nothing is hardcoded.
type styles struct {
	accent  lipgloss.Color // focused pane border, selected row, progress fill
	dim     lipgloss.Color // unfocused border, separators, muted text
	success lipgloss.Color
	failure lipgloss.Color

	selected         lipgloss.Style // active filter tab / All-logs label, progress fill
	cursor           lipgloss.Style // the list-cursor row: a solid reverse-video bar
	muted            lipgloss.Style // dimmed / secondary text
	sep              lipgloss.Style // separators (── label ──)
	header           lipgloss.Style // header title text
	excluded         lipgloss.Style // pre-run excluded step (dim + strikethrough)
	selectedExcluded lipgloss.Style // cursor on an excluded step (dark green + strikethrough)
}

// buildStyles turns a Theme into ready-to-use lipgloss styles.
func buildStyles(t settings.Theme) styles {
	accent := lipgloss.Color(t.Accent)
	dim := lipgloss.Color(t.Dim)
	return styles{
		accent:   accent,
		dim:      dim,
		success:  lipgloss.Color(t.Success),
		failure:  lipgloss.Color(t.Failure),
		selected: lipgloss.NewStyle().Bold(true).Foreground(accent),
		// The list cursor is a reverse-video bar (accent becomes the background)
		// rather than accent-colored text: a green-on-green foreground was
		// invisible against the green ✓ glyphs and labels in the History pane.
		cursor:   lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(accent),
		muted:    lipgloss.NewStyle().Foreground(dim),
		sep:      lipgloss.NewStyle().Foreground(dim),
		header:   lipgloss.NewStyle().Bold(true),
		excluded: lipgloss.NewStyle().Foreground(dim).Strikethrough(true),
		// Cursor on an excluded step: keep the accent (so it still reads as the
		// selected row) but faint it to a darker green and strike it through, so
		// the exclusion is visible without having to move the cursor off it.
		selectedExcluded: lipgloss.NewStyle().Foreground(accent).Faint(true).Strikethrough(true),
	}
}
