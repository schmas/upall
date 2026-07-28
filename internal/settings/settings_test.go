package settings

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	d := Defaults()

	if d.Theme.Accent != "42" || d.Theme.Dim != "240" {
		t.Errorf("theme = %+v, want accent 42 / dim 240", d.Theme)
	}
	if d.History.Dir != "" || d.History.Keep != 0 {
		t.Errorf("history = %+v, want empty dir / keep 0", d.History)
	}
	if d.UI.DefaultFilter != "all" || !d.UI.Wrap || !d.UI.Follow || d.UI.WideThreshold != 90 {
		t.Errorf("ui = %+v, want all/wrap/follow/90", d.UI)
	}
	if !d.Notify.Enabled {
		t.Error("notify should default enabled")
	}
	if d.Run.Shell != "bash" {
		t.Errorf("run.shell = %q, want bash", d.Run.Shell)
	}
	if !d.Update.Enabled || d.Update.CheckInterval != 16*time.Hour {
		t.Errorf("update = %+v, want enabled / 16h", d.Update)
	}
	// Every known action has a default binding.
	for _, a := range knownActions {
		if len(d.Keys[a]) == 0 {
			t.Errorf("action %q has no default binding", a)
		}
	}
	if got := d.Keys["quit"]; len(got) != 2 || got[0] != "q" {
		t.Errorf("quit binding = %v, want [q ctrl+c]", got)
	}
	if got := d.Keys["stop"]; len(got) != 1 || got[0] != "x" {
		t.Errorf("stop binding = %v, want [x]", got)
	}
	if got := d.Keys["self-update"]; len(got) != 1 || got[0] != "U" {
		t.Errorf("self-update binding = %v, want [U]", got)
	}
	if !isKnownAction("self-update") {
		t.Error("self-update should be a known action")
	}
}

// TestDefaultKeysAreUnambiguousPerAction proves the new self-update binding
// does not shadow an existing one. Some keys are deliberately shared across
// panes (enter drives start, follow and expand), so this only checks that "U"
// is not already taken.
func TestSelfUpdateKeyDoesNotCollide(t *testing.T) {
	for action, keys := range Defaults().Keys {
		if action == "self-update" {
			continue
		}
		for _, k := range keys {
			if k == "U" {
				t.Errorf("action %q already binds U", action)
			}
		}
	}
}

// TestDefaultsFreshMap proves Defaults() hands out an independent map each call
// so a caller mutating one result cannot corrupt another.
func TestDefaultsFreshMap(t *testing.T) {
	a := Defaults()
	a.Keys["quit"] = []string{"X"}
	b := Defaults()
	if b.Keys["quit"][0] == "X" {
		t.Error("mutating one Defaults() Keys map leaked into another")
	}
}
