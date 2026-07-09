// Package config turns TOML step definitions into runnable engine.Steps. It
// loads two layers — embedded defaults (read-only) and a user layer under
// $XDG_CONFIG_HOME/upall/steps.d — merges them field-wise by key, and resolves
// which steps apply to the host platform. Nothing about the step set is
// hardcoded in Go; every step comes from TOML.
package config

import (
	"time"

	"github.com/schmas/upall/internal/engine"
)

// File is one TOML config file: a schema version and zero or more steps.
type File struct {
	Schema int       `toml:"schema"`
	Steps  []StepDef `toml:"step"`
}

// StepDef is the decoded TOML form of a step. Scalar fields are POINTERS so the
// merge can tell "unset" (nil, inherit from the lower layer) from an explicit
// zero value like sudo=false or order=0 — a plain bool/int could not. Slices
// use nil-vs-non-nil for the same distinction; Env merges key-wise.
type StepDef struct {
	Key     string            `toml:"key"`
	Label   *string           `toml:"label"`
	OS      []string          `toml:"os"`
	Distro  []string          `toml:"distro"`
	Detect  *string           `toml:"detect"`
	Shell   *string           `toml:"shell"`
	Sudo    *bool             `toml:"sudo"`
	Run     []string          `toml:"run"`
	Env     map[string]string `toml:"env"`
	Enabled *bool             `toml:"enabled"`
	Order   *int              `toml:"order"`
	Timeout *string           `toml:"timeout"`
}

// Resolved pairs a runtime Step with whether it applies to this host.
type Resolved struct {
	engine.Step
	Applies  bool // platform (os/distro) predicate matched
	DetectOK bool // detect snippet passed (or none); only meaningful when Applies
}

// toStep converts a merged StepDef to a runtime engine.Step, filling defaults
// for unset optional fields.
func toStep(d StepDef) (engine.Step, error) {
	s := engine.Step{
		Key:    d.Key,
		Label:  d.Key,
		OS:     d.OS,
		Distro: d.Distro,
		Run:    d.Run,
		Env:    d.Env,
		Order:  deref(d.Order, 0),
		Sudo:   deref(d.Sudo, false),
		Detect: deref(d.Detect, ""),
		Shell:  deref(d.Shell, ""),
	}
	if d.Label != nil {
		s.Label = *d.Label
	}
	if d.Timeout != nil {
		dur, err := time.ParseDuration(*d.Timeout)
		if err != nil {
			return s, err
		}
		s.Timeout = dur
	}
	return s, nil
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
