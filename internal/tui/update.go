package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/schmas/upall/internal/engine"
)

// Update is the sole place step state changes. All runner events arrive as
// messages here, so the runner goroutine never touches shared model state.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case startMsg:
		m.states[msg.i] = engine.StateRunning
		m.starts[msg.i] = time.Now()
		m.activeIdx = msg.i
		if m.follow {
			m.sel = msg.i
			m.rebuildContent()
			m.vp.GotoBottom()
		}
		return m, nil

	case linesMsg:
		for i, lines := range msg {
			for _, ln := range lines {
				m.rings[i].append(ln)
			}
		}
		if _, touched := msg[m.sel]; touched || m.isAllLogs() {
			m.rebuildContent()
			if m.follow && m.running {
				m.vp.GotoBottom()
			}
		}
		return m, nil

	case doneMsg:
		m.states[msg.i] = msg.res.State
		m.durs[msg.i] = msg.res
		if m.activeIdx == msg.i {
			m.activeIdx = -1
		}
		if m.sel == msg.i || m.isAllLogs() {
			m.rebuildContent()
		}
		return m, nil

	case skipMsg:
		m.states[msg.i] = engine.StateSkipped
		m.skips[msg.i] = msg.reason
		return m, nil

	case RunDoneMsg:
		m.running = false
		m.activeIdx = -1
		return m, nil

	case tickMsg:
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case pagerDoneMsg:
		return m, nil
	}

	// Forward anything else (mouse wheel, pgup/pgdn) to the viewport.
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		if m.running && m.activeIdx >= 0 {
			m.states[m.activeIdx] = engine.StateAborted
		}
		m.rc.cancel()
		return m, tea.Quit

	case key.Matches(msg, m.keys.Up):
		m.follow = false
		if m.sel > 0 {
			m.sel--
		}
		m.rebuildContent()
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.follow = false
		if m.sel < m.allLogsIndex() {
			m.sel++
		}
		m.rebuildContent()
		return m, nil

	case key.Matches(msg, m.keys.Follow):
		m.follow = true
		if m.activeIdx >= 0 {
			m.sel = m.activeIdx
		}
		m.rebuildContent()
		m.vp.GotoBottom()
		return m, nil

	case key.Matches(msg, m.keys.All):
		m.follow = false
		m.sel = m.allLogsIndex()
		m.rebuildContent()
		return m, nil

	case key.Matches(msg, m.keys.Retry):
		return m, m.retry()

	case key.Matches(msg, m.keys.Pager):
		if m.sel < len(m.steps) && m.runDir != "" {
			return m, pagerCmd(engine.LogPath(m.runDir, m.sel+1, m.steps[m.sel].Key))
		}
		return m, nil

	case key.Matches(msg, m.keys.Top):
		m.follow = false
		m.vp.GotoTop()
		return m, nil

	case key.Matches(msg, m.keys.Bottom):
		m.vp.GotoBottom()
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// retry re-runs the selected step, but ONLY when no run is active and the step
// terminally failed. This guard is the run-state machine that prevents a
// RunOne/RunAll race on shared state.
func (m *Model) retry() tea.Cmd {
	if m.running || m.sel >= len(m.steps) || m.states[m.sel] != engine.StateFailed {
		return nil
	}
	i := m.sel
	m.rings[i].reset()
	m.states[i] = engine.StatePending
	m.durs[i] = engine.Result{}
	m.running = true
	m.rebuildContent()
	return func() tea.Msg {
		m.rc.launch(func() { m.rc.runner.RunOne(m.rc.ctx, m.rc.steps, i) })
		return nil
	}
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.wide = w >= wideThreshold

	headerH, footerH := 3, 1
	bodyH := h - headerH - footerH
	if bodyH < 1 {
		bodyH = 1
	}
	var logW, logH int
	if m.wide {
		logW = w - masterWidth - 1
		logH = bodyH
	} else {
		masterH := len(m.steps) + 2
		if masterH > bodyH-1 {
			masterH = bodyH - 1
		}
		logW = w
		logH = bodyH - masterH
	}
	if logW < 1 {
		logW = 1
	}
	if logH < 1 {
		logH = 1
	}
	m.vp.Width = logW
	m.vp.Height = logH
	m.ready = true
	// Match the running child's pty to the log pane so wrapping/progress fit.
	m.rc.runner.SetSize(uint16(logH), uint16(logW))
	m.rebuildContent()
}

// rebuildContent regenerates the viewport body from the selected ring (or the
// concatenated "All logs"), wrapped ANSI-safely to the pane width.
func (m *Model) rebuildContent() {
	if !m.ready {
		return
	}
	if m.isAllLogs() {
		m.vp.SetContent(m.wrap(m.allLogsBody()))
		return
	}
	m.vp.SetContent(m.wrap(m.rings[m.sel].String()))
}

func (m *Model) allLogsBody() string {
	var b strings.Builder
	for i, st := range m.steps {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(sepStyle.Render("── " + st.Label + " ──"))
		b.WriteString("\n")
		b.WriteString(m.rings[i].String())
		b.WriteString("\n")
	}
	return b.String()
}

// wrap hard-wraps each line to the viewport width, preserving ANSI styling.
func (m *Model) wrap(s string) string {
	w := m.vp.Width
	if w < 1 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = ansi.Hardwrap(ln, w, true)
	}
	return strings.Join(lines, "\n")
}
