//go:build !unix

package update

import (
	"errors"
	"runtime"
)

// Reexec has no non-unix implementation (no execve equivalent, and no non-unix
// release build exists). Callers get a clear error instead of a build failure.
func Reexec(exePath string) error {
	return errors.New("update: self-update is not supported on " + runtime.GOOS)
}
