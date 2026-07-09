package plain

import (
	"fmt"
	"strings"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/notify"
)

// ANSI codes used by the plain sink; blanked at call sites when color is off.
const (
	bold  = "\033[1m"
	dim   = "\033[2m"
	green = "\033[0;32m"
	red   = "\033[0;31m"
	reset = "\033[0m"
)

// End prints the closing summary, a failure hint + notification if anything
// failed, and returns the failed-step count (the process exit code).
func (s *Sink) End(title string) int {
	passed, failed, skipped := s.tally()
	total := len(s.steps)

	fmt.Fprintln(s.out)
	bar := strings.Repeat("━", s.width)
	fmt.Fprintf(s.out, "%s%s%s\n", s.c(bold), bar, s.c(reset))
	fmt.Fprintf(s.out, "%s  %s Summary%s  (%d passed, %d failed, %d skipped, %d total)\n",
		s.c(bold), title, s.c(reset), passed, failed, skipped, total)
	for i, st := range s.steps {
		fmt.Fprintf(s.out, "  %2d. %-22s %s %s%s%s\n",
			i+1, st.Label, s.stateLabel(s.states[i]),
			s.c(dim), engine.Hms(s.durs[i].Dur), s.c(reset))
	}
	if s.runDir != "" {
		fmt.Fprintf(s.out, "%slogs: %s%s\n", s.c(dim), s.runDir, s.c(reset))
	}
	fmt.Fprintf(s.out, "%s%s%s\n", s.c(bold), bar, s.c(reset))

	if failed > 0 {
		fmt.Fprintf(s.out, "%s⚠️  %d step(s) failed.%s\n", s.c(red), failed, s.c(reset))
		s.reviewFailures()
		notify.Failure(title, fmt.Sprintf("%d step(s) failed", failed))
	} else {
		fmt.Fprintf(s.out, "%s✅ All updates completed successfully!%s\n", s.c(green), s.c(reset))
	}
	return failed
}

func (s *Sink) tally() (passed, failed, skipped int) {
	for _, st := range s.states {
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

func (s *Sink) stateLabel(st engine.State) string {
	switch st {
	case engine.StateOK:
		return "✅ " + s.c(green) + "success" + s.c(reset)
	case engine.StateFailed:
		return "❌ " + s.c(red) + "failed" + s.c(reset)
	case engine.StateAborted:
		return "⊗ " + s.c(red) + "aborted" + s.c(reset)
	case engine.StateSkipped:
		return "⊘ " + s.c(dim) + "skipped" + s.c(reset)
	default:
		return "· " + s.c(dim) + "pending" + s.c(reset)
	}
}

// reviewFailures lists each failed step's logfile for later paging.
func (s *Sink) reviewFailures() {
	if s.runDir == "" {
		return
	}
	for i, st := range s.steps {
		if s.states[i] == engine.StateFailed || s.states[i] == engine.StateAborted {
			log := engine.LogPath(s.runDir, i+1, st.Key)
			fmt.Fprintf(s.out, "   %s%s%s  %s\n", s.c(dim), st.Label, s.c(reset), log)
		}
	}
}
