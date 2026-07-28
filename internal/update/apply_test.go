package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// oldBinaryScript is the "installed" binary a test starts from; the archive
// installs newBinaryScript over it. Both are shell scripts so the --version
// smoke test can actually run them.
const (
	oldBinaryScript    = "#!/bin/sh\necho 'upall v1.0.0'\n"
	newBinaryScript    = "#!/bin/sh\necho 'upall v9.9.9'\n"
	brokenBinaryScript = "#!/bin/sh\nexit 3\n"
)

// releaseServer is a fake GitHub: the release JSON plus the two assets it
// names, with per-asset request counters so tests can assert that a rejected
// URL is never fetched.
type releaseServer struct {
	srv          *httptest.Server
	archiveHits  int
	checksumHits int
	archive      []byte
	archiveName  string
	// forceScheme, when set, replaces the scheme of the asset URLs advertised
	// in the release JSON (used to exercise the allowlist).
	forceScheme string
}

func newReleaseServer(t *testing.T, binaryScript string, padding int) *releaseServer {
	t.Helper()

	rs := &releaseServer{archiveName: archiveAssetName()}
	rs.archive = buildArchive(t, binaryScript, padding)

	sum := sha256.Sum256(rs.archive)
	checksums := fmt.Sprintf("%s  %s\n%s  upall_other_arch.tar.gz\n",
		hex.EncodeToString(sum[:]), rs.archiveName, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/schmas/upall/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := rs.assetBase()
		body, err := json.Marshal(releaseResponse{
			TagName: "v9.9.9",
			Assets: []Asset{
				{Name: rs.archiveName, URL: base + "/download/" + rs.archiveName},
				{Name: checksumsAssetName, URL: base + "/download/" + checksumsAssetName},
			},
		})
		if err != nil {
			t.Errorf("marshal release response: %v", err)
			return
		}
		w.Write(body)
	})
	mux.HandleFunc("/download/"+rs.archiveName, func(w http.ResponseWriter, r *http.Request) {
		rs.archiveHits++
		// Explicit length so resp.ContentLength is known and the progress
		// callback can report a real total.
		w.Header().Set("Content-Length", strconv.Itoa(len(rs.archive)))
		w.Write(rs.archive)
	})
	mux.HandleFunc("/download/"+checksumsAssetName, func(w http.ResponseWriter, r *http.Request) {
		rs.checksumHits++
		w.Write([]byte(checksums))
	})

	rs.srv = httptest.NewServer(mux)
	t.Cleanup(rs.srv.Close)

	// Point Check at the fake API and allow its (http, 127.0.0.1) asset URLs.
	// The real allowlist is exercised directly in TestRequireTrustedAssetURL
	// and via the forceScheme case below.
	prevBase := apiBaseURL
	apiBaseURL = rs.srv.URL
	t.Cleanup(func() { apiBaseURL = prevBase })

	setValidateAssetURL(t, func(rawURL string) error {
		u, err := url.Parse(rawURL)
		if err != nil {
			return err
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("test: refusing scheme %q", u.Scheme)
		}
		return nil
	})

	return rs
}

// setValidateAssetURL swaps the URL guard for the duration of one test and
// restores whatever was there before, so seams never leak between tests.
func setValidateAssetURL(t *testing.T, fn func(string) error) {
	t.Helper()
	prev := validateAssetURL
	validateAssetURL = fn
	t.Cleanup(func() { validateAssetURL = prev })
}

func (rs *releaseServer) assetBase() string {
	if rs.forceScheme == "" {
		return rs.srv.URL
	}
	return rs.forceScheme + "://" + strings.TrimPrefix(rs.srv.URL, "http://")
}

// buildArchive produces a release-shaped tar.gz: the upall binary plus a
// README.md entry (which extraction must skip). padding bytes of
// incompressible data go into the README so the download is large enough to
// produce several progress callbacks.
func buildArchive(t *testing.T, binaryScript string, padding int) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	write := func(name string, mode int64, body []byte) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("tar header %s: %v", name, err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("tar write %s: %v", name, err)
		}
	}

	readme := []byte("# upall\n")
	if padding > 0 {
		noise := make([]byte, padding)
		if _, err := rand.Read(noise); err != nil {
			t.Fatalf("random padding: %v", err)
		}
		readme = append(readme, noise...)
	}
	write(binaryName, 0o755, []byte(binaryScript))
	write("README.md", 0o644, readme)

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// installFakeBinary writes an executable "upall" into its own temp dir and
// points resolveExePath at it, so Apply operates there instead of on the test
// process's real executable.
func installFakeBinary(t *testing.T, script string) string {
	t.Helper()

	dir := t.TempDir()
	exe := filepath.Join(dir, binaryName)
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	prev := resolveExePath
	resolveExePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { resolveExePath = prev })
	return exe
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// assertNoTempFiles proves neither a staging file nor an unpromoted rollback
// copy was left behind.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	for _, pattern := range []string{stagingPattern, rollbackPattern} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatalf("glob %s: %v", pattern, err)
		}
		if len(matches) != 0 {
			t.Errorf("leftover %s files: %v", pattern, matches)
		}
	}
}

func TestApply_ReplacesBinaryAndKeepsRollbackCopy(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	// 256KB of padding => several 64KB download chunks.
	newReleaseServer(t, newBinaryScript, 256<<10)

	var calls []int64
	var lastTotal int64
	onProgress := func(downloaded, total int64) {
		calls = append(calls, downloaded)
		lastTotal = total
	}

	res, err := Apply(context.Background(), &http.Client{}, "v1.0.0", onProgress)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.NewExePath != exe {
		t.Errorf("NewExePath = %q, want %q", res.NewExePath, exe)
	}
	if res.ChezmoiNote == "" {
		t.Error("ChezmoiNote is empty; the post-update note should always be returned")
	}
	if got := readFile(t, exe); got != newBinaryScript {
		t.Errorf("live binary was not replaced: %q", got)
	}

	dir := filepath.Dir(exe)
	if got := readFile(t, filepath.Join(dir, oldBinaryName)); got != oldBinaryScript {
		t.Errorf("%s does not hold the previous binary: %q", oldBinaryName, got)
	}
	assertNoTempFiles(t, dir)

	if fi, err := os.Stat(exe); err != nil {
		t.Fatalf("stat installed binary: %v", err)
	} else if fi.Mode().Perm() != 0o755 {
		t.Errorf("installed binary mode = %v, want 0755", fi.Mode().Perm())
	}

	if len(calls) < 2 {
		t.Fatalf("expected multiple progress callbacks, got %d", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] <= calls[i-1] {
			t.Fatalf("progress not increasing: %v", calls)
		}
	}
	if last := calls[len(calls)-1]; last != lastTotal {
		t.Errorf("final progress = %d, want total %d", last, lastTotal)
	}
}

func TestApply_NilProgressCallback(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	newReleaseServer(t, newBinaryScript, 0)

	if _, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil); err != nil {
		t.Fatalf("Apply with nil onProgress: %v", err)
	}
	if got := readFile(t, exe); got != newBinaryScript {
		t.Errorf("live binary was not replaced: %q", got)
	}
}

func TestApply_ChecksumMismatchLeavesTargetAlone(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	rs := newReleaseServer(t, newBinaryScript, 0)
	// Corrupt the served archive after its checksum was computed.
	rs.archive = append(rs.archive, 0x00)

	_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
	if err == nil {
		t.Fatal("expected a checksum mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}
	if got := readFile(t, exe); got != oldBinaryScript {
		t.Errorf("live binary was modified: %q", got)
	}
	dir := filepath.Dir(exe)
	if _, err := os.Stat(filepath.Join(dir, oldBinaryName)); !os.IsNotExist(err) {
		t.Errorf("%s should not exist after an aborted update", oldBinaryName)
	}
	assertNoTempFiles(t, dir)
}

func TestApply_FailedSmokeTestRollsBack(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	newReleaseServer(t, brokenBinaryScript, 0)

	_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
	if err == nil {
		t.Fatal("expected a smoke-test failure")
	}
	if !strings.Contains(err.Error(), "smoke test") {
		t.Errorf("error = %v, want a smoke-test failure", err)
	}
	if got := readFile(t, exe); got != oldBinaryScript {
		t.Errorf("rollback did not restore the previous binary: %q", got)
	}
	assertNoTempFiles(t, filepath.Dir(exe))
}

func TestApply_MissingAssets(t *testing.T) {
	tests := []struct {
		name    string
		drop    string
		wantErr string
	}{
		{"missing archive", archiveAssetName(), archiveAssetName()},
		{"missing checksums", checksumsAssetName, checksumsAssetName},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exe := installFakeBinary(t, oldBinaryScript)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assets := []Asset{
					{Name: archiveAssetName(), URL: "https://github.com/a.tar.gz"},
					{Name: checksumsAssetName, URL: "https://github.com/checksums.txt"},
				}
				kept := assets[:0]
				for _, a := range assets {
					if a.Name != tc.drop {
						kept = append(kept, a)
					}
				}
				json.NewEncoder(w).Encode(releaseResponse{TagName: "v9.9.9", Assets: kept})
			}))
			defer srv.Close()

			prev := apiBaseURL
			apiBaseURL = srv.URL
			defer func() { apiBaseURL = prev }()

			_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
			if err == nil {
				t.Fatal("expected a missing-asset error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to name %s", err, tc.wantErr)
			}
			if got := readFile(t, exe); got != oldBinaryScript {
				t.Errorf("live binary was modified: %q", got)
			}
		})
	}
}

func TestApply_RejectsUntrustedAssetURLBeforeDownloading(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	rs := newReleaseServer(t, newBinaryScript, 0)
	// Advertise the assets over plain http on 127.0.0.1 and restore the real
	// allowlist so it decides.
	rs.forceScheme = "http"
	setValidateAssetURL(t, requireTrustedAssetURL)

	_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
	if err == nil {
		t.Fatal("expected the allowlist to reject a non-https asset URL")
	}
	if !strings.Contains(err.Error(), "non-https") {
		t.Errorf("error = %v, want a non-https rejection", err)
	}
	if rs.archiveHits != 0 || rs.checksumHits != 0 {
		t.Errorf("assets were fetched despite rejection (archive=%d checksums=%d)", rs.archiveHits, rs.checksumHits)
	}
	if got := readFile(t, exe); got != oldBinaryScript {
		t.Errorf("live binary was modified: %q", got)
	}
}

func TestApply_RefusesSharedWritableInstallDir(t *testing.T) {
	exe := installFakeBinary(t, oldBinaryScript)
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Skipf("cannot make %s world-writable: %v", dir, err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })

	rs := newReleaseServer(t, newBinaryScript, 0)

	_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
	if err == nil {
		t.Fatal("expected a shared-writable-directory refusal")
	}
	if !strings.Contains(err.Error(), "world-writable") {
		t.Errorf("error = %v, want a world-writable refusal", err)
	}
	if strings.Contains(err.Error(), "sudo") {
		t.Errorf("error suggests sudo, which is an escalation path: %v", err)
	}
	if rs.archiveHits != 0 {
		t.Errorf("archive was downloaded before the directory check: %d hits", rs.archiveHits)
	}
}

func TestApply_AlreadyLatest(t *testing.T) {
	installFakeBinary(t, oldBinaryScript)
	rs := newReleaseServer(t, newBinaryScript, 0)

	_, err := Apply(context.Background(), &http.Client{}, "v9.9.9", nil)
	if err == nil {
		t.Fatal("expected an already-latest error")
	}
	if !strings.Contains(err.Error(), "already on the latest") {
		t.Errorf("error = %v, want an already-latest message", err)
	}
	if rs.archiveHits != 0 {
		t.Errorf("archive was downloaded with no update available: %d hits", rs.archiveHits)
	}
}

func TestApply_DevBuildCannotSelfUpdate(t *testing.T) {
	installFakeBinary(t, oldBinaryScript)

	_, err := Apply(context.Background(), &http.Client{}, "dev", nil)
	if err == nil {
		t.Fatal("expected a dev-build refusal")
	}
	if !strings.Contains(err.Error(), "cannot self-update") {
		t.Errorf("error = %v, want a dev-build refusal", err)
	}
}

func TestRequireTrustedAssetURL(t *testing.T) {
	tests := []struct {
		url  string
		ok   bool
		name string
	}{
		{"https://github.com/schmas/upall/releases/download/v1/upall.tar.gz", true, "github release"},
		{"https://GitHub.com/schmas/upall/releases/download/v1/upall.tar.gz", true, "host case is ignored"},
		{"https://objects.githubusercontent.com/blob", true, "objects host"},
		{"https://api.github.com/x", true, "api host"},
		{"http://github.com/schmas/upall/releases/download/v1/x.tar.gz", false, "plain http"},
		{"https://github.com.evil.test/x", false, "lookalike host"},
		{"https://raw.githubusercontent.com/x", false, "non-allowlisted github host"},
		{"https://github.com/attacker/upall/releases/download/v1/upall.tar.gz", false, "another repo's release"},
		{"https://github.com/schmas/upall/raw/main/x", false, "github path outside releases"},
		{"https://user:pass@github.com/schmas/upall/releases/download/v1/x.tar.gz", false, "embedded credentials"},
		{"file:///etc/passwd", false, "file scheme"},
		{"://nope", false, "unparsable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := requireTrustedAssetURL(tc.url)
			if tc.ok && err != nil {
				t.Errorf("requireTrustedAssetURL(%q) = %v, want nil", tc.url, err)
			}
			if !tc.ok && err == nil {
				t.Errorf("requireTrustedAssetURL(%q) = nil, want an error", tc.url)
			}
		})
	}
}

func TestHardenedClientRejectsUntrustedRedirect(t *testing.T) {
	setValidateAssetURL(t, requireTrustedAssetURL)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target was reached")
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	_, err := get(context.Background(), hardenedClient(&http.Client{}), redirector.URL)
	if err == nil {
		t.Fatal("expected the redirect to an untrusted host to be rejected")
	}
}

func TestChecksumFor(t *testing.T) {
	body := "aaa  upall_darwin_arm64.tar.gz\nbbb  upall_linux_amd64.tar.gz\n"
	if got, ok := checksumFor(body, "upall_linux_amd64.tar.gz"); !ok || got != "bbb" {
		t.Errorf("checksumFor = (%q, %v), want (\"bbb\", true)", got, ok)
	}
	if _, ok := checksumFor(body, "upall_windows_amd64.tar.gz"); ok {
		t.Error("checksumFor found an entry that is not in the body")
	}
}

func TestExtractBinary_SkipsOtherEntriesAndRejectsIrregular(t *testing.T) {
	dir := t.TempDir()

	archive := buildArchive(t, newBinaryScript, 0)
	tarball := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(tarball, archive, 0o600); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	got, err := extractBinary(tarball, dir)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	if content := readFile(t, got); content != newBinaryScript {
		t.Errorf("extracted content = %q, want the binary entry", content)
	}
	if fi, err := os.Stat(got); err != nil {
		t.Fatalf("stat staging file: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("staging file mode = %v, want 0600 until the smoke test", fi.Mode().Perm())
	}

	// A symlink entry named upall must be refused, not followed.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     binaryName,
		Linkname: "/etc/passwd",
		Typeflag: tar.TypeSymlink,
		Mode:     0o777,
	}); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	tw.Close()
	gz.Close()

	evil := filepath.Join(dir, "evil.tar.gz")
	if err := os.WriteFile(evil, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	if _, err := extractBinary(evil, dir); err == nil {
		t.Error("expected a symlink entry named upall to be refused")
	}
}

func TestExtractBinary_MissingEntry(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("# upall\n")
	tw.WriteHeader(&tar.Header{Name: "README.md", Size: int64(len(body)), Typeflag: tar.TypeReg, Mode: 0o644})
	tw.Write(body)
	tw.Close()
	gz.Close()

	tarball := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(tarball, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	if _, err := extractBinary(tarball, dir); err == nil {
		t.Error("expected an error when the archive has no upall entry")
	}
	assertNoTempFiles(t, dir)
}

// The size caps are security controls (oversized body / decompression bomb),
// so each one gets a test that shrinks it and proves the refusal fires.
func TestApply_SizeCapsRefuseOversizedContent(t *testing.T) {
	tests := []struct {
		name    string
		shrink  func(t *testing.T)
		wantErr string
	}{
		{
			name:    "archive download cap",
			shrink:  func(t *testing.T) { setInt64(t, &maxArchiveSize, 16) },
			wantErr: "larger than 16 bytes",
		},
		{
			name:    "checksums body cap",
			shrink:  func(t *testing.T) { setInt64(t, &maxChecksumsSize, 8) },
			wantErr: "larger than 8 bytes",
		},
		{
			name:    "extracted binary cap",
			shrink:  func(t *testing.T) { setInt64(t, &maxBinarySize, 4) },
			wantErr: "exceeds 4 bytes",
		},
		{
			name:    "total inflated cap",
			shrink:  func(t *testing.T) { setInt64(t, &maxArchiveContentSize, 4) },
			wantErr: "archive",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exe := installFakeBinary(t, oldBinaryScript)
			newReleaseServer(t, newBinaryScript, 0)
			tc.shrink(t)

			_, err := Apply(context.Background(), &http.Client{}, "v1.0.0", nil)
			if err == nil {
				t.Fatal("expected the size cap to refuse the content")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, tc.wantErr)
			}
			if got := readFile(t, exe); got != oldBinaryScript {
				t.Errorf("live binary was modified: %q", got)
			}
			assertNoTempFiles(t, filepath.Dir(exe))
		})
	}
}

func setInt64(t *testing.T, p *int64, v int64) {
	t.Helper()
	prev := *p
	*p = v
	t.Cleanup(func() { *p = prev })
}

func TestResolveSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, binaryName)
	if err := os.WriteFile(real, []byte(oldBinaryScript), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	link := filepath.Join(dir, "upall-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	got, err := resolveSymlinks(link)
	if err != nil {
		t.Fatalf("resolveSymlinks: %v", err)
	}
	// EvalSymlinks also resolves symlinked temp roots (/var -> /private/var on
	// darwin), so compare against the fully resolved real path.
	want, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	if got != want {
		t.Errorf("resolveSymlinks(%q) = %q, want %q", link, got, want)
	}

	if _, err := resolveSymlinks(filepath.Join(dir, "does-not-exist")); err == nil {
		t.Error("expected an error for a missing path")
	}
}

func TestSanitizeOutput(t *testing.T) {
	got := sanitizeOutput([]byte("\x1b[31mboom\x1b[0m\nsecond line\n"))
	if strings.Contains(got, "\x1b") {
		t.Errorf("sanitizeOutput kept escape sequences: %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("sanitizeOutput kept newlines: %q", got)
	}
	if !strings.Contains(got, "boom") {
		t.Errorf("sanitizeOutput dropped the message: %q", got)
	}
	if long := sanitizeOutput([]byte(strings.Repeat("x", 500))); len(long) > 220 {
		t.Errorf("sanitizeOutput did not cap length: %d chars", len(long))
	}
}

func TestGet_RequiresStatus200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := get(context.Background(), &http.Client{}, srv.URL); err == nil {
		t.Fatal("expected a non-200 status to be an error")
	}
}
