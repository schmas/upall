package main

import (
	"context"
	"os"
	"os/exec"
	"time"

	"github.com/schmas/upall/internal/engine"
)

// needsSudo reports whether any runnable (non-skipped) step requires sudo.
func needsSudo(steps []engine.Step) bool {
	for _, s := range steps {
		if s.Sudo && !s.Skip {
			return true
		}
	}
	return false
}

// primeSudo prompts for the sudo password once, before the run, while stdin is
// a real terminal. Inside the run the pty child has stdin=/dev/null and cannot
// prompt, so priming (plus the keepalive) is what keeps sudo steps working.
func primeSudo() error {
	cmd := exec.Command("sudo", "-v", "-p", "Enter your sudo password: ")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// startSudoKeepalive refreshes the sudo timestamp until ctx is cancelled, so a
// long run never lets credentials lapse mid-step. ctx spans the whole session,
// not just one step, so retried sudo steps stay authenticated.
func startSudoKeepalive(ctx context.Context) {
	go func() {
		t := time.NewTicker(50 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = exec.Command("sudo", "-n", "true").Run()
			}
		}
	}()
}
