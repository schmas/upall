package tui

import (
	"time"

	"github.com/schmas/upall/internal/engine"
	selfupdate "github.com/schmas/upall/internal/update"
)

// Messages carry runner events (via the sink) and timers into the update loop.
// Every mutation of step state happens in Update handling these — never on the
// runner goroutine — which is what keeps the model race-free.

type startMsg struct{ i int }

// bytesMsg is a coalesced batch of raw pty output keyed by step index. Batching
// (flushed on a tick, not per chunk) keeps heavy output from flooding the loop;
// the update loop feeds each step's bytes to its virtual-terminal emulator.
type bytesMsg map[int][]byte

type doneMsg struct {
	i   int
	res engine.Result
}

type skipMsg struct {
	i      int
	reason string
}

// RunDoneMsg signals that the runner goroutine (RunAll or a RunOne retry)
// returned, so no run is active anymore.
type RunDoneMsg struct{}

// pagerDoneMsg is delivered after the external pager exits.
type pagerDoneMsg struct{ err error }

type tickMsg time.Time

// updateCheckedMsg carries the launch version check's outcome. err is reported
// in the footer and never fails the TUI: not knowing whether an update exists
// is not a reason to interrupt a run.
type updateCheckedMsg struct {
	info *selfupdate.Info
	err  error
}

// updateProgress is one download-progress observation, sent from the apply
// goroutine's onProgress callback over a buffered channel. total is -1 when the
// server reported no length.
type updateProgress struct{ downloaded, total int64 }

// updateProgressMsg delivers a progress observation into the update loop. done
// is set when the progress channel closed, which tells the handler to stop
// re-issuing the listener.
type updateProgressMsg struct {
	updateProgress
	done bool
}

// updateAppliedMsg reports the finished download/verify/replace. On success the
// model records the new binary's path for Run to re-exec after teardown.
type updateAppliedMsg struct {
	res selfupdate.ApplyResult
	err error
}

// histSelectMsg fires after the History load-on-navigate debounce elapses. gen
// is the generation captured when the cursor last moved; the handler acts only
// when it still matches m.histSelGen, so rapid navigation loads the log only
// for the row the cursor finally settles on.
type histSelectMsg struct{ gen int }
