// Command upall updates every installed toolchain (OS packages, chezmoi,
// Homebrew, mise, rust, uv, Claude CLI, ClaudeKit, fisher, atuin) through a
// config-driven step engine. Steps come entirely from TOML — embedded defaults
// plus a user override layer — never from hardcoded Go.
//
// Phase 2 wires the plain streaming sink; the Bubble Tea TUI arrives in Phase 3.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/term"

	"github.com/schmas/upall/internal/config"
	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/plain"
	"github.com/schmas/upall/internal/platform"
	"github.com/schmas/upall/internal/tui"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		fail(err)
	}
	switch {
	case o.help:
		fmt.Print(usageText)
		return
	case o.version:
		fmt.Printf("upall %s\n", version)
		return
	}

	plat := platform.Detect()
	defs, err := config.Load()
	if err != nil {
		fail(err)
	}
	resolved, err := config.Resolve(defs, plat)
	if err != nil {
		fail(err)
	}
	if o.list {
		printList(os.Stdout, resolved)
		return
	}

	steps, err := config.SelectRun(resolved, o.selected)
	if err != nil {
		fail(err)
	}
	if len(steps) == 0 {
		fail(errors.New("no matching steps to run"))
	}

	os.Exit(run(steps, o.plain))
}

// run executes steps and returns the process exit code (the number of failed
// steps). It renders the TUI on an interactive terminal, or plain streaming for
// --plain / NO_COLOR / a non-TTY stdout.
func run(steps []engine.Step, plainFlag bool) int {
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	useTUI := stdoutTTY && !plainFlag && os.Getenv("NO_COLOR") == ""

	runDir, err := engine.NewRunDir(keepFromEnv())
	if err != nil {
		fmt.Fprintf(os.Stderr, "upall: warning: logging disabled: %v\n", err)
	}

	// The sudo keepalive spans the whole session (both modes), so a retried or
	// late sudo step never has to prompt inside the pty (stdin=/dev/null).
	saCtx, saCancel := context.WithCancel(context.Background())
	defer saCancel()
	if needsSudo(steps) && stdinTTY {
		if err := primeSudo(); err != nil {
			fail(fmt.Errorf("sudo is required for a selected step: %w", err))
		}
		startSudoKeepalive(saCtx)
	}

	if useTUI {
		failed, err := tui.Run(steps, runDir)
		if err != nil {
			fail(err)
		}
		return failed
	}

	// Plain mode: Ctrl-C / SIGTERM cancels the run and its child.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sink := plain.New(steps, os.Stdout, false, runDir)
	sink.Begin("upall")
	engine.NewRunner(runDir, sink).RunAll(ctx, steps)
	return sink.End("upall")
}

func keepFromEnv() int {
	if v := os.Getenv("UPALL_KEEP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return engine.DefaultKeep
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "upall: %v\n", err)
	os.Exit(2)
}
