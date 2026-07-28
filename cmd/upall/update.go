package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/schmas/upall/internal/settings"
	"github.com/schmas/upall/internal/update"
)

// Exit codes for the two update flags. A check that could not reach a verdict
// is deliberately distinct from a usage error so scripts can tell "couldn't
// tell" from "you passed the wrong thing".
const (
	exitOK          = 0
	exitCheckFailed = 1
	exitUsage       = 2
)

// checkTimeout bounds every version check, so a hung network never holds up a
// run or a flag invocation for long.
const checkTimeout = 5 * time.Second

// Seams over internal/update, swapped in tests so no test touches the network,
// the real binary, or exec.
var (
	checkForUpdate = update.MaybeCheck
	applyUpdate    = update.Apply
	cleanupOldExe  = update.CleanupOld
	reexecUpdated  = update.Reexec
)

// updateCachePath is where the version check caches its last observation. The
// TUI checks through the same path, so both surfaces share one cache entry.
func updateCachePath() string {
	return update.CachePath()
}

// disabledNotice reports the config kill switch. It covers the explicit flags
// too, not just the passive check — the point of the switch is to stop upall
// from fetching remote executable content at all.
func disabledNotice(w io.Writer) int {
	fmt.Fprintln(w, "upall: self-update is disabled ([update] enabled = false in config.toml)")
	return exitCheckFailed
}

// runCheckUpdate implements --check-update: a forced check that ignores cache
// freshness, printing the verdict. A failed check prints why and exits 1 — it
// never panics and is never silent.
func runCheckUpdate(ctx context.Context, out, errOut io.Writer, set settings.Settings, currentVersion string) int {
	if !set.Update.Enabled {
		return disabledNotice(errOut)
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	// interval 0 forces a live check but still refreshes the cache.
	info, err := checkForUpdate(ctx, http.DefaultClient, currentVersion, updateCachePath(), 0)
	if err != nil {
		fmt.Fprintf(errOut, "upall: update check failed: %v\n", err)
		return exitCheckFailed
	}
	if info == nil {
		fmt.Fprintf(out, "upall %s has no version information to compare (not a release build)\n", currentVersion)
		return exitOK
	}
	if !info.Available {
		fmt.Fprintf(out, "up to date (%s)\n", info.Latest)
		return exitOK
	}
	fmt.Fprintf(out, "update available: %s → %s\n", info.Current, info.Latest)
	return exitOK
}

// runDoUpdate implements --update: forced check, confirm gate, then the
// download/verify/replace in internal/update.
//
// It deliberately does not re-exec. The plan specified a re-exec here for
// symmetry with the TUI, but this process has nothing left to do afterwards —
// re-execing would replay --update in the new binary, costing a second API
// call to print "already up to date". Re-exec belongs to the TUI path, where
// the user is mid-session.
func runDoUpdate(ctx context.Context, in io.Reader, out, errOut io.Writer, o opts, set settings.Settings, currentVersion string, stdinTTY bool) int {
	if !set.Update.Enabled {
		return disabledNotice(errOut)
	}

	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	info, err := checkForUpdate(checkCtx, http.DefaultClient, currentVersion, updateCachePath(), 0)
	if err != nil {
		// Never proceed to a replace from an unknown state.
		fmt.Fprintf(errOut, "upall: update check failed: %v\n", err)
		return exitCheckFailed
	}
	if info == nil {
		fmt.Fprintf(errOut, "upall: this build (%s) has no version information and cannot self-update; install a release build instead\n", currentVersion)
		return exitCheckFailed
	}
	if !info.Available {
		fmt.Fprintf(out, "already up to date (%s)\n", info.Latest)
		return exitOK
	}

	// State only what is actually guaranteed: the archive's checksum is
	// verified, and both come from an allowlisted host over HTTPS. There is no
	// code signing, so this does not prove the release itself is trustworthy.
	fmt.Fprintf(out, "%s → %s — will be checksum-verified over HTTPS before replacing the binary\n", info.Current, info.Latest)

	switch {
	case o.yes:
		// Explicitly confirmed on the command line.
	case stdinTTY:
		ok, err := confirmPrompt(in, out, "Apply update?")
		if err != nil {
			fmt.Fprintf(errOut, "upall: could not read confirmation: %v\n", err)
			return exitCheckFailed
		}
		if !ok {
			fmt.Fprintln(out, "update cancelled")
			return exitOK
		}
	default:
		fmt.Fprintln(errOut, "upall: --update needs a confirmation; re-run with --yes for non-interactive use")
		return exitUsage
	}

	res, err := applyUpdate(ctx, http.DefaultClient, currentVersion, nil)
	if err != nil {
		fmt.Fprintf(errOut, "upall: %v\n", err)
		return exitCheckFailed
	}
	fmt.Fprintf(out, "updated %s → %s\n", info.Current, info.Latest)
	if res.ChezmoiNote != "" {
		fmt.Fprintln(out, res.ChezmoiNote)
	}
	return exitOK
}

// updateCheckResult carries the passive check's outcome off its goroutine.
type updateCheckResult struct {
	info *update.Info
	err  error
}

// startPassiveCheck kicks off a cached version check concurrently with the
// run. The channel is buffered so the sender never blocks even when nobody
// reads it, and the context is independent of the run's so cancelling the run
// does not turn into a spurious "check failed" notice.
//
// os.Exit does not wait for goroutines: a check still in flight when the run
// ends is simply killed. That is accepted — the cache is shared with the next
// launch, so the notice surfaces soon either way. It must never be "fixed"
// with a blocking wait, which would delay step execution.
func startPassiveCheck(currentVersion string, set settings.Settings) <-chan updateCheckResult {
	ch := make(chan updateCheckResult, 1)
	// Resolve everything the goroutine needs before starting it, so it never
	// reads shared state concurrently with the caller.
	check, cachePath, interval := checkForUpdate, updateCachePath(), set.Update.CheckInterval
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		defer cancel()

		info, err := check(ctx, http.DefaultClient, currentVersion, cachePath, interval)
		ch <- updateCheckResult{info: info, err: err}
	}()
	return ch
}

// reportPassiveCheck prints the trailing notice, if a result is already
// waiting. It never blocks and never affects the exit code: an update notice
// and a failed check are both informational.
func reportPassiveCheck(w io.Writer, ch <-chan updateCheckResult) {
	if ch == nil {
		return
	}
	select {
	case res := <-ch:
		switch {
		case res.err != nil:
			fmt.Fprintf(w, "upall: update check failed: %v (will retry next run)\n", res.err)
		case res.info != nil && res.info.Available:
			fmt.Fprintf(w, "upall: update available %s → %s — run 'upall --update'\n", res.info.Current, res.info.Latest)
		}
	default:
		// Not finished yet — stay silent; the next run reads it from cache.
	}
}

// cleanupPreviousUpdate discards the rollback breadcrumb a previous
// self-update left behind. Reaching this point proves the current binary
// runs, so the previous one is no longer needed. Best-effort by design: a
// cleanup nicety must never fail or delay a normal run.
func cleanupPreviousUpdate() {
	exe, err := update.ExePath()
	if err != nil {
		return
	}
	_ = cleanupOldExe(exe)
}
