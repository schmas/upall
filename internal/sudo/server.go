// Package sudo asks for the sudo password once per run and serves it to
// `sudo -A` in every step through an askpass helper.
package sudo

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HelperArg is argv[1] that puts the upall binary in askpass helper mode.
const HelperArg = "__sudo-askpass"

// socketName is short on purpose: macOS limits a socket path to 104 bytes.
const socketName = "s"

// writeTimeout stops a stuck client from blocking the accept loop.
const writeTimeout = 2 * time.Second

// Server serves the sudo password, held only in memory, on a Unix socket.
// The protocol has no request body: it accepts a connection, writes the
// password bytes (no newline) and closes.
type Server struct {
	listener net.Listener
	path     string
	done     chan struct{} // closed when the accept loop returns

	mu       sync.Mutex
	password []byte
	budget   int // requests left to answer; < 0 means no limit
	closed   bool

	// Check mode (ArmCheck): over is closed on the first request beyond the
	// budget; held keeps such requests open until they are released.
	over      chan struct{}
	overFired bool
	held      []net.Conn
}

// Listen creates the socket at dir/s with mode 0600 and starts serving.
// The caller creates dir (mode 0700) and removes it.
func Listen(dir string) (*Server, error) {
	path := filepath.Join(dir, socketName)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, err
	}
	s := &Server{listener: l, path: path, done: make(chan struct{})}
	go s.serve()
	return s, nil
}

// SocketPath returns the path of the Unix socket.
func (s *Server) SocketPath() string { return s.path }

// Arm sets the password and how many requests it answers. budget < 0 means no
// limit. After the budget is used, a request gets no data (the helper exits 1).
// Arm stores its own copy of pw, so the caller may zero its slice.
// Arm also ends check mode (see ArmCheck) and releases held requests.
func (s *Server) Arm(pw []byte, budget int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseLocked()
	zero(s.password)
	s.password = append([]byte(nil), pw...)
	s.budget = budget
}

// ArmCheck arms pw for one request, for a password check. A later request
// gets no reply and is not closed: the server holds it, so the helper (and
// the PAM conversation that runs it) waits, and the returned channel is
// closed. The caller then stops sudo before PAM can count a second failure,
// and calls Release. Arm, ArmCheck and Close also release held requests.
func (s *Server) ArmCheck(pw []byte) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseLocked()
	zero(s.password)
	s.password = append([]byte(nil), pw...)
	s.budget = 1
	s.over = make(chan struct{})
	s.overFired = false
	return s.over
}

// Release ends check mode and closes held requests: their helpers exit 1
// with no output. Later requests beyond the budget are closed at once.
func (s *Server) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseLocked()
}

func (s *Server) releaseLocked() {
	for _, c := range s.held {
		_ = c.Close()
	}
	s.held = nil
	s.over = nil
	s.overFired = false
}

// Close stops serving, waits for the accept loop and zeroes the password.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	err := s.listener.Close()
	<-s.done

	s.mu.Lock()
	s.releaseLocked()
	zero(s.password)
	s.password = nil
	s.budget = 0
	s.mu.Unlock()
	return err
}

func (s *Server) serve() {
	defer close(s.done)
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		s.answer(conn)
	}
}

// answer writes the password to conn, if the budget allows, and closes it.
// In check mode, a request beyond the budget is held open instead (see
// ArmCheck).
func (s *Server) answer(conn net.Conn) {
	s.mu.Lock()
	var reply []byte
	if len(s.password) > 0 && s.budget != 0 {
		reply = append(reply, s.password...)
		if s.budget > 0 {
			s.budget--
		}
	} else if s.over != nil {
		s.held = append(s.held, conn)
		if !s.overFired {
			s.overFired = true
			close(s.over)
		}
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	defer conn.Close()
	defer zero(reply)

	if len(reply) == 0 {
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, _ = conn.Write(reply)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
