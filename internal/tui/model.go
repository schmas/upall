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

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/history"
	"github.com/schmas/upall/internal/settings"
)

const (
	scrollbackCap = 1500 // scrollback lines kept per step in the pane (full log on disk)
	defaultCols   = 80   // emulator size before the first WindowSizeMsg resizes it
	defaultRows   = 24
	headerH       = 3   // rendered rows of the bordered header bar
	footerH       = 1   // the single footer line
	barWidth      = 12  // progress-bar cells in the header
	noWrapWidth   = 512 // scratch-emulator width when wrap is off (lines get clipped, not wrapped)
	minWideWidth  = 50  // hard floor for the side-by-side layout, regardless of wide_threshold
)

// paneFocus identifies which of the three dashboard panes has keyboard focus.
// Tab cycles Steps → History → Output → Steps.
type paneFocus int

const (
	FocusSteps paneFocus = iota
	FocusHistory
	FocusOutput
)

// rect is a pane's outer rectangle on screen (including its border), used both
// to size the box and to hit-test mouse clicks.
type rect struct{ x, y, w, h int }

func inRect(x, y int, r rect) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// outKind discriminates what the Output pane is showing. Live kinds source from
// the current run's per-step emulators; history kinds (wired in phase 5) source
// from past run logfiles.
type outKind int

const (
	outLiveStep outKind = iota // one live step's emulator
	outLiveAll                 // every live step concatenated
	outHistStep                // one past-run step's logfile (phase 5)
	outHistAll                 // a past run's steps concatenated (phase 5)
)

// outSel is the Output pane's selection: which source it renders. step is the
// canonical step index (live) or the history child index; run selects a past
// run (phase 5). It replaces the old flat `sel int` so the Output can source
// from either a live run or history through one code path.
type outSel struct {
	kind outKind
	step int
	run  int
}

// stepFilter is the view-only Steps filter. It never changes what runs.
type stepFilter int

const (
	FilterAll stepFilter = iota
	FilterPending
	FilterDone
)

// parseFilter maps the config's default_filter string to a stepFilter.
func parseFilter(s string) stepFilter {
	switch s {
	case "pending":
		return FilterPending
	case "done":
		return FilterDone
	default:
		return FilterAll
	}
}

// histRowKind labels a row in the flattened History list.
type histRowKind int

const (
	histRowHeader histRowKind = iota // a run row (▸/▾)
	histRowStep                      // an expanded run's step child
	histRowAll                       // an expanded run's "All logs" child
)

// histRow is one visible row in the History pane: a run header or, when that run
// is expanded, one of its step children or the All-logs child.
type histRow struct {
	run  int
	kind histRowKind
	step int // step-child index into run.Steps (histRowStep only)
}

// runControl holds everything needed to drive and cancel a run. The model holds
// a pointer to it; the runner is filled in after the tea.Program exists (the
// sink needs the program), and the model sees it through the pointer.
type runControl struct {
	ctx    context.Context
	cancel context.CancelFunc
	// runCancel cancels only the CURRENT run (the stop key), leaving ctx alive so
	// retry/re-run still work. Set by launchRun; nil when no run has launched.
	runCancel context.CancelFunc
	runner    *engine.Runner
	steps     []engine.Step
	launch    func(func()) // spawn a runner goroutine; sends RunDoneMsg when it returns
	wg        sync.WaitGroup
}

// Model is the Bubble Tea model. It is used as a pointer, so Update mutates in
// place and only the update loop ever writes these fields.
type Model struct {
	rc      *runControl
	steps   []engine.Step
	root    string            // run-log root; history is browsed here and new runs written under it
	keep    int               // run-log dirs to retain when a run creates a new one
	runDir  string            // the current run's dir; "" until the first run actually starts
	set     settings.Settings // user config; drives keys/theme/behavior (phase 2+)
	version string            // build version, shown in the header; "dev" outside a release build

	terms  []*vt.Emulator
	states []engine.State
	durs   []engine.Result
	starts []time.Time
	skips  []string

	vp   viewport.Model
	spin spinner.Model
	keys keyMap
	st   styles

	focus    paneFocus
	showHelp bool // '?' toggles a fuller footer hint

	out      outSel     // what the Output pane renders (selection)
	filter   stepFilter // view-only Steps filter
	included []bool     // per-step pre-run include flag (len n, default all true)
	wrap     bool       // Output wraps long history lines (from config)

	// Read-only run history (scanned once at launch) and its browse state.
	runs          []history.Run
	histExpanded  []bool       // per-run expand flag (len == len(runs))
	histCursor    int          // cursor over the flattened History rows
	histSelGen    int          // generation counter debouncing load-on-navigate
	scratch       *vt.Emulator // scratch emulator for decoding history logs
	histTruncated bool         // current history render was capped (show hint)

	follow     bool
	running    bool
	started    bool // false on the idle dashboard, before the run is confirmed
	typing     bool // type mode: keystrokes forward to the active step's pty
	dirty      bool // All-logs content needs a rebuild (throttled to the tick)
	activeIdx  int
	totalStart time.Time
	totalEnd   time.Time // set when a run goes idle; freezes the header timer

	width, height int
	wide          bool
	ready         bool
	quitting      bool

	// Pane rectangles, recomputed on resize; the view composes to match and the
	// mouse handler hit-tests against them.
	stepsRect rect
	histRect  rect
	outRect   rect
}

// New builds the model. rc.runner/launch are wired by Run after the program is
// created. root is the run-log root browsed by History; the current run's dir is
// created lazily on the first run so merely opening upall records nothing.
func New(steps []engine.Step, root string, keep int, rc *runControl, set settings.Settings, version string) *Model {
	n := len(steps)
	m := &Model{
		rc:        rc,
		steps:     steps,
		root:      root,
		keep:      keep,
		set:       set,
		version:   version,
		terms:     make([]*vt.Emulator, n),
		states:    make([]engine.State, n),
		durs:      make([]engine.Result, n),
		starts:    make([]time.Time, n),
		skips:     make([]string, n),
		spin:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		keys:      keysFrom(set),
		st:        buildStyles(set.Theme),
		focus:     FocusSteps,
		filter:    parseFilter(set.UI.DefaultFilter),
		included:  makeAllTrue(n),
		out:       outSel{kind: outLiveStep, step: 0},
		wrap:      set.UI.Wrap,
		follow:    set.UI.Follow,
		activeIdx: -1,
	}
	// Read-only history of past runs, scanned up front (cheap: manifests only, no
	// logfiles) and refreshed after each run. Nothing is excluded yet — there is
	// no current run until the user starts one. A failed scan yields empty history.
	m.runs = scanHistory(root, "")
	m.histExpanded = make([]bool, len(m.runs))
	// A scratch emulator decodes on-disk history logs into the Output pane; it is
	// reset and re-fed per selection (never concurrent with the step emulators).
	m.scratch = vt.NewEmulator(defaultCols, defaultRows)
	m.scratch.SetScrollbackSize(scrollbackCap)
	go func(e *vt.Emulator) { _, _ = io.Copy(io.Discard, e) }(m.scratch)
	// One virtual-terminal emulator per step, fed raw pty bytes on the update
	// goroutine. Each gets a drain goroutine that discards the emulator's reply
	// stream (device-attribute / cursor-position answers): those replies are
	// written synchronously into an unbuffered pipe during Write, so without a
	// reader a step that queries the terminal would deadlock the update loop.
	// Those replies only ever loop back into this display emulator, never to the
	// child (the child's real pty is drained separately by the runner). Emulators
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
// TUI opens on the idle dashboard (steps shown as pending) and waits for the
// user to press the start key, so nothing executes until then.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, tickCmd())
}

// begin launches the run once the user confirms from the idle dashboard. launch
// is called synchronously on the update goroutine so its wg.Add happens-before
// any later quit reap (no WaitGroup race).
func (m *Model) begin() {
	m.ensureRunDir()
	m.applyExclusions()
	m.started = true
	m.running = true
	m.totalStart = time.Now()
	m.totalEnd = time.Time{}
	m.refreshHistory()
	m.launchRun(func(ctx context.Context) { m.rc.runner.RunAll(ctx, m.rc.steps) })
}

// launchRun starts a runner on a per-run child of the session context and
// records that child's cancel on runControl, so the stop key can cancel just
// this run without touching the session context (retry/re-run stay possible).
// Called synchronously on the update goroutine, like the launch sites it wraps,
// so its wg.Add happens-before any later quit reap. The deferred cancel releases
// the child context when the run ends normally (no leak / vet lostcancel).
func (m *Model) launchRun(run func(context.Context)) {
	runCtx, runCancel := context.WithCancel(m.rc.ctx)
	m.rc.runCancel = runCancel
	m.rc.launch(func() {
		defer runCancel()
		run(runCtx)
	})
}

// ensureRunDir creates the run's log directory on the first run and points the
// runner at it. Creating it lazily (rather than at startup) is what keeps merely
// opening upall from recording an empty run — and from rotating real history. A
// no-op once the dir exists, or when logging is disabled (root == "").
func (m *Model) ensureRunDir() {
	if m.runDir != "" || m.root == "" {
		return
	}
	dir, err := engine.NewRunDir(m.root, m.keep)
	if err != nil {
		return // logging disabled; the run still proceeds without on-disk logs
	}
	m.runDir = dir
	m.rc.runner.RunDir = dir
}

// recordManifest writes the current run's manifest so the History browser can
// list it. Gated on an actual run: nothing is written until a run has started
// and its dir exists. Best-effort — a write failure never affects the run.
func (m *Model) recordManifest() {
	if m.runDir == "" || !m.started {
		return
	}
	_ = engine.WriteManifest(m.runDir, m.steps, m.states, m.durs, time.Now())
}

// refreshHistory re-scans the run-log root so the History pane reflects what is
// on disk. The current run is hidden only while it is in flight (it lives in the
// Steps pane); once finished it is included so it shows as the latest entry.
func (m *Model) refreshHistory() {
	exclude := ""
	if m.running {
		exclude = m.runDir
	}
	m.runs = scanHistory(m.root, exclude)
	m.histExpanded = make([]bool, len(m.runs))
	if rows := len(m.histRows()); m.histCursor >= rows {
		m.histCursor = rows - 1
	}
	if m.histCursor < 0 {
		m.histCursor = 0
	}
}

// applyExclusions flags every pre-run-excluded step Skip before launch, so the
// runner reports it StateSkipped via the existing Sink.Skip path. m.steps and
// m.rc.steps alias the same backing array; setting Skip before the runner
// goroutine starts is race-free (the run has not begun reading yet).
func (m *Model) applyExclusions() {
	for i := range m.rc.steps {
		if i < len(m.included) && !m.included[i] {
			m.rc.steps[i].Skip = true
			m.rc.steps[i].SkipReason = "excluded"
		}
	}
}

func makeAllTrue(n int) []bool {
	b := make([]bool, n)
	for i := range b {
		b[i] = true
	}
	return b
}

// scanHistory lists past runs under root, optionally excluding one dir (the
// in-progress run, which is shown in the Steps pane instead). An empty root or a
// scan error yields empty history. Best-effort by design.
func scanHistory(root, exclude string) []history.Run {
	if root == "" {
		return nil
	}
	runs, err := history.Scan(root, time.Now())
	if err != nil {
		return nil
	}
	if exclude == "" {
		return runs
	}
	out := make([]history.Run, 0, len(runs))
	for _, r := range runs {
		if r.Dir != exclude {
			out = append(out, r)
		}
	}
	return out
}

// histRows flattens the History pane into selectable rows: each run header,
// followed by its step children and an All-logs child when expanded.
func (m *Model) histRows() []histRow {
	var rows []histRow
	for r := range m.runs {
		rows = append(rows, histRow{run: r, kind: histRowHeader})
		if m.histExpanded[r] {
			for j := range m.runs[r].Steps {
				rows = append(rows, histRow{run: r, kind: histRowStep, step: j})
			}
			rows = append(rows, histRow{run: r, kind: histRowAll})
		}
	}
	return rows
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// isAllLogs reports whether the Output shows the live "All logs" concatenation.
func (m *Model) isAllLogs() bool { return m.out.kind == outLiveAll }

// isLiveStep reports whether the Output shows a single live step's emulator.
func (m *Model) isLiveStep() bool { return m.out.kind == outLiveStep }

// canType reports whether type mode can be entered: the Output pane must be
// showing the live output of the step that is currently running, so keystrokes
// have somewhere to go (e.g. an interactive sudo password).
func (m *Model) canType() bool {
	return m.running && m.isLiveStep() && m.out.step == m.activeIdx
}

// includedCount is the number of steps in the run set (M in the header N/M).
func (m *Model) includedCount() int {
	n := 0
	for _, v := range m.included {
		if v {
			n++
		}
	}
	return n
}

// visibleStepIndices returns the canonical indices of steps the active filter
// shows, in run order. Filtering is view-only; the run set is unaffected.
func (m *Model) visibleStepIndices() []int {
	out := make([]int, 0, len(m.steps))
	for i := range m.steps {
		if m.filterShows(m.states[i]) {
			out = append(out, i)
		}
	}
	return out
}

// filterShows reports whether a step in state st is visible under the filter.
func (m *Model) filterShows(st engine.State) bool {
	switch m.filter {
	case FilterPending: // hide finished — show pending/running only
		return st == engine.StatePending || st == engine.StateRunning
	case FilterDone: // only ✓/✗ outcomes
		return st == engine.StateOK || st == engine.StateFailed || st == engine.StateAborted
	default:
		return true
	}
}

// States/Durations expose the final run state so Run can render the summary.
func (m *Model) States() []engine.State     { return m.states }
func (m *Model) Durations() []engine.Result { return m.durs }
