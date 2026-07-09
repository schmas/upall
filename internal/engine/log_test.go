package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRotateKeepsNewest(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"20240101-000000", "20240102-000000", "20240103-000000",
		"20240104-000000", "20240105-000000",
	}
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	rotate(root, 3)

	for _, n := range []string{"20240103-000000", "20240104-000000", "20240105-000000"} {
		if _, err := os.Stat(filepath.Join(root, n)); err != nil {
			t.Errorf("expected %s kept: %v", n, err)
		}
	}
	for _, n := range []string{"20240101-000000", "20240102-000000"} {
		if _, err := os.Stat(filepath.Join(root, n)); !os.IsNotExist(err) {
			t.Errorf("expected %s removed", n)
		}
	}
}

func TestNewRunDirCreatesLatestSymlink(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	dir, err := NewRunDir(10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("run dir not created: %v", err)
	}
	link := filepath.Join(cache, "upall", "latest")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("latest symlink: %v", err)
	}
	if target != dir {
		t.Fatalf("latest -> %q, want %q", target, dir)
	}
}
