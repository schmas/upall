package engine

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DefaultKeep is how many run-log directories to retain when UPALL_KEEP is unset.
const DefaultKeep = 10

// NewRunDir creates a timestamped run directory under ~/.cache/upall, refreshes
// the `latest` symlink, and rotates old runs down to keep. It shells out to
// nothing (pure Go), so it works identically on macOS and Linux.
func NewRunDir(keep int) (string, error) {
	if keep < 1 {
		keep = DefaultKeep
	}
	root := filepath.Join(cacheHome(), "upall")
	dir := filepath.Join(root, time.Now().Format("20060102-150405"))
	// 0700: captured tool output may include tokens/paths; keep it user-only.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	link := filepath.Join(root, "latest")
	_ = os.Remove(link)
	_ = os.Symlink(dir, link)
	rotate(root, keep)
	return dir, nil
}

// cacheHome resolves XDG_CACHE_HOME with a ~/.cache fallback.
func cacheHome() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".cache")
}

// rotate keeps the newest keep run dirs (names sort chronologically) and removes
// the rest. The `latest` symlink is left untouched.
func rotate(root string, keep int) {
	matches, _ := filepath.Glob(filepath.Join(root, "20*"))
	var dirs []string
	for _, m := range matches {
		if fi, err := os.Lstat(m); err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			dirs = append(dirs, m)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	for _, d := range dirs[min(keep, len(dirs)):] {
		_ = os.RemoveAll(d)
	}
}
