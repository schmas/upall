package plain

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/schmas/upall/internal/engine"
	notifypkg "github.com/schmas/upall/internal/notify"
)

// ANSI codes used by the plain sink; blanked at call sites when color is off.
const (
	bold  = "\033[1m"
	dim   = "\033[2m"
	green = "\033[0;32m"
	red   = "\033[0;31m"
	reset = "\033[0m"
)

const summaryWidth = 72

// End prints the run summary and returns the failed-step count (exit code). The
// plain sink always ran the steps, so it records the manifest unconditionally.
func (s *Sink) End(title string) int {
	return RenderSummary(s.out, title, s.steps, s.states, s.durs, s.runDir, s.color, s.notify, true)
}

// RenderSummary writes the closing summary (counts, per-step results, log dir)
// and, on failure, a hint plus a desktop notification. It is shared by the plain
// sink's End and the TUI's on-quit normal-buffer flush so both look identical.
// Returns the number of failed/aborted steps.
func RenderSummary(out io.Writer, title string, steps []engine.Step, states []engine.State, durs []engine.Result, runDir string, color, notify, record bool) int {
	c := colorer(color)
	passed, failed, skipped := tally(states)

	fmt.Fprintln(out)
	bar := strings.Repeat("━", summaryWidth)
	fmt.Fprintf(out, "%s%s%s\n", c(bold), bar, c(reset))
	fmt.Fprintf(out, "%s  %s Summary%s  (%d passed, %d failed, %d skipped, %d total)\n",
		c(bold), title, c(reset), passed, failed, skipped, len(steps))
	for i, st := range steps {
		fmt.Fprintf(out, "  %2d. %-22s %s %s%s%s\n",
			i+1, st.Label, stateLabel(states[i], color), c(dim), engine.Hms(durs[i].Dur), c(reset))
	}
	if runDir != "" {
		fmt.Fprintf(out, "%slogs: %s%s\n", c(dim), runDir, c(reset))
	}
	fmt.Fprintf(out, "%s%s%s\n", c(bold), bar, c(reset))

	if failed > 0 {
		fmt.Fprintf(out, "%s⚠️  %d step(s) failed.%s\n", c(red), failed, c(reset))
		reviewFailures(out, steps, states, runDir, color)
		if notify {
			notifypkg.Failure(title, fmt.Sprintf("%d step(s) failed", failed))
		}
	} else {
		fmt.Fprintf(out, "%s✅ All updates completed successfully!%s\n", c(green), c(reset))
	}

	// Record a per-run manifest for the history browser. Best-effort: a write
	// failure must not change the exit code or the summary output. Skipped when no
	// run happened (record == false) or logging is disabled (runDir == "").
	if record {
		_ = engine.WriteManifest(runDir, steps, states, durs, time.Now())
	}
	return failed
}

func colorer(color bool) func(string) string {
	return func(code string) string {
		if color {
			return code
		}
		return ""
	}
}

func tally(states []engine.State) (passed, failed, skipped int) {
	for _, st := range states {
		switch st {
		case engine.StateOK:
			passed++
		case engine.StateFailed, engine.StateAborted:
			failed++
		case engine.StateSkipped:
			skipped++
		}
	}
	return passed, failed, skipped
}

func stateLabel(st engine.State, color bool) string {
	c := colorer(color)
	switch st {
	case engine.StateOK:
		return "✅ " + c(green) + "success" + c(reset)
	case engine.StateFailed:
		return "❌ " + c(red) + "failed" + c(reset)
	case engine.StateAborted:
		return "⊗ " + c(red) + "aborted" + c(reset)
	case engine.StateSkipped:
		return "⊘ " + c(dim) + "skipped" + c(reset)
	default:
		return "· " + c(dim) + "pending" + c(reset)
	}
}

// reviewFailures lists each failed/aborted step's logfile for later paging.
func reviewFailures(out io.Writer, steps []engine.Step, states []engine.State, runDir string, color bool) {
	if runDir == "" {
		return
	}
	c := colorer(color)
	for i, st := range steps {
		if states[i] == engine.StateFailed || states[i] == engine.StateAborted {
			log := engine.LogPath(runDir, i+1, st.Key)
			fmt.Fprintf(out, "   %s%s%s  %s\n", c(dim), st.Label, c(reset), log)
		}
	}
}
