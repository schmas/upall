package tui

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// pressSelfUpdate handles the self-update key. It is only ever reached from
// updateIdle — handleKey routes every other state to handleUpdateKey instead
// (see below), which is what makes pressing U again while a check, confirm, or
// apply is already in flight a no-op rather than a second concurrent request.
// Priority order: a live run wins (pressing it mid-run must never reach an
// exec that would orphan a pty child or skip the manifest/reap sequence), then
// a known-available update (straight to confirm, no extra request). Anything
// else — never checked this session, cache said current, or the last check
// failed — starts a forced check (same seam as --check-update) so U always
// verifies instead of only repeating whatever the last check left behind.
func (m *Model) pressSelfUpdate() tea.Cmd {
	switch {
	case !m.set.Update.Enabled:
		m.updateNote = "self-update is off ([update] enabled = false)"
	case m.running:
		m.updateNote = "stop the run first to self-update"
	case m.updateInfo != nil:
		// An available release outranks an earlier failure: a check that failed
		// after a successful one, or a previous apply that failed, must not close
		// the retry path for the rest of the session.
		m.updateState = updateConfirming
	default:
		m.updateState = updateChecking
		return m.checkUpdateCmd(true)
	}
	return nil
}

// handleUpdateKey routes keys while checking, confirming, or applying. Quit is
// handled by the caller before this runs, so it is always reachable. Outside
// updateConfirming — checking (bounded by updateCheckTimeout) and applying —
// keys are swallowed: there is nothing safe for them to do mid-flight.
func (m *Model) handleUpdateKey(msg tea.KeyMsg) tea.Cmd {
	if m.updateState != updateConfirming {
		return nil
	}
	switch {
	case msg.Type == tea.KeyEnter, key.Matches(msg, confirmYes):
		return m.startUpdate()
	case msg.Type == tea.KeyEsc, key.Matches(msg, confirmNo):
		// Cancelling has zero side effects: nothing has been fetched yet.
		m.updateState = updateIdle
	}
	return nil
}

// confirmYes/confirmNo are the confirm prompt's own keys. They are fixed rather
// than rebindable: the prompt is modal and its two answers must not depend on
// the user's [keys] table (where 'y'/'n' may be bound to other actions).
var (
	confirmYes = key.NewBinding(key.WithKeys("y", "Y"))
	confirmNo  = key.NewBinding(key.WithKeys("n", "N"))
)

// startUpdate launches the download/verify/replace as a tea.Cmd and a listener
// for its progress ticks. Apply touches only the network and the filesystem —
// never the terminal — so it is safe inside a Cmd; the re-exec is not, and
// happens in Run after teardown instead.
func (m *Model) startUpdate() tea.Cmd {
	m.updateState = updateApplying
	m.updateDownloaded, m.updateTotal = 0, 0

	// Capacity 1 with a non-blocking send: a slow update loop misses intermediate
	// ticks rather than stalling the download. The apply Cmd is the only sender,
	// so it can close the channel when it returns, which stops the listener.
	ch := make(chan updateProgress, 1)
	m.updateProgressCh = ch
	// Bubble Tea does not wait for command goroutines on quit, so the apply gets
	// its own cancel (to abort a download the user quit out of) and a done
	// channel Run waits on (so the process cannot exit between the two renames
	// that swap the binary).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.applyCancel, m.applyDone = cancel, done
	version := m.version
	apply := func() tea.Msg {
		defer cancel()
		defer close(done)
		defer close(ch)
		res, err := applyUpdate(ctx, version, func(downloaded, total int64) {
			select {
			case ch <- updateProgress{downloaded: downloaded, total: total}:
			default:
			}
		})
		return updateAppliedMsg{res: res, err: err}
	}
	return tea.Batch(apply, listenProgress(ch))
}

// cancelApply aborts an in-flight download. It is called on quit: the replace
// itself is uninterruptible by design, so this only shortens the wait for a
// download the user no longer wants. A no-op when nothing is applying.
func (m *Model) cancelApply() {
	if m.applyCancel != nil {
		m.applyCancel()
	}
}

// listenProgress reads one progress tick and returns it as a message. The
// handler re-issues it after each tick, which is what keeps progress flowing
// without a goroutine writing model fields directly.
func listenProgress(ch chan updateProgress) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		p, ok := <-ch
		if !ok {
			return updateProgressMsg{done: true}
		}
		return updateProgressMsg{updateProgress: p}
	}
}

// recordUpdateCheck stores a version check's outcome, from either the silent
// launch check or a forced recheck U triggered. A failure is recorded, never
// fatal and never a dialog: the footer gains a quiet "(check failed)" suffix.
// msg.forced tells the caller which check this is: the launch check stays
// silent (footer badge only, as before), while a forced recheck must land
// somewhere visible — the confirm prompt when a release showed up, or a
// footer note saying what it found otherwise — since U was pressed
// specifically to find out.
//
// The two checks can overlap (U pressed during the launch check's own
// network round trip): a late launch-check result arriving after the forced
// one already moved the model out of updateIdle must not clobber what the
// forced check established (an open confirm prompt, or its note) — the
// forced check is the one the user is actually waiting on.
func (m *Model) recordUpdateCheck(msg updateCheckedMsg) {
	if !msg.forced && m.updateState != updateIdle {
		return
	}
	if msg.forced {
		m.updateState = updateIdle
	}
	if msg.err != nil {
		m.updateErr = msg.err
		m.updateInfo = nil
		if msg.forced {
			m.updateNote = "update check failed: " + msg.err.Error()
		}
		return
	}
	m.updateErr = nil
	if msg.info != nil && msg.info.Available {
		m.updateInfo = msg.info
		if msg.forced {
			m.updateState = updateConfirming
		}
		return
	}
	// No release info (unparsable local version) or already current.
	m.updateInfo = nil
	if msg.forced {
		if msg.info != nil {
			m.updateNote = "already on " + msg.info.Latest
		} else {
			m.updateNote = "no version info to compare (not a release build)"
		}
	}
}

// recordUpdateProgress advances the progress readout and re-arms the listener
// until the channel closes. Late ticks arriving after the apply resolved are
// dropped so a finished update cannot be redrawn as still downloading.
func (m *Model) recordUpdateProgress(msg updateProgressMsg) tea.Cmd {
	if msg.done || m.updateState != updateApplying {
		return nil
	}
	m.updateDownloaded, m.updateTotal = msg.downloaded, msg.total
	return listenProgress(m.updateProgressCh)
}

// recordUpdateApplied finishes the flow. On success it records the new binary
// for Run to exec after teardown and quits through the ordinary shutdown path,
// so the manifest, child reap, and summary all still happen. On failure it
// returns to idle with the reason discoverable, never a crash.
func (m *Model) recordUpdateApplied(msg updateAppliedMsg) tea.Cmd {
	m.updateState = updateIdle
	m.updateProgressCh = nil
	m.applyCancel, m.applyDone = nil, nil
	if msg.err != nil {
		// Deliberately NOT updateErr: that field means "the check failed" and
		// drives the footer's "(check failed)" suffix. An apply failure leaves the
		// check result true and the update still available to retry. The full text
		// goes to pendingUpdateErr because the footer cell truncates it.
		m.pendingUpdateErr = msg.err
		m.updateNote = "update failed: " + msg.err.Error()
		return nil
	}
	m.pendingReexec = msg.res.NewExePath
	m.pendingChezmoiNote = msg.res.ChezmoiNote
	m.quitting = true
	return tea.Quit
}
