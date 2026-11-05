package sudo

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/schmas/upall/internal/engine"
)

// ErrTooManyAttempts is returned after 3 failed password checks.
var ErrTooManyAttempts = errors.New("3 incorrect password attempts")

// maxAttempts matches sudo's own passwd_tries default.
const maxAttempts = 3

// maxSocketPath is the macOS limit for a Unix socket path, in bytes.
const maxSocketPath = 104

// Options describes the run for Setup.
type Options struct {
	Needed   bool // a selected step has sudo = true
	StdinTTY bool
	In       *os.File
	Out      io.Writer
	Exe      string // os.Executable()
}

// deps lets tests replace the system calls.
type deps struct {
	euid       func() int
	lookSudo   func() (string, error) // exec.LookPath("sudo")
	noPassword func(realSudo string) bool
	read       func() ([]byte, error)
	check      func(realSudo, askpass string, srv *Server, pw []byte) error
}

// Session holds what Setup made for the run: the private directory (askpass
// launcher, sudo wrapper, socket) and the password server.
type Session struct {
	dir     string
	srv     *Server
	overlay engine.EnvOverlay
}

// Overlay returns the env overlay for every step. It is the zero value when
// nothing is set.
func (s *Session) Overlay() engine.EnvOverlay {
	if s == nil {
		return engine.EnvOverlay{}
	}
	return s.overlay
}

// Close closes the server and removes the directory. It is safe on nil and
// when called twice.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	var errs []error
	if s.srv != nil {
		errs = append(errs, s.srv.Close())
		s.srv = nil
	}
	if s.dir != "" {
		errs = append(errs, os.RemoveAll(s.dir))
		s.dir = ""
	}
	s.overlay = engine.EnvOverlay{}
	return errors.Join(errs...)
}

// Setup decides, before the run, whether upall asks for the sudo password,
// and prepares the askpass helper and the sudo wrapper. The caller must call
// Close on the returned session when the run ends.
func Setup(o Options) (*Session, error) {
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	prompt := "[upall] sudo password for " + name + ": "
	return setup(o, deps{
		euid:       os.Geteuid,
		lookSudo:   func() (string, error) { return exec.LookPath("sudo") },
		noPassword: NoPasswordNeeded,
		read:       func() ([]byte, error) { return ReadPassword(o.In, o.Out, prompt) },
		check:      CheckPassword,
	})
}

// setup follows the decision table top-down: root, no TTY, not needed,
// NOPASSWD, then the prompt loop.
func setup(o Options, d deps) (*Session, error) {
	if d.euid() == 0 {
		return &Session{}, nil
	}
	realSudo, err := d.lookSudo()
	if err != nil {
		if o.StdinTTY && o.Needed {
			return nil, fmt.Errorf("sudo not found: %w", err)
		}
		return &Session{}, nil
	}
	if !o.StdinTTY {
		return wrapperOnly(realSudo)
	}
	if !o.Needed || d.noPassword(realSudo) {
		return &Session{}, nil
	}
	return askAndCheck(o, d, realSudo)
}

// wrapperOnly sets only the sudo wrapper with -n: a step that needs a
// password fails at once instead of waiting for its timeout.
func wrapperOnly(realSudo string) (*Session, error) {
	dir, err := makeDir()
	if err != nil {
		return nil, err
	}
	s := &Session{dir: dir}
	binDir, err := WriteWrapper(dir, realSudo, WrapNonInteractive)
	if err != nil {
		s.Close()
		return nil, err
	}
	s.overlay = engine.EnvOverlay{PathPrefix: binDir}
	return s, nil
}

// askAndCheck asks for the password up to maxAttempts times and checks each
// answer. On success the server serves the password with no limit.
func askAndCheck(o Options, d deps, realSudo string) (*Session, error) {
	dir, err := makeDir()
	if err != nil {
		return nil, err
	}
	s := &Session{dir: dir}
	fail := func(err error) (*Session, error) {
		s.Close()
		return nil, err
	}

	srv, err := Listen(dir)
	if err != nil {
		return fail(err)
	}
	s.srv = srv
	askpass, err := WriteAskpass(dir, o.Exe, srv.SocketPath())
	if err != nil {
		return fail(err)
	}

	for range maxAttempts {
		pw, err := d.read()
		if err != nil {
			zero(pw)
			return fail(err)
		}
		err = d.check(realSudo, askpass, srv, pw)
		if err == nil {
			srv.Arm(pw, -1)
			zero(pw)
			binDir, err := WriteWrapper(dir, realSudo, WrapAskpass)
			if err != nil {
				return fail(err)
			}
			s.overlay = engine.EnvOverlay{
				Vars:       map[string]string{"SUDO_ASKPASS": askpass},
				PathPrefix: binDir,
			}
			return s, nil
		}
		zero(pw)
		if !errors.Is(err, errIncorrect) {
			return fail(fmt.Errorf("check sudo password: %w", err))
		}
		fmt.Fprintln(o.Out, "Sorry, try again.")
	}
	return fail(ErrTooManyAttempts)
}

// makeDir creates the private directory (mode 0700). If the socket path in
// $TMPDIR would hit the macOS limit, it uses /tmp instead.
func makeDir() (string, error) {
	dir, err := os.MkdirTemp("", "upall-sudo-")
	if err != nil {
		return "", err
	}
	if len(filepath.Join(dir, socketName)) < maxSocketPath {
		return dir, nil
	}
	_ = os.RemoveAll(dir)
	return os.MkdirTemp("/tmp", "upall-sudo-")
}
