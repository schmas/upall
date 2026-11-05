package sudo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"reflect"
	"time"

	"golang.org/x/term"
)

// ErrInterrupted is returned when the user presses Ctrl-C at the prompt.
var ErrInterrupted = errors.New("interrupted")

type pwResult struct {
	pw  []byte
	err error
}

// echoOffWait bounds how long ReadPassword waits for the reader to turn echo
// off before it prints the prompt.
const echoOffWait = 200 * time.Millisecond

// ReadPassword prints prompt to out and reads one line from in with echo off.
// On SIGINT it restores the terminal state and returns ErrInterrupted.
func ReadPassword(in *os.File, out io.Writer, prompt string) ([]byte, error) {
	fd := int(in.Fd())
	saved, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	done := make(chan pwResult, 1)
	// term.ReadPassword cannot be cancelled. On Ctrl-C this goroutine stays
	// blocked on the read; the caller exits, so it does not matter.
	go func() {
		pw, err := term.ReadPassword(fd)
		done <- pwResult{pw, err}
	}()

	// Wait until the reader has turned echo off. Otherwise a Ctrl-C right
	// after Notify could restore the terminal before the reader changes it,
	// and the terminal would stay without echo.
	waitStateChange(fd, saved, done)

	fmt.Fprint(out, prompt)
	select {
	case r := <-done:
		fmt.Fprintln(out)
		return r.pw, r.err
	case <-sig:
		_ = term.Restore(fd, saved)
		fmt.Fprintln(out)
		return nil, ErrInterrupted
	}
}

// waitStateChange polls the terminal state until it differs from saved, the
// reader is done (done is only peeked: a result is put back) or echoOffWait
// passes.
func waitStateChange(fd int, saved *term.State, done chan pwResult) {
	deadline := time.Now().Add(echoOffWait)
	for time.Now().Before(deadline) {
		if st, err := term.GetState(fd); err != nil || !reflect.DeepEqual(st, saved) {
			return
		}
		select {
		case r := <-done:
			done <- r
			return
		case <-time.After(time.Millisecond):
		}
	}
}
