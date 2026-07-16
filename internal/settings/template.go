package settings

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DefaultTOML is the seed written by `upall --init-config`: a fully-commented
// config.toml. Every option is shown at its default value but commented out, so
// the file documents the defaults while parsing back to exactly Defaults()
// (nothing is actually set). Uncomment a line to override it.
func DefaultTOML() string {
	return `# upall configuration.
# Location: $XDG_CONFIG_HOME/upall/config.toml (fallback ~/.config/upall/).
# Every option below is shown at its default and commented out — uncomment and
# edit only what you want to change. Precedence: CLI flag > env > this file >
# built-in default.
schema = 1

# [keys] rebinds any action to a list of keys (replaces that action's default
# list). Known actions: up, down, top, bottom, start, follow, all-logs, retry,
# continue, restart, pager, stop, type, quit, focus-next, focus-prev,
# filter-next, filter-prev, toggle, expand, collapse, wrap, open-config,
# open-config-dir.
# [keys]
# up          = ["up", "k"]
# down        = ["down", "j"]
# top         = ["g", "home"]
# bottom      = ["G", "end"]
# start       = ["enter", "s"]
# follow      = ["enter"]
# all-logs    = ["a"]
# retry       = ["r"]
# continue    = ["u"]
# restart     = ["R"]
# pager       = ["l"]
# stop        = ["x"]
# type        = ["i"]
# quit        = ["q", "ctrl+c"]
# focus-next  = ["tab"]
# focus-prev  = ["shift+tab"]
# filter-next = ["right", "]"]
# filter-prev = ["left", "["]
# toggle      = [" "]
# expand      = ["right", "enter"]
# collapse    = ["left"]
# wrap        = ["w"]
# open-config     = ["c"]
# open-config-dir = ["C"]

# [theme] colors accept a named color, a 256-palette index, or a hex value.
# [theme]
# accent  = "42"   # focused pane border, selected row, progress fill
# dim     = "240"  # unfocused border, separators, muted text
# success = "42"   # success glyph/text
# failure = "203"  # failed/aborted glyph/text

# [history] controls where run logs are written and read, and how many runs to
# keep. dir is a single root used for both writing and browsing.
# [history]
# dir  = "~/.cache/upall"  # empty = default cache root
# keep = 10                # run-log dirs to retain

# [ui] TUI behavior.
# [ui]
# default_filter = "all"   # steps filter on launch: all | pending | done
# wrap           = true    # wrap long lines in the Output pane
# follow         = true    # follow the active step's output
# wide_threshold = 90      # cols at/above which panes sit side by side
# pager          = ""      # pager command; empty = $PAGER, then "less -R"

# [notify] desktop notification on a failed run.
# [notify]
# enabled = true

# [run] default shell for steps that do not set their own "shell". A per-step
# shell wins; the configured shell is used when present on PATH, else upall
# falls back to bash (or sh when bash is absent).
# [run]
# shell = "bash"
`
}

// InitConfig writes DefaultTOML() to the resolved config path, creating the
// directory as needed. It refuses to overwrite an existing file unless force is
// set. The resolved path is reported to w. Used by `upall --init-config`.
func InitConfig(w io.Writer, force bool) error {
	path := ConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve config path (no home directory)")
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}
	if err := writeDefaultConfig(path); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote %s\n", path)
	return nil
}

// EnsureConfig writes the commented default config.toml when none exists yet, so
// a first run leaves the user a documented file to edit. It is best-effort: an
// unresolvable or unwritable config path is not fatal (the caller falls back to
// in-memory Defaults()). Returns true when a file was freshly created.
func EnsureConfig() (bool, error) {
	path := ConfigPath()
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err == nil {
		return false, nil // already present — never clobber
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, err // stat failed for some other reason
	}
	if err := writeDefaultConfig(path); err != nil {
		return false, err
	}
	return true, nil
}

// writeDefaultConfig creates the config directory (if needed) and writes the
// commented defaults to path.
func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(DefaultTOML()), 0o644)
}
