package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseArgsUpdateFlags(t *testing.T) {
	tests := []struct {
		argv []string
		want opts
	}{
		{[]string{"--check-update"}, opts{checkUpdate: true}},
		{[]string{"--update"}, opts{doUpdate: true}},
		{[]string{"--update", "--yes"}, opts{doUpdate: true, yes: true}},
		{[]string{"--update", "-y"}, opts{doUpdate: true, yes: true}},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.argv, " "), func(t *testing.T) {
			got, err := parseArgs(tc.argv)
			if err != nil {
				t.Fatalf("parseArgs(%v): %v", tc.argv, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseArgs(%v) = %+v, want %+v", tc.argv, got, tc.want)
			}
			if len(got.selected) != 0 {
				t.Errorf("update flags leaked into step names: %v", got.selected)
			}
		})
	}
}

func TestParseArgsStepNamesStillWork(t *testing.T) {
	o, err := parseArgs([]string{"brew", "mise", "--update"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.doUpdate {
		t.Error("--update not parsed alongside positionals")
	}
	if !reflect.DeepEqual(o.selected, []string{"brew", "mise"}) {
		t.Errorf("selected = %v, want [brew mise]", o.selected)
	}
}

func TestParseArgsUnknownUpdateLikeFlag(t *testing.T) {
	if _, err := parseArgs([]string{"--update-now"}); err == nil {
		t.Error("an unknown flag should error, not become a step name")
	}
}

// The exit-code contract is part of the documented interface, so --help must
// describe it rather than leaving it to the plan file.
func TestUsageDocumentsUpdateFlags(t *testing.T) {
	for _, want := range []string{"--check-update", "--update", "--yes", "Exit codes"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
