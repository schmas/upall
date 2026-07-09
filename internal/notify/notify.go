// Package notify sends a best-effort desktop notification when a run has
// failures. It probes for a notifier and stays silent (never errors) if none
// is available.
package notify

import (
	"fmt"
	"os/exec"
)

// Failure fires a desktop notification, trying terminal-notifier, then
// osascript (macOS), then notify-send (Linux). All failures are swallowed.
func Failure(title, msg string) {
	if p, err := exec.LookPath("terminal-notifier"); err == nil {
		_ = exec.Command(p, "-title", title, "-message", msg, "-sound", "default").Run()
		return
	}
	if p, err := exec.LookPath("osascript"); err == nil {
		script := fmt.Sprintf("display notification %q with title %q", msg, title)
		_ = exec.Command(p, "-e", script).Run()
		return
	}
	if p, err := exec.LookPath("notify-send"); err == nil {
		_ = exec.Command(p, title, msg).Run()
	}
}
