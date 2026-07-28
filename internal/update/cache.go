package update

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/schmas/upall/internal/engine"
)

// CachePath is the default location of the version-check cache. It lives under
// the engine cache root rather than the history dir, which the user can point
// anywhere. Both the CLI flags and the TUI check through this one path so a
// launch check and a forced check share one cache entry.
func CachePath() string {
	return filepath.Join(engine.CacheRoot(), "update-check.json")
}

// MaybeCheck is the single entry point both plain-mode and the TUI call: it
// returns a cached Info if the last check is within interval, otherwise
// performs a live Check and (on success) writes a fresh cache entry.
//
// A forced check (interval <= 0) always bypasses the freshness gate but
// still writes a fresh cache entry afterward. A failed Check is never
// written to the cache, so a transient failure never suppresses the next
// attempt for a full interval.
func MaybeCheck(ctx context.Context, client *http.Client, currentVersion, cachePath string, interval time.Duration) (*Info, error) {
	if _, ok := parseVersion(currentVersion); !ok {
		return nil, nil
	}

	if interval > 0 {
		if cached, ok := readCache(cachePath); ok && isFresh(cached.CheckedAt, interval) {
			available, _ := compareVersions(currentVersion, cached.Tag)
			return &Info{
				Current:   currentVersion,
				Latest:    cached.Tag,
				Available: available,
				Assets:    cached.Assets,
			}, nil
		}
	}

	info, err := Check(ctx, client, currentVersion)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, nil
	}

	_ = writeCache(cachePath, cachedObservation{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Tag:       info.Latest,
		Assets:    info.Assets,
	})
	return info, nil
}

// readCache reads and parses the cache file. It returns ok=false on any
// error (missing file, corrupt JSON) — callers treat that as a cache miss,
// never a fatal error.
func readCache(cachePath string) (cachedObservation, bool) {
	var obs cachedObservation
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return obs, false
	}
	if err := json.Unmarshal(data, &obs); err != nil {
		return obs, false
	}
	if obs.Tag == "" || obs.CheckedAt == "" {
		return obs, false
	}
	return obs, true
}

// isFresh reports whether checkedAt (RFC3339) is within interval of now.
func isFresh(checkedAt string, interval time.Duration) bool {
	t, err := time.Parse(time.RFC3339, checkedAt)
	if err != nil {
		return false
	}
	return time.Since(t) < interval
}

// writeCache atomically writes obs to cachePath: a temp file in the same
// directory, then os.Rename over the destination, so two processes writing
// concurrently never truncate/corrupt each other's file.
func writeCache(cachePath string, obs cachedObservation) error {
	dir := filepath.Dir(cachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(obs)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, cachePath)
}
