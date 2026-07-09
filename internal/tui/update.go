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

	case tea.MouseMsg:
		return m.handleMouse(msg)

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
				m.rings[i].append(sanitize(ln))
			}
		}
		// A single selected step rebuilds per batch (responsive live follow);
		// "All logs" concatenates every ring, so it is throttled to the tick.
		if m.isAllLogs() {
			m.dirty = true
		} else if _, touched := msg[m.sel]; touched {
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
		m.totalEnd = time.Now()
		return m, nil

	case tickMsg:
		if m.dirty {
			m.rebuildContent()
			if m.follow && m.running {
				m.vp.GotoBottom()
			}
			m.dirty = false
		}
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
	// Preview mode: nothing runs until the user confirms. Only start/quit/nav.
	if !m.started {
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			m.rc.cancel()
			return m, tea.Quit
		case key.Matches(msg, m.keys.Start):
			m.begin()
			return m, nil
		case key.Matches(msg, m.keys.Up):
			if m.sel > 0 {
				m.sel--
			}
			return m, nil
		case key.Matches(msg, m.keys.Down):
			if m.sel < m.allLogsIndex() {
				m.sel++
			}
			return m, nil
		}
		return m, nil
	}

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

	case key.Matches(msg, m.keys.Restart):
		return m, m.restartAll()

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

// handleMouse maps a left-click in the master pane to a step selection (or the
// "All logs" row), in both the preview and the running dashboard. Wheel and
// motion events fall through to the viewport so scrolling still works.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m.forwardToViewport(msg)
	}

	// Preview: the step list starts below the header and one blank line; there
	// is no "All logs" row yet.
	if !m.started {
		if i := msg.Y - previewTop; i >= 0 && i < len(m.steps) {
			m.sel = i
		}
		return m, nil
	}

	// Running: in the wide layout the master pane is the left column only, so a
	// click in the log pane should scroll, not reselect.
	if m.wide && msg.X > masterWidth {
		return m.forwardToViewport(msg)
	}
	if row := msg.Y - headerHeight; row >= 0 && row <= m.allLogsIndex() {
		m.sel = row
		m.follow = false
		m.rebuildContent()
	}
	return m, nil
}

func (m *Model) forwardToViewport(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	// launch synchronously (on the update goroutine) so wg.Add happens-before
	// any subsequent quit reap; only the runner itself is a goroutine.
	m.rc.launch(func() { m.rc.runner.RunOne(m.rc.ctx, m.rc.steps, i) })
	return nil
}

// restartAll re-runs every step from a clean slate. Like retry it fires only
// when no run is active, so it cannot race a live run on shared state; it resets
// all step state and the total timer, then relaunches RunAll on the still-live
// session context.
func (m *Model) restartAll() tea.Cmd {
	if m.running {
		return nil
	}
	for i := range m.steps {
		m.rings[i].reset()
		m.states[i] = engine.StatePending
		m.durs[i] = engine.Result{}
		m.starts[i] = time.Time{}
	}
	m.running = true
	m.activeIdx = -1
	m.follow = true
	m.totalStart = time.Now()
	m.totalEnd = time.Time{}
	m.rebuildContent()
	m.rc.launch(func() { m.rc.runner.RunAll(m.rc.ctx, m.rc.steps) })
	return nil
}

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.wide = w >= wideThreshold

	headerH, footerH := headerHeight, 1
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
