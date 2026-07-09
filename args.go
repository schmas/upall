package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/schmas/upall/internal/config"
)

const usageText = `upall — update every installed toolchain, with per-step live logs.

Usage:
  upall                 Run every applicable step, in order.
  upall brew mise       Run only the named steps.
  upall --list          List steps (key, label, applies?, detect-ok?).
  upall --plain         Force plain output (no color); still tees logs.
  upall --version       Print version.
  upall -h | --help     Show this help.

Env:
  UPALL_KEEP=N          Retain N run-log dirs under ~/.cache/upall (default 10).

Steps are config-driven. Defaults are embedded; extend or override them with
TOML in $XDG_CONFIG_HOME/upall/steps.d/ (fallback ~/.config/upall/steps.d/).
`

type opts struct {
	list     bool
	plain    bool
	version  bool
	help     bool
	selected []string
}

// parseArgs parses the flat argv. Steps are positional; anything starting with
// '-' that is not a known flag is an error (matches v2).
func parseArgs(argv []string) (opts, error) {
	var o opts
	for _, a := range argv {
		switch a {
		case "-h", "--help":
			o.help = true
		case "--list":
			o.list = true
		case "--plain":
			o.plain = true
		case "--version", "-V":
			o.version = true
		default:
			if strings.HasPrefix(a, "-") {
				return o, fmt.Errorf("unknown option: %s (see --help)", a)
			}
			o.selected = append(o.selected, a)
		}
	}
	return o, nil
}

// printList renders `--list`: one row per resolved step with applies/detect flags.
func printList(w io.Writer, resolved []config.Resolved) {
	for _, r := range resolved {
		fmt.Fprintf(w, "  %-10s %-16s applies=%-3s detect=%s\n",
			r.Key, r.Label, yesNo(r.Applies), detectCol(r))
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// detectCol is "-" when a step does not apply (detect is not evaluated then).
func detectCol(r config.Resolved) string {
	if !r.Applies {
		return "-"
	}
	return yesNo(r.DetectOK)
}
