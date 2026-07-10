package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/history"
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
			m.out = outSel{kind: outLiveStep, step: msg.i}
			m.rebuildContent()
			m.vp.GotoBottom()
		}
		return m, nil

	case bytesMsg:
		for i, b := range msg {
			// Only the update goroutine writes to an emulator, so the plain
			// (non-Safe) emulator is race-free; the drain goroutine started in
			// New keeps Write from blocking on the emulator's reply pipe.
			_, _ = m.terms[i].Write(b)
		}
		// A single selected step rebuilds per batch (responsive live follow);
		// "All logs" concatenates every emulator, so it is throttled to the tick.
		if m.isAllLogs() {
			m.dirty = true
		} else if m.isLiveStep() {
			if _, touched := msg[m.out.step]; touched {
				m.rebuildContent()
				if m.follow && m.running {
					m.vp.GotoBottom()
				}
			}
		}
		return m, nil

	case doneMsg:
		m.states[msg.i] = msg.res.State
		m.durs[msg.i] = msg.res
		if m.activeIdx == msg.i {
			m.activeIdx = -1
		}
		if (m.isLiveStep() && m.out.step == msg.i) || m.isAllLogs() {
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
		// The run finished: record its manifest and refresh the History pane so the
		// just-completed run shows up as the latest entry.
		m.recordManifest()
		m.refreshHistory()
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
	// Global keys work regardless of which pane is focused.
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		if m.running && m.activeIdx >= 0 {
			m.states[m.activeIdx] = engine.StateAborted
		}
		m.rc.cancel()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, m.keys.FocusNext):
		m.focus = (m.focus + 1) % 3
		return m, nil
	case key.Matches(msg, m.keys.FocusPrev):
		m.focus = (m.focus + 2) % 3
		return m, nil
	case key.Matches(msg, m.keys.OpenConfig):
		return m, openConfigFileCmd()
	case key.Matches(msg, m.keys.OpenConfigDir):
		return m, openConfigDirCmd()
	}

	// From the idle dashboard the start key launches the run. Gated to Steps
	// focus so Enter can drive expand/collapse in the History pane instead.
	if !m.started && m.focus == FocusSteps && key.Matches(msg, m.keys.Start) {
		m.begin()
		return m, nil
	}

	switch m.focus {
	case FocusOutput:
		return m.handleOutputKey(msg)
	case FocusHistory:
		return m.handleHistoryKey(msg)
	default:
		return m.handleStepsKey(msg)
	}
}

// handleStepsKey routes keys when the Steps pane is focused: filter cycling,
// cursor navigation over the visible rows, follow, all-logs, pre-run toggle,
// retry/re-run, and pager on the selected step.
func (m *Model) handleStepsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.FilterPrev):
		m.cycleFilter(-1)
	case key.Matches(msg, m.keys.FilterNext):
		m.cycleFilter(+1)
	case key.Matches(msg, m.keys.Up):
		m.follow = false
		m.setStepCursor(m.currentStepCursor() - 1)
		m.rebuildContent()
	case key.Matches(msg, m.keys.Down):
		m.follow = false
		m.setStepCursor(m.currentStepCursor() + 1)
		m.rebuildContent()
	case key.Matches(msg, m.keys.Follow):
		m.follow = true
		if m.activeIdx >= 0 {
			m.out = outSel{kind: outLiveStep, step: m.activeIdx}
		}
		m.rebuildContent()
		m.vp.GotoBottom()
	case key.Matches(msg, m.keys.All):
		m.follow = false
		m.out = outSel{kind: outLiveAll}
		m.rebuildContent()
	case key.Matches(msg, m.keys.Toggle):
		m.toggleSelected()
	case key.Matches(msg, m.keys.Retry):
		return m, m.retry()
	case key.Matches(msg, m.keys.Restart):
		return m, m.restartAll()
	case key.Matches(msg, m.keys.Pager):
		return m, m.pagerForSelected()
	}
	return m, nil
}

// The Steps pane cursor runs over visible rows: index 0 is the "All logs" row,
// 1..k are the filter-visible steps. currentStepCursor derives that index from
// the current selection (0 if the selected step is hidden by the filter).
func (m *Model) currentStepCursor() int {
	if !m.isLiveStep() {
		return 0
	}
	for c, idx := range m.visibleStepIndices() {
		if idx == m.out.step {
			return c + 1
		}
	}
	return 0
}

// setStepCursor moves the selection to visible-row c (clamped): 0 selects All
// logs, c>=1 selects the (c-1)th visible step.
func (m *Model) setStepCursor(c int) {
	vis := m.visibleStepIndices()
	if c <= 0 {
		m.out = outSel{kind: outLiveAll}
		return
	}
	if c > len(vis) {
		c = len(vis)
	}
	if len(vis) == 0 {
		m.out = outSel{kind: outLiveAll}
		return
	}
	m.out = outSel{kind: outLiveStep, step: vis[c-1]}
}

// cycleFilter advances the filter by d (wrapping) and clamps the cursor so it
// keeps pointing at a still-visible row.
func (m *Model) cycleFilter(d int) {
	m.filter = stepFilter((int(m.filter) + d + 3) % 3)
	m.clampStepCursor()
	m.rebuildContent()
}

// clampStepCursor keeps the selection on a visible row after a filter change:
// if the selected step is now hidden, it snaps to the nearest visible step at
// or before it, else the first visible step, else All logs.
func (m *Model) clampStepCursor() {
	if !m.isLiveStep() {
		return
	}
	vis := m.visibleStepIndices()
	if len(vis) == 0 {
		m.out = outSel{kind: outLiveAll}
		return
	}
	best := vis[0]
	for _, idx := range vis {
		if idx == m.out.step {
			return // still visible
		}
		if idx <= m.out.step {
			best = idx
		}
	}
	m.out = outSel{kind: outLiveStep, step: best}
}

// toggleSelected flips the selected step's pre-run include flag. Only valid on
// the idle dashboard and when a real step (not All logs) is selected.
func (m *Model) toggleSelected() {
	if m.started || !m.isLiveStep() {
		return
	}
	if i := m.out.step; i >= 0 && i < len(m.included) {
		m.included[i] = !m.included[i]
	}
}

// handleOutputKey routes keys when the Output pane is focused: scrolling and the
// pager. Unhandled keys fall through to the viewport for wheel/page motion.
func (m *Model) handleOutputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Top):
		m.follow = false
		m.vp.GotoTop()
		return m, nil
	case key.Matches(msg, m.keys.Bottom):
		m.vp.GotoBottom()
		return m, nil
	case key.Matches(msg, m.keys.Pager):
		return m, m.pagerForSelected()
	}
	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// handleHistoryKey routes keys when the History pane is focused: cursor
// navigation over the flattened rows, expand/collapse, selection into Output,
// and the pager on a selected history step. It never mutates run state — the
// History pane is strictly read-only.
func (m *Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.histRows()
	switch {
	case key.Matches(msg, m.keys.Up):
		if m.histCursor > 0 {
			m.histCursor--
		}
	case key.Matches(msg, m.keys.Down):
		if m.histCursor < len(rows)-1 {
			m.histCursor++
		}
	case key.Matches(msg, m.keys.Collapse):
		m.collapseAtCursor(rows)
	case key.Matches(msg, m.keys.Expand):
		m.expandOrSelectAtCursor(rows)
	case key.Matches(msg, m.keys.Pager):
		return m, m.pagerForHistory()
	}
	return m, nil
}

// expandOrSelectAtCursor expands a collapsed run header, or otherwise selects
// the row's log source into the Output pane.
func (m *Model) expandOrSelectAtCursor(rows []histRow) {
	if m.histCursor < 0 || m.histCursor >= len(rows) {
		return
	}
	row := rows[m.histCursor]
	switch row.kind {
	case histRowHeader:
		if !m.histExpanded[row.run] {
			m.histExpanded[row.run] = true
			return
		}
		m.out = outSel{kind: outHistAll, run: row.run}
	case histRowStep:
		m.out = outSel{kind: outHistStep, run: row.run, step: row.step}
	case histRowAll:
		m.out = outSel{kind: outHistAll, run: row.run}
	}
	// Stop following the live run so a step start cannot yank the Output away
	// from the history log the user is reading.
	m.follow = false
	m.rebuildContent()
}

// collapseAtCursor collapses the run under the cursor and snaps the cursor back
// to that run's header row.
func (m *Model) collapseAtCursor(rows []histRow) {
	if m.histCursor < 0 || m.histCursor >= len(rows) {
		return
	}
	run := rows[m.histCursor].run
	if m.histExpanded[run] {
		m.histExpanded[run] = false
		for i, r := range m.histRows() {
			if r.run == run && r.kind == histRowHeader {
				m.histCursor = i
				break
			}
		}
	}
}

// pagerForSelected opens the selected live step's logfile in the pager, using
// its canonical index so the correct log is paged regardless of filtering.
func (m *Model) pagerForSelected() tea.Cmd {
	if m.isLiveStep() && m.out.step >= 0 && m.out.step < len(m.steps) && m.runDir != "" {
		i := m.out.step
		return pagerCmd(engine.LogPath(m.runDir, i+1, m.steps[i].Key), m.set.UI.Pager)
	}
	return nil
}

// pagerForHistory opens the selected history step's logfile in the pager.
func (m *Model) pagerForHistory() tea.Cmd {
	if m.out.kind != outHistStep || m.out.run >= len(m.runs) {
		return nil
	}
	steps := m.runs[m.out.run].Steps
	if m.out.step >= 0 && m.out.step < len(steps) {
		return pagerCmd(steps[m.out.step].LogPath, m.set.UI.Pager)
	}
	return nil
}

// handleMouse routes a left-click to the pane it landed in: it focuses that pane
// and, in the Steps pane, selects the clicked step row (or the All-logs row).
// Wheel and motion events fall through to the viewport so scrolling still works.
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m.forwardToViewport(msg)
	}
	switch {
	case inRect(msg.X, msg.Y, m.stepsRect):
		m.focus = FocusSteps
		// Content rows below the top border: 0 = filter tabs, 1 = All logs,
		// 2.. = the filter-visible steps.
		switch row := msg.Y - (m.stepsRect.y + 1); {
		case row == 0:
			m.cycleFilter(+1)
		case row == 1:
			m.follow = false
			m.out = outSel{kind: outLiveAll}
			m.rebuildContent()
		case row >= 2:
			if vis := m.visibleStepIndices(); row-2 < len(vis) {
				m.follow = false
				m.out = outSel{kind: outLiveStep, step: vis[row-2]}
				m.rebuildContent()
			}
		}
		return m, nil
	case inRect(msg.X, msg.Y, m.outRect):
		m.focus = FocusOutput
		return m.forwardToViewport(msg)
	case inRect(msg.X, msg.Y, m.histRect):
		m.focus = FocusHistory
		// Content rows start one line below the box's top border. Clicking a run
		// header toggles it; clicking a step/all child selects its log into Output.
		rows := m.histRows()
		if row := msg.Y - (m.histRect.y + 1); row >= 0 && row < len(rows) {
			m.histCursor = row
			if rows[row].kind == histRowHeader {
				m.toggleExpandAtCursor(rows)
			} else {
				m.expandOrSelectAtCursor(rows)
			}
		}
		return m, nil
	}
	return m, nil
}

// toggleExpandAtCursor expands a collapsed run header or collapses an expanded
// one. Mouse clicks on a header use this because toggling is the intuitive tree
// interaction; the keyboard splits the two across ⏎ (expand/select) and ←
// (collapse).
func (m *Model) toggleExpandAtCursor(rows []histRow) {
	if m.histCursor < 0 || m.histCursor >= len(rows) {
		return
	}
	row := rows[m.histCursor]
	if row.kind != histRowHeader {
		return
	}
	if m.histExpanded[row.run] {
		m.collapseAtCursor(rows)
	} else {
		m.histExpanded[row.run] = true
	}
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
	if m.running || !m.isLiveStep() {
		return nil
	}
	i := m.out.step
	if i < 0 || i >= len(m.steps) || m.states[i] != engine.StateFailed {
		return nil
	}
	resetTerm(m.terms[i])
	m.states[i] = engine.StatePending
	m.durs[i] = engine.Result{}
	m.running = true
	// Re-arm the header timer so it ticks live during the retry instead of
	// showing the previous run's frozen value (mirrors restartAll).
	m.totalStart = time.Now()
	m.totalEnd = time.Time{}
	m.refreshHistory() // hide the current run while it re-runs
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
	m.ensureRunDir()
	m.applyExclusions()
	for i := range m.steps {
		resetTerm(m.terms[i])
		m.states[i] = engine.StatePending
		m.durs[i] = engine.Result{}
		m.starts[i] = time.Time{}
	}
	m.started = true
	m.running = true
	m.activeIdx = -1
	m.follow = true
	m.totalStart = time.Now()
	m.totalEnd = time.Time{}
	m.refreshHistory() // hide the current run while it re-runs
	m.rebuildContent()
	m.rc.launch(func() { m.rc.runner.RunAll(m.rc.ctx, m.rc.steps) })
	return nil
}

// resize recomputes the three pane rectangles, the viewport size, and every
// emulator's size. Wide places Steps over History in a left column with Output
// filling the right; narrow stacks Steps, Output, History. The rectangles are
// kept consistent with the view composition so mouse hit-testing lines up.
func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	// A hard floor guards the side-by-side layout: a user setting wide_threshold
	// very low must not force a wide split too narrow to fit both columns.
	m.wide = w >= m.set.UI.WideThreshold && w >= minWideWidth

	bodyH := h - headerH - footerH
	if bodyH < 3 {
		bodyH = 3
	}

	if m.wide {
		leftW := clampInt(w/3, 28, 42)
		if leftW > w-20 { // always leave room for the Output pane
			leftW = w - 20
		}
		stepsH := (bodyH + 1) / 2
		histH := bodyH - stepsH
		m.stepsRect = rect{0, headerH, leftW, stepsH}
		m.histRect = rect{0, headerH + stepsH, leftW, histH}
		m.outRect = rect{leftW, headerH, w - leftW, bodyH}
	} else {
		// Distribute bodyH across the three stacked panes so they always sum to
		// bodyH (no vertical overflow). Each gets >=2 rows (its border) whenever
		// bodyH >= 6; below that the terminal is too small for three panes anyway.
		histH := clampInt(bodyH/4, 2, bodyH-4)
		stepsHi := bodyH - histH - 2
		if bodyH/2 < stepsHi {
			stepsHi = bodyH / 2
		}
		stepsH := clampInt(len(m.steps)+3, 2, stepsHi)
		outH := bodyH - stepsH - histH
		m.stepsRect = rect{0, headerH, w, stepsH}
		m.outRect = rect{0, headerH + stepsH, w, outH}
		m.histRect = rect{0, headerH + stepsH + outH, w, histH}
	}

	m.vp.Width = clampInt(m.outRect.w-2, 1, m.outRect.w)
	m.vp.Height = clampInt(m.outRect.h-2, 1, m.outRect.h)
	m.ready = true
	// Resize every emulator to the Output pane so each wraps its own output to the
	// visible width, then match the running child's pty so its own wrapping and
	// progress redraws line up with what the emulator expects.
	for _, e := range m.terms {
		e.Resize(m.vp.Width, m.vp.Height)
	}
	// The scratch (history) emulator matches the Output width so decoded logs
	// wrap the same way; when wrap is off it is sized wide so long lines are
	// clipped by the box rather than wrapped.
	scratchW := m.vp.Width
	if !m.wrap {
		scratchW = noWrapWidth
	}
	m.scratch.Resize(scratchW, m.vp.Height)
	m.rc.runner.SetSize(uint16(m.vp.Height), uint16(m.vp.Width))
	m.rebuildContent()
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// rebuildContent regenerates the viewport body from the selected step's emulator
// (or the concatenated "All logs"). The emulator already wrapped its output to
// the pane width on resize, so no extra hard-wrap is applied here.
func (m *Model) rebuildContent() {
	if !m.ready {
		return
	}
	switch m.out.kind {
	case outLiveAll:
		m.vp.SetContent(m.allLogsBody())
	case outLiveStep:
		if m.out.step >= 0 && m.out.step < len(m.terms) {
			m.vp.SetContent(renderTerm(m.terms[m.out.step]))
		}
	case outHistStep:
		m.vp.SetContent(m.historyStepBody())
	case outHistAll:
		m.vp.SetContent(m.historyAllBody())
	}
}

// historyStepBody decodes one past-run step's logfile (capped) through the
// scratch emulator. Logs are read only here — never during Scan.
func (m *Model) historyStepBody() string {
	m.histTruncated = false
	if m.out.run < 0 || m.out.run >= len(m.runs) {
		return ""
	}
	steps := m.runs[m.out.run].Steps
	if m.out.step < 0 || m.out.step >= len(steps) {
		return ""
	}
	return m.decodeHistoryLog(steps[m.out.step])
}

// historyAllBody concatenates every step's capped log for the selected run,
// mirroring the live All-logs layout.
func (m *Model) historyAllBody() string {
	if m.out.run < 0 || m.out.run >= len(m.runs) {
		return ""
	}
	m.histTruncated = false
	var b strings.Builder
	for i, rs := range m.runs[m.out.run].Steps {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.st.sep.Render("── " + rs.Label + " ──"))
		b.WriteString("\n")
		b.WriteString(m.decodeHistoryLog(rs))
		b.WriteString("\n")
	}
	return b.String()
}

// decodeHistoryLog reads a history logfile, caps it to the scrollback bound,
// feeds it through the scratch emulator, and returns the rendered text. It sets
// histTruncated when the read was capped (the Output subtitle then hints at the
// pager for the full log).
func (m *Model) decodeHistoryLog(rs history.RunStep) string {
	data, err := history.LoadLog(rs)
	if err != nil {
		return m.st.muted.Render("(log unavailable)")
	}
	capped, truncated := capLog(data, scrollbackCap)
	if truncated {
		m.histTruncated = true
	}
	resetTerm(m.scratch)
	_, _ = m.scratch.Write(capped)
	return renderTerm(m.scratch)
}

// capLog keeps only the last maxLines lines of b so a huge logfile cannot stall
// the update loop; the full file is always available via the pager. It reports
// whether anything was dropped.
func capLog(b []byte, maxLines int) ([]byte, bool) {
	count := 0
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '\n' {
			count++
			if count > maxLines {
				return b[i+1:], true
			}
		}
	}
	return b, false
}

func (m *Model) allLogsBody() string {
	var b strings.Builder
	for i, st := range m.steps {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.st.sep.Render("── " + st.Label + " ──"))
		b.WriteString("\n")
		b.WriteString(renderTerm(m.terms[i]))
		b.WriteString("\n")
	}
	return b.String()
}
