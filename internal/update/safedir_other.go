//go:build !unix

package update

import (
	"errors"
	"runtime"
)

// checkSafeExeDir has no non-unix implementation: the ownership and permission
// model it relies on does not exist there, and no non-unix release build does
// either (goreleaser targets darwin/linux only). Refusing keeps go build sane
// if that ever changes.
func checkSafeExeDir(dir string) error {
	return errors.New("update: self-update is not supported on " + runtime.GOOS)
}
