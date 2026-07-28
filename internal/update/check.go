package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// apiBaseURL is the GitHub API host. It is a package var (not a literal
// inline in the request-building code) so tests can point it at an
// httptest.Server; the repo path below stays a const since that is not
// meant to be user-configurable.
var apiBaseURL = "https://api.github.com"

const repoPath = "schmas/upall"

type releaseResponse struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Check hits the GitHub Releases API for the latest release and compares it
// against currentVersion. It always makes a network call — callers wanting
// the cached/rate-limited path should use MaybeCheck instead.
//
// currentVersion being unparsable (e.g. "dev", the non-release-build
// default) is a legitimate "not applicable" case: Check returns (nil, nil)
// without making a network call. A malformed or missing tag_name in an
// otherwise-200 response is different — GitHub only 200s with a real
// release body, so that is a hard error.
func Check(ctx context.Context, client *http.Client, currentVersion string) (*Info, error) {
	if _, ok := parseVersion(currentVersion); !ok {
		return nil, nil
	}

	url := apiBaseURL + "/repos/" + repoPath + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("update: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "upall-self-update")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: GitHub API returned %s", resp.Status)
	}

	var rel releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("update: decode release response: %w", err)
	}
	if _, ok := parseVersion(rel.TagName); !ok {
		return nil, fmt.Errorf("update: release has no usable tag_name (got %q)", rel.TagName)
	}

	available, _ := compareVersions(currentVersion, rel.TagName)
	return &Info{
		Current:   currentVersion,
		Latest:    rel.TagName,
		Available: available,
		Assets:    rel.Assets,
	}, nil
}
