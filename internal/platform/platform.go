// Package platform describes the host OS/distro/arch so the config layer can
// decide which steps apply. Detection is tolerant: files that are absent on a
// given OS (e.g. /etc/os-release on macOS) yield empty fields, never an error.
package platform

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strings"
)

// Platform is the runtime descriptor a step's os/distro predicate matches against.
type Platform struct {
	OS     string // runtime.GOOS ("darwin", "linux")
	Distro string // /etc/os-release ID ("ubuntu", "arch", ...); empty on macOS
	IDLike string // /etc/os-release ID_LIKE ("debian", ...)
	WSL    bool   // running under Windows Subsystem for Linux
	Arch   string // runtime.GOARCH ("amd64", "arm64")
}

// Detect reads the host descriptor. Missing or unreadable /etc/os-release and
// /proc/version are treated as "not present" (empty Distro/IDLike, WSL=false).
func Detect() Platform {
	p := Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	if f, err := os.Open("/etc/os-release"); err == nil {
		p.Distro, p.IDLike = parseOSRelease(f)
		_ = f.Close()
	}
	if b, err := os.ReadFile("/proc/version"); err == nil {
		p.WSL = isWSL(string(b))
	}
	return p
}

// parseOSRelease extracts ID and ID_LIKE from an os-release stream.
func parseOSRelease(r io.Reader) (id, idLike string) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "ID="):
			id = unquote(strings.TrimPrefix(line, "ID="))
		case strings.HasPrefix(line, "ID_LIKE="):
			idLike = unquote(strings.TrimPrefix(line, "ID_LIKE="))
		}
	}
	return id, idLike
}

// isWSL reports whether a /proc/version string names a Microsoft kernel.
func isWSL(procVersion string) bool {
	return strings.Contains(strings.ToLower(procVersion), "microsoft")
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	return strings.Trim(s, `"'`)
}
