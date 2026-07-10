package tui

import (
	"os"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/schmas/upall/internal/config"
	"github.com/schmas/upall/internal/settings"
)

// openConfigFileCmd reveals config.toml in the OS default handler, writing the
// documented default first if it does not exist yet so the key always opens a
// real file to edit. The filesystem work runs inside the command (not at
// key-press time) to keep Update side-effect-free until Bubble Tea executes it.
func openConfigFileCmd() tea.Cmd {
	return func() tea.Msg {
		// Best-effort: EnsureConfig writes the commented default (and its dir) when
		// absent; an unwritable path just means we open whatever already exists.
		_, _ = settings.EnsureConfig()
		revealPath(settings.ConfigPath())
		return nil
	}
}

// openConfigDirCmd reveals the config directory (steps.d + config.toml live
// under it) in the OS file manager, creating it first so the folder opens even
// on a fresh install with no config yet.
func openConfigDirCmd() tea.Cmd {
	return func() tea.Msg {
		if dir := config.ConfigDir(); dir != "" {
			_ = os.MkdirAll(dir, 0o755)
			revealPath(dir)
		}
		return nil
	}
}

// revealPath launches the OS "open" utility on path, detached, so the file
// manager / editor comes up beside the still-running TUI (unlike the pager,
// which suspends). Fire-and-forget: any failure is swallowed because opening a
// config file must never crash a run. An empty path or unknown platform is a
// no-op.
func revealPath(path string) {
	if path == "" {
		return
	}
	name, args := openTool()
	if name == "" {
		return
	}
	_ = exec.Command(name, append(args, path)...).Start()
}

// openTool picks the platform's reveal-in-GUI command. Linux/BSD use xdg-open;
// macOS uses open; Windows uses `cmd /c start`.
func openTool() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "windows":
		return "cmd", []string{"/c", "start", ""}
	default:
		return "xdg-open", nil
	}
}
