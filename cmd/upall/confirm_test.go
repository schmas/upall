package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestConfirmPrompt(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  yes  \n", true},
		{"n\n", false},
		{"\n", false},
		{"nope\n", false},
		{"", false}, // EOF with nothing typed declines
		{"y", true}, // no trailing newline
		{"yeah", false},
	}
	for _, tc := range tests {
		t.Run(strings.TrimSpace(tc.input), func(t *testing.T) {
			var w bytes.Buffer
			got, err := confirmPrompt(strings.NewReader(tc.input), &w, "Apply update?")
			if err != nil {
				t.Fatalf("confirmPrompt(%q): %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("confirmPrompt(%q) = %v, want %v", tc.input, got, tc.want)
			}
			if !strings.Contains(w.String(), "Apply update? [y/N]") {
				t.Errorf("prompt not written: %q", w.String())
			}
		})
	}
}

// errReader fails on the first read, standing in for a broken stdin.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("stdin exploded") }

func TestConfirmPromptReadError(t *testing.T) {
	var w bytes.Buffer
	if _, err := confirmPrompt(errReader{}, &w, "Apply update?"); err == nil {
		t.Error("a read failure should be reported, not silently treated as a decline")
	}
}
