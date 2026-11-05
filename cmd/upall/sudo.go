package main

import "github.com/schmas/upall/internal/engine"

// needsSudo reports whether any runnable (non-skipped) step requires sudo.
// upall must have the password before the run starts: Setup asks for it once,
// then serves it to `sudo -A` in every step through the askpass helper.
func needsSudo(steps []engine.Step) bool {
	for _, s := range steps {
		if s.Sudo && !s.Skip {
			return true
		}
	}
	return false
}
