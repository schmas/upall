package engine

import "testing"

// TestResolveShell covers the shell precedence: an explicit per-step shell wins
// verbatim; else the configured default is used when present on PATH; else the
// availability-checked fallback (defaultShell: bash→sh).
func TestResolveShell(t *testing.T) {
	// "sh" is present on every unix host the tests run on; use it as the known
	// "on PATH" shell and a nonsense name as the known-absent one.
	const absent = "no-such-shell-xyzzy"
	fallback := defaultShell()

	tests := []struct {
		name        string
		stepShell   string
		configShell string
		want        string
	}{
		{"per-step wins verbatim", "sh", "bash", "sh"},
		{"per-step used even when absent (fail loud)", absent, "sh", absent},
		{"config used when on PATH", "", "sh", "sh"},
		{"config ignored when absent → fallback", "", absent, fallback},
		{"empty config → fallback", "", "", fallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveShell(tt.stepShell, tt.configShell); got != tt.want {
				t.Errorf("resolveShell(%q, %q) = %q, want %q", tt.stepShell, tt.configShell, got, tt.want)
			}
		})
	}
}
