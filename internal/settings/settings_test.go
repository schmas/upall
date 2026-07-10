package settings

import "testing"

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
	// Every known action has a default binding.
	for _, a := range knownActions {
		if len(d.Keys[a]) == 0 {
			t.Errorf("action %q has no default binding", a)
		}
	}
	if got := d.Keys["quit"]; len(got) != 2 || got[0] != "q" {
		t.Errorf("quit binding = %v, want [q ctrl+c]", got)
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
