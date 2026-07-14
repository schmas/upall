package tui

import (
	"context"
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

// Run drives the full TUI session over steps and returns the failed-step count.
// On quit it leaves the alt screen and prints the summary to the normal buffer,
// preserving it in scrollback.
func Run(steps []engine.Step, root string, keep int, set settings.Settings) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runControl{ctx: ctx, cancel: cancel, steps: steps}
	m := New(steps, root, keep, rc, set)

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
	// Record the manifest only if a run actually started; merely opening upall and
	// quitting must not leave a history entry. runDir is "" when no run began.
	failed := plain.RenderSummary(os.Stdout, "upall", steps, fm.States(), fm.Durations(), fm.runDir, true, set.Notify.Enabled, fm.started)
	return failed, err
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
