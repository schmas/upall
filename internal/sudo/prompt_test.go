package sudo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// openPTY returns a pty pair. The master output is drained in the background.
func openPTY(t *testing.T) (ptmx, tty *os.File) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	t.Cleanup(func() { tty.Close(); ptmx.Close() })
	go io.Copy(io.Discard, ptmx)
	return ptmx, tty
}

type readResult struct {
	pw  []byte
	err error
}

func TestReadPassword(t *testing.T) {
	ptmx, tty := openPTY(t)
	before, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	done := make(chan readResult, 1)
	go func() {
		pw, err := ReadPassword(tty, &out, "[upall] sudo password for me: ")
		done <- readResult{pw, err}
	}()
	if _, err := ptmx.Write([]byte("p w'x\n")); err != nil {
		t.Fatal(err)
	}
	var r readResult
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ReadPassword did not return")
	}
	if r.err != nil || string(r.pw) != "p w'x" {
		t.Fatalf("pw = %q, err = %v", r.pw, r.err)
	}
	if got := out.String(); got != "[upall] sudo password for me: \n" {
		t.Fatalf("out = %q", got)
	}
	after, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("terminal state not restored")
	}
}

// TestReadPasswordInterrupt covers Ctrl-C at the prompt: ReadPassword returns
// ErrInterrupted and the terminal gets its echo back.
func TestReadPasswordInterrupt(t *testing.T) {
	ptmx, tty := openPTY(t)
	// The reader goroutine stays blocked on the tty after Ctrl-C. End its
	// line before the pty is closed (cleanups run last-in, first-out): on
	// macOS, closing a tty with a blocked reader hangs.
	t.Cleanup(func() {
		_, _ = ptmx.Write([]byte("\n"))
		time.Sleep(50 * time.Millisecond)
	})
	before, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	// Keep the test process alive if a SIGINT arrives before ReadPassword
	// registers its own handler.
	guard := make(chan os.Signal, 16)
	signal.Notify(guard, os.Interrupt)
	defer signal.Stop(guard)

	var out bytes.Buffer
	done := make(chan readResult, 1)
	go func() {
		pw, err := ReadPassword(tty, &out, "pw: ")
		done <- readResult{pw, err}
	}()

	var r readResult
	deadline := time.After(5 * time.Second)
loop:
	for {
		select {
		case r = <-done:
			break loop
		case <-deadline:
			t.Fatal("ReadPassword did not return on SIGINT")
		case <-time.After(20 * time.Millisecond):
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		}
	}
	if !errors.Is(r.err, ErrInterrupted) || r.pw != nil {
		t.Fatalf("pw = %q, err = %v, want ErrInterrupted", r.pw, r.err)
	}
	if got := out.String(); got != "pw: \n" {
		t.Fatalf("out = %q", got)
	}
	// Give a late echo-off from the reader a chance to show up.
	time.Sleep(50 * time.Millisecond)
	after, err := term.GetState(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("terminal state not restored after interrupt")
	}
}

const readPasswordChildEnv = "UPALL_SUDO_TEST_READPASSWORD_CHILD"

// readPasswordChild runs ReadPassword on its controlling terminal (stdin) and
// reports the error and whether the terminal state is the same as before.
func readPasswordChild() int {
	fd := int(os.Stdin.Fd())
	before, err := term.GetState(fd)
	if err != nil {
		fmt.Println("getstate:", err)
		return 2
	}
	_, err = ReadPassword(os.Stdin, os.Stdout, "pw: ")
	after, _ := term.GetState(fd)
	fmt.Printf("err=%v same=%v\n", err, reflect.DeepEqual(before, after))
	return 0
}

// TestReadPasswordCtrlC types a real Ctrl-C (byte 0x03) on the child's
// controlling terminal, so the tty driver sends SIGINT, as in a real shell.
func TestReadPasswordCtrlC(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), readPasswordChildEnv+"=1")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	defer ptmx.Close()
	defer cmd.Process.Kill()

	var out bytes.Buffer
	readUntil := func(want string) {
		t.Helper()
		buf := make([]byte, 256)
		_ = ptmx.SetReadDeadline(time.Now().Add(5 * time.Second))
		for !strings.Contains(out.String(), want) {
			n, err := ptmx.Read(buf)
			out.Write(buf[:n])
			if err != nil {
				t.Fatalf("waiting for %q: %v (output %q)", want, err, out.String())
			}
		}
	}
	readUntil("pw: ") // ReadPassword prints the prompt after echo is off
	if _, err := ptmx.Write([]byte{3}); err != nil {
		t.Fatal(err)
	}
	readUntil("same=")
	readUntil("\n")
	if !strings.Contains(out.String(), "err=interrupted same=true") {
		t.Fatalf("child output = %q, want err=interrupted same=true", out.String())
	}
}

func TestReadPasswordNotATerminal(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := ReadPassword(f, io.Discard, "pw: "); err == nil {
		t.Fatal("err = nil, want an error for a non-terminal")
	}
}
