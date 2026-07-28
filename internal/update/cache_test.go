package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newLatestServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"` + tag + `","assets":[]}`))
	}))
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	t.Cleanup(func() {
		apiBaseURL = origBase
		srv.Close()
	})
	return srv
}

func TestMaybeCheck_CreatesCacheDirAndFile(t *testing.T) {
	newLatestServer(t, "v2.0.0")
	root := t.TempDir()
	cachePath := filepath.Join(root, "nested", "update-check.json")

	info, err := MaybeCheck(context.Background(), http.DefaultClient, "v1.0.0", cachePath, time.Hour)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if info == nil || !info.Available {
		t.Fatalf("info = %+v, want available", info)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file to be created: %v", err)
	}
}

func TestMaybeCheck_HitsServerOnceWithinInterval(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Write([]byte(`{"tag_name":"v2.0.0","assets":[]}`))
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	for i := 0; i < 2; i++ {
		if _, err := MaybeCheck(context.Background(), srv.Client(), "v1.0.0", cachePath, time.Hour); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("hits = %d, want 1", hits)
	}
}

func TestMaybeCheck_FailedCheckDoesNotWriteCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	if _, err := MaybeCheck(context.Background(), srv.Client(), "v1.0.0", cachePath, time.Hour); err == nil {
		t.Fatal("expected error from a 503 response")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file after a failed check, stat err = %v", err)
	}
}

func TestMaybeCheck_CorruptCacheIsTreatedAsMiss(t *testing.T) {
	newLatestServer(t, "v2.0.0")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	if err := os.WriteFile(cachePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := MaybeCheck(context.Background(), http.DefaultClient, "v1.0.0", cachePath, time.Hour)
	if err != nil {
		t.Fatalf("err = %v, want nil (corrupt cache should force a live check)", err)
	}
	if info == nil || !info.Available {
		t.Fatalf("info = %+v, want available", info)
	}
}

func TestMaybeCheck_ConcurrentCallsDoNotCorruptCacheFile(t *testing.T) {
	newLatestServer(t, "v2.0.0")
	cachePath := filepath.Join(t.TempDir(), "update-check.json")

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			MaybeCheck(context.Background(), http.DefaultClient, "v1.0.0", cachePath, time.Hour)
		}()
	}
	wg.Wait()

	if _, ok := readCache(cachePath); !ok {
		t.Fatal("expected cache file to parse cleanly after concurrent writes")
	}
}

func TestMaybeCheck_DevVersionSkipsEverything(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "update-check.json")
	info, err := MaybeCheck(context.Background(), http.DefaultClient, "dev", cachePath, time.Hour)
	if err != nil || info != nil {
		t.Fatalf("info=%+v err=%v, want nil,nil", info, err)
	}
}
