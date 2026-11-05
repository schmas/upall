package sudo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets the test binary act as the askpass helper, the same way the
// upall binary does: `<exe> __sudo-askpass <sock>`. It also runs the
// ReadPassword child for TestReadPasswordCtrlC.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == HelperArg {
		os.Exit(RunHelper(os.Args[2], os.Stdout))
	}
	if os.Getenv(readPasswordChildEnv) == "1" {
		os.Exit(readPasswordChild())
	}
	os.Exit(m.Run())
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoPasswordNeeded(t *testing.T) {
	ok := writeScript(t, `[ "$*" = "-n -k true" ] || exit 2
[ -t 0 ] && exit 3
exit 0
`)
	if !NoPasswordNeeded(ok) {
		t.Fatal("NoPasswordNeeded = false, want true")
	}
	fail := writeScript(t, "exit 1\n")
	if NoPasswordNeeded(fail) {
		t.Fatal("NoPasswordNeeded = true, want false")
	}
	if NoPasswordNeeded(filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("NoPasswordNeeded = true for a missing binary")
	}
}

// ttyChecker is a fake `dscl -authonly`: it prompts on its terminal with echo
// off, reads one line and exits 0 only if the line is the expected password.
func ttyChecker(t *testing.T, want string) string {
	t.Helper()
	return writeScript(t, `stty -echo
printf 'Password:'
IFS= read -r line
stty echo
[ "$line" = `+shQuote(want)+` ]
`)
}

func TestCheckOnTTY(t *testing.T) {
	want := "p w'x\"$y"
	fake := ttyChecker(t, want)
	if err := checkOnTTY([]string{fake}, []byte(want)); err != nil {
		t.Fatalf("right password: %v", err)
	}
	if err := checkOnTTY([]string{fake}, []byte("wrong")); !errors.Is(err, errIncorrect) {
		t.Fatalf("wrong password: err = %v, want errIncorrect", err)
	}
}

func TestCheckOnTTYNoPrompt(t *testing.T) {
	old := promptTimeout
	promptTimeout = 200 * time.Millisecond
	t.Cleanup(func() { promptTimeout = old })
	fake := writeScript(t, "sleep 5\n")
	start := time.Now()
	err := checkOnTTY([]string{fake}, []byte("pw"))
	if err == nil || errors.Is(err, errIncorrect) {
		t.Fatalf("err = %v, want a no-prompt error", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v, want the checker killed after the timeout", d)
	}
}

func TestCheckOnTTYMissingBinary(t *testing.T) {
	err := checkOnTTY([]string{filepath.Join(t.TempDir(), "missing")}, []byte("pw"))
	if err == nil || errors.Is(err, errIncorrect) {
		t.Fatalf("err = %v, want a start error", err)
	}
}

// fakeSudo acts like real `sudo -k -A true` with pam_unix: after a wrong
// password it prints "Sorry, try again." and asks $SUDO_ASKPASS again, up to
// 3 times. It does not stop when the helper fails: real sudo gives even an
// empty answer to PAM, which counts as a failure. Each askpass call that
// returns counts as one PAM attempt. The log gets "call" before each askpass
// call, "attempt" for each PAM attempt and "exit" on a normal exit.
func fakeSudo(t *testing.T, want string) (sudo, log string) {
	t.Helper()
	log = filepath.Join(t.TempDir(), "log")
	q := shQuote(log)
	sudo = writeScript(t, `[ "$*" = "-k -A true" ] || exit 2
[ -t 0 ] && exit 3
for i in 1 2 3; do
  echo call >> `+q+`
  p=$("$SUDO_ASKPASS")
  echo attempt >> `+q+`
  if [ "$p" = `+shQuote(want)+` ]; then echo exit >> `+q+`; exit 0; fi
  echo 'Sorry, try again.' >&2
done
echo exit >> `+q+`
exit 1
`)
	return sudo, log
}

func countLines(t *testing.T, path, line string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), line+"\n")
}

func askpassFor(t *testing.T, srv *Server) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	askpass, err := WriteAskpass(filepath.Dir(srv.SocketPath()), exe, srv.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	return askpass
}

func TestCheckWithAskpass(t *testing.T) {
	srv := newServer(t)
	askpass := askpassFor(t, srv)
	sudo, log := fakeSudo(t, "good pw")

	// A typo: the helper answers the first request only. sudo asks again; the
	// server holds that request, and the check kills sudo before the second
	// PAM attempt. So one typo is exactly one PAM failure.
	start := time.Now()
	if err := checkWithAskpass(sudo, askpass, srv, []byte("bad")); !errors.Is(err, errIncorrect) {
		t.Fatalf("wrong password: err = %v, want errIncorrect", err)
	}
	if d := time.Since(start); d >= helperTimeout {
		t.Fatalf("check took %v, want sudo killed before the helper timeout", d)
	}
	if n := countLines(t, log, "attempt"); n != 1 {
		t.Fatalf("PAM attempts = %d, want 1", n)
	}
	if n := countLines(t, log, "call"); n != 2 {
		t.Fatalf("askpass calls = %d, want 2 (one answered, one held)", n)
	}
	if n := countLines(t, log, "exit"); n != 0 {
		t.Fatal("fake sudo exited normally, want it killed")
	}
	// The held helper must not reach PAM later either.
	time.Sleep(200 * time.Millisecond)
	if n := countLines(t, log, "attempt"); n != 1 {
		t.Fatalf("PAM attempts after the check = %d, want 1", n)
	}

	// The next attempt re-arms the server with the new password.
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if err := checkWithAskpass(sudo, askpass, srv, []byte("good pw")); err != nil {
		t.Fatalf("right password: %v", err)
	}
	if a, e := countLines(t, log, "attempt"), countLines(t, log, "exit"); a != 1 || e != 1 {
		t.Fatalf("right password: attempts = %d, exits = %d, want 1, 1", a, e)
	}
}

// TestCheckWithAskpassOtherFailure: a sudo that exits non-zero for another
// reason before a second request still maps to errIncorrect.
func TestCheckWithAskpassOtherFailure(t *testing.T) {
	srv := newServer(t)
	askpass := askpassFor(t, srv)
	sudo := writeScript(t, `p=$("$SUDO_ASKPASS")
echo 'user is not in the sudoers file' >&2
exit 1
`)
	if err := checkWithAskpass(sudo, askpass, srv, []byte("pw")); !errors.Is(err, errIncorrect) {
		t.Fatalf("err = %v, want errIncorrect", err)
	}
}

func TestCheckWithAskpassMissingSudo(t *testing.T) {
	srv := newServer(t)
	err := checkWithAskpass(filepath.Join(t.TempDir(), "missing"), "/bin/true", srv, []byte("pw"))
	if err == nil || errors.Is(err, errIncorrect) {
		t.Fatalf("err = %v, want a start error", err)
	}
}
