package tui

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/plain"
	"github.com/schmas/upall/internal/settings"
)

// reapTimeout bounds how long Run waits, after cancelling, for the runner
// goroutine to finish killing its child (SIGTERM→grace→SIGKILL) so quitting
// mid-run does not orphan processes.
const reapTimeout = 5 * time.Second

// applyReapTimeout bounds how long Run waits, after the program loop ends, for
// an abandoned self-update to finish replacing the binary. It covers the
// uninterruptible part of the sequence: the two renames plus the new binary's
// --version smoke test (itself capped inside internal/update).
const applyReapTimeout = 10 * time.Second

// Run drives the full TUI session over steps and returns the failed-step count
// plus, when a self-update was applied, the path of the replaced binary the
// caller must re-exec. On quit it leaves the alt screen and prints the summary
// to the normal buffer, preserving it in scrollback.
//
// The re-exec path is returned rather than executed here on purpose: exec must
// happen only after every caller-side teardown (notably the sudo keepalive) is
// done, and this function cannot know about that.
func Run(steps []engine.Step, root string, keep int, set settings.Settings, version string) (int, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runControl{ctx: ctx, cancel: cancel, steps: steps}
	m := New(steps, root, keep, rc, set, version)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	sink := NewSink(p)
	// The run dir is created lazily on the first run (m.ensureRunDir), so the
	// runner starts with no dir; the model points it at the dir once a run begins.
	rc.runner = engine.NewRunner("", sink)
	rc.runner.DefaultShell = set.Run.Shell
	rc.launch = func(fn func()) {
		rc.wg.Add(1)
		go func() {
			defer rc.wg.Done()
			fn()
			p.Send(RunDoneMsg{})
		}()
	}

	sink.Start()
	final, err := p.Run()
	sink.Stop()
	cancel()
	waitGroup(&rc.wg, reapTimeout)

	fm, _ := final.(*Model)
	if fm == nil {
		fm = m
	}
	// Bubble Tea abandons command goroutines on quit, so quitting mid-update
	// would otherwise let os.Exit land between the two renames that swap the
	// binary — leaving nothing installed at all. The download was already
	// cancelled on the way out, and the replace itself is sub-second, so this
	// wait is bounded and normally instant.
	waitApply(fm.applyDone, applyReapTimeout)
	// Record the manifest only if a run actually started; merely opening upall and
	// quitting must not leave a history entry. runDir is "" when no run began.
	failed := plain.RenderSummary(os.Stdout, "upall", steps, fm.States(), fm.Durations(), fm.runDir, true, set.Notify.Enabled, fm.started)
	// The alt screen is gone and children are reaped by now, so the update's
	// follow-up note belongs here — visible in scrollback next to the summary.
	if fm.pendingChezmoiNote != "" {
		fmt.Fprintln(os.Stderr, fm.pendingChezmoiNote)
	}
	// A failed apply only got a width-truncated footer line while the dashboard
	// was up, and its worst case is manual recovery instructions — so print it in
	// full, here, where it survives in scrollback.
	if fm.pendingUpdateErr != nil {
		fmt.Fprintf(os.Stderr, "upall: %v\n", fm.pendingUpdateErr)
	}
	return failed, fm.pendingReexec, err
}

// waitApply waits up to d for an in-flight self-update to finish. A nil channel
// (no update was started) returns immediately. A timeout is reported rather than
// waited out: at that point the binary is either already swapped or was never
// touched, and hanging the exit any longer helps nobody.
func waitApply(done <-chan struct{}, d time.Duration) {
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(d):
		fmt.Fprintln(os.Stderr, "upall: warning: a self-update was still in progress at exit; run 'upall --version' to check which build is installed")
	}
}

// waitGroup waits for wg with a timeout so a stuck child cannot hang exit.
func waitGroup(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(d):
	}
}
