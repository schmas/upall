// Command upall updates every installed toolchain (OS packages, chezmoi,
// Homebrew, mise, rust, uv, Claude CLI, AgentKit, fisher, atuin) through a
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

	// Getting this far proves the current binary works, so a previous
	// self-update's rollback copy can go.
	cleanupPreviousUpdate()

	set, err := settings.Load()
	if err != nil {
		fail(err)
	}

	// The update flags need the loaded settings (for the [update] kill switch
	// and the check interval) but no steps at all, so they sit between the two.
	switch {
	case o.checkUpdate:
		os.Exit(runCheckUpdate(context.Background(), os.Stdout, os.Stderr, set, version))
	case o.doUpdate:
		stdinTTY := term.IsTerminal(int(os.Stdin.Fd()))
		os.Exit(runDoUpdate(context.Background(), os.Stdin, os.Stdout, os.Stderr, o, set, version, stdinTTY))
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

	os.Exit(run(steps, o.plain, set, version))
}

// run executes steps and returns the process exit code (the number of failed
// steps). It renders the TUI on an interactive terminal, or plain streaming for
// --plain / NO_COLOR / a non-TTY stdout.
func run(steps []engine.Step, plainFlag bool, set settings.Settings, version string) int {
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
		failed, newExe, err := tui.Run(steps, root, keep, set, version)
		if err != nil {
			fail(err)
		}
		if newExe != "" {
			// Everything the old process owned is done: the terminal is restored,
			// children are reaped, and cancelling the keepalive here (rather than
			// leaving it to the deferred call, which exec would skip) is the last
			// step before the process image is replaced.
			saCancel()
			if err := reexecUpdated(newExe); err != nil {
				// Reexec only returns on failure. The update itself landed, so
				// report it and exit normally — the new binary runs next launch.
				fmt.Fprintf(os.Stderr, "upall: %v\n", err)
			}
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
	// Plain mode owns the passive version check: the TUI runs its own single
	// check from Init(), so a TUI launch must not fire a second one here. The
	// goroutine runs alongside the steps and never delays them.
	var updateCh <-chan updateCheckResult
	if set.Update.Enabled {
		updateCh = startPassiveCheck(version, set)
	}

	sink := plain.New(steps, os.Stdout, false, runDir, set.Notify.Enabled)
	sink.Begin(fmt.Sprintf("upall %s", version))
	runner := engine.NewRunner(runDir, sink)
	runner.DefaultShell = set.Run.Shell
	runner.RunAll(ctx, steps)

	// Summary first, then the update notice as a clearly separate trailing
	// line on stderr, so anything parsing plain-mode step output is unaffected.
	code := sink.End("upall")
	reportPassiveCheck(os.Stderr, updateCh)
	return code
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "upall: %v\n", err)
	os.Exit(2)
}
