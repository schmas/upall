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

// startPTY starts cmd with stdout+stderr wired to a fresh pty slave and stdin
// wired to /dev/null, in its own process group. Returning the pty master lets
// the caller read the child's colored output. stdin=/dev/null means any read
// by the child gets EOF immediately rather than blocking on input the UI can
// never provide; the process group (Setpgid) lets the caller kill the whole
// subtree (shell + grandchildren) on timeout or quit.
func startPTY(cmd *exec.Cmd, size ptySize) (*os.File, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	sz := size.or(defaultPTYSize)
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: sz.Rows, Cols: sz.Cols})

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		return nil, err
	}
	cmd.Stdin = devnull
	cmd.Stdout = tty
	cmd.Stderr = tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = tty.Close()
		_ = devnull.Close()
		return nil, err
	}
	// The child holds its own dup'd copies after Start; the parent keeps only
	// the master for reading.
	_ = tty.Close()
	_ = devnull.Close()
	return ptmx, nil
}

// setPTYSize resizes a live pty master. Safe to call from another goroutine.
func setPTYSize(ptmx *os.File, size ptySize) {
	sz := size.or(defaultPTYSize)
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: sz.Rows, Cols: sz.Cols})
}

// killGroup signals the child's whole process group: SIGTERM, then SIGKILL if
// it does not exit within the grace window. Setpgid at start guarantees the
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
