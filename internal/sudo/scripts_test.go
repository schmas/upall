package sudo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestShQuote(t *testing.T) {
	cases := map[string]string{
		"a b":  `'a b'`,
		"it's": `'it'\''s'`,
		"":     `''`,
	}
	for in, want := range cases {
		if got := shQuote(in); got != want {
			t.Errorf("shQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// printArgsScript is a fake executable that prints its arguments, one per line.
const printArgsScript = "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done\n"

func writeFake(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "fake tool's")
	if err := os.WriteFile(p, []byte(printArgsScript), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func outLines(t *testing.T, name string, args ...string) []string {
	t.Helper()
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		t.Fatalf("run %s: %v", name, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func testDirs(t *testing.T) map[string]string {
	base := t.TempDir()
	return map[string]string{
		"plain":   filepath.Join(base, "plain"),
		"special": filepath.Join(base, "sp ace's dir"),
	}
}

func TestWrapperPassesArgs(t *testing.T) {
	for name, dir := range testDirs(t) {
		t.Run(name, func(t *testing.T) {
			fake := writeFake(t, filepath.Join(dir, "real"))
			for mode, flag := range map[WrapperMode]string{WrapAskpass: "-A", WrapNonInteractive: "-n"} {
				wdir := filepath.Join(dir, "w")
				binDir, err := WriteWrapper(wdir, fake, mode)
				if err != nil {
					t.Fatal(err)
				}
				if want := filepath.Join(wdir, "bin"); binDir != want {
					t.Fatalf("binDir = %q, want %q", binDir, want)
				}
				got := outLines(t, filepath.Join(binDir, "sudo"), "apt-get", "a b")
				if want := []string{flag, "apt-get", "a b"}; !equal(got, want) {
					t.Fatalf("mode %v: got %q, want %q", mode, got, want)
				}
				fi, err := os.Stat(filepath.Join(binDir, "sudo"))
				if err != nil {
					t.Fatal(err)
				}
				if perm := fi.Mode().Perm(); perm != 0o700 {
					t.Fatalf("wrapper mode = %o, want 700", perm)
				}
			}
		})
	}
}

func TestAskpassLauncherRunsExe(t *testing.T) {
	for name, dir := range testDirs(t) {
		t.Run(name, func(t *testing.T) {
			fake := writeFake(t, filepath.Join(dir, "real"))
			sock := filepath.Join(dir, "sock'et", "s")
			adir := filepath.Join(dir, "a")
			launcher, err := WriteAskpass(adir, fake, sock)
			if err != nil {
				t.Fatal(err)
			}
			if want := filepath.Join(adir, "askpass"); launcher != want {
				t.Fatalf("launcher = %q, want %q", launcher, want)
			}
			got := outLines(t, launcher)
			if want := []string{HelperArg, sock}; !equal(got, want) {
				t.Fatalf("got %q, want %q", got, want)
			}
			fi, err := os.Stat(launcher)
			if err != nil {
				t.Fatal(err)
			}
			if perm := fi.Mode().Perm(); perm != 0o700 {
				t.Fatalf("launcher mode = %o, want 700", perm)
			}
		})
	}
}

func TestScriptsStartWithShebang(t *testing.T) {
	dir := t.TempDir()
	launcher, err := WriteAskpass(dir, "/bin/true", "/tmp/s")
	if err != nil {
		t.Fatal(err)
	}
	binDir, err := WriteWrapper(dir, "/usr/bin/sudo", WrapAskpass)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{launcher, filepath.Join(binDir, "sudo")} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(b), "#!/bin/sh\n") {
			t.Errorf("%s does not start with #!/bin/sh: %q", p, b)
		}
	}
}
