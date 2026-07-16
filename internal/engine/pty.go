package engine

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// ptySize is the winsize applied to a step's pty. The runner updates it on
// terminal resize (Setsize on the live master); a sane default keeps early
// output well-wrapped before the first WindowSizeMsg arrives.
type ptySize struct{ Rows, Cols uint16 }

func (s ptySize) or(def ptySize) ptySize {
	if s.Rows == 0 || s.Cols == 0 {
		return def
	}
	return s
}

var defaultPTYSize = ptySize{Rows: 40, Cols: 120}

// startPTY starts cmd with stdin+stdout+stderr all wired to a fresh pty slave,
// as its own session leader with that slave as the controlling terminal.
// Returning the pty master lets the caller read the child's colored output
// and write input back (e.g. the TUI's type mode feeding an interactive sudo
// password). A live stdin means a child that reads without anyone typing
// blocks rather than getting instant EOF; per-step Timeout and the TUI's stop
// key are the safety nets. Setsid+Setctty (the same attr set creack/pty's own
// pty.Start uses) make /dev/tty inside the child resolve to this captured
// pty rather than upall's own terminal, so a direct-tty prompt like sudo's
// surfaces in the Output pane. setsid() also makes the child's pgid equal its
// pid, so killGroup's process-group kill still reaps the whole subtree.
//
// Trade-off: making the child a session leader means a step that intentionally
// backgrounds a process past its own completion (`some-daemon &`) no longer
// keeps that process's terminal alive the way a plain Setpgid child did — the
// session (and its controlling terminal) goes away with the session leader, so
// the backgrounded process loses the pty rather than lingering on it. See
// TestBackgroundSlaveHolderDoesNotHang: it still proves the runner never
// hangs, just via prompt session teardown now instead of the cancelreader
// force-cancel fallback it originally guarded.
func startPTY(cmd *exec.Cmd, size ptySize) (*os.File, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	sz := size.or(defaultPTYSize)
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: sz.Rows, Cols: sz.Cols})

	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true

	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		return nil, err
	}
	// The child holds its own dup'd copies after Start; the parent keeps only
	// the master for reading and writing.
	_ = tty.Close()
	return ptmx, nil
}

// setPTYSize resizes a live pty master. Safe to call from another goroutine.
func setPTYSize(ptmx *os.File, size ptySize) {
	sz := size.or(defaultPTYSize)
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: sz.Rows, Cols: sz.Cols})
}

// killGroup signals the child's whole process group: SIGTERM, then SIGKILL if
// it does not exit within the grace window. Setsid at start guarantees the
// group id equals the child pid.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
}

func killGroupHard(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := cmd.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}
