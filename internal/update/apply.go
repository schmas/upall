package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const (
	// binaryName is the tar entry (and installed file name) we replace. Real
	// release tarballs also carry a README.md entry, which is skipped.
	binaryName = "upall"

	// checksumsAssetName is goreleaser's default checksum file name.
	checksumsAssetName = "checksums.txt"

	// oldBinaryName is the rollback breadcrumb left next to the live binary
	// after a successful update. CleanupOld removes it on the next launch.
	// The binary being replaced is moved aside under a unique name first and
	// only promoted to this fixed name once the replacement passes its smoke
	// test — while a rollback is still the only copy of a working upall, no
	// other process (including the smoke-tested child, which runs CleanupOld
	// at startup) can find or delete it.
	oldBinaryName = ".upall.old"

	// stagingPattern and rollbackPattern are os.CreateTemp patterns; both live
	// in the target's own directory so every rename is same-filesystem and
	// therefore atomic. CleanupOld sweeps leftovers of both.
	stagingPattern  = ".upall-*.new"
	rollbackPattern = ".upall-*.old"

	// downloadChunkSize is the copy buffer, and so also the progress-callback
	// granularity — reporting per read of this size, not per byte.
	downloadChunkSize = 64 << 10

	// smokeTestTimeout bounds the `--version` run of the freshly installed
	// binary that decides whether to keep it or roll back.
	smokeTestTimeout = 5 * time.Second

	// chezmoiNote is returned on every successful apply. chezmoi's documented
	// install path caches the release archive for 168h and plain `chezmoi
	// update` does not re-fetch inside that window, so an updated binary can
	// be silently restored to the previous version on the user's next run.
	// Emitting this unconditionally is never wrong, only sometimes
	// irrelevant — cheaper and more reliable than detecting chezmoi.
	chezmoiNote = "note: if upall is managed by chezmoi, run 'chezmoi apply --refresh-externals' to keep it in sync — otherwise chezmoi may restore the previous version"
)

// Size caps on everything read from the network. They are vars, not consts,
// so tests can shrink them and prove the refusal actually fires — these are
// security controls, not tuning knobs, and must not silently regress.
var (
	// maxArchiveSize caps the compressed archive download (a real release
	// tarball is a few MB).
	maxArchiveSize int64 = 64 << 20

	// maxArchiveContentSize caps total decompressed bytes read from the
	// archive, across all entries — including entries that are skipped, which
	// still have to be inflated to reach the next header. Without this, a
	// small gzip stream can cost unbounded CPU.
	maxArchiveContentSize int64 = 128 << 20

	// maxBinarySize caps the single binary entry written to disk.
	maxBinarySize int64 = 64 << 20

	// maxChecksumsSize caps the checksums.txt body read.
	maxChecksumsSize int64 = 1 << 20
)

// allowedAssetHosts is the download allowlist: every asset URL must be https
// and resolve to one of these hosts before any request is made. github.com
// URLs are additionally scoped to this repo's release-download path, since the
// checksums are published alongside the archive and so prove only that the
// bytes arrived intact from wherever the release JSON pointed — not that the
// pointer itself was legitimate.
var allowedAssetHosts = map[string]bool{
	"github.com":                    true,
	"objects.githubusercontent.com": true,
	"api.github.com":                true,
}

// githubReleasePath is the only github.com path prefix asset URLs may use.
// Redirect targets on the other allowlisted hosts (objects.githubusercontent.com
// serves opaque blob paths) are not path-scoped.
const githubReleasePath = "/" + repoPath + "/releases/download/"

// validateAssetURL guards every download and every redirect hop. It is a
// package var so tests can point downloads at an httptest.Server; production
// always uses requireTrustedAssetURL, which is itself unit tested.
var validateAssetURL = requireTrustedAssetURL

// resolveExePath resolves the running binary's real path. It is a package var
// so tests can operate on a fake binary in a temp dir instead of replacing
// the test process's own executable.
var resolveExePath = defaultExePath

// ApplyResult describes a completed in-place update. NewExePath is the path
// the caller should hand to Reexec once all of its own teardown is done.
type ApplyResult struct {
	NewExePath  string
	ChezmoiNote string
}

// Apply downloads the latest release, verifies its checksum, and replaces the
// running binary in place with a rollback-safe rename sequence.
//
// It resolves the release assets with a live Check — never from the on-disk
// cache — so a stale or poisoned cache entry can never influence what gets
// fetched. onProgress may be nil; when set it is called with the downloaded
// and total byte counts of the archive download (total is -1 when the server
// does not report a length).
//
// Apply does NOT re-exec. It returns the path of the replaced binary; calling
// Reexec is the caller's job, and must happen only after terminal and child
// process teardown has completed.
func Apply(ctx context.Context, client *http.Client, currentVersion string, onProgress func(downloaded, total int64)) (ApplyResult, error) {
	var res ApplyResult

	// Resolve and vet the target before spending a download on it.
	exePath, err := resolveExePath()
	if err != nil {
		return res, err
	}
	exeDir := filepath.Dir(exePath)
	if err := checkSafeExeDir(exeDir); err != nil {
		return res, err
	}

	// Even the release-metadata fetch goes through the redirect-checked client:
	// this response is what names the asset URLs, so it is part of the trust
	// chain, not a preamble to it.
	dl := hardenedClient(client)

	info, err := Check(ctx, dl, currentVersion)
	if err != nil {
		return res, err
	}
	if info == nil {
		return res, fmt.Errorf("update: this build reports version %q and cannot self-update; install a release build instead", currentVersion)
	}
	if !info.Available {
		return res, fmt.Errorf("update: already on the latest release (%s)", info.Latest)
	}

	archiveName := archiveAssetName()
	archive, ok := findAsset(info.Assets, archiveName)
	if !ok {
		return res, fmt.Errorf("update: release %s has no %s asset", info.Latest, archiveName)
	}
	sums, ok := findAsset(info.Assets, checksumsAssetName)
	if !ok {
		return res, fmt.Errorf("update: release %s has no %s asset", info.Latest, checksumsAssetName)
	}
	if err := validateAssetURL(archive.URL); err != nil {
		return res, err
	}
	if err := validateAssetURL(sums.URL); err != nil {
		return res, err
	}

	tarballPath, err := downloadToTemp(ctx, dl, archive.URL, onProgress)
	if err != nil {
		return res, err
	}
	defer os.Remove(tarballPath)

	sumsBody, err := downloadToMemory(ctx, dl, sums.URL, maxChecksumsSize)
	if err != nil {
		return res, err
	}
	want, ok := checksumFor(string(sumsBody), archiveName)
	if !ok {
		return res, fmt.Errorf("update: %s has no entry for %s", checksumsAssetName, archiveName)
	}
	got, err := sha256OfFile(tarballPath)
	if err != nil {
		return res, err
	}
	if got != want {
		return res, fmt.Errorf("update: checksum mismatch for %s (want %s, got %s) — refusing to install", archiveName, want, got)
	}

	newPath, err := extractBinary(tarballPath, exeDir)
	if err != nil {
		return res, err
	}
	if err := replaceBinary(ctx, exePath, newPath); err != nil {
		// No-op once the rename has consumed newPath; matters on the paths
		// that fail before it.
		os.Remove(newPath)
		return res, err
	}

	return ApplyResult{NewExePath: exePath, ChezmoiNote: chezmoiNote}, nil
}

// CleanupOld removes the .upall.old rollback breadcrumb next to exePath, plus
// any staging or rollback temp files a killed update left behind. It is called
// best-effort at every startup: a process that got far enough to run proves
// the current binary works, so the previous one is no longer needed. A missing
// breadcrumb is the common case and not an error.
//
// Callers must invoke this only from a fully started process, never from
// anything an in-flight update might run (see replaceBinary).
func CleanupOld(exePath string) error {
	if exePath == "" {
		return nil
	}
	dir := filepath.Dir(exePath)
	if err := os.Remove(filepath.Join(dir, oldBinaryName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// A crashed or killed update leaves staging files in the user's bin
	// directory with nothing else to ever clean them up. Unfinished rollback
	// copies (rollbackPattern) are deliberately NOT swept: one can be the only
	// remaining copy of a working binary, and no other process can tell an
	// abandoned one from an in-flight one.
	matches, err := filepath.Glob(filepath.Join(dir, stagingPattern))
	if err != nil {
		return err
	}
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ExePath resolves the running binary the same way Apply does, so a caller
// wiring up CleanupOld points at the same directory Apply writes to.
func ExePath() (string, error) {
	return resolveExePath()
}

// defaultExePath resolves the running binary, following a symlink if the
// install path is one (chezmoi may or may not symlink — resolve either way).
func defaultExePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("update: locate running binary: %w", err)
	}
	return resolveSymlinks(exe)
}

// resolveSymlinks is split from defaultExePath so the symlink handling can be
// tested without touching the test process's own executable.
func resolveSymlinks(exe string) (string, error) {
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("update: resolve %s: %w", exe, err)
	}
	return resolved, nil
}

// archiveAssetName matches goreleaser's name_template
// ("upall_{{ .Os }}_{{ .Arch }}" + tar.gz), which uses the same lowercase
// values as runtime.GOOS/GOARCH.
func archiveAssetName() string {
	return fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, runtime.GOOS, runtime.GOARCH)
}

func findAsset(assets []Asset, name string) (Asset, bool) {
	for _, a := range assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// requireTrustedAssetURL enforces https, the host allowlist, and — on
// github.com — this repo's release-download path.
func requireTrustedAssetURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("update: unparsable asset URL %q: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("update: refusing non-https asset URL %q", rawURL)
	}
	if u.User != nil {
		return fmt.Errorf("update: refusing asset URL carrying credentials %q", u.Redacted())
	}
	host := strings.ToLower(u.Hostname())
	if !allowedAssetHosts[host] {
		return fmt.Errorf("update: refusing asset URL from untrusted host %q", u.Host)
	}
	if host == "github.com" && !strings.HasPrefix(u.Path, githubReleasePath) {
		return fmt.Errorf("update: refusing github.com asset URL outside %s (got %q)", githubReleasePath, u.Path)
	}
	return nil
}

// hardenedClient copies the caller's client and adds a redirect check — Go's
// default follows redirects to any scheme or host.
func hardenedClient(client *http.Client) *http.Client {
	c := *client
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("update: too many redirects")
		}
		return validateAssetURL(req.URL.String())
	}
	return &c
}

// downloadToTemp streams url into a uniquely named temp file and returns its
// path. The caller owns removal of that file.
func downloadToTemp(ctx context.Context, client *http.Client, url string, onProgress func(downloaded, total int64)) (string, error) {
	resp, err := get(ctx, client, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp("", "upall-download-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("update: create download temp file: %w", err)
	}
	name := tmp.Name()

	body := io.LimitReader(resp.Body, maxArchiveSize+1)
	n, err := copyWithProgress(tmp, body, resp.ContentLength, onProgress)
	if err == nil && n > maxArchiveSize {
		err = fmt.Errorf("archive is larger than %d bytes", maxArchiveSize)
	}
	if err != nil {
		tmp.Close()
		os.Remove(name)
		return "", fmt.Errorf("update: download %s: %w", url, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("update: close download temp file: %w", err)
	}
	return name, nil
}

// downloadToMemory reads a small asset (checksums.txt) with a hard size cap.
func downloadToMemory(ctx context.Context, client *http.Client, url string, max int64) ([]byte, error) {
	resp, err := get(ctx, client, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("update: download %s: %w", url, err)
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("update: %s is larger than %d bytes — refusing", url, max)
	}
	return body, nil
}

func get(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: build request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "upall-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: fetch %s: %w", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update: fetch %s returned %s", url, resp.Status)
	}
	return resp, nil
}

// copyWithProgress copies src to dst in downloadChunkSize chunks, reporting
// after each one. The loop is explicit rather than io.Copy so the callback
// granularity does not depend on which fast path io.Copy picks.
func copyWithProgress(dst io.Writer, src io.Reader, total int64, onProgress func(downloaded, total int64)) (int64, error) {
	buf := make([]byte, downloadChunkSize)
	var done int64
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			w, err := dst.Write(buf[:n])
			done += int64(w)
			if err != nil {
				return done, err
			}
			if onProgress != nil {
				onProgress(done, total)
			}
		}
		if readErr == io.EOF {
			return done, nil
		}
		if readErr != nil {
			return done, readErr
		}
	}
}

func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("update: open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("update: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumFor finds name's hex sha256 in a goreleaser checksums.txt body,
// whose lines are "<hex sha256>  <filename>".
func checksumFor(body, name string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			return fields[0], true
		}
	}
	return "", false
}

// extractBinary writes the archive's upall entry to a uniquely named temp file
// in destDir (same filesystem as the live binary, so the later rename is
// atomic). Other entries — real tarballs also ship README.md — are skipped,
// not rejected. os.CreateTemp creates the file 0600; it stays that way until
// the smoke test is about to run.
func extractBinary(tarballPath, destDir string) (string, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return "", fmt.Errorf("update: open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("update: read archive: %w", err)
	}
	defer gz.Close()

	// Bound total inflated bytes, not just the entry we keep: skipped entries
	// still have to be decompressed to reach the next header.
	tr := tar.NewReader(io.LimitReader(gz, maxArchiveContentSize))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("update: read archive: %w", err)
		}
		if h.Name != binaryName {
			continue
		}
		if h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("update: archive entry %q is not a regular file — refusing", binaryName)
		}

		out, err := os.CreateTemp(destDir, stagingPattern)
		if err != nil {
			return "", fmt.Errorf("update: create staging file in %s: %w", destDir, err)
		}
		name := out.Name()
		n, err := io.Copy(out, io.LimitReader(tr, maxBinarySize+1))
		if err == nil && n > maxBinarySize {
			err = fmt.Errorf("update: %s entry exceeds %d bytes — refusing", binaryName, maxBinarySize)
		}
		// Flush to disk before this file becomes the live binary: a crash
		// right after the rename must not leave a truncated executable.
		if err == nil {
			err = out.Sync()
		}
		if err != nil {
			out.Close()
			os.Remove(name)
			return "", err
		}
		if err := out.Close(); err != nil {
			os.Remove(name)
			return "", fmt.Errorf("update: close staging file: %w", err)
		}
		return name, nil
	}
	return "", fmt.Errorf("update: archive contains no %q entry", binaryName)
}

// replaceBinary swaps newPath in for exePath so that no failure leaves the
// user without a working upall.
//
// The live binary is moved aside under a unique name, the replacement is
// renamed into place and smoke-tested with --version, and the old binary is
// renamed back over it if that fails. Every rename is same-directory and
// therefore atomic, and the rollback rename overwrites its destination
// directly — the working binary is never unlinked first.
//
// The rollback copy only takes the well-known .upall.old name after the smoke
// test passes. Until then it is the sole copy of a working binary, and the
// smoke-tested child is itself an upall that sweeps .upall.old at startup.
func replaceBinary(ctx context.Context, exePath, newPath string) error {
	exeDir := filepath.Dir(exePath)

	holder, err := os.CreateTemp(exeDir, rollbackPattern)
	if err != nil {
		return fmt.Errorf("update: reserve rollback slot in %s: %w", exeDir, err)
	}
	rollbackPath := holder.Name()
	holder.Close()

	if err := os.Chmod(newPath, 0o755); err != nil {
		os.Remove(rollbackPath)
		return fmt.Errorf("update: chmod staging file: %w", err)
	}
	if err := os.Rename(exePath, rollbackPath); err != nil {
		os.Remove(rollbackPath)
		return fmt.Errorf("update: move current binary aside: %w", err)
	}
	if err := os.Rename(newPath, exePath); err != nil {
		// Nothing installed yet — put the working binary back.
		if rbErr := os.Rename(rollbackPath, exePath); rbErr != nil {
			return fmt.Errorf("update: install new binary: %w; ROLLBACK ALSO FAILED — the working binary is at %s, move it back to %s manually: %v", err, rollbackPath, exePath, rbErr)
		}
		return fmt.Errorf("update: install new binary: %w", err)
	}
	if err := smokeTest(ctx, exePath); err != nil {
		if rbErr := os.Rename(rollbackPath, exePath); rbErr != nil {
			return fmt.Errorf("%w; ROLLBACK ALSO FAILED — the previous binary is at %s, move it back to %s manually: %v", err, rollbackPath, exePath, rbErr)
		}
		return fmt.Errorf("%w; rolled back to the previous version", err)
	}

	// Proven good: publish the rollback copy under the name CleanupOld knows.
	if err := os.Rename(rollbackPath, filepath.Join(exeDir, oldBinaryName)); err != nil {
		// The update itself succeeded; only the breadcrumb's name is off.
		os.Remove(rollbackPath)
	}
	return nil
}

// smokeTest proves the freshly installed binary actually runs before the
// caller re-execs into it. It deliberately does not inherit the caller's
// cancellation: this runs inside the window where the live binary has already
// been replaced, so a user quitting mid-update must not abort the check that
// decides whether to keep or roll back.
func smokeTest(ctx context.Context, exePath string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), smokeTestTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, exePath, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("update: new binary failed its --version smoke test: %w (output: %s)", err, sanitizeOutput(out))
	}
	return nil
}

// sanitizeOutput makes a failed binary's output safe to render: it is
// untrusted, and Phase 5 puts errors on a terminal, so control sequences are
// stripped and the length is capped.
func sanitizeOutput(out []byte) string {
	const maxLen = 200

	s := strings.TrimSpace(string(out))
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
	if len(s) > maxLen {
		s = s[:maxLen] + "…"
	}
	return s
}
