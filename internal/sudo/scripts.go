package sudo

import (
	"os"
	"path/filepath"
	"strings"
)

// WrapperMode selects the flag the sudo wrapper adds.
type WrapperMode int

const (
	// WrapAskpass adds -A: sudo asks the askpass helper for the password.
	WrapAskpass WrapperMode = iota
	// WrapNonInteractive adds -n: sudo fails instead of asking.
	WrapNonInteractive
)

// WriteAskpass writes dir/askpass, a launcher that runs
// `<exe> __sudo-askpass <sock>`, with mode 0700. It returns the launcher path.
func WriteAskpass(dir, exe, sock string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "askpass")
	script := "#!/bin/sh\nexec " + shQuote(exe) + " " + HelperArg + " " + shQuote(sock) + "\n"
	if err := writeExecutable(path, script); err != nil {
		return "", err
	}
	return path, nil
}

// WriteWrapper writes dir/bin/sudo, a script that runs `<realSudo> -A "$@"`
// (or -n) with mode 0700. It returns the bin directory, the PATH prefix.
func WriteWrapper(dir, realSudo string, mode WrapperMode) (string, error) {
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return "", err
	}
	flag := "-A"
	if mode == WrapNonInteractive {
		flag = "-n"
	}
	script := "#!/bin/sh\nexec " + shQuote(realSudo) + " " + flag + " \"$@\"\n"
	if err := writeExecutable(filepath.Join(binDir, "sudo"), script); err != nil {
		return "", err
	}
	return binDir, nil
}

func writeExecutable(path, content string) error {
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return err
	}
	// WriteFile applies the umask and keeps the mode of an existing file.
	return os.Chmod(path, 0o700)
}

// shQuote quotes s as one POSIX shell word, in single quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
