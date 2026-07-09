package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadManifestRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "20240102-030405")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	steps := []Step{{Key: "brew", Label: "Homebrew"}, {Key: "os-apt", Label: "OS Update"}}
	states := []State{StateOK, StateFailed}
	durs := []Result{{Dur: 3 * time.Second}, {Dur: 1500 * time.Millisecond}}
	finished := RunDirTime(dir).Add(5 * time.Second)

	if err := WriteManifest(dir, steps, states, durs, finished); err != nil {
		t.Fatal(err)
	}

	// Logs may be sensitive; the manifest is user-only too.
	fi, err := os.Stat(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("manifest mode = %v, want 0600", fi.Mode().Perm())
	}

	m, err := ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Schema != manifestSchema {
		t.Errorf("schema = %d, want %d", m.Schema, manifestSchema)
	}
	if !m.Started.Equal(RunDirTime(dir)) {
		t.Errorf("started = %v, want %v (from dir name)", m.Started, RunDirTime(dir))
	}
	if !m.Finished.Equal(finished) {
		t.Errorf("finished = %v, want %v", m.Finished, finished)
	}
	if len(m.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(m.Steps))
	}
	s0 := m.Steps[0]
	if s0.Pos != 1 || s0.Key != "brew" || s0.Label != "Homebrew" || s0.State != "ok" || s0.DurMs != 3000 {
		t.Errorf("step0 = %+v", s0)
	}
	if m.Steps[1].State != "failed" || m.Steps[1].DurMs != 1500 {
		t.Errorf("step1 = %+v", m.Steps[1])
	}
}

func TestWriteManifestEmptyRunDirIsNoop(t *testing.T) {
	if err := WriteManifest("", nil, nil, nil, time.Now()); err != nil {
		t.Errorf("empty runDir should be a no-op, got %v", err)
	}
}
