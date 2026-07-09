// Package history is the read-only view over past run-log directories. It scans
// the cache root, parses each run's manifest (falling back to logfile names for
// legacy runs), and loads individual logfiles lazily on demand. It never writes
// anything and never reads a logfile during a scan.
package history

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/schmas/upall/internal/engine"
)

// Run is one past run, newest-first when returned from Scan. HasManifest is
// false for legacy runs (steps recovered from logfile names, status/durations
// unknown).
type Run struct {
	Dir         string
	When        time.Time
	Label       string
	Status      engine.State
	Dur         time.Duration
	Steps       []RunStep
	HasManifest bool
}

// RunStep is one step within a past run. LogPath points at its on-disk logfile,
// read only via LoadLog.
type RunStep struct {
	Pos     int
	Key     string
	Label   string
	State   engine.State
	Dur     time.Duration
	LogPath string
}

// Scan lists the runs under root, newest first. It reads each run's
// manifest.json but never opens a logfile, so it stays cheap. now anchors the
// human labels. A missing root yields an empty slice with no error.
func Scan(root string, now time.Time) ([]Run, error) {
	dirs := engine.RunDirs(root)
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))

	runs := make([]Run, 0, len(dirs))
	for _, dir := range dirs {
		runs = append(runs, scanOne(dir, now))
	}
	return runs, nil
}

// scanOne builds a Run from one directory: manifest if present, else the
// legacy logfile-name fallback.
func scanOne(dir string, now time.Time) Run {
	when := engine.RunDirTime(dir)
	run := Run{
		Dir:    dir,
		When:   when,
		Label:  Label(when, now),
		Status: engine.StatePending, // neutral until proven otherwise
	}

	m, err := engine.ReadManifest(dir)
	if err != nil {
		run.Steps = legacySteps(dir)
		return run
	}

	run.HasManifest = true
	run.Steps = make([]RunStep, len(m.Steps))
	anyFailed := false
	var total time.Duration
	for i, ms := range m.Steps {
		st := engine.ParseState(ms.State)
		if st == engine.StateFailed || st == engine.StateAborted {
			anyFailed = true
		}
		total += time.Duration(ms.DurMs) * time.Millisecond
		run.Steps[i] = RunStep{
			Pos:     ms.Pos,
			Key:     ms.Key,
			Label:   ms.Label,
			State:   st,
			Dur:     time.Duration(ms.DurMs) * time.Millisecond,
			LogPath: engine.LogPath(dir, ms.Pos, ms.Key),
		}
	}
	// Run duration is the sum of the (accurate) per-step durations rather than
	// finished-minus-started: a TUI run's start time is the run-dir creation
	// time, which precedes the user pressing start, so the wall-clock span would
	// include the idle gap.
	run.Dur = total
	if anyFailed {
		run.Status = engine.StateFailed
	} else {
		run.Status = engine.StateOK
	}
	return run
}

// legacySteps recovers steps from a manifest-less run by parsing NN-<key>.log
// filenames. State/durations are unknown, so state stays neutral (StatePending)
// and the label falls back to the key.
func legacySteps(dir string) []RunStep {
	names, _ := filepath.Glob(filepath.Join(dir, "[0-9][0-9]-*.log"))
	sort.Strings(names)
	steps := make([]RunStep, 0, len(names))
	for _, path := range names {
		base := strings.TrimSuffix(filepath.Base(path), ".log")
		i := strings.IndexByte(base, '-')
		if i < 0 {
			continue
		}
		pos, err := strconv.Atoi(base[:i])
		if err != nil {
			continue
		}
		key := base[i+1:]
		steps = append(steps, RunStep{
			Pos:     pos,
			Key:     key,
			Label:   key,
			State:   engine.StatePending,
			LogPath: path,
		})
	}
	return steps
}

// LoadLog reads a single step's logfile on demand. This is the only path that
// touches logfile bytes; Scan never does.
func LoadLog(rs RunStep) ([]byte, error) {
	return os.ReadFile(rs.LogPath)
}
