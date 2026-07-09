package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
)

var (
	sepStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	titleStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1).Border(lipgloss.RoundedBorder())
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	masterStyle   = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

// View renders the whole dashboard. On quit it returns empty so the alt screen
// is left clean; Run then prints the summary to the normal buffer.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return "starting upall…"
	}
	if !m.started {
		return m.renderPreview()
	}
	body := m.renderBody()
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.help.View(m.keys))
}

// renderPreview lists the steps that will run and waits for the user to confirm
// before anything executes. Steps that do not apply or whose tool is absent were
// already dropped in SelectRun, so every row here will run.
func (m *Model) renderPreview() string {
	w := m.width - 2 // account for border sides
	if w < 1 {
		w = 1
	}
	title := titleStyle.Width(w).Render(
		fmt.Sprintf("upall  •  %d step(s) will run", len(m.steps)))

	rows := make([]string, 0, len(m.steps))
	for i, st := range m.steps {
		rows = append(rows, m.previewRow(i, st))
	}
	footer := dimStyle.Render("  ⏎ start · ↑/↓ or click to move · q quit")
	return lipgloss.JoinVertical(lipgloss.Left, title, "", strings.Join(rows, "\n"), "", footer)
}

func (m *Model) previewRow(i int, st engine.Step) string {
	label := ansi.Truncate(st.Label, masterWidth, "…")
	row := fmt.Sprintf("  • %s", label)
	if i == m.sel {
		return selectedStyle.Render(row)
	}
	return row
}

func (m *Model) renderBody() string {
	log := m.vp.View()
	master := m.renderMaster()
	if m.wide {
		return lipgloss.JoinHorizontal(lipgloss.Top, master, log)
	}
	return lipgloss.JoinVertical(lipgloss.Left, master, log)
}

func (m *Model) renderHeader() string {
	// The timer ticks live while a run is in flight and freezes at the moment the
	// run went idle (totalEnd), so a finished dashboard stops counting.
	end := m.totalEnd
	if end.IsZero() {
		end = time.Now()
	}
	elapsed := engine.Hms(end.Sub(m.totalStart))
	strip := m.progressStrip()
	title := fmt.Sprintf("upall  •  %s   %s", elapsed, strip)
	w := m.width - 2 // account for border sides
	if w < 1 {
		w = 1
	}
	return titleStyle.Width(w).Render(ansi.Truncate(title, w, "…"))
}

func (m *Model) progressStrip() string {
	var b strings.Builder
	for _, st := range m.states {
		b.WriteString(engine.Glyph(st))
	}
	return b.String()
}

func (m *Model) renderMaster() string {
	rows := make([]string, 0, len(m.steps)+1)
	for i := range m.steps {
		rows = append(rows, m.stepRow(i))
	}
	rows = append(rows, m.allLogsRow())
	content := strings.Join(rows, "\n")
	if m.wide {
		return masterStyle.Width(masterWidth).Height(m.vp.Height).Render(content)
	}
	return content
}

func (m *Model) stepRow(i int) string {
	glyph := engine.Glyph(m.states[i])
	if m.states[i] == engine.StateRunning {
		glyph = m.spin.View()
	}
	label := ansi.Truncate(m.steps[i].Label, masterWidth-10, "…")
	row := fmt.Sprintf("%s %-*s %s", glyph, masterWidth-10, label, m.stepElapsed(i))
	if i == m.sel {
		return selectedStyle.Render(row)
	}
	return row
}

func (m *Model) allLogsRow() string {
	row := "≡ All logs"
	if m.isAllLogs() {
		return selectedStyle.Render(row)
	}
	return dimStyle.Render(row)
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
