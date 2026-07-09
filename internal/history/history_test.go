package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/schmas/upall/internal/engine"
)

// mkManifestRun creates a run dir with a manifest for the given step states.
func mkManifestRun(t *testing.T, root, name string, states ...engine.State) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	steps := make([]engine.Step, len(states))
	durs := make([]engine.Result, len(states))
	for i := range states {
		steps[i] = engine.Step{Key: "s" + string(rune('a'+i)), Label: "Step"}
		durs[i] = engine.Result{Dur: time.Second}
	}
	finished := engine.RunDirTime(dir).Add(3 * time.Second)
	if err := engine.WriteManifest(dir, steps, states, durs, finished); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestScanReverseChronoManifestAndLegacy(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.Local)

	newest := mkManifestRun(t, root, "20260709-090000", engine.StateOK, engine.StateFailed)
	mkManifestRun(t, root, "20260708-090000", engine.StateOK)

	// Legacy run: only logfiles, no manifest.
	legacy := filepath.Join(root, "20260707-090000")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"01-brew.log", "02-os-apt.log"} {
		if err := os.WriteFile(filepath.Join(legacy, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A `latest` symlink and a stray non-run dir must be ignored.
	_ = os.Symlink(newest, filepath.Join(root, "latest"))
	if err := os.MkdirAll(filepath.Join(root, "notarun"), 0o700); err != nil {
		t.Fatal(err)
	}

	runs, err := Scan(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3 (symlink + stray dir ignored)", len(runs))
	}
	// Newest first.
	if runs[0].Dir != newest || runs[2].Dir != legacy {
		t.Fatalf("order wrong: %s .. %s", runs[0].Dir, runs[2].Dir)
	}
	// Manifest run: status derived, label human, duration set.
	if !runs[0].HasManifest || runs[0].Status != engine.StateFailed {
		t.Errorf("newest run: manifest=%v status=%v, want manifest+failed", runs[0].HasManifest, runs[0].Status)
	}
	if runs[0].Label != "today 09:00" {
		t.Errorf("label = %q, want 'today 09:00'", runs[0].Label)
	}
	if runs[0].Dur != 2*time.Second { // sum of two 1s steps
		t.Errorf("dur = %v, want 2s (sum of per-step durations)", runs[0].Dur)
	}
	// Legacy run: no manifest, steps from filenames, neutral status.
	if runs[2].HasManifest {
		t.Error("legacy run should have HasManifest=false")
	}
	if len(runs[2].Steps) != 2 || runs[2].Steps[1].Key != "os-apt" {
		t.Errorf("legacy steps = %+v, want brew + os-apt", runs[2].Steps)
	}
	if runs[2].Status != engine.StatePending {
		t.Errorf("legacy status = %v, want neutral (pending)", runs[2].Status)
	}
}

// TestScanIsLazyLoadLogReads proves Scan never needs logfiles (a run with a
// manifest but no logs scans fine) and LoadLog is the only reader.
func TestScanIsLazyLoadLogReads(t *testing.T) {
	root := t.TempDir()
	dir := mkManifestRun(t, root, "20260709-090000", engine.StateOK)

	runs, err := Scan(root, time.Now())
	if err != nil || len(runs) != 1 {
		t.Fatalf("scan should succeed without logfiles: err=%v runs=%d", err, len(runs))
	}
	rs := runs[0].Steps[0]

	// No logfile on disk yet — LoadLog (the only reader) fails here, proving Scan
	// did not depend on reading it.
	if _, err := LoadLog(rs); err == nil {
		t.Error("LoadLog should fail when the logfile is absent (logs read only on demand)")
	}
	if err := os.WriteFile(rs.LogPath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := LoadLog(rs); err != nil || string(b) != "hello" {
		t.Errorf("LoadLog = %q err=%v, want 'hello'", b, err)
	}
	_ = dir
}
