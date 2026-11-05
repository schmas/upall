package sudo

import (
	"fmt"
	"os/user"
)

// dsclPath is a variable so tests can use a fake checker.
var dsclPath = "/usr/bin/dscl"

// CheckPassword tests pw without accepting a biometric approval where possible.
// darwin: `dscl . -authonly <user>` with pw given on its terminal (pty) — never
// argv. dscl checks the password against Open Directory, not PAM, so Touch ID
// cannot approve a mistyped password. realSudo, askpass and srv are not used.
func CheckPassword(realSudo, askpass string, srv *Server, pw []byte) error {
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("current user: %w", err)
	}
	return checkOnTTY([]string{dsclPath, ".", "-authonly", u.Username}, pw)
}
