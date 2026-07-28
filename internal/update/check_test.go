package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		name              string
		current, latest   string
		wantAvail, wantOK bool
	}{
		{"equal", "v1.2.3", "v1.2.3", false, true},
		{"older current", "v1.2.3", "v1.3.0", true, true},
		{"newer current", "v1.3.0", "v1.2.3", false, true},
		{"no v prefix", "1.2.3", "1.2.4", true, true},
		{"dev current", "dev", "v1.2.3", false, false},
		{"malformed current", "not-a-version", "v1.2.3", false, false},
		{"malformed latest", "v1.2.3", "not-a-version", false, false},
		{"empty latest", "v1.2.3", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			avail, ok := compareVersions(tc.current, tc.latest)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && avail != tc.wantAvail {
				t.Fatalf("available = %v, want %v", avail, tc.wantAvail)
			}
		})
	}
}

func TestCheck_DevVersionSkipsNetworkCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	info, err := Check(context.Background(), srv.Client(), "dev")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if info != nil {
		t.Fatalf("info = %+v, want nil", info)
	}
	if called {
		t.Fatal("expected no network call for an unparsable current version")
	}
}

func TestCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"tag_name":"v2.0.0","assets":[{"name":"upall_darwin_arm64.tar.gz","browser_download_url":"https://github.com/schmas/upall/releases/download/v2.0.0/upall_darwin_arm64.tar.gz"}]}`))
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	info, err := Check(context.Background(), srv.Client(), "v1.0.0")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if info == nil || !info.Available || info.Latest != "v2.0.0" {
		t.Fatalf("info = %+v, want available v2.0.0", info)
	}
	if len(info.Assets) != 1 {
		t.Fatalf("assets = %+v, want 1 asset", info.Assets)
	}
}

func TestCheck_NonOKStatusIsError(t *testing.T) {
	origBase := apiBaseURL
	defer func() { apiBaseURL = origBase }()

	for _, code := range []int{403, 404, 500} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		apiBaseURL = srv.URL
		_, err := Check(context.Background(), srv.Client(), "v1.0.0")
		srv.Close()
		if err == nil {
			t.Fatalf("status %d: expected error, got nil", code)
		}
	}
}

func TestCheck_EmptyTagNameIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"","assets":[]}`))
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	_, err := Check(context.Background(), srv.Client(), "v1.0.0")
	if err == nil {
		t.Fatal("expected error for empty tag_name on a 200 response")
	}
}

func TestCheck_MalformedTagNameIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"not-a-version","assets":[]}`))
	}))
	defer srv.Close()
	origBase := apiBaseURL
	apiBaseURL = srv.URL
	defer func() { apiBaseURL = origBase }()

	_, err := Check(context.Background(), srv.Client(), "v1.0.0")
	if err == nil {
		t.Fatal("expected error for malformed tag_name on a 200 response")
	}
}
