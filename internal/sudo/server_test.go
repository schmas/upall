package sudo

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("", "upall-sudo-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s, err := Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestServerAnswersHelper(t *testing.T) {
	s := newServer(t)
	s.Arm([]byte("pw"), -1)
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		if code := RunHelper(s.SocketPath(), &buf); code != 0 {
			t.Fatalf("call %d: code = %d, want 0", i, code)
		}
		if buf.String() != "pw\n" {
			t.Fatalf("call %d: out = %q, want %q", i, buf.String(), "pw\n")
		}
	}
}

func TestServerArmCopiesPassword(t *testing.T) {
	s := newServer(t)
	pw := []byte("pw")
	s.Arm(pw, -1)
	pw[0] = 'x'
	var buf bytes.Buffer
	if code := RunHelper(s.SocketPath(), &buf); code != 0 || buf.String() != "pw\n" {
		t.Fatalf("code = %d, out = %q", code, buf.String())
	}
}

func TestServerBudget(t *testing.T) {
	s := newServer(t)
	s.Arm([]byte("pw"), 1)
	var first bytes.Buffer
	if code := RunHelper(s.SocketPath(), &first); code != 0 || first.String() != "pw\n" {
		t.Fatalf("first: code = %d, out = %q", code, first.String())
	}
	var second bytes.Buffer
	if code := RunHelper(s.SocketPath(), &second); code != 1 {
		t.Fatalf("second: code = %d, want 1", code)
	}
	if second.Len() != 0 {
		t.Fatalf("second: out = %q, want empty", second.String())
	}
}

func TestServerUnarmed(t *testing.T) {
	s := newServer(t)
	var buf bytes.Buffer
	if code := RunHelper(s.SocketPath(), &buf); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if buf.Len() != 0 {
		t.Fatalf("out = %q, want empty", buf.String())
	}
}

func TestServerClosed(t *testing.T) {
	s := newServer(t)
	s.Arm([]byte("pw"), -1)
	sock := s.SocketPath()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if code := RunHelper(sock, &buf); code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if buf.Len() != 0 {
		t.Fatalf("out = %q, want empty", buf.String())
	}
}

func TestServerCloseZeroesPassword(t *testing.T) {
	s := newServer(t)
	s.Arm([]byte("secret"), -1)
	s.mu.Lock()
	stored := s.password
	s.mu.Unlock()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, b := range stored {
		if b != 0 {
			t.Fatalf("stored password not zeroed: %v", stored)
		}
	}
}

func TestServerCloseIdempotent(t *testing.T) {
	s := newServer(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestSocketPathLength(t *testing.T) {
	s := newServer(t)
	if n := len(s.SocketPath()); n >= 104 {
		t.Fatalf("socket path length = %d, want < 104 (%s)", n, s.SocketPath())
	}
}

// TestServerArmCheckHoldsExtra: during a check, a request beyond the budget
// gets no reply and stays open, and the server signals it. Release lets the
// helper exit 1 with no output.
func TestServerArmCheckHoldsExtra(t *testing.T) {
	s := newServer(t)
	over := s.ArmCheck([]byte("pw"))
	var first bytes.Buffer
	if code := RunHelper(s.SocketPath(), &first); code != 0 || first.String() != "pw\n" {
		t.Fatalf("first: code = %d, out = %q", code, first.String())
	}
	select {
	case <-over:
		t.Fatal("over signalled before an extra request")
	default:
	}

	type res struct {
		code int
		out  string
	}
	second := make(chan res, 1)
	go func() {
		var buf bytes.Buffer
		code := RunHelper(s.SocketPath(), &buf)
		second <- res{code, buf.String()}
	}()
	select {
	case <-over:
	case <-time.After(2 * time.Second):
		t.Fatal("over not signalled for the extra request")
	}
	select {
	case r := <-second:
		t.Fatalf("extra request answered before release: %+v", r)
	case <-time.After(200 * time.Millisecond):
	}
	s.Release()
	select {
	case r := <-second:
		if r.code != 1 || r.out != "" {
			t.Fatalf("extra request after release: %+v, want code 1 and no output", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("extra request not released")
	}

	// After Release, an unanswered request is closed at once, as before.
	var third bytes.Buffer
	if code := RunHelper(s.SocketPath(), &third); code != 1 || third.Len() != 0 {
		t.Fatalf("third: code = %d, out = %q", code, third.String())
	}
}

// TestServerArmReleasesHeld: Arm and Close release held connections.
func TestServerArmReleasesHeld(t *testing.T) {
	for _, name := range []string{"arm", "close"} {
		t.Run(name, func(t *testing.T) {
			s := newServer(t)
			over := s.ArmCheck([]byte("pw"))
			var buf bytes.Buffer
			RunHelper(s.SocketPath(), &buf)
			done := make(chan int, 1)
			go func() {
				var b bytes.Buffer
				done <- RunHelper(s.SocketPath(), &b)
			}()
			<-over
			if name == "arm" {
				s.Arm([]byte("pw2"), -1)
			} else {
				s.Close()
			}
			select {
			case code := <-done:
				if code != 1 {
					t.Fatalf("code = %d, want 1", code)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("held connection not released")
			}
		})
	}
}

func TestSocketPermissions(t *testing.T) {
	s := newServer(t)
	fi, err := os.Stat(s.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("socket mode = %o, want 600", perm)
	}
}
