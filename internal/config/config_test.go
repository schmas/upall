package config

import (
	"strings"
	"testing"
	"time"

	"github.com/schmas/upall/internal/engine"
	"github.com/schmas/upall/internal/platform"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }
func boolp(b bool) *bool    { return &b }

// TestMergeClobberDirection is the mandatory test from the plan: a user file
// that sets ONLY `order` must reorder the step while inheriting run/sudo/detect.
func TestMergeClobberDirection(t *testing.T) {
	base := StepDef{
		Key: "brew", Label: strp("Homebrew"), Detect: strp("command -v brew"),
		Run: []string{"brew update", "brew upgrade"}, Sudo: boolp(false), Order: intp(30),
	}
	over := StepDef{Key: "brew", Order: intp(5)} // only order set

	merged := Merge([]StepDef{base, over})
	if len(merged) != 1 {
		t.Fatalf("merged len = %d, want 1", len(merged))
	}
	m := merged[0]
	if m.Order == nil || *m.Order != 5 {
		t.Errorf("order = %v, want 5", m.Order)
	}
	if len(m.Run) != 2 {
		t.Errorf("run = %v, want inherited 2 commands", m.Run)
	}
	if m.Sudo == nil || *m.Sudo != false {
		t.Errorf("sudo = %v, want inherited false (not clobbered to nil)", m.Sudo)
	}
	if m.Detect == nil || *m.Detect != "command -v brew" {
		t.Errorf("detect = %v, want inherited", m.Detect)
	}
}

func TestMergeOverrideWins(t *testing.T) {
	base := StepDef{Key: "os", Sudo: boolp(true), Run: []string{"osupdate"}}
	over := StepDef{Key: "os", Sudo: boolp(false), Run: []string{"echo replaced"}}
	m := Merge([]StepDef{base, over})[0]
	if *m.Sudo != false {
		t.Error("sudo override should win")
	}
	if len(m.Run) != 1 || m.Run[0] != "echo replaced" {
		t.Errorf("run override should replace, got %v", m.Run)
	}
}

func TestMergeEnvKeywise(t *testing.T) {
	base := StepDef{Key: "ck", Env: map[string]string{"UPALL_ACTIVE": "1", "NO_COLOR": "1"}}
	over := StepDef{Key: "ck", Env: map[string]string{"NO_COLOR": "0", "EXTRA": "x"}}
	m := Merge([]StepDef{base, over})[0]
	if m.Env["UPALL_ACTIVE"] != "1" {
		t.Error("inherited env key lost")
	}
	if m.Env["NO_COLOR"] != "0" {
		t.Error("overridden env key not applied")
	}
	if m.Env["EXTRA"] != "x" {
		t.Error("added env key missing")
	}
}

func TestResolveDropsDisabled(t *testing.T) {
	defs := []StepDef{{Key: "a", Enabled: boolp(false)}, {Key: "b"}}
	resolved, err := Resolve(defs, platform.Platform{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Key != "b" {
		t.Fatalf("resolved = %v, want only b", keysOf(resolved))
	}
}

// TestEmbeddedRunOrder asserts the resolved run order is driven by each step's
// explicit `order` (not the alphabetical embed glob order), and that exactly one
// per-OS step applies to a given host — the apt one on Ubuntu.
func TestEmbeddedRunOrder(t *testing.T) {
	defs, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve(defs, platform.Platform{OS: "linux", Distro: "ubuntu", IDLike: "debian", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	// Non-decreasing order: sequence comes from `order`, never the glob order.
	for i := 1; i < len(resolved); i++ {
		if resolved[i-1].Order > resolved[i].Order {
			t.Fatalf("resolved not sorted by order: %v", keysOf(resolved))
		}
	}
	// Exactly one OS step applies on Ubuntu.
	var osApplied []string
	for _, r := range resolved {
		if strings.HasPrefix(r.Key, "os-") && r.Applies {
			osApplied = append(osApplied, r.Key)
		}
	}
	if len(osApplied) != 1 || osApplied[0] != "os-apt" {
		t.Fatalf("OS steps applying on Ubuntu = %v, want [os-apt]", osApplied)
	}
	// The toolchain steps keep v2's relative order after the OS step.
	want := []string{"nix", "chezmoi", "brew", "mise", "rust", "uv", "claude", "ck", "fisher", "atuin"}
	var got []string
	for _, r := range resolved {
		if !strings.HasPrefix(r.Key, "os-") {
			got = append(got, r.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("toolchain keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("toolchain order[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestCompoundDetectViaShell(t *testing.T) {
	if !detectOK("true && true") {
		t.Error("`true && true` should pass")
	}
	if detectOK("true && false") {
		t.Error("`true && false` should fail (compound guard)")
	}
	if !detectOK("") {
		t.Error("empty detect should apply")
	}
	if !detectOK("command -v sh") {
		t.Error("`command -v sh` should pass")
	}
}

func TestParseFileErrors(t *testing.T) {
	if _, err := parseFile("bad.toml", []byte("= not valid =")); err == nil {
		t.Error("malformed TOML should error, not panic")
	}
	if _, err := parseFile("v2.toml", []byte("schema = 2\n")); err == nil {
		t.Error("unsupported schema should error")
	}
	if _, err := parseFile("nokey.toml", []byte("schema = 1\n[[step]]\nlabel = \"x\"\n")); err == nil {
		t.Error("step without key should error")
	}
	if _, err := parseFile("ok.toml", []byte("schema = 1\n[[step]]\nkey = \"x\"\n")); err != nil {
		t.Errorf("valid file should parse: %v", err)
	}
}

func TestPlatformMatches(t *testing.T) {
	linux := platform.Platform{OS: "linux", Distro: "ubuntu", IDLike: "debian"}
	darwin := platform.Platform{OS: "darwin"}

	if !platformMatches(engine.Step{OS: []string{"linux"}}, linux) {
		t.Error("linux step should match linux")
	}
	if platformMatches(engine.Step{OS: []string{"linux"}}, darwin) {
		t.Error("linux step should NOT match darwin")
	}
	if !platformMatches(engine.Step{Distro: []string{"debian"}}, linux) {
		t.Error("debian step should match ubuntu via ID_LIKE")
	}
	if !platformMatches(engine.Step{Distro: []string{"ubuntu"}}, linux) {
		t.Error("ubuntu step should match ubuntu")
	}
	if platformMatches(engine.Step{Distro: []string{"arch"}}, linux) {
		t.Error("arch step should NOT match ubuntu")
	}
	if !platformMatches(engine.Step{}, darwin) {
		t.Error("empty predicate should match anything")
	}
}

func TestSelectRun(t *testing.T) {
	resolved := []Resolved{
		{Step: engine.Step{Key: "a"}, Applies: true, DetectOK: true},
		{Step: engine.Step{Key: "b"}, Applies: true, DetectOK: false},
		{Step: engine.Step{Key: "c"}, Applies: false},
	}
	if _, err := SelectRun(resolved, []string{"zzz"}); err == nil {
		t.Error("unknown selected key should error")
	}
	all, err := SelectRun(resolved, nil)
	if err != nil {
		t.Fatal(err)
	}
	// b (detect miss) and c (not applicable) are dropped entirely; only a runs.
	if len(all) != 1 || all[0].Key != "a" {
		t.Fatalf("run set = %v, want [a] (b hidden: detect failed, c hidden: n/a)", keysOfSteps(all))
	}
	// A detect miss is still a KNOWN key, so selecting it is not an error — it
	// just resolves to nothing runnable.
	if none, err := SelectRun(resolved, []string{"b"}); err != nil || len(none) != 0 {
		t.Errorf("selecting detect-miss b = %v, err=%v; want empty", keysOfSteps(none), err)
	}
	sub, _ := SelectRun(resolved, []string{"a"})
	if len(sub) != 1 || sub[0].Key != "a" {
		t.Errorf("subset = %v, want [a]", keysOfSteps(sub))
	}
}

func TestToStepTimeout(t *testing.T) {
	s, err := toStep(StepDef{Key: "x", Timeout: strp("5m")})
	if err != nil || s.Timeout != 5*time.Minute {
		t.Fatalf("timeout = %v err=%v, want 5m", s.Timeout, err)
	}
	if _, err := toStep(StepDef{Key: "y", Timeout: strp("bogus")}); err == nil {
		t.Error("bad timeout should error")
	}
}

func keysOf(rs []Resolved) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Key
	}
	return out
}

func keysOfSteps(ss []engine.Step) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Key
	}
	return out
}
