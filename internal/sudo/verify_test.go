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

// fakeSudo acts like `sudo -k -A true`: it asks $SUDO_ASKPASS up to 3 times,
// as sudo does, and logs each askpass call to the returned file.
func fakeSudo(t *testing.T, want string) (sudo, calls string) {
	t.Helper()
	calls = filepath.Join(t.TempDir(), "calls")
	sudo = writeScript(t, `[ "$*" = "-k -A true" ] || exit 2
[ -t 0 ] && exit 3
for i in 1 2 3; do
  echo call >> `+shQuote(calls)+`
  p=$("$SUDO_ASKPASS") || exit 1
  [ "$p" = `+shQuote(want)+` ] && exit 0
done
exit 1
`)
	return sudo, calls
}

func countCalls(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "call\n")
}

func TestCheckWithAskpass(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	srv := newServer(t)
	askpass, err := WriteAskpass(filepath.Dir(srv.SocketPath()), exe, srv.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	sudo, calls := fakeSudo(t, "good pw")

	// A typo: the helper answers the first request only, so sudo gets the
	// typed password exactly once (one PAM failure), and the retry gets nothing.
	if err := checkWithAskpass(sudo, askpass, srv, []byte("bad")); !errors.Is(err, errIncorrect) {
		t.Fatalf("wrong password: err = %v, want errIncorrect", err)
	}
	if n := countCalls(t, calls); n != 2 {
		t.Fatalf("askpass calls = %d, want 2 (one answered, one refused)", n)
	}

	// The next attempt re-arms the server with the new password.
	if err := checkWithAskpass(sudo, askpass, srv, []byte("good pw")); err != nil {
		t.Fatalf("right password: %v", err)
	}
	if n := countCalls(t, calls); n != 3 {
		t.Fatalf("askpass calls = %d, want 3", n)
	}
}

func TestCheckWithAskpassMissingSudo(t *testing.T) {
	srv := newServer(t)
	err := checkWithAskpass(filepath.Join(t.TempDir(), "missing"), "/bin/true", srv, []byte("pw"))
	if err == nil || errors.Is(err, errIncorrect) {
		t.Fatalf("err = %v, want a start error", err)
	}
}
