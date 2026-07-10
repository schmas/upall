package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// manifestSchema is the on-disk manifest version.
const manifestSchema = 1

// ManifestName is the per-run manifest filename inside a run directory.
const ManifestName = "manifest.json"

// runDirLayout is the timestamp format of a run directory's base name; it is
// also how the run's start time is recovered (the plain sink records no start
// timestamp, so the dir name is the single source of truth for both modes).
const runDirLayout = "20060102-150405"

// Manifest is the machine-readable record of one run, written at run end so the
// history browser can show status and durations without reading logfiles.
type Manifest struct {
	Schema   int            `json:"schema"`
	Started  time.Time      `json:"started"`
	Finished time.Time      `json:"finished"`
	Steps    []ManifestStep `json:"steps"`
}

// ManifestStep is one step's recorded outcome.
type ManifestStep struct {
	Pos   int    `json:"pos"`
	Key   string `json:"key"`
	Label string `json:"label"`
	State string `json:"state"`
	DurMs int64  `json:"dur_ms"`
}

// WriteManifest writes <runDir>/manifest.json for the run. runDir=="" is a
// no-op (logging disabled). started is parsed from the run-dir base name;
// finished is supplied by the caller. The file is mode 0600 — logs may be
// sensitive, so keep the record user-only too.
func WriteManifest(runDir string, steps []Step, states []State, durs []Result, finished time.Time) error {
	if runDir == "" {
		return nil
	}
	m := Manifest{
		Schema:   manifestSchema,
		Started:  RunDirTime(runDir),
		Finished: finished,
		Steps:    make([]ManifestStep, len(steps)),
	}
	for i, s := range steps {
		var st State
		if i < len(states) {
			st = states[i]
		}
		var dur time.Duration
		if i < len(durs) {
			dur = durs[i].Dur
		}
		m.Steps[i] = ManifestStep{
			Pos:   i + 1,
			Key:   s.Key,
			Label: s.Label,
			State: st.String(),
			DurMs: dur.Milliseconds(),
		}
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(runDir, ManifestName), data, 0o600)
}

// ReadManifest reads and decodes a run's manifest.json.
func ReadManifest(runDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(runDir, ManifestName))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// RunDirTime recovers a run's start time from its directory base name, or the
// zero time if the name is not a run timestamp.
func RunDirTime(runDir string) time.Time {
	t, err := time.ParseInLocation(runDirLayout, filepath.Base(runDir), time.Local)
	if err != nil {
		return time.Time{}
	}
	return t
}
