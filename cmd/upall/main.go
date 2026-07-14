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
	"syscall"

	"golang.org/x/term"

	"github.com/schmas/upall/internal/config"
	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/plain"
	"github.com/schmas/upall/internal/platform"
	"github.com/schmas/upall/internal/settings"
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
	case o.configPath:
		fmt.Println(settings.ConfigPath())
		return
	case o.initConfig:
		if err := settings.InitConfig(os.Stdout, o.force); err != nil {
			fail(err)
		}
		return
	}

	// On a normal run, seed a commented config.toml if the user has none yet, so
	// there is always a documented file to edit. Best-effort: never fatal.
	if created, err := settings.EnsureConfig(); err != nil {
		fmt.Fprintf(os.Stderr, "upall: warning: could not create default config: %v\n", err)
	} else if created {
		fmt.Fprintf(os.Stderr, "upall: created default config at %s\n", settings.ConfigPath())
	}

	set, err := settings.Load()
	if err != nil {
		fail(err)
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

	os.Exit(run(steps, o.plain, set))
}

// run executes steps and returns the process exit code (the number of failed
// steps). It renders the TUI on an interactive terminal, or plain streaming for
// --plain / NO_COLOR / a non-TTY stdout.
func run(steps []engine.Step, plainFlag bool, set settings.Settings) int {
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
	useTUI := stdoutTTY && !plainFlag && os.Getenv("NO_COLOR") == ""

	// History dir is a single root used for both writing new runs and browsing
	// past ones. Keep honors precedence: UPALL_KEEP env › config › default.
	root := set.History.Dir
	if root == "" {
		root = engine.CacheRoot()
	}
	keep := settings.ResolveKeep(set.History.Keep)

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
		// The TUI creates its run dir lazily, on the first run, so merely opening
		// the dashboard records nothing and never rotates real history.
		failed, err := tui.Run(steps, root, keep, set)
		if err != nil {
			fail(err)
		}
		return failed
	}

	// Plain mode always runs, so create the run dir up front.
	runDir, err := engine.NewRunDir(root, keep)
	if err != nil {
		fmt.Fprintf(os.Stderr, "upall: warning: logging disabled: %v\n", err)
	}
	// Plain mode: Ctrl-C / SIGTERM cancels the run and its child.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	sink := plain.New(steps, os.Stdout, false, runDir, set.Notify.Enabled)
	sink.Begin("upall")
	runner := engine.NewRunner(runDir, sink)
	runner.DefaultShell = set.Run.Shell
	runner.RunAll(ctx, steps)
	return sink.End("upall")
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "upall: %v\n", err)
	os.Exit(2)
}
