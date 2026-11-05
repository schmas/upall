package engine

import (
	"strings"
	"testing"
)

// envMap turns a KEY=VALUE slice into a map for assertions.
func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

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

func TestBuildEnvOverlayVars(t *testing.T) {
	ov := EnvOverlay{Vars: map[string]string{"SUDO_ASKPASS": "/x/askpass"}}
	got := envMap(buildEnv(nil, ov))
	if got["SUDO_ASKPASS"] != "/x/askpass" {
		t.Errorf("SUDO_ASKPASS = %q, want /x/askpass", got["SUDO_ASKPASS"])
	}
}

func TestBuildEnvStepEnvBeatsOverlayVars(t *testing.T) {
	ov := EnvOverlay{Vars: map[string]string{"SUDO_ASKPASS": "/overlay"}}
	got := envMap(buildEnv(map[string]string{"SUDO_ASKPASS": "/step"}, ov))
	if got["SUDO_ASKPASS"] != "/step" {
		t.Errorf("SUDO_ASKPASS = %q, want /step", got["SUDO_ASKPASS"])
	}
}

func TestBuildEnvPathPrefix(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	got := envMap(buildEnv(nil, EnvOverlay{PathPrefix: "/w"}))
	if got["PATH"] != "/w:/usr/bin:/bin" {
		t.Errorf("PATH = %q, want /w:/usr/bin:/bin", got["PATH"])
	}
}

func TestBuildEnvPathPrefixWithStepPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	got := envMap(buildEnv(map[string]string{"PATH": "/s"}, EnvOverlay{PathPrefix: "/w"}))
	if got["PATH"] != "/w:/s" {
		t.Errorf("PATH = %q, want /w:/s", got["PATH"])
	}
}

func TestBuildEnvZeroOverlay(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("SUDO_ASKPASS", "")
	got := envMap(buildEnv(nil, EnvOverlay{}))
	if got["PATH"] != "/usr/bin:/bin" {
		t.Errorf("PATH = %q, want unchanged /usr/bin:/bin", got["PATH"])
	}
	if v := got["SUDO_ASKPASS"]; v != "" {
		t.Errorf("SUDO_ASKPASS = %q, want empty with a zero overlay", v)
	}
	if got["GIT_TERMINAL_PROMPT"] != "0" {
		t.Errorf("non-interactive defaults missing: %v", got["GIT_TERMINAL_PROMPT"])
	}
}
