package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/schmas/upall/internal/settings"
)

// keyMap is the TUI's key bindings, built from Settings.Keys so every action is
// rebindable. It also satisfies help.KeyMap for the optional full-help listing.
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
	bind := func(action, helpKey, helpDesc string) key.Binding {
		return key.NewBinding(key.WithKeys(keysOf(action)...), key.WithHelp(helpKey, helpDesc))
	}
	return keyMap{
		Up:         bind("up", "↑/k", "up"),
		Down:       bind("down", "↓/j", "down"),
		Top:        bind("top", "g", "top"),
		Bottom:     bind("bottom", "G", "bottom"),
		Start:      bind("start", "⏎", "start"),
		Follow:     bind("follow", "⏎", "follow"),
		All:        bind("all-logs", "a", "all logs"),
		Retry:      bind("retry", "r", "retry"),
		Continue:   bind("continue", "u", "continue"),
		Restart:    bind("restart", "R", "re-run all"),
		Pager:      bind("pager", "l", "pager"),
		Stop:       bind("stop", "x", "stop"),
		TypeMode:   bind("type", "i", "type"),
		Quit:       bind("quit", "q", "quit"),
		FocusNext:  bind("focus-next", "tab", "next pane"),
		FocusPrev:  bind("focus-prev", "⇧tab", "prev pane"),
		FilterNext: bind("filter-next", "→", "filter →"),
		FilterPrev: bind("filter-prev", "←", "filter ←"),
		Toggle:     bind("toggle", "space", "toggle"),
		Expand:     bind("expand", "→", "expand"),
		Collapse:   bind("collapse", "←", "collapse"),
		Wrap:       bind("wrap", "w", "wrap"),

		OpenConfig:    bind("open-config", "c", "config file"),
		OpenConfigDir: bind("open-config-dir", "C", "config dir"),

		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

// ShortHelp / FullHelp satisfy help.KeyMap.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.FocusNext, k.Up, k.Down, k.Follow, k.All, k.Retry, k.Pager, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.FocusNext, k.FocusPrev},
		{k.Start, k.Follow, k.All, k.Retry, k.Continue, k.Restart, k.Pager},
		{k.FilterPrev, k.FilterNext, k.Toggle, k.Expand, k.Collapse},
		{k.Wrap, k.OpenConfig, k.OpenConfigDir, k.TypeMode, k.Stop, k.Quit},
	}
}
