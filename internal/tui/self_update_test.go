package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/settings"
	selfupdate "github.com/schmas/upall/internal/update"
)

// availableInfo is a check result reporting a newer release, shaped as the seam
// would return it in production.
func availableInfo() *selfupdate.Info {
	return &selfupdate.Info{Current: "v1.4.0", Latest: "v1.5.0", Available: true}
}

// stubApply swaps the apply seam for one test and restores the package default
// afterwards, so no test leaks a stub into the next one.
func stubApply(t *testing.T, fn func(context.Context, string, func(int64, int64)) (selfupdate.ApplyResult, error)) {
	t.Helper()
	prev := applyUpdate
	applyUpdate = fn
	t.Cleanup(func() { applyUpdate = prev })
}

func pressKey(m *Model, r rune) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// isQuit reports whether cmd is tea.Quit (or a batch is not: quit is returned
// bare, so a direct type check is enough).
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// runCmds executes a (possibly batched) tea.Cmd to completion, returning the
// messages it produced. Batched commands run in order, which is deterministic
// for the apply batch: the apply command emits its progress tick and closes the
// channel before the listener reads it.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, runCmds(c)...)
	}
	return out
}

// progressOf finds the progress tick among a batch's messages.
func progressOf(t *testing.T, msgs []tea.Msg) updateProgressMsg {
	t.Helper()
	for _, msg := range msgs {
		if p, ok := msg.(updateProgressMsg); ok {
			return p
		}
	}
	t.Fatal("no progress message was produced")
	return updateProgressMsg{}
}

// TestInitChecksOnlyWhenEnabled proves the [update] kill switch reaches the
// TUI's own check, not just the CLI flags: disabled means no release request.
func TestInitChecksOnlyWhenEnabled(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	before := updateChecks.Load()
	runCmds(m.Init())
	if updateChecks.Load() != before+1 {
		t.Error("enabled config should fire exactly one launch check")
	}

	set := settings.Defaults()
	set.Update.Enabled = false
	m.set = set
	before = updateChecks.Load()
	runCmds(m.Init())
	if updateChecks.Load() != before {
		t.Error("disabled config should fire no check at all")
	}
}

func TestFooterShowsUpdateBadge(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	if got := footerText(m); !strings.Contains(got, "test") {
		t.Fatalf("footer should show the plain version before any check: %q", got)
	}

	m.Update(updateCheckedMsg{info: availableInfo()})
	if got := footerText(m); !strings.Contains(got, "v1.4.0 → v1.5.0") {
		t.Errorf("footer badge missing after an available update: %q", got)
	}

	// A later successful check that finds nothing clears the badge.
	m.Update(updateCheckedMsg{info: &selfupdate.Info{Current: "v1.5.0", Latest: "v1.5.0"}})
	if got := footerText(m); strings.Contains(got, "→") {
		t.Errorf("badge should clear once up to date: %q", got)
	}
}

func TestFooterShowsCheckFailureQuietly(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	m.Update(updateCheckedMsg{err: errors.New("dial tcp: no route to host")})
	got := footerText(m)
	if !strings.Contains(got, "(check failed)") {
		t.Errorf("failed check should leave a footer suffix: %q", got)
	}
	// Quiet means quiet: the reason is revealed on demand, not shoved into the bar.
	if strings.Contains(got, "no route to host") {
		t.Errorf("the raw error belongs behind the self-update key, not the footer: %q", got)
	}
	if m.updateState != updateIdle {
		t.Error("a failed check must not open any update state")
	}
}

func TestSelfUpdateKeyIsNoOpWhileRunning(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	startRunning(m)

	pressKey(m, 'U')
	if m.updateState != updateIdle {
		t.Fatal("self-update must not start while a run is active")
	}
	if got := footerText(m); !strings.Contains(got, "stop the run first") {
		t.Errorf("footer should say why nothing happened: %q", got)
	}
	if *launched != 0 {
		t.Error("the self-update key must not launch anything")
	}
}

// TestSelfUpdateKeyRetriesAfterFailedCheck pins the retry behavior: a failed
// check must not permanently close the door. Pressing U again fires a fresh
// forced check instead of only re-showing the stale reason, and a release
// found this time opens the confirm prompt directly.
func TestSelfUpdateKeyRetriesAfterFailedCheck(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{err: errors.New("github api: 403 rate limited")})

	prev := checkForUpdate
	t.Cleanup(func() { checkForUpdate = prev })
	checkForUpdate = func(string, time.Duration) (*selfupdate.Info, error) {
		return availableInfo(), nil
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	if m.updateState != updateChecking {
		t.Fatalf("U should start a forced check, state = %v", m.updateState)
	}
	if got := footerText(m); !strings.Contains(got, "checking for the latest release") {
		t.Errorf("footer should show the checking state: %q", got)
	}

	msgs := runCmds(cmd)
	if len(msgs) != 1 {
		t.Fatalf("forced check should produce exactly one message, got %d", len(msgs))
	}
	m.Update(msgs[0])
	if m.updateState != updateConfirming {
		t.Fatalf("a release found on retry should open confirm, state = %v", m.updateState)
	}
}

// TestSelfUpdateKeyForcesLiveCheck covers U with nothing known yet: it must
// perform a live (uncached) check and land on a footer note, not an instant
// "no update available" synthesized from stale state.
func TestSelfUpdateKeyForcesLiveCheck(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	var gotInterval time.Duration
	prev := checkForUpdate
	t.Cleanup(func() { checkForUpdate = prev })
	checkForUpdate = func(_ string, interval time.Duration) (*selfupdate.Info, error) {
		gotInterval = interval
		return &selfupdate.Info{Current: "v1.4.0", Latest: "v1.4.0"}, nil
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	if m.updateState != updateChecking {
		t.Fatalf("U with nothing known should start checking, state = %v", m.updateState)
	}
	if cmd == nil {
		t.Fatal("U should return the forced-check command")
	}

	msgs := runCmds(cmd)
	for _, msg := range msgs {
		m.Update(msg)
	}
	if gotInterval != 0 {
		t.Errorf("a forced check must ignore the cache interval, got %v", gotInterval)
	}
	if m.updateState != updateIdle {
		t.Fatalf("up to date should resolve to idle, state = %v", m.updateState)
	}
	if got := footerText(m); !strings.Contains(got, "already on v1.4.0") {
		t.Errorf("footer should report what the live check found: %q", got)
	}
}

// TestSelfUpdateKeyIgnoresSecondPressWhileChecking proves U does not fire a
// second concurrent request while one is already in flight: handleKey routes
// any key to handleUpdateKey once updateState leaves updateIdle, and
// handleUpdateKey returns nil for anything but updateConfirming, so
// pressSelfUpdate itself is never re-entered mid-check.
func TestSelfUpdateKeyIgnoresSecondPressWhileChecking(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	calls := 0
	prev := checkForUpdate
	t.Cleanup(func() { checkForUpdate = prev })
	checkForUpdate = func(string, time.Duration) (*selfupdate.Info, error) {
		calls++
		return nil, nil
	}

	_, firstCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	if firstCmd == nil {
		t.Fatal("the first press should return the forced-check command")
	}
	if m.updateState != updateChecking {
		t.Fatalf("the first press should enter checking, state = %v", m.updateState)
	}

	_, secondCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	if secondCmd != nil {
		t.Fatal("a second press mid-check must not return a fresh check command")
	}
	if m.updateState != updateChecking {
		t.Fatalf("a second press mid-check must stay in checking, state = %v", m.updateState)
	}
	if calls != 0 {
		t.Fatal("neither command has been run yet, so the seam must not have been called")
	}
}

// TestLateLaunchCheckDoesNotClobberForcedResult covers the race between the
// passive launch check (Model.Init) and a forced recheck U starts before the
// launch check has resolved: the forced check's outcome is what the user is
// waiting on, so a launch-check result landing after it must not overwrite
// the confirm prompt it opened.
func TestLateLaunchCheckDoesNotClobberForcedResult(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)

	pressKey(m, 'U') // starts the forced check; m.updateState == updateChecking
	m.Update(updateCheckedMsg{info: availableInfo(), forced: true})
	if m.updateState != updateConfirming {
		t.Fatalf("forced check should open confirm, state = %v", m.updateState)
	}

	// The launch check (forced: false, its zero value) finally lands with a
	// failure. It must be dropped, not shown, since it is no longer the check
	// the user is looking at.
	m.Update(updateCheckedMsg{err: errors.New("dial tcp: no route to host")})
	if m.updateState != updateConfirming {
		t.Errorf("a late launch-check result must not close the confirm prompt, state = %v", m.updateState)
	}
	if m.updateInfo == nil {
		t.Error("a late launch-check result must not erase the release the forced check found")
	}
	if got := footerText(m); !strings.Contains(got, "Update to v1.5.0?") {
		t.Errorf("confirm prompt should still be showing: %q", got)
	}
}

func TestConfirmCancelHasNoSideEffects(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	applied := 0
	stubApply(t, func(context.Context, string, func(int64, int64)) (selfupdate.ApplyResult, error) {
		applied++
		return selfupdate.ApplyResult{}, nil
	})

	pressKey(m, 'U')
	if m.updateState != updateConfirming {
		t.Fatal("an available update should open the confirm prompt")
	}
	got := footerText(m)
	if !strings.Contains(got, "Update to v1.5.0?") || !strings.Contains(got, "checksum-verified over HTTPS") {
		t.Errorf("confirm prompt missing, or overselling the guarantee: %q", got)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.updateState != updateIdle {
		t.Error("esc should return to idle")
	}
	if applied != 0 || *launched != 0 || m.pendingReexec != "" {
		t.Error("cancelling must not fetch, launch, or stage anything")
	}
}

// TestConfirmEnterAppliesWithoutStartingARun pins the key-collision fix: enter
// is also bound to start/follow, and while confirming it must only confirm.
func TestConfirmEnterAppliesWithoutStartingARun(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	applied := make(chan string, 1)
	stubApply(t, func(_ context.Context, version string, _ func(int64, int64)) (selfupdate.ApplyResult, error) {
		applied <- version
		return selfupdate.ApplyResult{NewExePath: "/tmp/upall"}, nil
	})

	pressKey(m, 'U')
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.updateState != updateApplying {
		t.Fatalf("enter should start applying, state = %v", m.updateState)
	}
	if m.started || m.running || *launched != 0 {
		t.Fatal("confirming an update must never start the run")
	}
	runCmds(cmd)
	select {
	case v := <-applied:
		if v != m.version {
			t.Errorf("apply got version %q, want %q", v, m.version)
		}
	default:
		t.Fatal("apply was never invoked")
	}
}

func TestApplySuccessStagesReexecAndQuits(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.updateState = updateApplying

	_, cmd := m.Update(updateAppliedMsg{res: selfupdate.ApplyResult{
		NewExePath:  "/usr/local/bin/upall",
		ChezmoiNote: "run chezmoi apply --refresh-externals",
	}})

	if m.pendingReexec != "/usr/local/bin/upall" {
		t.Errorf("pendingReexec = %q, want the new binary path", m.pendingReexec)
	}
	if m.pendingChezmoiNote == "" {
		t.Error("the chezmoi note should be stashed for Run to print")
	}
	if m.updateState != updateIdle {
		t.Error("the flow should end idle, not stuck applying")
	}
	if !isQuit(cmd) {
		t.Fatal("a successful apply must quit through the normal shutdown path")
	}
	if !m.quitting {
		t.Error("quitting should be set so the alt screen is left clean")
	}
}

func TestApplyFailureReturnsToIdle(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.updateState = updateApplying

	_, cmd := m.Update(updateAppliedMsg{err: errors.New("checksum mismatch")})

	if cmd != nil {
		t.Error("a failed apply must not quit")
	}
	if m.updateState != updateIdle || m.pendingReexec != "" {
		t.Error("a failed apply must leave the model idle with nothing staged")
	}
	if got := footerText(m); !strings.Contains(got, "checksum mismatch") {
		t.Errorf("the failure reason should be visible: %q", got)
	}
}

// TestProgressRendersAndReArmsListener drives staged progress through the real
// confirm→apply path: the readout must advance and the listener must re-arm,
// since forgetting to re-issue it stalls progress silently.
func TestProgressRendersAndReArmsListener(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	stubApply(t, func(_ context.Context, _ string, onProgress func(int64, int64)) (selfupdate.ApplyResult, error) {
		onProgress(512*1024, 4*1024*1024)
		return selfupdate.ApplyResult{NewExePath: "/tmp/upall"}, nil
	})

	pressKey(m, 'U')
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("confirm should return the apply + listen batch")
	}
	if m.updateProgressCh == nil {
		t.Fatal("applying should own a progress channel")
	}
	if got := footerText(m); !strings.Contains(got, "updating") {
		t.Errorf("footer should show the applying state: %q", got)
	}

	// Running the batch proves the whole chain: onProgress → channel → listener.
	_, next := m.Update(progressOf(t, runCmds(cmd)))
	if next == nil {
		t.Fatal("the progress handler must re-issue the listener or progress stalls")
	}
	if got := footerText(m); !strings.Contains(got, "12%") || !strings.Contains(got, "512.0KiB") {
		t.Errorf("progress readout wrong: %q", got)
	}

	// No Content-Length: an indeterminate readout, never a divide by zero.
	m.Update(updateProgressMsg{updateProgress: updateProgress{downloaded: 2048, total: -1}})
	if got := footerText(m); !strings.Contains(got, "2.0KiB downloaded") || strings.Contains(got, "%") {
		t.Errorf("indeterminate progress readout wrong: %q", got)
	}

	// A closed channel ends the loop instead of re-arming forever.
	if _, last := m.Update(updateProgressMsg{done: true}); last != nil {
		t.Error("a closed progress channel must stop the listener")
	}
}

func TestQuitWorksWhileConfirmingAndApplying(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state updateState
	}{{"checking", updateChecking}, {"confirming", updateConfirming}, {"applying", updateApplying}} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, canceled := testModel(demoSteps())
			sizeUp(m)
			m.updateState = tc.state

			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
			if !isQuit(cmd) {
				t.Fatal("quit must stay reachable mid-update")
			}
			if !*canceled {
				t.Error("quit should still cancel the session context")
			}
		})
	}
}

// TestApplyingSwallowsOtherKeys proves the dashboard's normal bindings cannot
// fire while the binary is being replaced.
func TestApplyingSwallowsOtherKeys(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.updateState = updateApplying

	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune("R")},
		{Type: tea.KeyTab},
	} {
		m.Update(k)
	}
	if m.started || m.running || *launched != 0 || m.focus != FocusSteps {
		t.Error("no dashboard key should act while applying")
	}
}

// TestCheckingSwallowsOtherKeys proves the dashboard's normal bindings cannot
// fire while a forced check U started is still in flight.
func TestCheckingSwallowsOtherKeys(t *testing.T) {
	m, launched, _ := testModel(demoSteps())
	sizeUp(m)
	m.updateState = updateChecking

	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRunes, Runes: []rune("R")},
		{Type: tea.KeyTab},
	} {
		m.Update(k)
	}
	if m.started || m.running || *launched != 0 || m.focus != FocusSteps {
		t.Error("no dashboard key should act while checking")
	}
}

// TestListenProgressClosedChannel proves the listener reports closure rather
// than blocking forever once the apply goroutine is done sending.
func TestListenProgressClosedChannel(t *testing.T) {
	ch := make(chan updateProgress, 1)
	close(ch)
	cmd := listenProgress(ch)
	if cmd == nil {
		t.Fatal("listenProgress returned no command")
	}
	msg, ok := cmd().(updateProgressMsg)
	if !ok || !msg.done {
		t.Fatalf("closed channel should yield done, got %#v", msg)
	}
	if listenProgress(nil) != nil {
		t.Error("a nil channel has nothing to listen to")
	}
}

// TestSeamSignaturesMatchTheUpdatePackage keeps the production defaults honest:
// each seam must accept exactly what the model passes it.
func TestSeamSignaturesMatchTheUpdatePackage(t *testing.T) {
	var _ func(string, time.Duration) (*selfupdate.Info, error) = checkForUpdate
	var _ func(context.Context, string, func(int64, int64)) (selfupdate.ApplyResult, error) = applyUpdate
}

// TestApplyFailureKeepsRetryOpen pins the split between a failed *check* and a
// failed *apply*: the badge must keep telling the truth (an update is still
// available) and pressing the key again must reopen the prompt, not report a
// check failure that never happened.
func TestApplyFailureKeepsRetryOpen(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	m.updateState = updateApplying

	m.Update(updateAppliedMsg{err: errors.New("install new binary: read-only file system")})
	if m.updateErr != nil {
		t.Error("an apply failure is not a check failure")
	}
	if got := footerText(m); strings.Contains(got, "(check failed)") {
		t.Errorf("badge must not claim the check failed: %q", got)
	}
	if m.pendingUpdateErr == nil {
		t.Error("the full error must be kept for Run to print after teardown")
	}
	// Once the note is retired, the badge still advertises the same update.
	pressKey(m, 'a')
	if !strings.Contains(footerText(m), "v1.4.0 → v1.5.0") {
		t.Error("the update is still available, so the badge should stay")
	}

	pressKey(m, 'U')
	if m.updateState != updateConfirming {
		t.Fatal("a failed apply must leave retry reachable")
	}
}

// TestNoteDoesNotDisplaceSafetyHints proves a transient note is additive: the
// stop hint and the sudo-prompt "type password" hint must survive it.
func TestNoteDoesNotDisplaceSafetyHints(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	startRunning(m)
	m.awaitInput = true

	pressKey(m, 'U') // no-op while running: sets the note
	got := footerText(m)
	for _, want := range []string{"x stop", "i type password", "stop the run first"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer lost %q: %s", want, got)
		}
	}
}

func TestMouseIsGatedWhileConfirming(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	pressKey(m, 'U')

	click := tea.MouseMsg{X: m.histRect.x + 1, Y: m.histRect.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	m.Update(click)
	if m.focus != FocusSteps {
		t.Error("a click must not steal focus while the confirm prompt is up")
	}
	if m.updateState != updateConfirming {
		t.Error("a click must not dismiss the confirm prompt")
	}
}

func TestMouseClearsTransientNote(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	set := settings.Defaults()
	set.Update.Enabled = false
	m.set = set
	sizeUp(m)
	pressKey(m, 'U') // "self-update is off ([update] enabled = false)"
	if !strings.Contains(footerText(m), "self-update is off") {
		t.Fatal("expected the note first")
	}
	m.Update(tea.MouseMsg{X: 1, Y: m.stepsRect.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if strings.Contains(footerText(m), "self-update is off") {
		t.Error("a click should retire the note like a keypress does")
	}
}

// TestQuitDuringApplyCancelsAndIsWaitedFor covers the interrupted-install path:
// quit must cancel the download, and Run must not return until the apply
// goroutine is done — otherwise os.Exit can land between the two renames that
// swap the binary.
func TestQuitDuringApplyCancelsAndIsWaitedFor(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	sizeUp(m)
	m.Update(updateCheckedMsg{info: availableInfo()})
	released := make(chan struct{})
	stubApply(t, func(ctx context.Context, _ string, _ func(int64, int64)) (selfupdate.ApplyResult, error) {
		<-ctx.Done() // stands in for a download aborted by the quit
		close(released)
		return selfupdate.ApplyResult{}, ctx.Err()
	})

	pressKey(m, 'U')
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	go runCmds(cmd)

	_, quitCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !isQuit(quitCmd) {
		t.Fatal("quit must work while applying")
	}
	waitApply(m.applyDone, 5*time.Second)
	select {
	case <-released:
	default:
		t.Fatal("Run returned before the in-flight apply finished")
	}
}

func TestWaitApplyNilAndTimeout(t *testing.T) {
	waitApply(nil, time.Second) // must not block

	blocked := make(chan struct{})
	start := time.Now()
	waitApply(blocked, 10*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Error("waitApply should give up at its timeout, not hang the exit")
	}
}

func TestSelfUpdateKeySaysWhenDisabled(t *testing.T) {
	m, _, _ := testModel(demoSteps())
	set := settings.Defaults()
	set.Update.Enabled = false
	m.set = set
	sizeUp(m)

	pressKey(m, 'U')
	if m.updateState != updateIdle {
		t.Fatal("the kill switch must block the flow")
	}
	if got := footerText(m); !strings.Contains(got, "self-update is off") {
		t.Errorf("the no-op should name the kill switch: %q", got)
	}
}
