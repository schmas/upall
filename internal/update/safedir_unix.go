//go:build unix

package update

import (
	"fmt"
	"os"
	"syscall"
)

// checkSafeExeDir refuses to replace a binary that lives somewhere another
// user could tamper with: a group- or world-writable install directory, or one
// owned by a different uid (not ours to manage). Both cases point at a manual
// reinstall — deliberately never at sudo, which would turn a permission
// boundary into a root-level write.
//
// This is a sanity gate, not a TOCTOU defense: it runs once, before a download
// that takes seconds, and inspects only the immediate parent directory.
func checkSafeExeDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("update: stat %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("update: %s is not a directory", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("update: %s is group- or world-writable; upall will not replace itself there — reinstall manually (chezmoi apply --refresh-externals, or go install)", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		// Unknown platform detail; the permission check above still applied.
		return nil
	}
	if uid := os.Getuid(); int(st.Uid) != uid {
		return fmt.Errorf("update: %s is owned by uid %d, not the current user (uid %d); upall will not replace itself there — reinstall manually (chezmoi apply --refresh-externals, or go install)", dir, st.Uid, uid)
	}
	return nil
}
