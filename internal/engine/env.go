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

// buildEnv returns the child environment: the current environment, then the
// non-interactive defaults, then force-color defaults, then the step's own env
// (highest precedence). If the step requests NO_COLOR, the force-color vars are
// dropped so the two do not conflict (used by the ck step to keep its pane clean).
func buildEnv(stepEnv map[string]string) []string {
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
	for k, v := range stepEnv {
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
