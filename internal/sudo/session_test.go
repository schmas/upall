package sudo

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// fakeDeps records how often read and check run.
type fakeDeps struct {
	euid     int
	sudoErr  error
	noPass   bool
	reads    [][]byte // returned in order; the last one repeats
	readErr  error
	checkOK  func(attempt int, pw []byte) error
	nRead    int
	nCheck   int
	nNoPass  int
	askpass  string // last askpass path given to check
	checkPws []string
}

func (f *fakeDeps) deps() deps {
	return deps{
		euid: func() int { return f.euid },
		lookSudo: func() (string, error) {
			if f.sudoErr != nil {
				return "", f.sudoErr
			}
			return "/usr/bin/sudo", nil
		},
		noPassword: func(string) bool { f.nNoPass++; return f.noPass },
		read: func() ([]byte, error) {
			f.nRead++
			if f.readErr != nil {
				return nil, f.readErr
			}
			i := min(f.nRead-1, len(f.reads)-1)
			return append([]byte(nil), f.reads[i]...), nil
		},
		check: func(realSudo, askpass string, srv *Server, pw []byte) error {
			f.nCheck++
			f.askpass = askpass
			f.checkPws = append(f.checkPws, string(pw))
			if srv == nil {
				return errors.New("nil server")
			}
			return f.checkOK(f.nCheck, pw)
		},
	}
}

// shortTempDir points TMPDIR at a new, short, empty directory and returns it.
func shortTempDir(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("", "ut")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("TMPDIR", base)
	return base
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s is not empty: %v", dir, entries)
	}
}

func assertEmptySession(t *testing.T, s *Session) {
	t.Helper()
	ov := s.Overlay()
	if len(ov.Vars) != 0 || ov.PathPrefix != "" {
		t.Fatalf("overlay = %+v, want zero value", ov)
	}
}

func opts(needed, tty bool) Options {
	return Options{Needed: needed, StdinTTY: tty, Out: &bytes.Buffer{}, Exe: "/bin/true"}
}

func TestSetupRoot(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 0}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertEmptySession(t, s)
	if f.nRead != 0 || f.nNoPass != 0 {
		t.Fatalf("read = %d, noPassword = %d, want 0", f.nRead, f.nNoPass)
	}
	assertEmptyDir(t, base)
}

func TestSetupNoTTYWrapperOnly(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501}
	s, err := setup(opts(true, false), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ov := s.Overlay()
	if _, ok := ov.Vars["SUDO_ASKPASS"]; ok {
		t.Fatalf("SUDO_ASKPASS set: %+v", ov)
	}
	if ov.PathPrefix == "" {
		t.Fatal("PathPrefix empty, want wrapper dir")
	}
	b, err := os.ReadFile(filepath.Join(ov.PathPrefix, "sudo"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("'/usr/bin/sudo' -n ")) {
		t.Fatalf("wrapper = %q, want -n", b)
	}
	if f.nRead != 0 || f.nNoPass != 0 {
		t.Fatalf("read = %d, noPassword = %d, want 0", f.nRead, f.nNoPass)
	}
}

func TestSetupNotNeeded(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 501}
	s, err := setup(opts(false, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertEmptySession(t, s)
	if f.nRead != 0 || f.nNoPass != 0 {
		t.Fatalf("read = %d, noPassword = %d, want 0", f.nRead, f.nNoPass)
	}
	assertEmptyDir(t, base)
}

func TestSetupNoPasswordSkipsPrompt(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 501, noPass: true}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertEmptySession(t, s)
	if f.nRead != 0 {
		t.Fatalf("read count = %d, want 0", f.nRead)
	}
	assertEmptyDir(t, base)
}

func TestSetupSudoNotFound(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 501, sudoErr: errors.New("not found")}
	if _, err := setup(opts(true, true), f.deps()); err == nil {
		t.Fatal("err = nil, want sudo not found")
	}
	for _, o := range []Options{opts(false, true), opts(true, false)} {
		s, err := setup(o, f.deps())
		if err != nil {
			t.Fatalf("%+v: %v", o, err)
		}
		assertEmptySession(t, s)
		s.Close()
	}
	assertEmptyDir(t, base)
}

func okOn(n int) func(int, []byte) error {
	return func(attempt int, _ []byte) error {
		if attempt == n {
			return nil
		}
		return errIncorrect
	}
}

func assertArmed(t *testing.T, s *Session) {
	t.Helper()
	ov := s.Overlay()
	ap := ov.Vars["SUDO_ASKPASS"]
	if ap == "" || ov.PathPrefix == "" {
		t.Fatalf("overlay = %+v, want SUDO_ASKPASS and PathPrefix", ov)
	}
	if _, err := os.Stat(ap); err != nil {
		t.Fatalf("askpass: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(ov.PathPrefix, "sudo"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("'/usr/bin/sudo' -A ")) {
		t.Fatalf("wrapper = %q, want -A", b)
	}
	fi, err := os.Stat(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
}

func TestSetupFirstAttemptOK(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("pw")}, checkOK: okOn(1)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertArmed(t, s)
	if f.nRead != 1 || f.nCheck != 1 {
		t.Fatalf("read = %d, check = %d, want 1, 1", f.nRead, f.nCheck)
	}
}

func TestSetupSecondAttemptOK(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("bad"), []byte("good")}, checkOK: okOn(2)}
	o := opts(true, true)
	s, err := setup(o, f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertArmed(t, s)
	if f.nRead != 2 {
		t.Fatalf("read count = %d, want 2", f.nRead)
	}
	if got := o.Out.(*bytes.Buffer).String(); got != "Sorry, try again.\n" {
		t.Fatalf("out = %q", got)
	}
	var buf bytes.Buffer
	if code := RunHelper(s.srv.SocketPath(), &buf); code != 0 || buf.String() != "good\n" {
		t.Fatalf("helper: code = %d, out = %q", code, buf.String())
	}
}

func TestSetupThirdAttemptOK(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("a"), []byte("b"), []byte("c")}, checkOK: okOn(3)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertArmed(t, s)
	if f.nCheck != 3 {
		t.Fatalf("check count = %d, want 3", f.nCheck)
	}
}

func TestSetupServesPassword(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("s3cret")}, checkOK: okOn(1)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ { // budget is unlimited
		var buf bytes.Buffer
		if code := RunHelper(s.srv.SocketPath(), &buf); code != 0 || buf.String() != "s3cret\n" {
			t.Fatalf("call %d: code = %d, out = %q", i, code, buf.String())
		}
	}
	if f.askpass != s.Overlay().Vars["SUDO_ASKPASS"] {
		t.Fatalf("check askpass = %q, overlay = %q", f.askpass, s.Overlay().Vars["SUDO_ASKPASS"])
	}
}

func TestSetupThreeFailures(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("bad")}, checkOK: okOn(99)}
	o := opts(true, true)
	s, err := setup(o, f.deps())
	if !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("err = %v, want ErrTooManyAttempts", err)
	}
	if s != nil {
		t.Fatalf("session = %+v, want nil", s)
	}
	if f.nCheck != 3 || f.nRead != 3 {
		t.Fatalf("check = %d, read = %d, want 3, 3", f.nCheck, f.nRead)
	}
	if got := o.Out.(*bytes.Buffer).String(); got != "Sorry, try again.\nSorry, try again.\nSorry, try again.\n" {
		t.Fatalf("out = %q", got)
	}
	assertEmptyDir(t, base)
}

func TestSetupCheckErrorStops(t *testing.T) {
	base := shortTempDir(t)
	boom := errors.New("cannot start checker")
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("pw")}, checkOK: func(int, []byte) error { return boom }}
	if _, err := setup(opts(true, true), f.deps()); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if f.nCheck != 1 {
		t.Fatalf("check count = %d, want 1", f.nCheck)
	}
	assertEmptyDir(t, base)
}

func TestSetupInterrupted(t *testing.T) {
	base := shortTempDir(t)
	f := &fakeDeps{euid: 501, readErr: ErrInterrupted}
	s, err := setup(opts(true, true), f.deps())
	if !errors.Is(err, ErrInterrupted) {
		t.Fatalf("err = %v, want ErrInterrupted", err)
	}
	if s != nil {
		t.Fatalf("session = %+v, want nil", s)
	}
	if f.nCheck != 0 {
		t.Fatalf("check count = %d, want 0", f.nCheck)
	}
	assertEmptyDir(t, base)
}

func TestSetupLongTMPDIR(t *testing.T) {
	base := shortTempDir(t)
	long := filepath.Join(base, string(bytes.Repeat([]byte("d"), 100)))
	if err := os.Mkdir(long, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("pw")}, checkOK: okOn(1)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if n := len(s.srv.SocketPath()); n >= 104 {
		t.Fatalf("socket path length = %d", n)
	}
	if filepath.Dir(s.dir) != "/tmp" {
		t.Fatalf("dir = %q, want under /tmp", s.dir)
	}
	assertEmptyDir(t, long)
}

func TestSessionClose(t *testing.T) {
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("pw")}, checkOK: okOn(1)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	dir, sock := s.dir, s.srv.SocketPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir still exists: %v", err)
	}
	var buf bytes.Buffer
	if code := RunHelper(sock, &buf); code != 1 || buf.Len() != 0 {
		t.Fatalf("helper after Close: code = %d, out = %q", code, buf.String())
	}
}

func TestSessionCloseNilSafe(t *testing.T) {
	if err := (*Session)(nil).Close(); err != nil {
		t.Fatalf("nil Close: %v", err)
	}
	if ov := (*Session)(nil).Overlay(); len(ov.Vars) != 0 || ov.PathPrefix != "" {
		t.Fatalf("nil Overlay = %+v", ov)
	}
	shortTempDir(t)
	f := &fakeDeps{euid: 501, reads: [][]byte{[]byte("pw")}, checkOK: okOn(1)}
	s, err := setup(opts(true, true), f.deps())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
