// Package engine is the UI-agnostic execution core for upall: a sequential
// step runner that captures each step through a pty (for real colors and
// progress), streams output to a Sink, tees raw bytes to a logfile, and
// enforces a per-step timeout that doubles as a hang watchdog.
package engine

import (
	"fmt"
	"time"
)

// State is a step's lifecycle state.
type State int

const (
	// StatePending is the initial state before a step runs.
	StatePending State = iota
	// StateSkipped means the step did not apply (platform/detect) and was not run.
	StateSkipped
	// StateRunning means the step is currently executing.
	StateRunning
	// StateOK means the step finished with exit code 0.
	StateOK
	// StateFailed means the step exited non-zero or timed out.
	StateFailed
	// StateAborted means the run was cancelled (quit) while the step ran.
	StateAborted
)

// String returns the lowercase name of the state.
func (s State) String() string {
	switch s {
	case StateSkipped:
		return "skipped"
	case StateRunning:
		return "running"
	case StateOK:
		return "ok"
	case StateFailed:
		return "failed"
	case StateAborted:
		return "aborted"
	default:
		return "pending"
	}
}

// ParseState is the inverse of String: it maps a state name back to a State,
// defaulting to StatePending for unknown/empty input (used when reading a run
// manifest).
func ParseState(s string) State {
	switch s {
	case "skipped":
		return StateSkipped
	case "running":
		return StateRunning
	case "ok":
		return StateOK
	case "failed":
		return StateFailed
	case "aborted":
		return StateAborted
	default:
		return StatePending
	}
}

// Glyph returns the single-rune status marker for a state.
func Glyph(s State) string {
	switch s {
	case StateOK:
		return "✓"
	case StateFailed:
		return "✗"
	case StateRunning:
		return "▶"
	case StateSkipped:
		return "⊘"
	case StateAborted:
		return "⊗"
	default:
		return "·"
	}
}

// Step is the runtime form of a configured update step. All fields are
// read-only once a run starts: the runner never mutates a Step. Lifecycle
// state is reported through the Sink and owned by the consumer, which keeps
// the runner goroutine free of shared-memory writes (race-safe by design).
type Step struct {
	Key     string   // stable identifier, e.g. "brew"
	Label   string   // human label, e.g. "Homebrew"
	OS      []string // GOOS predicate; empty means "any"
	Distro  []string // /etc/os-release ID predicate; empty means "any"
	Detect  string   // shell snippet; exit 0 => applies (evaluated by config layer)
	Shell   string   // shell used to run commands; empty => default (bash|sh)
	Sudo    bool     // step needs sudo primed before the run
	Run     []string // commands; each run via `shell -c`, all attempted, fail if any fails
	Env     map[string]string
	Order   int           // explicit sort order mirroring v2
	Timeout time.Duration // per-step hang watchdog; 0 means no timeout

	// Skip is set by the config layer when the platform/detect predicate did
	// not match; the runner reports it via Sink.Skip instead of executing.
	Skip       bool
	SkipReason string
}

// Result is the outcome of running a step, reported via Sink.StepDone.
type Result struct {
	State  State
	RC     int
	Dur    time.Duration
	Reason string // e.g. "timed out"; empty on success
}

// Hms formats a duration as "1h2m" / "3m4s" / "5s", mirroring v2's upall_hms.
func Hms(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s >= 3600:
		return fmt.Sprintf("%dh%dm", s/3600, (s%3600)/60)
	case s >= 60:
		return fmt.Sprintf("%dm%ds", s/60, s%60)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
