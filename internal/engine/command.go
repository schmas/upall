package engine

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/muesli/cancelreader"
)

// outcome distinguishes how a single command finished so the step can be marked
// failed (timeout) vs aborted (quit) vs simply done (ran to completion, any rc).
type outcome int

const (
	outcomeDone outcome = iota
	outcomeTimeout
	outcomeAborted
)

const (
	killGrace  = 3 * time.Second
	drainGrace = 2 * time.Second
)

// runCmd runs one command through a pty, streaming the raw output to both teeW
// (the logfile) and the sink, unmodified. parent is the run context (quit); sctx
// additionally carries the per-step timeout. It returns the exit code and how the
// command finished.
func (r *Runner) runCmd(parent, sctx context.Context, shell, cmdline string, env []string, i int, teeW io.Writer) (int, outcome) {
	c := exec.Command(shell, "-c", cmdline)
	c.Env = env
	ptmx, err := startPTY(c, r.currentSize())
	if err != nil {
		r.sink.Output(i, []byte("upall: cannot start command: "+err.Error()+"\n"))
		return 127, outcomeDone
	}
	r.setActive(ptmx)

	// A pty master read cannot be unblocked by Close() — the fd is not in Go's
	// runtime poller — so if a step backgrounds a process that keeps the slave
	// open past the child's exit, a plain drain would block forever. cancelreader
	// makes the read cancelable; we cancel it after a short grace so the runner
	// never hangs while still draining buffered output on the common path.
	var reader io.Reader = ptmx
	cr, crErr := cancelreader.NewReader(ptmx)
	if crErr == nil {
		reader = cr
	}

	copyDone := make(chan struct{})
	go func() {
		sw := &sinkWriter{i: i, sink: r.sink}
		_, _ = io.Copy(io.MultiWriter(teeW, sw), reader)
		close(copyDone)
	}()

	waitErr, oc := waitCmd(parent, sctx, c)

	// Let buffered output drain on the common path; if a lingering slave-holder
	// blocks EOF, cancel the read (safe to call while the read is in flight).
	select {
	case <-copyDone:
	case <-time.After(drainGrace):
		if crErr == nil {
			cr.Cancel()
		}
	}
	// Join the copy goroutine BEFORE closing so Close never races the in-flight
	// read (cancelreader touches the fd from the read goroutine). The extra
	// timeout is a last-resort backstop for a cancelreader-init failure.
	select {
	case <-copyDone:
	case <-time.After(drainGrace):
	}
	r.setActive(nil)
	if crErr == nil {
		_ = cr.Close()
	}
	_ = ptmx.Close()

	return exitCode(waitErr), oc
}

// waitCmd waits for the command, cancelling it (SIGTERM then SIGKILL) if sctx
// fires first. It classifies a cancellation as aborted (parent/quit) or timeout
// (per-step deadline).
func waitCmd(parent, sctx context.Context, c *exec.Cmd) (error, outcome) {
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	select {
	case err := <-done:
		return err, outcomeDone
	case <-sctx.Done():
		killGroup(c)
		select {
		case err := <-done:
			return err, classifyCancel(parent)
		case <-time.After(killGrace):
			killGroupHard(c)
			return <-done, classifyCancel(parent)
		}
	}
}

// classifyCancel tells timeout (step deadline) apart from abort (run cancelled).
func classifyCancel(parent context.Context) outcome {
	if parent.Err() != nil {
		return outcomeAborted
	}
	return outcomeTimeout
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// sinkWriter adapts an io.Writer onto Sink.Output, forwarding each raw pty read
// to the sink verbatim. The sink owns any line-splitting or sanitizing; the raw
// bytes also reach the logfile untouched via the MultiWriter this sits behind.
// io.Copy reuses its buffer between writes, so a sink that retains p must copy it
// (documented on Sink.Output).
type sinkWriter struct {
	i    int
	sink Sink
}

func (w *sinkWriter) Write(p []byte) (int, error) {
	w.sink.Output(w.i, p)
	return len(p), nil
}
