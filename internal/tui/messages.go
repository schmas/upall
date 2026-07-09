package tui

import (
	"time"

	"github.com/schmas/upall/internal/engine"
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
