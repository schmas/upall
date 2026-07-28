package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/schmas/upall/internal/settings"
	"github.com/schmas/upall/internal/update"
)

// fakeCheck installs a checkForUpdate seam and counts calls, so a test can
// assert both the verdict handling and that a disabled config makes no call at
// all. No test in this package touches the network.
func fakeCheck(t *testing.T, info *update.Info, err error) *int {
	t.Helper()
	calls := 0
	prev := checkForUpdate
	checkForUpdate = func(ctx context.Context, c *http.Client, currentVersion, cachePath string, interval time.Duration) (*update.Info, error) {
		calls++
		return info, err
	}
	t.Cleanup(func() { checkForUpdate = prev })
	return &calls
}

func fakeApply(t *testing.T, res update.ApplyResult, err error) *int {
	t.Helper()
	calls := 0
	prev := applyUpdate
	applyUpdate = func(ctx context.Context, c *http.Client, currentVersion string, onProgress func(int64, int64)) (update.ApplyResult, error) {
		calls++
		return res, err
	}
	t.Cleanup(func() { applyUpdate = prev })
	return &calls
}

func enabledSettings() settings.Settings {
	s := settings.Defaults()
	s.Update.Enabled = true
	return s
}

func disabledSettings() settings.Settings {
	s := settings.Defaults()
	s.Update.Enabled = false
	return s
}

func availableInfo() *update.Info {
	return &update.Info{Current: "v1.4.0", Latest: "v1.5.0", Available: true}
}

func TestRunCheckUpdate(t *testing.T) {
	tests := []struct {
		name     string
		info     *update.Info
		err      error
		set      settings.Settings
		wantCode int
		wantOut  string
		wantErr  string
		wantCall bool
	}{
		{
			name: "update available", info: availableInfo(), set: enabledSettings(),
			wantCode: exitOK, wantOut: "update available: v1.4.0 → v1.5.0", wantCall: true,
		},
		{
			name: "up to date", info: &update.Info{Current: "v1.5.0", Latest: "v1.5.0"}, set: enabledSettings(),
			wantCode: exitOK, wantOut: "up to date (v1.5.0)", wantCall: true,
		},
		{
			name: "dev build", info: nil, set: enabledSettings(),
			wantCode: exitOK, wantOut: "no version information", wantCall: true,
		},
		{
			name: "check failed", err: errors.New("GitHub API returned 403 Forbidden"), set: enabledSettings(),
			wantCode: exitCheckFailed, wantErr: "update check failed: GitHub API returned 403", wantCall: true,
		},
		{
			name: "disabled in config", info: availableInfo(), set: disabledSettings(),
			wantCode: exitCheckFailed, wantErr: "self-update is disabled", wantCall: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := fakeCheck(t, tc.info, tc.err)
			var out, errOut bytes.Buffer

			code := runCheckUpdate(context.Background(), &out, &errOut, tc.set, "v1.4.0")

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if tc.wantOut != "" && !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tc.wantOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErr)
			}
			if got := *calls > 0; got != tc.wantCall {
				t.Errorf("check called = %v, want %v", got, tc.wantCall)
			}
		})
	}
}

func TestRunDoUpdateConfirmGate(t *testing.T) {
	tests := []struct {
		name      string
		stdin     string
		yes       bool
		stdinTTY  bool
		wantCode  int
		wantApply bool
		wantOut   string
		wantErr   string
	}{
		{
			name: "--yes skips the prompt", yes: true, stdinTTY: false,
			wantCode: exitOK, wantApply: true, wantOut: "updated v1.4.0 → v1.5.0",
		},
		{
			name: "tty confirms with y", stdin: "y\n", stdinTTY: true,
			wantCode: exitOK, wantApply: true, wantOut: "Apply update?",
		},
		{
			name: "tty confirms with yes", stdin: "YES\n", stdinTTY: true,
			wantCode: exitOK, wantApply: true,
		},
		{
			name: "tty declines with n", stdin: "n\n", stdinTTY: true,
			wantCode: exitOK, wantApply: false, wantOut: "update cancelled",
		},
		{
			name: "bare newline declines", stdin: "\n", stdinTTY: true,
			wantCode: exitOK, wantApply: false, wantOut: "update cancelled",
		},
		{
			name: "no tty and no --yes is a usage error", stdinTTY: false,
			wantCode: exitUsage, wantApply: false, wantErr: "re-run with --yes",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeCheck(t, availableInfo(), nil)
			applyCalls := fakeApply(t, update.ApplyResult{NewExePath: "/tmp/upall"}, nil)
			var out, errOut bytes.Buffer

			code := runDoUpdate(context.Background(), strings.NewReader(tc.stdin), &out, &errOut,
				opts{doUpdate: true, yes: tc.yes}, enabledSettings(), "v1.4.0", tc.stdinTTY)

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", code, tc.wantCode, errOut.String())
			}
			if got := *applyCalls > 0; got != tc.wantApply {
				t.Errorf("apply called = %v, want %v", got, tc.wantApply)
			}
			if tc.wantOut != "" && !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("stdout = %q, want it to contain %q", out.String(), tc.wantOut)
			}
			if tc.wantErr != "" && !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tc.wantErr)
			}
		})
	}
}

// The confirm prompt must state only what Phase 2 actually guarantees:
// checksum verification over allowlisted HTTPS, not a signed or vetted release.
func TestRunDoUpdateGuaranteeWordingIsNotOversold(t *testing.T) {
	fakeCheck(t, availableInfo(), nil)
	fakeApply(t, update.ApplyResult{}, nil)
	var out, errOut bytes.Buffer

	runDoUpdate(context.Background(), strings.NewReader("n\n"), &out, &errOut,
		opts{doUpdate: true}, enabledSettings(), "v1.4.0", true)

	got := out.String()
	if !strings.Contains(got, "checksum-verified over HTTPS") {
		t.Errorf("prompt should state the checksum/HTTPS guarantee: %q", got)
	}
	for _, oversold := range []string{"signed", "safe", "verified publisher", "trusted"} {
		if strings.Contains(strings.ToLower(got), oversold) {
			t.Errorf("prompt oversells the guarantee with %q: %q", oversold, got)
		}
	}
}

func TestRunDoUpdateNonHappyPaths(t *testing.T) {
	tests := []struct {
		name      string
		info      *update.Info
		checkErr  error
		applyErr  error
		set       settings.Settings
		wantCode  int
		wantApply bool
		want      string
	}{
		{
			name: "disabled in config", info: availableInfo(), set: disabledSettings(),
			wantCode: exitCheckFailed, want: "self-update is disabled",
		},
		{
			name: "check failed does not attempt a replace", checkErr: errors.New("dial tcp: timeout"), set: enabledSettings(),
			wantCode: exitCheckFailed, want: "update check failed",
		},
		{
			name: "already latest", info: &update.Info{Current: "v1.5.0", Latest: "v1.5.0"}, set: enabledSettings(),
			wantCode: exitOK, want: "already up to date",
		},
		{
			name: "dev build cannot update", info: nil, set: enabledSettings(),
			wantCode: exitCheckFailed, want: "cannot self-update",
		},
		{
			name: "apply failure reports and exits 1", info: availableInfo(), applyErr: errors.New("update: checksum mismatch"), set: enabledSettings(),
			wantCode: exitCheckFailed, wantApply: true, want: "checksum mismatch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeCheck(t, tc.info, tc.checkErr)
			applyCalls := fakeApply(t, update.ApplyResult{}, tc.applyErr)
			var out, errOut bytes.Buffer

			code := runDoUpdate(context.Background(), strings.NewReader(""), &out, &errOut,
				opts{doUpdate: true, yes: true}, tc.set, "v1.4.0", false)

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if got := *applyCalls > 0; got != tc.wantApply {
				t.Errorf("apply called = %v, want %v", got, tc.wantApply)
			}
			combined := out.String() + errOut.String()
			if !strings.Contains(combined, tc.want) {
				t.Errorf("output = %q, want it to contain %q", combined, tc.want)
			}
		})
	}
}

func TestRunDoUpdatePrintsChezmoiNote(t *testing.T) {
	fakeCheck(t, availableInfo(), nil)
	fakeApply(t, update.ApplyResult{NewExePath: "/tmp/upall", ChezmoiNote: "note: run chezmoi apply --refresh-externals"}, nil)
	var out, errOut bytes.Buffer

	runDoUpdate(context.Background(), strings.NewReader(""), &out, &errOut,
		opts{doUpdate: true, yes: true}, enabledSettings(), "v1.4.0", false)

	if !strings.Contains(out.String(), "chezmoi apply --refresh-externals") {
		t.Errorf("chezmoi note not printed: %q", out.String())
	}
}

// The passive check must never hold up a run. A deliberately slow checker
// proves startPassiveCheck returns before the check resolves, and that an
// unfinished check prints nothing.
func TestPassiveCheckNeverBlocksTheRun(t *testing.T) {
	release := make(chan struct{})
	prev := checkForUpdate
	checkForUpdate = func(ctx context.Context, c *http.Client, currentVersion, cachePath string, interval time.Duration) (*update.Info, error) {
		<-release
		return availableInfo(), nil
	}
	t.Cleanup(func() {
		checkForUpdate = prev
		close(release)
	})

	start := time.Now()
	ch := startPassiveCheck("v1.4.0", enabledSettings())
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("startPassiveCheck blocked for %v", elapsed)
	}

	// Nothing to report yet: the read is non-blocking and stays silent.
	var buf bytes.Buffer
	reportPassiveCheck(&buf, ch)
	if buf.Len() != 0 {
		t.Errorf("unfinished check printed %q, want silence", buf.String())
	}
}

func TestReportPassiveCheck(t *testing.T) {
	tests := []struct {
		name string
		res  updateCheckResult
		want string
	}{
		{
			name: "available prints a notice",
			res:  updateCheckResult{info: availableInfo()},
			want: "update available v1.4.0 → v1.5.0 — run 'upall --update'",
		},
		{
			name: "up to date stays silent",
			res:  updateCheckResult{info: &update.Info{Current: "v1.5.0", Latest: "v1.5.0"}},
			want: "",
		},
		{
			name: "dev build stays silent",
			res:  updateCheckResult{},
			want: "",
		},
		{
			name: "failure explains itself and says it will retry",
			res:  updateCheckResult{err: errors.New("dial tcp: timeout")},
			want: "update check failed: dial tcp: timeout (will retry next run)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan updateCheckResult, 1)
			ch <- tc.res
			var buf bytes.Buffer

			reportPassiveCheck(&buf, ch)

			if tc.want == "" {
				if buf.Len() != 0 {
					t.Errorf("printed %q, want silence", buf.String())
				}
				return
			}
			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("printed %q, want it to contain %q", buf.String(), tc.want)
			}
		})
	}
}

// A nil channel is the TUI-mode case (no passive check was ever started).
func TestReportPassiveCheckNilChannel(t *testing.T) {
	var buf bytes.Buffer
	reportPassiveCheck(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("nil channel printed %q, want silence", buf.String())
	}
}

func TestCleanupPreviousUpdateIsBestEffort(t *testing.T) {
	calls := 0
	prev := cleanupOldExe
	cleanupOldExe = func(exePath string) error {
		calls++
		if exePath == "" {
			t.Error("cleanup called with an empty exe path")
		}
		return errors.New("permission denied")
	}
	t.Cleanup(func() { cleanupOldExe = prev })

	// A failing cleanup must not panic or propagate — it is a nicety.
	cleanupPreviousUpdate()

	if calls != 1 {
		t.Errorf("cleanup called %d times, want 1", calls)
	}
}
