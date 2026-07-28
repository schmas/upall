//go:build unix

package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Reexec replaces the current process image with exePath, preserving argv and
// the environment, so an updated upall picks up where the old one left off
// without the user quitting and reopening it.
//
// On success it never returns — the process image is gone. Callers MUST invoke
// it only after all of their own teardown (terminal restore, child reap, sudo
// keepalive cancel) has completed; this package knows nothing about that
// sequence and cannot undo a premature exec.
func Reexec(exePath string) error {
	path, args, env, err := reexecArgs(exePath)
	if err != nil {
		return err
	}
	if err := syscall.Exec(path, args, env); err != nil {
		return fmt.Errorf("update: re-exec %s: %w", path, err)
	}
	return nil
}

// reexecArgs resolves everything the exec call needs. It is split out from
// Reexec because syscall.Exec itself cannot be unit tested — it would replace
// the test process — while this part can.
func reexecArgs(exePath string) (path string, args, env []string, err error) {
	if exePath == "" {
		return "", nil, nil, errors.New("update: no executable path to re-exec")
	}
	abs, err := filepath.Abs(exePath)
	if err != nil {
		return "", nil, nil, fmt.Errorf("update: resolve %s: %w", exePath, err)
	}
	args = os.Args
	if len(args) == 0 {
		// argv[0] must exist; execve with an empty argv confuses the callee.
		args = []string{abs}
	}
	return abs, args, os.Environ(), nil
}
