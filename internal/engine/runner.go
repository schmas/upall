package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Runner executes steps sequentially, one at a time. A single Runner drives one
// run; the TUI reuses the same Runner for a retry (RunOne) so there is never
// more than one runner goroutine touching a pty at once.
type Runner struct {
	RunDir string // per-step logs go here; "" disables file teeing (tests)
	// DefaultShell is the configured fallback shell for steps without their own
	// shell. Empty ("") means "unset" and behaves exactly like the pre-config
	// default (defaultShell(): bash→sh). See resolveShell.
	DefaultShell string
	sink         Sink

	mu     sync.Mutex
	size   ptySize
	active *os.File // current command's pty master, for live resize
}

// NewRunner builds a Runner that reports to sink and tees per-step logs into
// runDir (pass "" to skip file logging).
func NewRunner(runDir string, sink Sink) *Runner {
	return &Runner{RunDir: runDir, sink: sink}
}

// SetSize records the terminal size and resizes the currently-running pty, if
// any. Safe to call concurrently with a run (e.g. from the TUI update loop).
func (r *Runner) SetSize(rows, cols uint16) {
	r.mu.Lock()
	r.size = ptySize{Rows: rows, Cols: cols}
	active := r.active
	sz := r.size
	r.mu.Unlock()
	if active != nil {
		setPTYSize(active, sz)
	}
}

func (r *Runner) currentSize() ptySize {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *Runner) setActive(f *os.File) {
	r.mu.Lock()
	r.active = f
	r.mu.Unlock()
}

// RunAll runs every step in order. It stops launching new steps once ctx is
// cancelled; a step already running when ctx is cancelled is reported aborted.
func (r *Runner) RunAll(ctx context.Context, steps []Step) {
	for i := range steps {
		if ctx.Err() != nil {
			return
		}
		r.runStep(ctx, steps, i)
	}
}

// RunOne runs a single step by index (used by the TUI retry). It no-ops if the
// run context is already cancelled, so a retry launched during a quit race does
// not fork a child on a dead context.
func (r *Runner) RunOne(ctx context.Context, steps []Step, i int) {
	if i < 0 || i >= len(steps) || ctx.Err() != nil {
		return
	}
	r.runStep(ctx, steps, i)
}

func (r *Runner) runStep(ctx context.Context, steps []Step, i int) {
	st := steps[i]
	if st.Skip {
		r.sink.Skip(i, st.SkipReason)
		return
	}
	r.sink.StepStart(i)

	sctx := ctx
	if st.Timeout > 0 {
		var cancel context.CancelFunc
		sctx, cancel = context.WithTimeout(ctx, st.Timeout)
		defer cancel()
	}

	teeW := io.Writer(io.Discard)
	if r.RunDir != "" {
		// 0600: logs may contain sensitive tool output; keep them user-only.
		if f, err := os.OpenFile(LogPath(r.RunDir, i+1, st.Key), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600); err == nil {
			defer f.Close()
			teeW = f
		}
	}

	start := time.Now()
	res := r.execStep(ctx, sctx, st, i, teeW)
	res.Dur = time.Since(start)
	r.sink.StepDone(i, res)
}

// execStep runs every command of a step. All commands are attempted even if one
// fails (mirrors v2's `cmd || rc=1` chaining); the step fails if any command
// fails. It stops early only on timeout or quit.
func (r *Runner) execStep(parent, sctx context.Context, st Step, i int, teeW io.Writer) Result {
	shell := resolveShell(st.Shell, r.DefaultShell)
	env := buildEnv(st.Env)
	overallRC := 0
	for _, cmdline := range st.Run {
		rc, oc := r.runCmd(parent, sctx, shell, cmdline, env, i, teeW)
		switch oc {
		case outcomeAborted:
			return Result{State: StateAborted, RC: rc, Reason: "aborted"}
		case outcomeTimeout:
			return Result{State: StateFailed, RC: rc, Reason: "timed out"}
		}
		if rc != 0 {
			overallRC = rc
		}
	}
	if overallRC != 0 {
		return Result{State: StateFailed, RC: overallRC}
	}
	return Result{State: StateOK}
}

// LogPath is the deterministic per-step log path. Both the runner and the
// consumer compute it the same way so state need not be shared to find a log.
func LogPath(runDir string, pos int, key string) string {
	return filepath.Join(runDir, fmt.Sprintf("%02d-%s.log", pos, key))
}
