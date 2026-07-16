// Package settings loads the optional user config file
// ($XDG_CONFIG_HOME/upall/config.toml) that overrides upall's built-in
// defaults for key bindings, run-history location/retention, theme colors, TUI
// behavior, and desktop notifications. A missing file is not an error — every
// field falls back to Defaults(). Only fields the user actually sets win, so
// the config is a partial overlay, mirroring the steps.d merge in
// internal/config.
package settings

// Settings is the fully-resolved configuration after merging the user's
// config.toml over Defaults(). Every field always has a usable value.
type Settings struct {
	Keys    map[string][]string // action name → the keys that trigger it
	Theme   Theme
	History History
	UI      UI
	Notify  Notify
	Run     Run
}

// Theme holds the TUI's configurable colors. Each is a Lip Gloss color string:
// a named color, a 256-palette index, or a hex value are all accepted.
type Theme struct {
	Accent  string // focused pane border, selected row, progress fill
	Dim     string // unfocused border, separators, muted text
	Success string // ✓ success glyph/text
	Failure string // ✗ failed/aborted glyph/text
}

// History controls where run-log directories are written and read, and how many
// are retained. Dir is a single root used for BOTH writing new runs and reading
// the history browser, so there is one source of truth.
type History struct {
	Dir  string // run-log root; empty → engine's default cache root
	Keep int    // dirs to retain; 0 → unset (engine.DefaultKeep)
}

// UI holds TUI behavior toggles and thresholds.
type UI struct {
	DefaultFilter string // steps filter on launch: "all" | "pending" | "done"
	Wrap          bool   // Output pane wraps long lines
	Follow        bool   // follow the active step's output by default
	WideThreshold int    // cols at/above which panes sit side by side
	Pager         string // pager command; empty → $PAGER, then "less -R"
}

// Notify toggles the desktop notification on a failed run.
type Notify struct {
	Enabled bool
}

// Run holds run-execution behavior. Shell is the default shell for steps that
// do not set their own; a per-step shell wins, and the engine still falls back
// to bash→sh when the configured shell is not on PATH.
type Run struct {
	Shell string // default shell for steps without an explicit shell
}

// knownActions is the closed set of rebindable actions. The user's [keys]
// table is validated against it (an unknown action is a config error) and
// Defaults() supplies a binding for every entry. The TUI (phase 2) turns this
// map into key.Bindings.
var knownActions = []string{
	"up", "down", "top", "bottom",
	"start", "follow", "all-logs", "retry", "continue", "restart", "pager", "stop", "quit",
	"focus-next", "focus-prev",
	"filter-next", "filter-prev", "toggle",
	"expand", "collapse", "wrap",
	"open-config", "open-config-dir",
}

// isKnownAction reports whether name is a rebindable action.
func isKnownAction(name string) bool {
	for _, a := range knownActions {
		if a == name {
			return true
		}
	}
	return false
}

// defaultKeys returns a fresh action→keys map (a new map every call so callers
// may mutate the result without affecting Defaults()).
func defaultKeys() map[string][]string {
	return map[string][]string{
		"up":              {"up", "k"},
		"down":            {"down", "j"},
		"top":             {"g", "home"},
		"bottom":          {"G", "end"},
		"start":           {"enter", "s"},
		"follow":          {"enter"},
		"all-logs":        {"a"},
		"retry":           {"r"},
		"continue":        {"u"},
		"restart":         {"R"},
		"pager":           {"l"},
		"stop":            {"x"},
		"quit":            {"q", "ctrl+c"},
		"focus-next":      {"tab"},
		"focus-prev":      {"shift+tab"},
		"filter-next":     {"right", "]"},
		"filter-prev":     {"left", "["},
		"toggle":          {" "},
		"expand":          {"right", "enter"},
		"collapse":        {"left"},
		"wrap":            {"w"},
		"open-config":     {"c"},
		"open-config-dir": {"C"},
	}
}

// Defaults returns the built-in configuration used when no config.toml exists
// (or for every field the user leaves unset).
func Defaults() Settings {
	return Settings{
		Keys: defaultKeys(),
		Theme: Theme{
			Accent:  "42",  // green — the target palette for the redesign
			Dim:     "240", // grey
			Success: "42",
			Failure: "203",
		},
		History: History{Dir: "", Keep: 0},
		UI: UI{
			DefaultFilter: "all",
			Wrap:          true,
			Follow:        true,
			WideThreshold: 90,
			Pager:         "",
		},
		Notify: Notify{Enabled: true},
		Run:    Run{Shell: "bash"},
	}
}
