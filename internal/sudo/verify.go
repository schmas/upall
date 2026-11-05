package sudo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// checkOnTTY starts argv on a new pty, waits for its prompt (a ':'), writes
// pw and a newline to the pty and returns nil on exit 0. The password goes
// only through the pty, never through argv or the environment. Output from
// the checker is discarded.
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
		seen := false
		for {
			n, err := ptmx.Read(buf)
			if !seen && bytes.IndexByte(buf[:n], ':') >= 0 {
				seen = true
				close(prompted)
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

// checkWithAskpass arms srv with pw for one request, then runs
// `<realSudo> -k -A true` with SUDO_ASKPASS=askpass. sudo asks the helper
// again after a wrong password; the helper then gets nothing, so a typo
// causes one failed authentication only. Output is discarded.
func checkWithAskpass(realSudo, askpass string, srv *Server, pw []byte) error {
	srv.Arm(pw, 1)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, realSudo, "-k", "-A", "true")
	cmd.Env = append(os.Environ(), "SUDO_ASKPASS="+askpass)
	return exitResult(ctx, realSudo, cmd.Run())
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
