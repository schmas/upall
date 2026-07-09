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

// run executes steps with the plain sink and returns the process exit code
// (the number of failed steps). Ctrl-C / SIGTERM cancels the run and its child.
func run(steps []engine.Step, plainFlag bool) int {
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	color := stdoutTTY && os.Getenv("NO_COLOR") == "" && !plainFlag

	runDir, _ := engine.NewRunDir(keepFromEnv())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if needsSudo(steps) && stdinTTY {
		if err := primeSudo(); err != nil {
			fail(fmt.Errorf("sudo is required for a selected step: %w", err))
		}
		startSudoKeepalive(ctx)
	}

	sink := plain.New(steps, os.Stdout, color, runDir)
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
