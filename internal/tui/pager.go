package tui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// pagerCmd opens path in a pager as a suspended child. tea.ExecProcess restores
// the alt screen when the pager exits (including on error), so the terminal is
// never left corrupted. $PAGER is honored when it resolves; otherwise it falls
// back to `less -R`, then `more`. A missing/empty path is a no-op.
func pagerCmd(path string) tea.Cmd {
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		return nil
	}
	name, args := resolvePager()
	if name == "" {
		return nil
	}
	c := exec.Command(name, append(args, path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return pagerDoneMsg{err: err} })
}

// resolvePager picks the pager binary and its base args.
func resolvePager() (string, []string) {
	if p := strings.Fields(os.Getenv("PAGER")); len(p) > 0 {
		if bin, err := exec.LookPath(p[0]); err == nil {
			return bin, p[1:]
		}
	}
	if bin, err := exec.LookPath("less"); err == nil {
		return bin, []string{"-R"}
	}
	if bin, err := exec.LookPath("more"); err == nil {
		return bin, nil
	}
	return "", nil
}
