// Package tui renders a run as an alt-screen master/detail dashboard over the
// config-driven engine. It bakes in the red-team safety fixes: a run-state
// machine (safe retry, no races), quit cancellation, bounded ring-buffer
// viewports, a robust pager, and message-only state mutation.
package tui

import (
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
)

const (
	ringCap       = 1500 // lines kept per step in the viewport (full log on disk)
	masterWidth   = 30   // master pane width in wide layout
	wideThreshold = 90   // cols at/above which panes sit side by side
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

	rings  []*ring
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
	activeIdx  int
	totalStart time.Time

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
		rc:         rc,
		steps:      steps,
		runDir:     runDir,
		rings:      make([]*ring, n),
		states:     make([]engine.State, n),
		durs:       make([]engine.Result, n),
		starts:     make([]time.Time, n),
		skips:      make([]string, n),
		spin:       spinner.New(spinner.WithSpinner(spinner.Dot)),
		help:       help.New(),
		keys:       defaultKeys(),
		follow:     true,
		activeIdx:  -1,
		totalStart: time.Now(),
	}
	for i := range m.rings {
		m.rings[i] = newRing(ringCap)
	}
	return m
}

// Init starts the run, the spinner, and the elapsed-time ticker.
func (m *Model) Init() tea.Cmd {
	m.running = true
	return tea.Batch(m.spin.Tick, tickCmd(), m.startRunCmd())
}

// startRunCmd launches the initial RunAll on a background goroutine.
func (m *Model) startRunCmd() tea.Cmd {
	return func() tea.Msg {
		m.rc.launch(func() { m.rc.runner.RunAll(m.rc.ctx, m.rc.steps) })
		return nil
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) allLogsIndex() int { return len(m.steps) }
func (m *Model) isAllLogs() bool   { return m.sel == m.allLogsIndex() }

// States/Durations expose the final run state so Run can render the summary.
func (m *Model) States() []engine.State     { return m.states }
func (m *Model) Durations() []engine.Result { return m.durs }
