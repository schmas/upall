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

// stdinScan is the part of the -A wrapper that runs the real sudo without -A
// when sudo's own options contain -S or --stdin: sudo rejects -A together
// with -S. It scans only sudo's options: it stops at "--" or at the command,
// and skips the value of an option that takes one, so `sudo pacman -S` and
// `sudo -u -S` keep -A. Combined short flags (-kS) are not detected.
const stdinScan = `skip=
for a in "$@"; do
  if [ -n "$skip" ]; then skip=; continue; fi
  case $a in
    -S|--stdin) exec REAL "$@" ;;
    -C|-D|-g|-h|-p|-R|-r|-t|-T|-U|-u) skip=1 ;;
    --close-from|--chdir|--group|--host|--prompt|--chroot|--role|--type|--command-timeout|--other-user|--user) skip=1 ;;
    --) break ;;
    -*) ;;
    *) break ;;
  esac
done
`

// WriteWrapper writes dir/bin/sudo, a script that runs `<realSudo> -A "$@"`
// (or -n) with mode 0700. In -A mode it leaves -A out for `sudo -S` (see
// stdinScan). It returns the bin directory, the PATH prefix.
func WriteWrapper(dir, realSudo string, mode WrapperMode) (string, error) {
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return "", err
	}
	quoted := shQuote(realSudo)
	script := "#!/bin/sh\n"
	flag := "-n"
	if mode == WrapAskpass {
		flag = "-A"
		script += strings.ReplaceAll(stdinScan, "REAL", quoted)
	}
	script += "exec " + quoted + " " + flag + " \"$@\"\n"
	if err := writeExecutable(filepath.Join(binDir, "sudo"), script); err != nil {
		return "", err
	}
	return binDir, nil
}

// writeFailingAskpass writes dir/askpass-none, a script that always exits 1
// with no output, with mode 0700. It returns its path.
func writeFailingAskpass(dir string) (string, error) {
	path := filepath.Join(dir, "askpass-none")
	if err := writeExecutable(path, "#!/bin/sh\nexit 1\n"); err != nil {
		return "", err
	}
	return path, nil
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
