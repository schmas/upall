//go:build unix

package update

// The syscall.Exec call in Reexec cannot be unit tested: on success it replaces
// the running test process with the target binary, so there is no test process
// left to assert anything. Only the argument/path resolution that runs before
// it is covered here; the exec itself is covered by the required manual smoke
// test in the plan's Phase 2 success criteria (build two versions, --update the
// older one, confirm --version reports the newer one after the re-exec).

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReexecArgs(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName)

	path, args, env, err := reexecArgs(exe)
	if err != nil {
		t.Fatalf("reexecArgs: %v", err)
	}
	if path != exe {
		t.Errorf("path = %q, want %q", path, exe)
	}
	if !reflect.DeepEqual(args, os.Args) {
		t.Errorf("args = %v, want the current argv %v", args, os.Args)
	}
	if len(env) != len(os.Environ()) {
		t.Errorf("env has %d entries, want the current environment's %d", len(env), len(os.Environ()))
	}
}

func TestReexecArgs_RelativePathIsMadeAbsolute(t *testing.T) {
	path, _, _, err := reexecArgs("./" + binaryName)
	if err != nil {
		t.Fatalf("reexecArgs: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path = %q, want an absolute path", path)
	}
}

func TestReexecArgs_EmptyPath(t *testing.T) {
	if _, _, _, err := reexecArgs(""); err == nil {
		t.Error("expected an error for an empty executable path")
	}
}
