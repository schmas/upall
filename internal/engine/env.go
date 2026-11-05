package engine

import (
	"os"
	"os/exec"
	"strings"
)

// nonInteractiveEnv keeps children from blocking on a prompt. Combined with a
// /dev/null stdin (see pty.go), a `read` returns EOF instead of hanging the UI.
var nonInteractiveEnv = map[string]string{
	"GIT_TERMINAL_PROMPT":     "0",
	"DEBIAN_FRONTEND":         "noninteractive",
	"HOMEBREW_NO_INTERACTIVE": "1",
}

// forceColorEnv persuades tools to emit ANSI even though stdout is a pty we
// capture rather than the user's terminal. Skipped for steps that set NO_COLOR.
var forceColorEnv = map[string]string{
	"CLICOLOR_FORCE": "1",
	"FORCE_COLOR":    "1",
	"HOMEBREW_COLOR": "1",
}

var colorVars = []string{"CLICOLOR_FORCE", "FORCE_COLOR", "HOMEBREW_COLOR"}

// EnvOverlay is a run-level addition to every step's environment.
type EnvOverlay struct {
	Vars       map[string]string // set after the non-interactive and color defaults, before step env
	PathPrefix string            // directory put first on PATH, after step env is applied
}

// buildEnv returns the child environment: the current environment, then the
// non-interactive defaults, then force-color defaults, then the overlay vars,
// then the step's own env (highest precedence). If the step requests NO_COLOR,
// the force-color vars are dropped so the two do not conflict (used by the ck
// step to keep its pane clean). Finally ov.PathPrefix, if set, is put first on
// the resulting PATH.
func buildEnv(stepEnv map[string]string, ov EnvOverlay) []string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	for k, v := range nonInteractiveEnv {
		m[k] = v
	}
	if _, noColor := stepEnv["NO_COLOR"]; !noColor {
		for k, v := range forceColorEnv {
			m[k] = v
		}
	} else {
		for _, k := range colorVars {
			delete(m, k)
		}
	}
	for k, v := range ov.Vars {
		m[k] = v
	}
	for k, v := range stepEnv {
		m[k] = v
	}
	if ov.PathPrefix != "" {
		if p := m["PATH"]; p != "" {
			m["PATH"] = ov.PathPrefix + ":" + p
		} else {
			m["PATH"] = ov.PathPrefix
		}
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// NonInteractiveEnviron returns os.Environ() plus the non-interactive vars. It
// is for probes such as config `detect` snippets, which run the same class of
// compound shell guards as steps and must likewise never block on a prompt.
func NonInteractiveEnviron() []string {
	m := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	for k, v := range nonInteractiveEnv {
		m[k] = v
	}
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// defaultShell picks bash when available (v2 ran steps as bash), else sh.
func defaultShell() string {
	if _, err := exec.LookPath("bash"); err == nil {
		return "bash"
	}
	return "sh"
}

// resolveShell applies the shell precedence for a step: an explicit per-step
// shell wins verbatim (a bad value fails loud, as before); otherwise the
// configured default is used when present on PATH; otherwise defaultShell()
// (bash→sh). The config default is availability-checked so the documented
// "bash" default still degrades to sh on a host without bash — matching the
// pre-config behavior when nothing is set.
func resolveShell(stepShell, configShell string) string {
	if stepShell != "" {
		return stepShell
	}
	if configShell != "" {
		if _, err := exec.LookPath(configShell); err == nil {
			return configShell
		}
	}
	return defaultShell()
}
