package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/schmas/upall/internal/settings"
)

// keyMap is the TUI's key bindings, built from Settings.Keys so every action is
// rebindable. Each binding also carries a caption computed from its ACTUAL keys
// (see bind), which is what lets the keybindings panel print a rebind instead
// of a hardcoded default.
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	Start    key.Binding
	Follow   key.Binding
	All      key.Binding
	Retry    key.Binding
	Continue key.Binding
	Restart  key.Binding
	Pager    key.Binding
	Stop     key.Binding
	TypeMode key.Binding
	Quit     key.Binding

	FocusNext key.Binding
	FocusPrev key.Binding

	FilterNext key.Binding
	FilterPrev key.Binding
	Toggle     key.Binding

	Expand   key.Binding
	Collapse key.Binding

	Wrap key.Binding

	OpenConfig    key.Binding
	OpenConfigDir key.Binding

	SelfUpdate key.Binding

	Help key.Binding
}

// keysFrom builds the key bindings from the resolved settings. An action the
// user leaves unset falls back to the built-in default, so a partial [keys]
// table never disables a binding.
func keysFrom(set settings.Settings) keyMap {
	def := settings.Defaults().Keys
	keysOf := func(action string) []string {
		if k := set.Keys[action]; len(k) > 0 {
			return k
		}
		return def[action]
	}
	// The caption is DERIVED from the resolved keys rather than written by hand,
	// so the keybindings panel can never print a key the user has rebound away.
	// key.Matches ignores the help field, so this is behavior-neutral.
	bind := func(action, desc string) key.Binding {
		keys := keysOf(action)
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(formatKeys(keys), desc))
	}
	return keyMap{
		Up:         bind("up", "move up"),
		Down:       bind("down", "move down"),
		Top:        bind("top", "jump to the top"),
		Bottom:     bind("bottom", "jump to the bottom"),
		Start:      bind("start", "start the run"),
		Follow:     bind("follow", "follow the running step"),
		All:        bind("all-logs", "show every step's output"),
		Retry:      bind("retry", "re-run the selected step"),
		Continue:   bind("continue", "resume from the aborted step"),
		Restart:    bind("restart", "re-run every step"),
		Pager:      bind("pager", "open the full log in the pager"),
		Stop:       bind("stop", "stop the running step"),
		TypeMode:   bind("type", "type into the running step (sudo password)"),
		Quit:       bind("quit", "quit upall"),
		FocusNext:  bind("focus-next", "focus the next pane"),
		FocusPrev:  bind("focus-prev", "focus the previous pane"),
		FilterNext: bind("filter-next", "next step filter"),
		FilterPrev: bind("filter-prev", "previous step filter"),
		Toggle:     bind("toggle", "include/exclude the step before a run"),
		Expand:     bind("expand", "expand the past run"),
		Collapse:   bind("collapse", "collapse the past run"),
		Wrap:       bind("wrap", "toggle wrapping of history output"),

		OpenConfig:    bind("open-config", "open config.toml"),
		OpenConfigDir: bind("open-config-dir", "open the config directory"),

		SelfUpdate: bind("self-update", "check for a newer release"),

		Help: bind("help", "open this panel"),
	}
}
