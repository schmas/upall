// Package tui renders a run as an alt-screen master/detail dashboard over the
// config-driven engine. It bakes in the red-team safety fixes: a run-state
// machine (safe retry, no races), quit cancellation, bounded virtual-terminal
// viewports, a robust pager, and message-only state mutation.
package tui

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"

	"github.com/schmas/upall/internal/engine"
)

const (
	scrollbackCap = 1500 // scrollback lines kept per step in the pane (full log on disk)
	defaultCols   = 80   // emulator size before the first WindowSizeMsg resizes it
	defaultRows   = 24
	masterWidth   = 30 // master pane width in wide layout
	wideThreshold = 90 // cols at/above which panes sit side by side
	headerHeight  = 3  // rendered rows of the bordered title/header
	previewTop    = 4  // header rows + one blank line before the preview list
)

// runControl holds everything needed to drive and cancel a run. The model holds
// a pointer to it; the runner is filled in after the tea.Program exists (the
// sink needs the program), and the model sees it through the pointer.
type runControl struct {
	ctx    context.Context
	cancel context.CancelFunc
	runner *engine.Runner
	steps  []engine.Step
	launch func(func()) // spawn a runner goroutine; sends RunDoneMsg when it returns
	wg     sync.WaitGroup
}

// Model is the Bubble Tea model. It is used as a pointer, so Update mutates in
// place and only the update loop ever writes these fields.
type Model struct {
	rc     *runControl
	steps  []engine.Step
	runDir string

	terms  []*vt.Emulator
	states []engine.State
	durs   []engine.Result
	starts []time.Time
	skips  []string

	vp   viewport.Model
	spin spinner.Model
	help help.Model
	keys keyMap

	sel        int // 0..n-1 selects a step; n selects the synthetic "All logs"
	follow     bool
	running    bool
	started    bool // false while the preview is shown, before the run is confirmed
	dirty      bool // All-logs content needs a rebuild (throttled to the tick)
	activeIdx  int
	totalStart time.Time
	totalEnd   time.Time // set when a run goes idle; freezes the header timer

	width, height int
	wide          bool
	ready         bool
	quitting      bool
}

// New builds the model. rc.runner/launch are wired by Run after the program is
// created.
func New(steps []engine.Step, runDir string, rc *runControl) *Model {
	n := len(steps)
	m := &Model{
		rc:        rc,
		steps:     steps,
		runDir:    runDir,
		terms:     make([]*vt.Emulator, n),
		states:    make([]engine.State, n),
		durs:      make([]engine.Result, n),
		starts:    make([]time.Time, n),
		skips:     make([]string, n),
		spin:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:      help.New(),
		keys:      defaultKeys(),
		follow:    true,
		activeIdx: -1,
	}
	// One virtual-terminal emulator per step, fed raw pty bytes on the update
	// goroutine. Each gets a drain goroutine that discards the emulator's reply
	// stream (device-attribute / cursor-position answers): those replies are
	// written synchronously into an unbuffered pipe during Write, so without a
	// reader a step that queries the terminal would deadlock the update loop.
	// The child's stdin is /dev/null, so the replies are never needed. Emulators
	// are reset in place (never closed/recreated), so these goroutines live for
	// the program and the emulator's closed flag is never written — no race.
	for i := range m.terms {
		e := vt.NewEmulator(defaultCols, defaultRows)
		e.SetScrollbackSize(scrollbackCap)
		m.terms[i] = e
		go func(e *vt.Emulator) { _, _ = io.Copy(io.Discard, e) }(e)
	}
	return m
}

// Init starts the spinner and elapsed ticker. The run does NOT start here: the
// TUI opens on a preview of the steps that will run and waits for the user to
// confirm (see Update's preview handling), so nothing executes until then.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tickCmd())
}

// begin launches the run once the user confirms the preview. launch is called
// synchronously on the update goroutine so its wg.Add happens-before any later
// quit reap (no WaitGroup race).
func (m *Model) begin() {
	m.started = true
	m.running = true
	m.totalStart = time.Now()
	m.totalEnd = time.Time{}
	m.rc.launch(func() { m.rc.runner.RunAll(m.rc.ctx, m.rc.steps) })
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) allLogsIndex() int { return len(m.steps) }
func (m *Model) isAllLogs() bool   { return m.sel == m.allLogsIndex() }

// States/Durations expose the final run state so Run can render the summary.
func (m *Model) States() []engine.State     { return m.states }
func (m *Model) Durations() []engine.Result { return m.durs }
