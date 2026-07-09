package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
)

// View renders the three-pane dashboard: a header bar, the Steps/History/Output
// panes, and a context-sensitive footer. On quit it returns empty so the alt
// screen is left clean; Run then prints the summary to the normal buffer.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "starting upall…"
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.renderHeaderBar(), m.renderBody(), m.renderFooterBar())
}

// renderBody composes the three panes per layout: wide puts Steps over History
// in a left column with Output filling the right; narrow stacks all three.
func (m *Model) renderBody() string {
	steps := m.renderStepsPane()
	hist := m.renderHistoryPane()
	out := m.renderOutputPane()
	if m.wide {
		left := lipgloss.JoinVertical(lipgloss.Left, steps, hist)
		return lipgloss.JoinHorizontal(lipgloss.Top, left, out)
	}
	return lipgloss.JoinVertical(lipgloss.Left, steps, out, hist)
}

// renderHeaderBar shows the app name + elapsed on the left and the progress
// (N/M done, bar, %, run state) on the right, inside a full-width bordered bar.
func (m *Model) renderHeaderBar() string {
	innerW := m.width - 2
	if innerW < 1 {
		innerW = 1
	}
	// The elapsed timer ticks live while a run is in flight and freezes at the
	// moment the run went idle (totalEnd). Before the first run it reads 0s.
	elapsed := "0s"
	if !m.totalStart.IsZero() {
		end := m.totalEnd
		if end.IsZero() {
			end = time.Now()
		}
		elapsed = engine.Hms(end.Sub(m.totalStart))
	}
	left := m.st.header.Render("UPALL") + "  " + m.st.muted.Render(elapsed)

	done, total := m.doneCount(), m.includedCount()
	pct := 0
	if total > 0 {
		pct = done * 100 / total
	}
	state := "idle"
	if m.running {
		state = "running"
	}
	right := fmt.Sprintf("%d/%d %s %3d%% %s", done, total, m.progressBar(barWidth), pct, state)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.st.dim).
		Width(innerW).
		Render(padBetween(left, right, innerW))
}

// renderStepsPane draws the Steps box: the filter-tab row, the All-logs row,
// then one row per filter-visible step. The count shows done/included.
func (m *Model) renderStepsPane() string {
	rows := []string{m.filterTabs(), m.allLogsRow()}
	for _, i := range m.visibleStepIndices() {
		rows = append(rows, m.stepRow(i))
	}
	count := fmt.Sprintf("%d/%d", m.doneCount(), m.includedCount())
	return titledBox("Steps", count, strings.Join(rows, "\n"),
		m.focus == FocusSteps, m.stepsRect.w, m.stepsRect.h, m.st)
}

// filterTabs renders the All·Pending·Done tabs with the active one highlighted.
func (m *Model) filterTabs() string {
	names := [...]string{"All", "Pending", "Done"}
	parts := make([]string, len(names))
	for i, n := range names {
		if stepFilter(i) == m.filter {
			parts[i] = m.st.selected.Render(n)
		} else {
			parts[i] = m.st.muted.Render(n)
		}
	}
	return strings.Join(parts, m.st.muted.Render("·"))
}

// renderHistoryPane draws the scanned runs newest-first as expandable rows.
func (m *Model) renderHistoryPane() string {
	focused := m.focus == FocusHistory
	rows := m.histRows()
	var lines []string
	if len(rows) == 0 {
		lines = []string{m.st.muted.Render("no past runs")}
	} else {
		for i, r := range rows {
			lines = append(lines, m.histRowText(i, r, focused))
		}
	}
	count := fmt.Sprintf("%d", len(m.runs))
	return titledBox("History", count, strings.Join(lines, "\n"),
		focused, m.histRect.w, m.histRect.h, m.st)
}

// histRowText renders one History row: a run header with expand marker, status
// glyph, label and duration, or an indented child. The cursor row is
// highlighted when the pane is focused.
func (m *Model) histRowText(i int, r histRow, focused bool) string {
	var text string
	switch r.kind {
	case histRowHeader:
		run := m.runs[r.run]
		marker := "▸"
		if m.histExpanded[r.run] {
			marker = "▾"
		}
		dur := ""
		if run.HasManifest && run.Dur > 0 {
			dur = " " + engine.Hms(run.Dur)
		}
		text = fmt.Sprintf("%s %s %s%s", marker, m.glyph(run.Status), run.Label, dur)
	case histRowStep:
		rs := m.runs[r.run].Steps[r.step]
		text = fmt.Sprintf("   %s %s", m.glyph(rs.State), rs.Label)
	case histRowAll:
		text = "   ≡ All logs"
	}
	text = ansi.Truncate(text, m.histRect.w-2, "…")
	if focused && i == m.histCursor {
		return m.st.selected.Render(text)
	}
	return text
}

// renderOutputPane wraps the scrolling viewport in the Output box, titled with
// the current source and a subtitle of wrap state, line count, and (for capped
// history logs) a pager hint.
func (m *Model) renderOutputPane() string {
	title, count := m.outputTitleCount()
	return titledBox(title, count, m.vp.View(),
		m.focus == FocusOutput, m.outRect.w, m.outRect.h, m.st)
}

// outputTitleCount builds the Output box title (source label) and subtitle. The
// subtitle carries the line count and, for history sources, the wrap state and a
// truncation hint.
func (m *Model) outputTitleCount() (string, string) {
	lines := m.vp.TotalLineCount()
	var title, sub string
	switch m.out.kind {
	case outLiveAll:
		title = "Output · all logs"
		sub = fmt.Sprintf("%dL", lines)
	case outLiveStep:
		title = "Output"
		if m.out.step >= 0 && m.out.step < len(m.steps) {
			title = "Output · " + m.steps[m.out.step].Label
		}
		sub = fmt.Sprintf("%dL", lines)
	case outHistStep, outHistAll:
		title = m.historySourceLabel()
		wrap := "wrap:off"
		if m.wrap {
			wrap = "wrap:on"
		}
		sub = fmt.Sprintf("%s · %dL", wrap, lines)
		if m.histTruncated {
			sub = "truncated — l for full · " + sub
		}
	default:
		title = "Output"
	}
	return title, sub
}

// historySourceLabel names the currently-shown history source.
func (m *Model) historySourceLabel() string {
	if m.out.run < 0 || m.out.run >= len(m.runs) {
		return "Output"
	}
	run := m.runs[m.out.run]
	if m.out.kind == outHistStep && m.out.step >= 0 && m.out.step < len(run.Steps) {
		return "History · " + run.Label + " · " + run.Steps[m.out.step].Label
	}
	return "History · " + run.Label + " (all)"
}

// renderFooterBar is one line of hints tailored to the focused pane.
func (m *Model) renderFooterBar() string {
	var hint string
	switch m.focus {
	case FocusOutput:
		hint = "↑/↓ scroll · g/G top/bottom · l pager · tab pane · q quit"
	case FocusHistory:
		hint = "↑/↓ move · ⏎/→ expand · ← collapse · l pager · tab pane · q quit"
	default: // FocusSteps
		if m.started {
			hint = "↑/↓ move · ⏎ follow · a all · r retry · R re-run · l pager · tab pane · q quit"
		} else {
			hint = "⏎ start · ↑/↓ move · tab pane · q quit"
		}
	}
	if m.showHelp {
		hint = "tab/⇧tab pane · ↑/↓ move · ⏎ start/follow · a all · r retry · R re-run · l pager · g/G top/bottom · q quit · ? help"
	}
	w := m.width - 2
	if w < 1 {
		w = 1
	}
	return m.st.muted.Render(" " + ansi.Truncate(hint, w, "…"))
}

// glyph returns a step/run status marker colored by the theme: success/failure
// colors apply to ✓ and ✗/⊗; other states stay uncolored.
func (m *Model) glyph(st engine.State) string {
	g := engine.Glyph(st)
	switch st {
	case engine.StateOK:
		return lipgloss.NewStyle().Foreground(m.st.success).Render(g)
	case engine.StateFailed, engine.StateAborted:
		return lipgloss.NewStyle().Foreground(m.st.failure).Render(g)
	default:
		return g
	}
}

func (m *Model) stepRow(i int) string {
	glyph := m.glyph(m.states[i])
	if m.states[i] == engine.StateRunning {
		glyph = m.spin.View()
	}
	elapsed := m.stepElapsed(i)
	labelW := m.stepsRect.w - 2 - 3 - ansi.StringWidth(elapsed) // border, glyph+2 spaces
	if labelW < 1 {
		labelW = 1
	}
	label := ansi.Truncate(m.steps[i].Label, labelW, "…")
	row := fmt.Sprintf("%s %-*s %s", glyph, labelW, label, elapsed)
	switch {
	case m.isLiveStep() && m.out.step == i:
		return m.st.selected.Render(row)
	case i < len(m.included) && !m.included[i]:
		return m.st.excluded.Render(row) // pre-run excluded: dim + strikethrough
	default:
		return row
	}
}

func (m *Model) allLogsRow() string {
	row := "≡ All logs"
	if m.isAllLogs() {
		return m.st.selected.Render(row)
	}
	return m.st.muted.Render(row)
}

func (m *Model) stepElapsed(i int) string {
	switch m.states[i] {
	case engine.StateRunning:
		return engine.Hms(time.Since(m.starts[i]))
	case engine.StatePending, engine.StateSkipped:
		return ""
	default:
		return engine.Hms(m.durs[i].Dur)
	}
}

// doneCount counts included steps that have finished (N in the header N/M).
// Excluded steps are not part of the run set and never count.
func (m *Model) doneCount() int {
	n := 0
	for i, s := range m.states {
		if i < len(m.included) && !m.included[i] {
			continue
		}
		switch s {
		case engine.StateOK, engine.StateFailed, engine.StateAborted:
			n++
		}
	}
	return n
}

// progressBar renders a fixed-width block bar for the done fraction.
func (m *Model) progressBar(width int) string {
	total := m.includedCount()
	if total == 0 || width <= 0 {
		return ""
	}
	filled := width * m.doneCount() / total
	if filled > width {
		filled = width
	}
	return m.st.selected.Render(strings.Repeat("█", filled)) +
		m.st.muted.Render(strings.Repeat("░", width-filled))
}

// padBetween left-justifies left and right-justifies right across width, with at
// least one space between; right is truncated first if the two do not fit.
func padBetween(left, right string, width int) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if lw+rw >= width {
		avail := width - lw - 1
		if avail < 0 {
			return ansi.Truncate(left, width, "…")
		}
		right = ansi.Truncate(right, avail, "…")
		rw = ansi.StringWidth(right)
	}
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
