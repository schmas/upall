package tui

import (
	"context"
	"net/http"
	"time"

	selfupdate "github.com/schmas/upall/internal/update"
)

// updateCheckTimeout bounds the launch version check so a hung network never
// leaves the footer waiting on a check that will not land.
const updateCheckTimeout = 5 * time.Second

// updateApplyTimeout bounds the whole download/verify/replace sequence. It is
// generous because it covers a real archive download on a slow link, but still
// bounded so a stalled transfer cannot pin the "updating" state forever.
const updateApplyTimeout = 10 * time.Minute

// checkForUpdate and applyUpdate are the seams over internal/update. They are
// package vars, not constructor params: New has nine call sites across the
// tests, and a positional param would fan out to all of them. Tests reassign
// these once (see TestMain) so no test in this package touches the network,
// the real binary, or exec.
var (
	checkForUpdate = func(currentVersion string, interval time.Duration) (*selfupdate.Info, error) {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		return selfupdate.MaybeCheck(ctx, http.DefaultClient, currentVersion, selfupdate.CachePath(), interval)
	}

	// The apply takes a caller context so quitting mid-update can abort the
	// download. It cannot abort the binary replacement itself — the smoke test
	// deliberately ignores cancellation (internal/update: a user quitting must
	// not abort the check that decides keep-or-roll-back) — which is exactly why
	// Run also waits briefly for this to finish before the process exits.
	applyUpdate = func(ctx context.Context, currentVersion string, onProgress func(downloaded, total int64)) (selfupdate.ApplyResult, error) {
		ctx, cancel := context.WithTimeout(ctx, updateApplyTimeout)
		defer cancel()
		return selfupdate.Apply(ctx, http.DefaultClient, currentVersion, onProgress)
	}
)
