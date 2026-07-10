package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/schmas/upall/internal/engine"
)

func TestParsePartialOverridesOnlySetFields(t *testing.T) {
	s, err := parse("config.toml", []byte("schema = 1\n[ui]\nwrap = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s.UI.Wrap {
		t.Error("wrap should be overridden to false")
	}
	// Everything else stays default.
	if s.UI.WideThreshold != 90 || s.UI.DefaultFilter != "all" || !s.UI.Follow {
		t.Errorf("unset ui fields drifted: %+v", s.UI)
	}
	if s.Theme.Accent != "42" || s.History.Keep != 0 || !s.Notify.Enabled {
		t.Error("unrelated sections should keep defaults")
	}
	if len(s.Keys["quit"]) != 2 {
		t.Error("keys should keep defaults")
	}
}

func TestParseSchemaError(t *testing.T) {
	if _, err := parse("config.toml", []byte("schema = 2\n")); err == nil {
		t.Error("schema != 1 should error")
	}
}

func TestParseUnknownKeyActionError(t *testing.T) {
	_, err := parse("myconf.toml", []byte("schema = 1\n[keys]\nfly = [\"f\"]\n"))
	if err == nil {
		t.Fatal("unknown [keys] action should error")
	}
	if !strings.Contains(err.Error(), "myconf.toml") || !strings.Contains(err.Error(), "fly") {
		t.Errorf("error should name file and action: %v", err)
	}
}

func TestParseUnknownKeyErrors(t *testing.T) {
	_, err := parse("myconf.toml", []byte("schema = 1\n[theme]\naccnt = \"9\"\n"))
	if err == nil {
		t.Fatal("a mistyped key should error, not be silently ignored")
	}
	if !strings.Contains(err.Error(), "accnt") {
		t.Errorf("error should name the unknown key: %v", err)
	}
}

func TestParseKeyRebind(t *testing.T) {
	s, err := parse("config.toml", []byte("schema = 1\n[keys]\nquit = [\"Q\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Keys["quit"]; len(got) != 1 || got[0] != "Q" {
		t.Errorf("quit rebind = %v, want [Q]", got)
	}
	// Unset actions keep their defaults.
	if len(s.Keys["up"]) == 0 {
		t.Error("unset action should keep default binding")
	}
}

func TestParseHistoryDirExpandsHome(t *testing.T) {
	s, err := parse("config.toml", []byte("schema = 1\n[history]\ndir = \"~/runs\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if s.History.Dir != filepath.Join(home, "runs") {
		t.Errorf("dir = %q, want expanded under home", s.History.Dir)
	}
}

func TestResolveKeepPrecedence(t *testing.T) {
	// env beats config
	t.Setenv("UPALL_KEEP", "5")
	if got := ResolveKeep(20); got != 5 {
		t.Errorf("env keep = %d, want 5", got)
	}
	// config beats default when env unset
	os.Unsetenv("UPALL_KEEP")
	if got := ResolveKeep(20); got != 20 {
		t.Errorf("config keep = %d, want 20", got)
	}
	// default when neither set
	if got := ResolveKeep(0); got != engine.DefaultKeep {
		t.Errorf("default keep = %d, want %d", got, engine.DefaultKeep)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty dir, no config.toml
	s, err := Load()
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if !reflect.DeepEqual(s, Defaults()) {
		t.Error("missing file should yield Defaults()")
	}
}

func TestLoadReadsFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	confDir := filepath.Join(dir, "upall")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(confDir, "config.toml"),
		[]byte("schema = 1\n[notify]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Notify.Enabled {
		t.Error("config should disable notify")
	}
}

// TestDefaultTOMLRoundTrips proves the commented seed parses back to exactly
// Defaults() (everything is commented, so nothing is actually set).
func TestDefaultTOMLRoundTrips(t *testing.T) {
	s, err := parse("config.toml", []byte(DefaultTOML()))
	if err != nil {
		t.Fatalf("default template should parse: %v", err)
	}
	if !reflect.DeepEqual(s, Defaults()) {
		t.Errorf("template did not round-trip to Defaults()\n got: %+v\nwant: %+v", s, Defaults())
	}
}

// TestEnsureConfigSeedsWhenMissing proves the first run seeds a commented
// config, later runs leave it untouched, and the seed resolves to Defaults().
func TestEnsureConfigSeedsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "upall", "config.toml")

	created, err := EnsureConfig()
	if err != nil || !created {
		t.Fatalf("first EnsureConfig: created=%v err=%v", created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not seeded: %v", err)
	}
	s, err := parse("config.toml", data)
	if err != nil {
		t.Fatalf("seeded config should parse: %v", err)
	}
	if !reflect.DeepEqual(s, Defaults()) {
		t.Error("seeded config should resolve to Defaults()")
	}

	// A second run must not re-create or clobber it.
	created, err = EnsureConfig()
	if err != nil || created {
		t.Errorf("second EnsureConfig should be a no-op: created=%v err=%v", created, err)
	}
}

func TestInitConfigWritesAndRefuses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "upall", "config.toml")

	var buf bytes.Buffer
	if err := InitConfig(&buf, false); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if !strings.Contains(buf.String(), path) {
		t.Errorf("init should print resolved path %q, got %q", path, buf.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// All five sections documented.
	for _, sec := range []string{"[keys]", "[theme]", "[history]", "[ui]", "[notify]"} {
		if !strings.Contains(string(data), sec) {
			t.Errorf("template missing section %q", sec)
		}
	}

	// Second run without force refuses.
	if err := InitConfig(&buf, false); err == nil {
		t.Error("second init without --force should refuse")
	}
	// With force it overwrites.
	if err := InitConfig(&buf, true); err != nil {
		t.Errorf("--force init should overwrite: %v", err)
	}
}
