package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the TUI's key bindings; it also satisfies help.KeyMap so the help
// bar renders straight from these definitions.
type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Start   key.Binding
	Follow  key.Binding
	All     key.Binding
	Retry   key.Binding
	Restart key.Binding
	Pager   key.Binding
	Top     key.Binding
	Bottom  key.Binding
	Quit    key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Start:   key.NewBinding(key.WithKeys("enter", "s"), key.WithHelp("⏎", "start")),
		Follow:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "follow")),
		All:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all logs")),
		Retry:   key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "retry failed")),
		Restart: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "re-run all")),
		Pager:   key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "pager")),
		Top:     key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:  key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp is the single-line help shown in the footer.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Follow, k.All, k.Retry, k.Restart, k.Pager, k.Quit}
}

// FullHelp is the expanded help (unused by default, provided for completeness).
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Follow, k.All},
		{k.Retry, k.Restart, k.Pager, k.Top, k.Bottom, k.Quit},
	}
}
