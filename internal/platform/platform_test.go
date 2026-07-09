package platform

import (
	"runtime"
	"strings"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	fixture := `NAME="Ubuntu"
ID=ubuntu
ID_LIKE=debian
VERSION_ID="22.04"
`
	id, idLike := parseOSRelease(strings.NewReader(fixture))
	if id != "ubuntu" {
		t.Errorf("ID = %q, want ubuntu", id)
	}
	if idLike != "debian" {
		t.Errorf("ID_LIKE = %q, want debian", idLike)
	}
}

func TestParseOSReleaseArchNoIDLike(t *testing.T) {
	id, idLike := parseOSRelease(strings.NewReader("ID=arch\n"))
	if id != "arch" || idLike != "" {
		t.Errorf("got id=%q idLike=%q, want arch/empty", id, idLike)
	}
}

func TestIsWSL(t *testing.T) {
	if !isWSL("Linux version 5.15.0-microsoft-standard-WSL2") {
		t.Error("expected WSL detected")
	}
	if isWSL("Linux version 6.1.0-arch1-1") {
		t.Error("expected non-WSL")
	}
}

func TestDetectTolerantOnDarwin(t *testing.T) {
	p := Detect()
	if p.OS != runtime.GOOS {
		t.Errorf("OS = %q, want %q", p.OS, runtime.GOOS)
	}
	if p.Arch != runtime.GOARCH {
		t.Errorf("Arch = %q, want %q", p.Arch, runtime.GOARCH)
	}
	// On darwin there is no /etc/os-release; Detect must return empty, not error.
	if runtime.GOOS == "darwin" && (p.Distro != "" || p.IDLike != "" || p.WSL) {
		t.Errorf("darwin should have empty distro fields, got %+v", p)
	}
}
