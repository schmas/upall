// Package update checks GitHub Releases for a newer upall build and (in
// apply.go) downloads, verifies, and applies it in place.
package update

import (
	"strconv"
	"strings"
)

// Asset is a single downloadable file attached to a GitHub release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

// Info is the caller-facing result of a version check. Available is always
// recomputed against the current version at construction time — never
// deserialized from cache directly — so a stale or poisoned cache entry can
// never smuggle in a stale verdict.
type Info struct {
	Current   string
	Latest    string
	Available bool
	Assets    []Asset
}

// cachedObservation is the on-disk cache shape: raw observation only, no
// derived verdict (no Available, no Current) so there is nothing to go stale.
type cachedObservation struct {
	CheckedAt string  `json:"checked_at"`
	Tag       string  `json:"tag_name"`
	Assets    []Asset `json:"assets"`
}

// compareVersions reports whether latest is newer than current. ok is false
// when current is unparsable (e.g. "dev") — this is the caller's signal to
// skip the check entirely rather than treat it as an error. latest is
// expected to already be validated non-empty/parsable by the caller (Check
// treats a malformed latest tag from a 200 response as a hard error, not an
// unparsable-skip case).
func compareVersions(current, latest string) (available bool, ok bool) {
	cur, curOK := parseVersion(current)
	if !curOK {
		return false, false
	}
	lat, latOK := parseVersion(latest)
	if !latOK {
		return false, false
	}
	for i := 0; i < 3; i++ {
		if lat[i] != cur[i] {
			return lat[i] > cur[i], true
		}
	}
	return false, true
}

// parseVersion strips a leading "v" and splits up to 3 numeric segments
// left-to-right. Any non-numeric or missing segment makes the version
// unparsable.
func parseVersion(v string) (segs [3]int, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return segs, false
	}
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			break
		}
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return segs, false
		}
		segs[i] = n
	}
	return segs, true
}
