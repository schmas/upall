package update

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupOld(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName)
	old := filepath.Join(dir, oldBinaryName)

	// Absent breadcrumb is the common case and not an error.
	if err := CleanupOld(exe); err != nil {
		t.Errorf("CleanupOld with no %s: %v", oldBinaryName, err)
	}

	if err := os.WriteFile(old, []byte("previous binary"), 0o755); err != nil {
		t.Fatalf("write %s: %v", oldBinaryName, err)
	}
	if err := CleanupOld(exe); err != nil {
		t.Fatalf("CleanupOld: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("%s still exists after CleanupOld (err=%v)", oldBinaryName, err)
	}

	// An empty exe path is a no-op, not a removal of something unrelated.
	if err := CleanupOld(""); err != nil {
		t.Errorf("CleanupOld(\"\"): %v", err)
	}
}

func TestCleanupOld_SweepsStagingButNotRollbackCopies(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName)

	staging, err := os.CreateTemp(dir, stagingPattern)
	if err != nil {
		t.Fatalf("create staging file: %v", err)
	}
	staging.Close()

	// An unpromoted rollback copy can be the only surviving copy of a working
	// binary while another process is mid-update — it must never be swept.
	rollback, err := os.CreateTemp(dir, rollbackPattern)
	if err != nil {
		t.Fatalf("create rollback copy: %v", err)
	}
	rollback.Close()

	if err := CleanupOld(exe); err != nil {
		t.Fatalf("CleanupOld: %v", err)
	}
	if _, err := os.Stat(staging.Name()); !os.IsNotExist(err) {
		t.Errorf("staging file survived CleanupOld (err=%v)", err)
	}
	if _, err := os.Stat(rollback.Name()); err != nil {
		t.Errorf("rollback copy was swept: %v", err)
	}
}
