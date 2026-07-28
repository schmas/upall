package tui

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	selfupdate "github.com/schmas/upall/internal/update"
)

// updateChecks counts how many times the check seam was called, so a test can
// assert the launch check fired (or did not) without any network access.
var updateChecks atomic.Int64

// TestMain replaces the self-update seams for the whole package. Model.Init
// fires a version check whenever [update] is enabled — which Defaults() does —
// so without this every test that runs the program loop would hit the real
// GitHub API. Tests needing a specific outcome override these locally.
func TestMain(mn *testing.M) {
	checkForUpdate = func(string, time.Duration) (*selfupdate.Info, error) {
		updateChecks.Add(1)
		return nil, nil
	}
	applyUpdate = func(context.Context, string, func(int64, int64)) (selfupdate.ApplyResult, error) {
		return selfupdate.ApplyResult{}, errors.New("applyUpdate not stubbed for this test")
	}
	os.Exit(mn.Run())
}
