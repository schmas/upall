package config

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/platform"
)

// detectTimeout bounds a single detect probe so a snippet that sources a slow or
// blocking shell config (e.g. `fish -c ...`) cannot hang startup.
const detectTimeout = 5 * time.Second

// Resolve turns merged defs into Resolved steps for a host: it drops
// enabled=false steps, converts each to a runtime Step, sorts by explicit order
// (stable, so equal orders keep load order), then records whether each applies
// to the platform and — for applicable steps — whether its detect passes.
func Resolve(defs []StepDef, plat platform.Platform) ([]Resolved, error) {
	out := make([]Resolved, 0, len(defs))
	for _, d := range defs {
		if d.Enabled != nil && !*d.Enabled {
			continue
		}
		st, err := toStep(d)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", d.Key, err)
		}
		r := Resolved{Step: st, Applies: platformMatches(st, plat)}
		if r.Applies {
			r.DetectOK = detectOK(st.Detect)
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return out, nil
}

// SelectRun builds the executable step list: steps that apply to the host AND
// whose detect passed, optionally narrowed to `selected` keys. Steps that do not
// apply, or whose tool is not installed (detect miss), are dropped entirely so
// they never appear in the UI. An unknown selected key is an error; use `--list`
// to see every resolved step with its applies/detect status.
func SelectRun(resolved []Resolved, selected []string) ([]engine.Step, error) {
	known := make(map[string]bool, len(resolved))
	for _, r := range resolved {
		known[r.Key] = true
	}
	sel := make(map[string]bool, len(selected))
	for _, k := range selected {
		if !known[k] {
			return nil, fmt.Errorf("unknown step: %s", k)
		}
		sel[k] = true
	}
	var out []engine.Step
	for _, r := range resolved {
		if !r.Applies || !r.DetectOK {
			continue
		}
		if len(sel) > 0 && !sel[r.Key] {
			continue
		}
		out = append(out, r.Step)
	}
	return out, nil
}

// platformMatches applies the os/distro predicate. An empty predicate matches
// anything; distro matches either the exact ID or an ID_LIKE token.
func platformMatches(s engine.Step, p platform.Platform) bool {
	if len(s.OS) > 0 && !contains(s.OS, p.OS) {
		return false
	}
	if len(s.Distro) > 0 {
		if !contains(s.Distro, p.Distro) && !containsAny(s.Distro, strings.Fields(p.IDLike)) {
			return false
		}
	}
	return true
}

// detectOK runs the detect snippet through `sh -c` and reports exit 0. Using a
// shell (not exec.LookPath) is what lets v2's compound guards — pipes, &&, [ ],
// function probes — port verbatim. Config is local and trusted, so shell-eval
// is safe here. Like step execution it is hardened against hangs: stdin is
// /dev/null, the environment is non-interactive, and a timeout treats a stuck
// probe as "not detected" rather than freezing startup.
func detectOK(snippet string) bool {
	if strings.TrimSpace(snippet) == "" {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), detectTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", snippet)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.Env = engine.NonInteractiveEnviron()
	if devnull, err := os.Open(os.DevNull); err == nil {
		cmd.Stdin = devnull
		defer devnull.Close()
	}
	return cmd.Run() == nil
}

func contains(hay []string, needle string) bool {
	if needle == "" {
		return false
	}
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func containsAny(hay, needles []string) bool {
	for _, n := range needles {
		if contains(hay, n) {
			return true
		}
	}
	return false
}
