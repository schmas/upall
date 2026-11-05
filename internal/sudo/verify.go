package sudo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
)

// errIncorrect means the checker ran and rejected the password. Any other
// error from a check means the check itself could not run.
var errIncorrect = errors.New("incorrect password")

// checkTimeout bounds one password check or NOPASSWD probe.
const checkTimeout = 30 * time.Second

// promptTimeout bounds the wait for the checker's prompt on its pty.
var promptTimeout = 5 * time.Second

// NoPasswordNeeded runs `<realSudo> -n -k true` with no stdin; true on exit 0.
func NoPasswordNeeded(realSudo string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	return exec.CommandContext(ctx, realSudo, "-n", "-k", "true").Run() == nil
}

// passwordPrompt is what checkOnTTY waits for, compared in lower case.
var passwordPrompt = []byte("password")

// maxPromptScan bounds how much early output checkOnTTY keeps to find the
// prompt.
const maxPromptScan = 4096

// checkOnTTY starts argv on a new pty, waits for its password prompt (the
// word "password", any case), writes pw and a newline to the pty and returns
// nil on exit 0. The password goes only through the pty, never through argv
// or the environment. Output from the checker is discarded.
func checkOnTTY(argv []string, pw []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	defer ptmx.Close()

	prompted := make(chan struct{})
	eof := make(chan struct{})
	go func() {
		defer close(eof)
		buf := make([]byte, 256)
		var seen []byte // early output, lower case, until the prompt shows
		found := false
		for {
			n, err := ptmx.Read(buf)
			if !found && n > 0 {
				seen = append(seen, bytes.ToLower(buf[:n])...)
				if bytes.Contains(seen, passwordPrompt) {
					found = true
					seen = nil
					close(prompted)
				} else if len(seen) > maxPromptScan {
					seen = seen[len(seen)-len(passwordPrompt):]
				}
			}
			if err != nil {
				return
			}
		}
	}()

	select {
	case <-prompted:
	case <-eof:
		_ = cmd.Wait()
		return fmt.Errorf("%s exited without a password prompt", argv[0])
	case <-time.After(promptTimeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("%s did not prompt for a password", argv[0])
	}

	if _, err := ptmx.Write(pw); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("write to %s: %w", argv[0], err)
	}
	if _, err := ptmx.Write([]byte{'\n'}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return fmt.Errorf("write to %s: %w", argv[0], err)
	}
	return exitResult(ctx, argv[0], cmd.Wait())
}

// checkWithAskpass arms srv with pw for one request (check mode), then runs
// `<realSudo> -k -A true` with SUDO_ASKPASS=askpass. sudo runs in its own
// session on a throwaway pty, and all its output is discarded: sudo writes
// "Sorry, try again." and the lecture to /dev/tty, which must not be upall's
// terminal. After a wrong password, sudo does not exit: it runs PAM again,
// and the PAM conversation runs the helper again. The server holds that
// second request, so PAM waits, and the check kills sudo there. This way a
// typo causes one failed authentication only (pam_faillock counts each one).
func checkWithAskpass(realSudo, askpass string, srv *Server, pw []byte) error {
	over := srv.ArmCheck(pw)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, realSudo, "-k", "-A", "true")
	cmd.Env = append(os.Environ(), "SUDO_ASKPASS="+askpass)
	ptmx, err := pty.Start(cmd) // Setsid + Setctty: the pty is sudo's /dev/tty
	if err != nil {
		srv.Release()
		return fmt.Errorf("run %s: %w", realSudo, err)
	}
	defer ptmx.Close()
	defer srv.Release() // runs before ptmx.Close: the held helper exits first
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	select {
	case err := <-waited:
		return exitResult(ctx, realSudo, err)
	case <-over:
		// A second request means the first password was wrong.
		_ = cmd.Process.Kill()
		<-waited
		return errIncorrect
	}
}

// exitResult maps a checker's exit to nil, errIncorrect or a run error.
func exitResult(ctx context.Context, name string, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s: timed out", name)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return errIncorrect
	}
	return fmt.Errorf("run %s: %w", name, err)
}
