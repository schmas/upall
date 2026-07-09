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
func Run(steps []engine.Step, runDir string, set settings.Settings) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	rc := &runControl{ctx: ctx, cancel: cancel, steps: steps}
	m := New(steps, runDir, rc, set)

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	sink := NewSink(p)
	rc.runner = engine.NewRunner(runDir, sink)
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
	failed := plain.RenderSummary(os.Stdout, "upall", steps, fm.States(), fm.Durations(), runDir, true, set.Notify.Enabled)
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
